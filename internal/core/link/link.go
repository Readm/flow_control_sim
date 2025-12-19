package link

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// CreateLinkHandler is a factory function that creates link handlers by type.
//
// Supported handler types:
// - "buffered": BufferedLinkHandler with ring buffer and backpressure
// - "bufferless": BufferlessLinkHandler (always-ready, no physical buffering)
//
// Parameters:
// - handlerType: type of link handler ("buffered", "bufferless")
// - latency: latency parameter (used by buffered, ignored by bufferless)
// - bandwidth: bandwidth parameter (used by buffered, ignored by bufferless)
//
// Returns:
// - LinkHandler instance, or panic if handlerType is unknown
func CreateLinkHandler(handlerType string, latency, bandwidth int) LinkHandler {
	switch handlerType {
	case "buffered":
		return NewBufferedLinkHandler(latency, bandwidth)
	case "bufferless":
		return NewBufferlessLinkHandler()
	default:
		panic(fmt.Sprintf("unknown link handler type: %s", handlerType))
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

	// ===== Handler pattern =====
	handler LinkHandler

	// ===== Link parameters =====
	latency      int
	bandwidth    int
	currentCycle int
	tickHook     func(cycle int)

	// ===== Buffered packets (Backlog) =====
	pendingPackets []ahead_port.PacketWithCycle
}

// NewLink creates a Link with BufferedLinkHandler by default.
func NewLink(sourceID, targetID, latency, bandwidth int) *Link {
	handler := NewBufferedLinkHandler(latency, bandwidth)
	return NewLinkWithHandler(sourceID, targetID, latency, bandwidth, handler)
}

// NewLinkWithHandler creates a new Link with a custom handler.
func NewLinkWithHandler(sourceID, targetID, latency, bandwidth int, handler LinkHandler) *Link {
	if latency <= 0 {
		panic("latency must be positive")
	}
	if bandwidth <= 0 {
		panic("bandwidth must be positive")
	}
	if handler == nil {
		panic("handler must not be nil")
	}

	return &Link{
		sourceID:       sourceID,
		targetID:       targetID,
		latency:        latency,
		bandwidth:      bandwidth,
		handler:        handler,
		currentCycle:   0,
		pendingPackets: make([]ahead_port.PacketWithCycle, 0),
	}
}

// SetUpstreamPort sets the port for receiving data from upstream.
func (l *Link) SetUpstreamPort(port ahead_port.OutPort) {
	l.fromUpstream = port
	// No default ready signals. Handlers must provide them in their Process method.
}

// SetDownstreamPort sets the port for sending data to downstream.
func (l *Link) SetDownstreamPort(port ahead_port.InPort) {
	l.toDownstream = port
}

// UpdateUpstreamReady updates the ready state of the upstream port for a specific cycle.
// This is useful for custom LinkHandlers to signal backpressure.
func (l *Link) UpdateUpstreamReady(cycle int, ready bool) {
	if l.fromUpstream != nil {
		l.fromUpstream.UpdateReady(cycle, ready)
	}
}

// AddPendingPacket allows external handlers to store a packet back into the link's pending list.
// This is useful for custom handlers that need to retry sending.
func (l *Link) AddPendingPacket(pwc ahead_port.PacketWithCycle) {
	l.pendingPackets = append(l.pendingPackets, pwc)
}

// Init initializes the link after being connected to the network.
func (l *Link) Init() {
	l.handler.Init(l)
}

// SourceID returns the ID of the upstream node.
func (l *Link) SourceID() int { return l.sourceID }

// TargetID returns the ID of the downstream node.
func (l *Link) TargetID() int { return l.targetID }

// Latency returns the configured delay in cycles.
func (l *Link) Latency() int { return l.latency }

// Bandwidth returns the maximum packets per cycle.
func (l *Link) Bandwidth() int { return l.bandwidth }

// GetHandler returns the handler for this link.
func (l *Link) GetHandler() LinkHandler {
	return l.handler
}

// SnapshotOccupancy reports the pending packet count per slot for buffered links.
func (l *Link) SnapshotOccupancy() []int {
	if bh, ok := l.handler.(*BufferedLinkHandler); ok {
		slots := bh.GetSlots()
		occupancy := make([]int, len(slots))
		for i, slot := range slots {
			occupancy[i] = len(slot)
		}
		return occupancy
	}
	return nil
}

// Tick processes a single cycle.
// Template: Receive -> handler.Process -> MarkDone
func (l *Link) Tick(cycle int) error {
	// ===== 1. Phase 1: Receive packets from upstream =====
	var incoming []packet.Packet
	waitCycle := cycle - l.latency
	if l.fromUpstream != nil && waitCycle >= 0 {
		incoming = l.fromUpstream.Receive(waitCycle)
		debug.Logf("Link %d->%d: Tick(%d) received %d packets from waitCycle=%d", l.sourceID, l.targetID, cycle, len(incoming), waitCycle)
	}

	// ===== 2. Phase 2: Process via Handler (Core Logic) =====
	if err := l.handler.Process(l, cycle, incoming); err != nil {
		return fmt.Errorf("link %d->%d handler failed: %w", l.sourceID, l.targetID, err)
	}

	// ===== 3. Phase 3: Mark this cycle as done for downstream =====
	if l.toDownstream != nil {
		l.toDownstream.MarkDone(cycle)
	}

	l.invokeTickHook(cycle)
	return nil
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
