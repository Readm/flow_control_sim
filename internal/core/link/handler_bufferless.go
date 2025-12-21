package link

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// BufferlessLinkHandler implements an always-ready flow control strategy without physical buffering.
// It attempts to send packets immediately. If downstream is busy, packets are kept in l.pendingPackets.
// BufferlessLinkHandler implements an always-ready flow control strategy without physical buffering.
// It attempts to send packets immediately. If downstream is busy, packets are kept in pending map.
type BufferlessLinkHandler struct {
	// pending stores packets that failed to send, indexed by their target retry cycle.
	// Key: Target Cycle, Value: List of packets
	pending map[int][]ahead_port.PacketWithCycle
}

// NewBufferlessLinkHandler creates a BufferlessLinkHandler.
// NewBufferlessLinkHandler creates a BufferlessLinkHandler.
func NewBufferlessLinkHandler() *BufferlessLinkHandler {
	return &BufferlessLinkHandler{
		pending: make(map[int][]ahead_port.PacketWithCycle),
	}
}

// Process implements the LinkHandler interface for BufferlessLinkHandler.
func (h *BufferlessLinkHandler) Process(l *Link, cycle int, targetCycle int, incoming []packet.Packet) error {
	// 1. Process pending packets scheduled for this cycle
	if pendingPkts, ok := h.pending[cycle]; ok && len(pendingPkts) > 0 {
		// Clear pending for this cycle, we will retry them
		delete(h.pending, cycle)

		for _, pkt := range pendingPkts {
			// Try to send
			if !h.sendPacket(l, cycle, pkt.Packet) {
				// Failed to send, schedule for next cycle
				nextCycle := cycle + 1

				// CRITICAL: Check against targetCycle limit
				if nextCycle > targetCycle {
					// Stop trying, keep in pending for next AdvanceTo run
					h.addToPending(nextCycle, pkt.Packet)
				} else {
					// We can retry in this run (in next Tick)
					// But BufferlessHandler doesn't automatically carry over to next Tick unless
					// we put it in a place where Next Tick picks it up.
					// Since Process is called per cycle, putting it in pending[nextCycle]
					// ensures it will be picked up when Tick(nextCycle) is called.
					h.addToPending(nextCycle, pkt.Packet)
				}
			}
		}
	}

	// 2. Process new incoming packets
	for _, pkt := range incoming {
		if !h.sendPacket(l, cycle, pkt) {
			// Failed to send, schedule for next cycle
			nextCycle := cycle + 1
			h.addToPending(nextCycle, pkt)
		}
	}

	// 3. Update ready state for upstream (next cycle)
	if l.fromUpstream != nil {
		l.fromUpstream.UpdateReady(cycle+1, true)
		debug.Logf("Link %d->%d: Set ready[%d]=true (bufferless)", l.sourceID, l.targetID, cycle+1)
	}

	return nil
}

func (h *BufferlessLinkHandler) addToPending(cycle int, pkt packet.Packet) {
	if h.pending == nil {
		h.pending = make(map[int][]ahead_port.PacketWithCycle)
	}
	h.pending[cycle] = append(h.pending[cycle], ahead_port.PacketWithCycle{
		Cycle:  cycle,
		Packet: pkt,
	})
}

// GetOccupancy returns the pending packets distribution relative to current cycle (handled by Link).
func (h *BufferlessLinkHandler) GetOccupancy(currentCycle int) []int {
	if len(h.pending) == 0 {
		return nil
	}

	// Find the max relative offset needed
	maxOffset := 0
	found := false
	for targetCycle := range h.pending {
		offset := targetCycle - currentCycle
		if offset >= 0 {
			if offset > maxOffset {
				maxOffset = offset
			}
			found = true
		}
	}

	if !found {
		return nil
	}

	// Create slice with size maxOffset + 1
	occupancy := make([]int, maxOffset+1)
	for targetCycle, pkts := range h.pending {
		offset := targetCycle - currentCycle
		if offset >= 0 {
			occupancy[offset] = len(pkts)
		}
	}

	return occupancy
}

// Reset resets the handler state.
func (h *BufferlessLinkHandler) Reset() {
	h.pending = make(map[int][]ahead_port.PacketWithCycle)
}

// ReadyDepth returns the number of cycles to pre-mark as ready for bootstrapping.
// Bufferless links are always ready, but need at least cycle 0 to start.
func (h *BufferlessLinkHandler) Init(l *Link) {
	// Signal cycle 0 as ready to start the clock
	l.UpdateUpstreamReady(0, true)
}

func (h *BufferlessLinkHandler) sendPacket(l *Link, targetCycle int, pkt packet.Packet) bool {
	if l.toDownstream == nil {
		return true
	}
	pwc := ahead_port.PacketWithCycle{
		Cycle:  targetCycle,
		Packet: pkt,
	}
	return l.toDownstream.TrySend(targetCycle, pwc)
}
