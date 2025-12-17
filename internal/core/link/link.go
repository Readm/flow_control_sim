package link

import (
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// CreateFlowControlStrategy is a factory function that creates flow control strategies by type.
//
// Supported strategy types:
// - "buffered": BufferedFlowControl with ring buffer and backpressure
// - "bufferless": BufferlessFlowControl (always-ready, no buffering, latency still applies)
//
// Parameters:
// - strategyType: type of flow control strategy ("buffered", "bufferless")
// - latency: latency parameter (used by buffered, ignored by bufferless)
// - bandwidth: bandwidth parameter (used by buffered, ignored by bufferless)
//
// Returns:
// - FlowControlStrategy instance, or panic if strategyType is unknown
func CreateFlowControlStrategy(strategyType string, latency, bandwidth int) FlowControlStrategy {
	switch strategyType {
	case "buffered":
		return NewBufferedFlowControl(latency, bandwidth)
	case "bufferless":
		return NewBufferlessFlowControl()
	default:
		panic(fmt.Sprintf("unknown flow control strategy type: %s", strategyType))
	}
}

// Link represents a directed edge in the topology.
// Link receives packets from upstream and forwards them to downstream with latency and bandwidth constraints.
// Link manages its own synchronization state and exposes InPort and OutPort interfaces.
type Link struct {
	sourceID int
	targetID int

	// Port references (set by NewLink, used internally)
	inPort  *linkInPort
	outPort *linkOutPort

	// +++ Flow control strategy (Phase 3 addition) +++
	flowControl FlowControlStrategy

	// Link's synchronization state
	componentSync *ahead_port.ComponentSync

	latency      int
	bandwidth    int
	currentCycle int
	tickHook     func(cycle int)

	pendingPackets []ahead_port.PacketWithCycle
}

// processPackets handles packet processing (receive, delay, forwarding) for the Link.
// It decides which packets to send based on flow control strategy and downstream readiness.
func (l *Link) processPackets(
	packets []packet.Packet,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(targetCycle int, pkt packet.Packet) bool,
	updateUpstreamReady func(cycle int, ready bool),
) {
	// Link just transparently forwards the updateUpstreamReady call to the upstream ports.
	// The Ready state for cycle+1 is determined by checking downstream readiness.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		updateUpstreamReady(cycle+1, checkReady(cycle+1))
	}()

	newPendingPackets := make([]ahead_port.PacketWithCycle, 0)

	// Check flow control type and use appropriate processing logic
	if bufferedFC, ok := l.flowControl.(*BufferedFlowControl); ok {
		// Use buffered flow control logic (with slots and backpressure)
		l.processPacketsBuffered(bufferedFC, packets, cycle, checkReady, sendPacket, &newPendingPackets)
	} else if _, ok := l.flowControl.(*BufferlessFlowControl); ok {
		// Use bufferless flow control logic (simple latency-based forwarding)
		l.processPacketsBufferless(packets, cycle, sendPacket, &newPendingPackets)
	} else {
		panic(fmt.Sprintf("Unsupported flow control type: %T", l.flowControl))
	}

	// Update pending packets
	l.pendingPackets = newPendingPackets

	// Notify upstream that we are ready for next cycle using waitGroup to wait for completion
	wg.Wait()
}

// processPacketsBuffered handles packet processing for BufferedFlowControl.
func (l *Link) processPacketsBuffered(
	fc *BufferedFlowControl,
	packets []packet.Packet,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(targetCycle int, pkt packet.Packet) bool,
	newPendingPackets *[]ahead_port.PacketWithCycle,
) {

	// Helper to add packet to slot (using flow control strategy)
	addToSlot := func(pkt ahead_port.PacketWithCycle, targetCycle int) {
		if !fc.CanAcceptPacket(cycle, targetCycle) {
			panic(fmt.Sprintf("Cannot accept packet for targetCycle %d at cycle %d", targetCycle, cycle))
		}
		fc.AddToSlot(pkt, targetCycle)
	}

	// 1. Process pending packets (already have correct TargetCycle)
	for _, pkt := range l.pendingPackets {
		targetCycle := pkt.Cycle
		if !fc.CanAcceptPacket(cycle, targetCycle) {
			*newPendingPackets = append(*newPendingPackets, pkt)
			continue
		}
		addToSlot(pkt, targetCycle)
	}

	// 2. Receive and process new packets from upstream
	// New design: send immediately with targetCycle, or buffer if downstream not ready
	for _, pkt := range packets {
		targetCycle := cycle + l.latency
		// Try to send immediately
		if !sendPacket(targetCycle, pkt) {
			// Downstream not ready, buffer for retry
			*newPendingPackets = append(*newPendingPackets, ahead_port.PacketWithCycle{
				Cycle:  targetCycle,
				Packet: pkt,
			})
		}
	}

	// 3. Decide whether to send based on downstream readiness
	downstreamReady := checkReady(cycle)
	if fc.CanSendPacket(cycle, downstreamReady) {
		slot := fc.GetSlot(cycle)
		var pendingInSlot []ahead_port.PacketWithCycle
		allSent := true

		for _, pwc := range slot {
			// Send packet with its stored target cycle
			if !sendPacket(pwc.Cycle, pwc.Packet) {
				pendingInSlot = append(pendingInSlot, pwc)
				allSent = false
			}
		}

		if allSent {
			fc.ClearSlot(cycle)
		} else {
			// Some packets failed to send. Retain them in the slot and apply backpressure.
			// This ensures they are retried in the next opportunity.
			fc.UpdateSlot(cycle, pendingInSlot)
			fc.IncrementBackpressure()
		}
	} else {
		fc.IncrementBackpressure()
	}
}

// processPacketsBufferless handles packet processing for BufferlessFlowControl.
// Simpler logic: packets are delayed by latency and forwarded immediately when ready.
func (l *Link) processPacketsBufferless(
	packets []packet.Packet,
	cycle int,
	sendPacket func(targetCycle int, pkt packet.Packet) bool,
	newPendingPackets *[]ahead_port.PacketWithCycle,
) {
	// 1. Process pending packets - send those whose time has come
	for _, pkt := range l.pendingPackets {
		if pkt.Cycle <= cycle {
			// Time to send this packet at its target cycle
			if !sendPacket(pkt.Cycle, pkt.Packet) {
				panic(fmt.Sprintf("Bufferless Link %d->%d failed to send packet at cycle %d", l.sourceID, l.targetID, cycle))
			}
		} else {
			// Still waiting
			*newPendingPackets = append(*newPendingPackets, pkt)
		}
	}

	// 2. Receive new packets and send immediately with targetCycle
	for _, pkt := range packets {
		targetCycle := cycle + l.latency
		// Send immediately with target cycle label
		if !sendPacket(targetCycle, pkt) {
			panic(fmt.Sprintf("Bufferless Link %d->%d failed to send packet at cycle %d (target %d)", l.sourceID, l.targetID, cycle, targetCycle))
		}
	}
}

// NewLink creates a Link with InPort and OutPort interfaces.
// Returns the Link instance, its InPort (for upstream to write), and its OutPort (for downstream to read).
// Use Plug() to connect the ports to upstream and downstream components.
//
// Parameters:
// - sourceID: ID of the source node
// - targetID: ID of the target node
// - latency: number of cycles for packet delivery (must be >= 0)
// - bandwidth: maximum packets per cycle (must be > 0)
//
// Note: This function creates a Link with BufferedFlowControl by default.
// For other flow control strategies, use NewLinkWithFlowControl.
func NewLink(sourceID, targetID, latency, bandwidth int) (*Link, ahead_port.InPort, ahead_port.OutPort) {
	// Create default flow control strategy (BufferedFlowControl)
	flowControl := NewBufferedFlowControl(latency, bandwidth)
	return NewLinkWithFlowControl(sourceID, targetID, latency, bandwidth, flowControl)
}

// NewLinkWithFlowControl creates a new Link with a custom flow control strategy.
//
// Parameters:
// - sourceID: ID of the source node
// - targetID: ID of the target node
// - latency: number of cycles for packet delivery (must be >= 0)
// - bandwidth: maximum packets per cycle (must be > 0)
// - flowControl: the flow control strategy to use
func NewLinkWithFlowControl(sourceID, targetID, latency, bandwidth int, flowControl FlowControlStrategy) (*Link, ahead_port.InPort, ahead_port.OutPort) {
	if latency < 0 {
		panic("latency must not be negative")
	}
	if bandwidth <= 0 {
		panic("bandwidth must be positive")
	}
	if flowControl == nil {
		panic("flowControl must not be nil")
	}

	link := &Link{
		sourceID:      sourceID,
		targetID:      targetID,
		latency:       latency,
		bandwidth:     bandwidth,
		flowControl:   flowControl,
		currentCycle:  0,
		componentSync: ahead_port.NewComponentSync(),
	}

	// Create ports
	inPort := &linkInPort{link: link}
	outPort := &linkOutPort{link: link}

	// Set up beforeGetHook for linkOutPort to handle waitDone
	outPort.SetBeforeGetHook(func(cycle int) {
		waitCycle := cycle - link.latency
		if waitCycle < 0 {
			waitCycle = 0
		}
		link.waitDone(waitCycle)
	})

	// Link holds port references
	link.inPort = inPort
	link.outPort = outPort

	// Initialize readyUntil for the first 'latency' cycles.
	// Rationale: Link is ready for the first 'latency' cycles because packets
	// are still in transit (haven't reached downstream yet).
	// This prevents deadlock in cyclic topologies during initialization.
	link.componentSync.SetReadyUntil(latency)
	debug.Logf("Link.NewLink: link %d->%d initialized with readyUntil=%d (latency=%d)",
		sourceID, targetID, latency, latency)

	return link, inPort, outPort
}

// SourceID returns the ID of the upstream node.
func (l *Link) SourceID() int {
	return l.sourceID
}

// TargetID returns the ID of the downstream node.
func (l *Link) TargetID() int {
	return l.targetID
}

// Latency returns the configured delay in cycles.
func (l *Link) Latency() int {
	return l.latency
}

// Bandwidth returns the maximum packets per cycle.
func (l *Link) Bandwidth() int {
	return l.bandwidth
}

// Tick processes a single cycle.
func (l *Link) Tick(cycle int) error {
	// ===== 1. Prepare updateUpstreamReady function =====
	// This function notifies upstream of Link's ready status
	updateUpstreamReady := func(cycle int, ready bool) {
		l.updateReady(cycle, ready)
	}

	// ===== 2. Get packets from upstream =====
	// Link at cycle N processes packets from upstream cycle (N - latency)
	var packets []packet.Packet
	if l.inPort.UpstreamOut != nil {
		waitCycle := cycle - l.latency
		if waitCycle < 0 {
			waitCycle = 0
		}
		packets = l.inPort.UpstreamOut.GetPackets(waitCycle)
	}

	// Get downstream's Ready check function
	checkReady := func(cycle int) bool {
		if l.outPort.DownstreamIn != nil {
			type readyChecker interface{ ready(int) bool }
			if rc, ok := l.outPort.DownstreamIn.(readyChecker); ok {
				return rc.ready(cycle)
			}
			ready, decided := l.outPort.DownstreamIn.IsReadyNonBlocking(cycle)
			if decided {
				return ready
			}
			return false
		}
		return true
	}

	// Get send function
	sendPacket := func(targetCycle int, pkt packet.Packet) bool {
		if l.outPort.DownstreamIn != nil {
			pwc := ahead_port.PacketWithCycle{
				Cycle:  targetCycle,
				Packet: pkt,
			}
			return l.outPort.DownstreamIn.TrySendPacket(targetCycle, pwc)
		}
		return true
	}

	// Call processPackets to process packets
	l.processPackets(
		packets,
		cycle,
		checkReady,
		sendPacket,
		updateUpstreamReady,
	)

	// ===== 4. Ensure Link's Done state is correct =====
	currentDone := l.componentSync.GetDone()
	if currentDone < cycle {
		l.componentSync.SetDone(cycle)
	}

	// ===== 5. Assert cycle+1 is decided (optional) =====
	_, decided := l.IsReadyNonBlocking(cycle + 1)
	if !decided {
		panic(fmt.Sprintf("Tick(cycle=%d) completed but cycle+1=%d is not decided in Link.", cycle, cycle+1))
	}
	l.invokeTickHook(cycle)
	return nil
}

// SnapshotOccupancy reports the pending packet count per slot.
// Phase 3: Updated to use flowControl
func (l *Link) SnapshotOccupancy() []int {
	if l.flowControl == nil {
		return nil
	}

	// Type assert to BufferedFlowControl to access slots
	fc, ok := l.flowControl.(*BufferedFlowControl)
	if !ok {
		// For non-buffered flow control, return empty
		return nil
	}

	slots := fc.GetSlots()
	occupancy := make([]int, len(slots))
	for i, slot := range slots {
		occupancy[i] = len(slot)
	}
	return occupancy
}

// Advance progresses the link by the specified number of cycles.
func (l *Link) Advance(cycles int) error {
	if cycles <= 0 {
		return nil
	}

	debug.Logf("Link.Advance: link=%d->%d, cycles=%d, starting from cycle=%d", l.sourceID, l.targetID, cycles, l.currentCycle)

	// Note: readyUntil initialization is handled by Network.Advance before parallel execution.
	// This prevents overwriting dynamically updated readyUntil values from updateUpstreamReady.

	for i := 0; i < cycles; i++ {
		cycle := l.currentCycle
		debug.Logf("Link.Advance: link=%d->%d, executing cycle=%d (%d/%d)", l.sourceID, l.targetID, cycle, i+1, cycles)
		if err := l.Tick(cycle); err != nil {
			debug.Logf("Link.Advance: link=%d->%d, cycle=%d failed: %v", l.sourceID, l.targetID, cycle, err)
			return err
		}
		l.currentCycle++
		debug.Logf("Link.Advance: link=%d->%d, cycle=%d completed", l.sourceID, l.targetID, cycle)
	}
	debug.Logf("Link.Advance: link=%d->%d, all cycles completed", l.sourceID, l.targetID)
	return nil
}

// SetTickHook registers a callback invoked after each successful Tick.
func (l *Link) SetTickHook(hook func(cycle int)) {
	l.tickHook = hook
}

func (l *Link) invokeTickHook(cycle int) {
	if l.tickHook != nil {
		l.tickHook(cycle)
	}
}

// ===== Link synchronization methods (internal) =====

// ready checks if Link is ready to receive data for the given cycle (internal method).
func (l *Link) ready(cycle int) bool {
	return l.componentSync.Ready(cycle)
}

// IsReadyNonBlocking checks ready state without blocking (internal method).
func (l *Link) IsReadyNonBlocking(cycle int) (bool, bool) {
	return l.componentSync.IsReadyNonBlocking(cycle)
}

// updateReady updates Link's ready state (internal method).
func (l *Link) updateReady(cycle int, ready bool) {
	l.componentSync.UpdateReady(cycle, ready)
}

// setReadyUntil sets readyUntil (internal method).
func (l *Link) setReadyUntil(cycle int) {
	l.componentSync.SetReadyUntil(cycle)
}

// waitDone waits for Link to complete targetCycle (internal method).
func (l *Link) waitDone(targetCycle int) {
	l.componentSync.WaitDone(targetCycle)
}

// GetVisualState returns the visual representation of this link.
func (l *Link) GetVisualState() string {
	if visualization.VisualizationMode == "none" {
		return ""
	}

	if visualization.VisualizationMode == "ascii" {
		// 显示链路上是否有packet在传输
		var packetsInFlight int
		if len(l.pendingPackets) > 0 {
			packetsInFlight = len(l.pendingPackets)
		}
		if packetsInFlight > 0 {
			return fmt.Sprintf("-[%d]-", packetsInFlight)
		}
		return "----"
	}

	return ""
}

// ===== Port implementations =====

// linkInPort implements InPort interface for Link.
type linkInPort struct {
	ahead_port.BaseInPort
	link *Link
}

// TrySendPacket attempts to send a packet to Link for the given cycle.
func (p *linkInPort) TrySendPacket(cycle int, pkt ahead_port.PacketWithCycle) bool {
	if p.InputChan == nil {
		panic("linkInPort.TrySendPacket() called before Plug()")
	}
	if !p.link.ready(cycle) {
		return false
	}
	p.InputChan <- pkt
	return true
}

// ready is an internal helper (not part of InPort interface).
func (p *linkInPort) ready(cycle int) bool {
	return p.link.ready(cycle)
}

// IsReadyNonBlocking checks Link's ready state without blocking.
func (p *linkInPort) IsReadyNonBlocking(cycle int) (bool, bool) {
	return p.link.IsReadyNonBlocking(cycle)
}

// Plug overrides BaseInPort.Plug to pass self.
func (p *linkInPort) Plug(out ahead_port.OutPort) chan ahead_port.PacketWithCycle {
	return p.BaseInPort.PlugWithSelf(p, out)
}

// linkOutPort implements OutPort interface for Link.
type linkOutPort struct {
	ahead_port.BaseOutPort
	link *Link
}

// WaitDone waits for Link to complete the given cycle.
func (p *linkOutPort) WaitDone(cycle int) {
	p.link.waitDone(cycle)
}

// Plug overrides BaseOutPort.Plug to pass self.
func (p *linkOutPort) Plug(in ahead_port.InPort) chan ahead_port.PacketWithCycle {
	return p.BaseOutPort.PlugWithSelf(p, in)
}
