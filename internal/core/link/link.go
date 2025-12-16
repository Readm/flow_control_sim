package link

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/visualization"
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

// LinkCycleProcessor is a custom cycle processor for Link that waits for Done(cycle-latency)
// instead of Done(cycle-1). This allows Link to process packets earlier, taking advantage
// of the latency buffer.
type LinkCycleProcessor struct {
	link      *Link                 // Reference to Link
	processor *LinkPacketProcessor  // Packet processing logic
	latency   int                   // Latency in cycles
}

// Tick implements the cycle processing workflow with custom wait logic.
func (lcp *LinkCycleProcessor) Tick(cycle int) error {
	if lcp.processor == nil {
		panic("LinkCycleProcessor.processor is nil")
	}

	link := lcp.link

	// ===== 1. Wait for upstream completion =====
	// Link at cycle N needs to wait for upstream to complete cycle - latency
	targetWaitCycle := cycle - lcp.latency
	if link.inPort.UpstreamOut != nil {
		link.inPort.UpstreamOut.WaitDone(targetWaitCycle)
	}

	// ===== 2. Prepare updateUpstreamReady function =====
	// This function notifies upstream of Link's ready status
	updateUpstreamReady := func(cycle int, ready bool) {
		link.updateReady(cycle, ready)
	}

	// ===== 3. Process packets =====
	// Get upstream's output channel (if plugged)
	var receiveChan <-chan ahead_port.PacketWithCycle
	if link.inPort.InputChan != nil {
		receiveChan = link.inPort.InputChan
	} else {
		// If not plugged, use empty channel
		receiveChan = make(chan ahead_port.PacketWithCycle)
	}

	// Get downstream's Ready check function
	checkReady := func(cycle int) bool {
		if link.outPort.DownstreamIn != nil {
			return link.outPort.DownstreamIn.Ready(cycle)
		}
		return true // If not plugged, default to ready
	}

	// Get send function
	sendPacket := func(pkt ahead_port.PacketWithCycle) {
		if link.outPort.OutputChan != nil {
			link.outPort.OutputChan <- pkt
		}
	}

	// Get setDone function (sets Link's own done state)
	setDone := func(cycle int) {
		link.setDone(cycle)
	}

	// Call PacketProcessor to process packets
	lcp.processor.ProcessPackets(
		receiveChan,
		cycle,
		checkReady,
		sendPacket,
		setDone,
		updateUpstreamReady,
	)

	// ===== 4. Ensure Link's Done state is correct =====
	currentDone := link.getDone()
	if currentDone < cycle {
		link.setDone(cycle)
	}

	// ===== 5. Assert cycle+1 is decided (optional, for debugging) =====
	_, decided := link.readyNonBlocking(cycle + 1)
	if !decided {
		panic(fmt.Sprintf("Tick(cycle=%d) completed but cycle+1=%d is not decided in Link. Processor must call updateUpstreamReady(cycle+1, ready) in ProcessPackets.", cycle, cycle+1))
	}

	return nil
}

// Link represents a directed edge in the topology.
// Link receives packets from upstream and forwards them to downstream with latency and bandwidth constraints.
// Link manages its own synchronization state and exposes InPort and OutPort interfaces.
type Link struct {
	sourceID          int
	targetID          int

	// Port references (set by NewLink, used internally)
	inPort            *linkInPort
	outPort           *linkOutPort

	// +++ Flow control strategy (Phase 3 addition) +++
	flowControl       FlowControlStrategy

	// Link's own synchronization state
	done              int64         // Link's done cycle (atomic)
	readyUntil        int64         // Link's ready until cycle (atomic)
	readyMap          map[int]bool  // Specific cycle ready status

	// Synchronization primitives
	doneMu            sync.Mutex
	doneCond          *sync.Cond
	waiterMu          sync.Mutex
	cond              *sync.Cond

	processor         *LinkCycleProcessor
	packetProc        *LinkPacketProcessor
	latency           int
	bandwidth         int
	currentCycle      int
	tickHookMu        sync.RWMutex
	tickHook          func(cycle int)
}

// LinkPacketProcessor handles packet processing for Link.
// It handles latency (delaying packets) and bandwidth constraints.
type LinkPacketProcessor struct {
	link           *Link
	pendingPackets []ahead_port.PacketWithCycle
	// DEPRECATED: Slots moved to BufferedFlowControl (will be removed in cleanup)
	// slots [][]ahead_port.PacketWithCycle
}

// NewLinkPacketProcessor creates a new LinkPacketProcessor.
func NewLinkPacketProcessor(link *Link) *LinkPacketProcessor {
	// Slots are now managed by flowControl strategy
	return &LinkPacketProcessor{
		link:           link,
		pendingPackets: make([]ahead_port.PacketWithCycle, 0),
	}
}

// ProcessPackets processes packets for Link: receive from upstream, apply latency, and send to downstream.
// Phase 3: Refactored to use FlowControlStrategy
func (l *LinkPacketProcessor) ProcessPackets(
	receiveChan <-chan ahead_port.PacketWithCycle,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(ahead_port.PacketWithCycle),
	setDone func(int),
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
	if bufferedFC, ok := l.link.flowControl.(*BufferedFlowControl); ok {
		// Use buffered flow control logic (with slots and backpressure)
		l.processPacketsBuffered(bufferedFC, receiveChan, cycle, checkReady, sendPacket, setDone, &newPendingPackets)
	} else if _, ok := l.link.flowControl.(*BufferlessFlowControl); ok {
		// Use bufferless flow control logic (simple latency-based forwarding)
		l.processPacketsBufferless(receiveChan, cycle, sendPacket, setDone, &newPendingPackets)
	} else {
		panic(fmt.Sprintf("Unsupported flow control type: %T", l.link.flowControl))
	}

	// Update pending packets
	l.pendingPackets = newPendingPackets

	// Notify upstream that we are ready for next cycle using waitGroup to wait for completion
	wg.Wait()
}

// processPacketsBuffered handles packet processing for BufferedFlowControl.
func (l *LinkPacketProcessor) processPacketsBuffered(
	fc *BufferedFlowControl,
	receiveChan <-chan ahead_port.PacketWithCycle,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(ahead_port.PacketWithCycle),
	setDone func(int),
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

	// 2. Receive and process new packets from channel
	for {
		select {
		case pkt := <-receiveChan:
			sourceCycle := pkt.Cycle
			targetCycle := sourceCycle + l.link.latency

			delayedPkt := ahead_port.PacketWithCycle{
				Cycle:  targetCycle,
				Packet: pkt.Packet,
			}

			if !fc.CanAcceptPacket(cycle, targetCycle) {
				*newPendingPackets = append(*newPendingPackets, delayedPkt)
				continue
			}
			addToSlot(delayedPkt, targetCycle)

		default:
			goto doneProcessing
		}
	}

doneProcessing:
	// Decide whether to send based on downstream readiness
	downstreamReady := checkReady(cycle)
	if fc.CanSendPacket(cycle, downstreamReady) {
		slot := fc.GetSlot(cycle)
		for _, pkt := range slot {
			pkt.Cycle = cycle
			sendPacket(pkt)
		}
		fc.ClearSlot(cycle)
	} else {
		fc.IncrementBackpressure()
	}

	setDone(cycle)
}

// processPacketsBufferless handles packet processing for BufferlessFlowControl.
// Simpler logic: packets are delayed by latency and forwarded immediately when ready.
func (l *LinkPacketProcessor) processPacketsBufferless(
	receiveChan <-chan ahead_port.PacketWithCycle,
	cycle int,
	sendPacket func(ahead_port.PacketWithCycle),
	setDone func(int),
	newPendingPackets *[]ahead_port.PacketWithCycle,
) {
	// 1. Process pending packets - send those whose time has come
	for _, pkt := range l.pendingPackets {
		if pkt.Cycle <= cycle {
			// Time to send this packet
			pkt.Cycle = cycle
			sendPacket(pkt)
		} else {
			// Still waiting
			*newPendingPackets = append(*newPendingPackets, pkt)
		}
	}

	// 2. Receive new packets and either send immediately or queue
	for {
		select {
		case pkt := <-receiveChan:
			sourceCycle := pkt.Cycle
			targetCycle := sourceCycle + l.link.latency

			delayedPkt := ahead_port.PacketWithCycle{
				Cycle:  targetCycle,
				Packet: pkt.Packet,
			}

			if targetCycle <= cycle {
				// Can send immediately
				delayedPkt.Cycle = cycle
				sendPacket(delayedPkt)
			} else {
				// Need to wait
				*newPendingPackets = append(*newPendingPackets, delayedPkt)
			}

		default:
			goto doneProcessing
		}
	}

doneProcessing:
	setDone(cycle)
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
		sourceID:    sourceID,
		targetID:    targetID,
		latency:     latency,
		bandwidth:   bandwidth,
		flowControl: flowControl,
		currentCycle: 0,
		done:         -1,
		readyUntil:   0,
		readyMap:     make(map[int]bool),
	}

	// Create ports
	inPort := &linkInPort{link: link}
	outPort := &linkOutPort{link: link}

	// Link holds port references
	link.inPort = inPort
	link.outPort = outPort

	// Create packet processor
	link.packetProc = NewLinkPacketProcessor(link)

	// Create cycle processor
	link.processor = &LinkCycleProcessor{
		link:      link,
		processor: link.packetProc,
		latency:   latency,
	}

	// Initialize readyUntil for the first 'latency' cycles.
	// Rationale: Link is ready for the first 'latency' cycles because packets
	// are still in transit (haven't reached downstream yet).
	// This prevents deadlock in cyclic topologies during initialization.
	link.readyUntil = int64(latency)
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
	if err := l.processor.Tick(cycle); err != nil {
		return err
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
	l.tickHookMu.Lock()
	defer l.tickHookMu.Unlock()
	l.tickHook = hook
}

func (l *Link) invokeTickHook(cycle int) {
	l.tickHookMu.RLock()
	defer l.tickHookMu.RUnlock()
	if l.tickHook != nil {
		l.tickHook(cycle)
	}
}

// ===== Link synchronization methods (internal) =====

// setDone sets Link's done state (internal method).
func (l *Link) setDone(cycle int) {
	atomic.StoreInt64(&l.done, int64(cycle))

	// Wake up waiting goroutines
	l.doneMu.Lock()
	if l.doneCond != nil {
		l.doneCond.Broadcast()
	}
	l.doneMu.Unlock()
}

// getDone gets Link's done state (internal method).
func (l *Link) getDone() int {
	return int(atomic.LoadInt64(&l.done))
}

// waitDone waits for Link to complete targetCycle (internal method).
func (l *Link) waitDone(targetCycle int) {
	currentDone := l.getDone()
	if currentDone >= targetCycle {
		return
	}

	l.doneMu.Lock()
	defer l.doneMu.Unlock()

	if l.doneCond == nil {
		l.doneCond = sync.NewCond(&l.doneMu)
	}

	for l.getDone() < targetCycle {
		l.doneCond.Wait()
	}
}

// ready checks if Link is ready to receive data for the given cycle (internal method).
func (l *Link) ready(cycle int) bool {
	// Fast path: if cycle < readyUntil, return true immediately
	readyUntil := atomic.LoadInt64(&l.readyUntil)
	if int64(cycle) < readyUntil {
		return true
	}

	// Check readyMap
	l.waiterMu.Lock()
	ready, exists := l.readyMap[cycle]
	l.waiterMu.Unlock()

	if exists {
		return ready
	}

	// Block and wait
	return l.waitForReady(cycle)
}

// readyNonBlocking checks ready state without blocking (internal method).
func (l *Link) readyNonBlocking(cycle int) (bool, bool) {
	readyUntil := atomic.LoadInt64(&l.readyUntil)
	if int64(cycle) < readyUntil {
		return true, true
	}

	l.waiterMu.Lock()
	ready, exists := l.readyMap[cycle]
	l.waiterMu.Unlock()

	if exists {
		return ready, true
	}

	return false, false
}

// waitForReady blocks until ready (internal method).
func (l *Link) waitForReady(cycle int) bool {
	l.waiterMu.Lock()
	defer l.waiterMu.Unlock()

	if l.cond == nil {
		l.cond = sync.NewCond(&l.waiterMu)
	}

	for {
		if ready, exists := l.readyMap[cycle]; exists {
			return ready
		}
		l.cond.Wait()
	}
}

// updateReady updates Link's ready state (internal method).
func (l *Link) updateReady(cycle int, ready bool) {
	l.waiterMu.Lock()
	defer l.waiterMu.Unlock()

	l.readyMap[cycle] = ready

	if l.cond != nil {
		l.cond.Broadcast()
	}
}

// setReadyUntil sets readyUntil (internal method).
func (l *Link) setReadyUntil(cycle int) {
	// Atomically update readyUntil
	for {
		current := atomic.LoadInt64(&l.readyUntil)
		if int64(cycle) <= current {
			return
		}
		if atomic.CompareAndSwapInt64(&l.readyUntil, current, int64(cycle)) {
			break
		}
	}

	// Wake up all waiting goroutines
	// Because readyUntil increased, previously blocked cycles may now be ready
	l.waiterMu.Lock()
	if l.cond != nil {
		l.cond.Broadcast()
	}
	l.waiterMu.Unlock()
}

// GetVisualState returns the visual representation of this link.
func (l *Link) GetVisualState() string {
	if visualization.VisualizationMode == "none" {
		return ""
	}

	if visualization.VisualizationMode == "ascii" {
		// 显示链路上是否有packet在传输
		var packetsInFlight int
		if l.packetProc != nil {
			packetsInFlight = len(l.packetProc.pendingPackets)
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

// Ready checks if Link is ready to receive data for the given cycle.
func (p *linkInPort) Ready(cycle int) bool {
	return p.link.ready(cycle)
}

// ReadyNonBlocking checks Link's ready state without blocking.
func (p *linkInPort) ReadyNonBlocking(cycle int) (bool, bool) {
	return p.link.readyNonBlocking(cycle)
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

// GetDone gets Link's current done cycle.
func (p *linkOutPort) GetDone() int {
	return p.link.getDone()
}

// Plug overrides BaseOutPort.Plug to pass self.
func (p *linkOutPort) Plug(in ahead_port.InPort) chan ahead_port.PacketWithCycle {
	return p.BaseOutPort.PlugWithSelf(p, in)
}
