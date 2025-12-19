package link

import (
	"fmt"

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
type Link struct {
	sourceID int
	targetID int

	// ===== Port references (not owned by Link) =====
	fromUpstream ahead_port.OutPort // Receive from upstream
	toDownstream ahead_port.InPort  // Send to downstream

	// ===== Flow control strategy =====
	flowControl FlowControlStrategy

	// ===== Link parameters =====
	latency      int
	bandwidth    int
	currentCycle int
	tickHook     func(cycle int)

	// ===== Buffered packets =====
	pendingPackets []ahead_port.PacketWithCycle
}

// NewLink creates a Link with BufferedFlowControl by default.
// Ports must be set separately using SetUpstreamPort and SetDownstreamPort, or via Connect().
//
// Parameters:
// - sourceID: ID of the source node
// - targetID: ID of the target node
// - latency: number of cycles for packet delivery (must be >= 0)
// - bandwidth: maximum packets per cycle (must be > 0)
func NewLink(sourceID, targetID, latency, bandwidth int) *Link {
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
func NewLinkWithFlowControl(sourceID, targetID, latency, bandwidth int, flowControl FlowControlStrategy) *Link {
	if latency <= 0 {
		panic("latency must be positive (0-latency creates combinational loops in sequential simulation)")
	}
	if bandwidth <= 0 {
		panic("bandwidth must be positive")
	}
	if flowControl == nil {
		panic("flowControl must not be nil")
	}

	return &Link{
		sourceID:       sourceID,
		targetID:       targetID,
		latency:        latency,
		bandwidth:      bandwidth,
		flowControl:    flowControl,
		currentCycle:   0,
		pendingPackets: make([]ahead_port.PacketWithCycle, 0),
	}
}

// SetUpstreamPort sets the port for receiving data from upstream.
// Link acts as downstream for this port.
func (l *Link) SetUpstreamPort(port ahead_port.OutPort) {
	l.fromUpstream = port
	// Initialize ready state for initial cycles
	// This is necessary because OutputQueue may try to send before Link's first Tick
	// For bufferless links, we're always ready, so initialize generously
	for i := 0; i < 10; i++ {
		l.fromUpstream.UpdateReady(i, l.flowControl.IsReady(i))
	}
}

// SetDownstreamPort sets the port for sending data to downstream.
// Link acts as upstream for this port.
func (l *Link) SetDownstreamPort(port ahead_port.InPort) {
	l.toDownstream = port
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
// This is dramatically simpler than the old implementation because Port handles all synchronization.
func (l *Link) Tick(cycle int) error {
	// ===== 1. Wait for upstream to complete necessary cycle =====
	waitCycle := cycle - l.latency
	if waitCycle >= 0 && l.fromUpstream != nil {
		l.fromUpstream.WaitUpstreamDone(waitCycle)
	}

	// ===== 2. Receive packets from upstream =====
	var packets []packet.Packet
	if l.fromUpstream != nil && waitCycle >= 0 {
		packets = l.fromUpstream.Receive(waitCycle)
		debug.Logf("Link %d->%d: Tick(%d) received %d packets from waitCycle=%d", l.sourceID, l.targetID, cycle, len(packets), waitCycle)
	} else {
		debug.Logf("Link %d->%d: Tick(%d) skip receive (waitCycle=%d)", l.sourceID, l.targetID, cycle, waitCycle)
	}

	// ===== 3. Process packets using flow control strategy =====
	newPending := make([]ahead_port.PacketWithCycle, 0)

	// Check flow control type and use appropriate processing logic
	if bufferedFC, ok := l.flowControl.(*BufferedFlowControl); ok {
		l.processPacketsBuffered(bufferedFC, packets, cycle, &newPending)
	} else if _, ok := l.flowControl.(*BufferlessFlowControl); ok {
		l.processPacketsBufferless(packets, cycle, &newPending)
	} else {
		panic(fmt.Sprintf("Unsupported flow control type: %T", l.flowControl))
	}

	l.pendingPackets = newPending
	debug.Logf("Link %d->%d: Tick(%d) now has %d pending packets", l.sourceID, l.targetID, cycle, len(newPending))

	// ===== 4. Update ready state for upstream =====
	// We delegate readiness logic to the Flow Control strategy.
	// IMPORTANT: Set ready for CURRENT cycle (for this tick) AND next cycle
	if l.fromUpstream != nil {
		// Set ready for current cycle (in case upstream hasn't sent yet)
		readyCurrent := l.flowControl.IsReady(cycle)
		l.fromUpstream.UpdateReady(cycle, readyCurrent)
		debug.Logf("Link %d->%d: Set ready[%d]=%v", l.sourceID, l.targetID, cycle, readyCurrent)

		// Set ready for next cycle (for next tick)
		readyNext := l.flowControl.IsReady(cycle + 1)
		l.fromUpstream.UpdateReady(cycle+1, readyNext)
		debug.Logf("Link %d->%d: Set ready[%d]=%v", l.sourceID, l.targetID, cycle+1, readyNext)
	}

	// ===== 5. Mark this cycle as done for downstream =====
	if l.toDownstream != nil {
		l.toDownstream.MarkDone(cycle)
	}

	l.invokeTickHook(cycle)
	return nil
}

// processPacketsBuffered handles packet processing for BufferedFlowControl.
func (l *Link) processPacketsBuffered(
	fc *BufferedFlowControl,
	packets []packet.Packet,
	cycle int,
	newPending *[]ahead_port.PacketWithCycle,
) {
	// Helper to check if downstream is ready
	checkDownstreamReady := func(targetCycle int) bool {
		if l.toDownstream == nil {
			return true
		}
		return l.toDownstream.IsReady(targetCycle)
	}

	// Helper to send packet
	sendPacket := func(targetCycle int, pkt packet.Packet) bool {
		if l.toDownstream == nil {
			return true
		}
		pwc := ahead_port.PacketWithCycle{
			Cycle:  targetCycle,
			Packet: pkt,
		}
		return l.toDownstream.TrySend(targetCycle, pwc)
	}

	// 1. Process pending packets
	for _, pkt := range l.pendingPackets {
		targetCycle := pkt.Cycle
		// If packet was targeted for a past cycle, reschedule for now
		if targetCycle < cycle {
			targetCycle = cycle
			pkt.Cycle = cycle
		}

		if fc.CanAcceptPacket(cycle, targetCycle) {
			fc.AddToSlot(pkt, targetCycle)
		} else {
			*newPending = append(*newPending, pkt)
		}
	}

	// 2. Process new packets from upstream
	for _, pkt := range packets {
		targetCycle := cycle
		if fc.CanAcceptPacket(cycle, targetCycle) {
			pwc := ahead_port.PacketWithCycle{
				Cycle:  targetCycle,
				Packet: pkt,
			}
			fc.AddToSlot(pwc, targetCycle)
		} else {
			// Cannot accept (bandwidth full or outside window), keep as pending
			*newPending = append(*newPending, ahead_port.PacketWithCycle{
				Cycle:  targetCycle,
				Packet: pkt,
			})
		}
	}

	// 3. Try to send packets from flow control slots
	downstreamReady := checkDownstreamReady(cycle)
	if fc.CanSendPacket(cycle, downstreamReady) {
		slot := fc.GetSlot(cycle)
		var pendingInSlot []ahead_port.PacketWithCycle
		allSent := true

		for _, pwc := range slot {
			if !sendPacket(pwc.Cycle, pwc.Packet) {
				pendingInSlot = append(pendingInSlot, pwc)
				allSent = false
			}
		}

		if allSent {
			fc.ClearSlot(cycle)
		} else {
			fc.UpdateSlot(cycle, pendingInSlot)
			fc.IncrementBackpressure()
		}
	} else {
		fc.IncrementBackpressure()
	}
}

// processPacketsBufferless handles packet processing for BufferlessFlowControl.
func (l *Link) processPacketsBufferless(
	packets []packet.Packet,
	cycle int,
	newPending *[]ahead_port.PacketWithCycle,
) {
	// Helper to send packet
	sendPacket := func(targetCycle int, pkt packet.Packet) bool {
		if l.toDownstream == nil {
			return true
		}
		pwc := ahead_port.PacketWithCycle{
			Cycle:  targetCycle,
			Packet: pkt,
		}
		return l.toDownstream.TryPeekSend(targetCycle, pwc)
	}

	// 1. Process pending packets
	for _, pkt := range l.pendingPackets {
		if pkt.Cycle <= cycle {
			if !sendPacket(pkt.Cycle, pkt.Packet) {
				// Downstream not ready, buffer this packet for retry in future cycles.
				// This simulates the packet being "on the wire" or in flight.
				*newPending = append(*newPending, pkt)
			}
		} else {
			*newPending = append(*newPending, pkt)
		}
	}

	// 2. Process new packets
	for _, pkt := range packets {
		targetCycle := cycle
		if !sendPacket(targetCycle, pkt) {
			// Downstream not ready, buffer this packet for retry.
			*newPending = append(*newPending, ahead_port.PacketWithCycle{
				Cycle:  targetCycle,
				Packet: pkt,
			})
		}
	}
}

// SnapshotOccupancy reports the pending packet count per slot.
func (l *Link) SnapshotOccupancy() []int {
	if l.flowControl == nil {
		return nil
	}

	fc, ok := l.flowControl.(*BufferedFlowControl)
	if !ok {
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

// GetVisualState returns the visual representation of this link.
func (l *Link) GetVisualState() string {
	if visualization.VisualizationMode == "none" {
		return ""
	}

	if visualization.VisualizationMode == "ascii" {
		packetsInFlight := len(l.pendingPackets)
		if packetsInFlight > 0 {
			return fmt.Sprintf("-[%d]-", packetsInFlight)
		}
		return "----"
	}

	return ""
}

// PendingPacketCount returns the number of buffered packets.
func (l *Link) PendingPacketCount() int {
	return len(l.pendingPackets)
}
