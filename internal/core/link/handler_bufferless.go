package link

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// BufferlessLinkHandler implements an always-ready flow control strategy without physical buffering.
// It attempts to send packets immediately. If downstream is busy, packets are kept in l.pendingPackets.
type BufferlessLinkHandler struct{}

// NewBufferlessLinkHandler creates a BufferlessLinkHandler.
func NewBufferlessLinkHandler() *BufferlessLinkHandler {
	return &BufferlessLinkHandler{}
}

// Process implements the LinkHandler interface for BufferlessLinkHandler.
func (h *BufferlessLinkHandler) Process(l *Link, cycle int, incoming []packet.Packet) error {
	// 1. Process pending packets from previous cycles (delayed due to downstream busy)
	currentPending := l.pendingPackets
	l.pendingPackets = make([]ahead_port.PacketWithCycle, 0)

	for _, pkt := range currentPending {
		if pkt.Cycle <= cycle {
			// When retrying in a bufferless link, use the CURRENT cycle.
			// The original packet cycle is no longer relevant for the downstream port.
			if !h.sendPacket(l, cycle, pkt.Packet) {
				l.pendingPackets = append(l.pendingPackets, pkt)
			}
		} else {
			l.pendingPackets = append(l.pendingPackets, pkt)
		}
	}

	// 2. Process new packets
	for _, pkt := range incoming {
		if !h.sendPacket(l, cycle, pkt) {
			l.pendingPackets = append(l.pendingPackets, ahead_port.PacketWithCycle{
				Cycle:  cycle,
				Packet: pkt,
			})
		}
	}

	// 3. Update ready state for upstream (next cycle)
	if l.fromUpstream != nil {
		// Bufferless links are always ready to accept packets
		l.fromUpstream.UpdateReady(cycle+1, true)
		debug.Logf("Link %d->%d: Set ready[%d]=true (bufferless)", l.sourceID, l.targetID, cycle+1)
	}

	return nil
}

// Reset resets the handler state.
func (h *BufferlessLinkHandler) Reset() {}

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
