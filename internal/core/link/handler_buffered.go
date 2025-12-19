package link

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// BufferedLinkHandler implements buffered flow control with backpressure.
//
// Mechanism:
// - Ring buffer (slots) stores packets during transit (latency cycles)
// - Backpressure counter tracks accumulated delays when downstream is not ready
// - Bandwidth limit enforces maximum packets per slot
type BufferedLinkHandler struct {
	// Ring buffer slots - one slot per latency cycle
	// Each slot stores packets destined for that cycle
	slots [][]ahead_port.PacketWithCycle

	// Backpressure counter - increments when downstream is not ready
	// This shifts the slot index to delay packet transmission
	totalBackpressure int

	// Configuration
	latency   int // Number of slots in ring buffer
	bandwidth int // Maximum packets per slot
}

// NewBufferedLinkHandler creates a BufferedLinkHandler.
func NewBufferedLinkHandler(latency, bandwidth int) *BufferedLinkHandler {
	if latency <= 0 {
		panic("latency must be positive for BufferedLinkHandler")
	}
	if bandwidth <= 0 {
		panic("bandwidth must be positive")
	}

	slots := make([][]ahead_port.PacketWithCycle, latency)
	return &BufferedLinkHandler{
		slots:             slots,
		latency:           latency,
		bandwidth:         bandwidth,
		totalBackpressure: 0,
	}
}

// Process implements the LinkHandler interface for BufferedLinkHandler.
func (h *BufferedLinkHandler) Process(l *Link, cycle int, incoming []packet.Packet) error {
	// 1. Process pending packets (those that couldn't fit in the window or were delayed)
	// These are stored in l.pendingPackets
	currentPending := l.pendingPackets
	l.pendingPackets = make([]ahead_port.PacketWithCycle, 0)

	for _, pkt := range currentPending {
		targetCycle := pkt.Cycle
		if targetCycle < cycle {
			targetCycle = cycle
			pkt.Cycle = cycle
		}

		if h.CanAcceptPacket(cycle, targetCycle) {
			h.AddToSlot(pkt, targetCycle)
		} else {
			l.pendingPackets = append(l.pendingPackets, pkt)
		}
	}

	// 2. Process new packets from upstream
	for _, pkt := range incoming {
		targetCycle := cycle
		if h.CanAcceptPacket(cycle, targetCycle) {
			pwc := ahead_port.PacketWithCycle{
				Cycle:  targetCycle,
				Packet: pkt,
			}
			h.AddToSlot(pwc, targetCycle)
		} else {
			l.pendingPackets = append(l.pendingPackets, ahead_port.PacketWithCycle{
				Cycle:  targetCycle,
				Packet: pkt,
			})
		}
	}

	// 3. Try to send packets from current slot to downstream
	downstreamReady := true
	if l.toDownstream != nil {
		downstreamReady = l.toDownstream.IsReady(cycle)
	}

	if downstreamReady {
		slot := h.GetSlot(cycle)
		var pendingInSlot []ahead_port.PacketWithCycle
		allSent := true

		for _, pwc := range slot {
			if l.toDownstream != nil {
				if !l.toDownstream.TrySend(cycle, pwc) {
					pendingInSlot = append(pendingInSlot, pwc)
					allSent = false
				}
			}
		}

		if allSent {
			h.ClearSlot(cycle)
		} else {
			h.UpdateSlot(cycle, pendingInSlot)
			h.totalBackpressure++
		}
	} else {
		h.totalBackpressure++
	}

	// 4. Update ready state for upstream (next cycle)
	if l.fromUpstream != nil {
		// Calculate if we have capacity for next cycle
		hasCapacity := h.CheckSpace(cycle + 1)
		l.fromUpstream.UpdateReady(cycle+1, hasCapacity)
		debug.Logf("Link %d->%d: Set ready[%d]=%v (buffered)", l.sourceID, l.targetID, cycle+1, hasCapacity)
	}

	return nil
}

// Reset resets the handler state.
func (h *BufferedLinkHandler) Reset() {
	h.totalBackpressure = 0
	for i := range h.slots {
		h.slots[i] = nil
	}
}

// ReadyDepth returns the number of cycles to pre-mark as ready for bootstrapping.
// For buffered links, we need to fill the pipeline (latency) plus one for cycle 0.
func (h *BufferedLinkHandler) Init(l *Link) {
	depth := h.latency + 1
	for i := 0; i < depth; i++ {
		l.UpdateUpstreamReady(i, true)
	}
}

func (h *BufferedLinkHandler) CanAcceptPacket(cycle int, targetCycle int) bool {
	if targetCycle-cycle >= h.latency {
		return false
	}
	targetSlotIndex := ((targetCycle-h.totalBackpressure)%h.latency + h.latency) % h.latency
	return len(h.slots[targetSlotIndex]) < h.bandwidth
}

func (h *BufferedLinkHandler) CheckSpace(cycle int) bool {
	targetSlotIndex := ((cycle-h.totalBackpressure)%h.latency + h.latency) % h.latency
	return len(h.slots[targetSlotIndex]) < h.bandwidth
}

func (h *BufferedLinkHandler) AddToSlot(pkt ahead_port.PacketWithCycle, targetCycle int) {
	targetSlotIndex := ((targetCycle-h.totalBackpressure)%h.latency + h.latency) % h.latency

	// Sanity check
	if len(h.slots[targetSlotIndex]) >= h.bandwidth {
		panic("BufferedLinkHandler.AddToSlot: slot is full (bandwidth limit exceeded)")
	}

	h.slots[targetSlotIndex] = append(h.slots[targetSlotIndex], pkt)
}

func (h *BufferedLinkHandler) GetSlot(cycle int) []ahead_port.PacketWithCycle {
	slotIndex := ((cycle-h.totalBackpressure)%h.latency + h.latency) % h.latency
	return h.slots[slotIndex]
}

func (h *BufferedLinkHandler) ClearSlot(cycle int) {
	slotIndex := ((cycle-h.totalBackpressure)%h.latency + h.latency) % h.latency
	h.slots[slotIndex] = nil
}

func (h *BufferedLinkHandler) UpdateSlot(cycle int, packets []ahead_port.PacketWithCycle) {
	slotIndex := ((cycle-h.totalBackpressure)%h.latency + h.latency) % h.latency
	h.slots[slotIndex] = packets
}

func (h *BufferedLinkHandler) GetLatency() int {
	return h.latency
}

func (h *BufferedLinkHandler) GetBandwidth() int {
	return h.bandwidth
}

func (h *BufferedLinkHandler) GetTotalBackpressure() int {
	return h.totalBackpressure
}

func (h *BufferedLinkHandler) GetSlots() [][]ahead_port.PacketWithCycle {
	return h.slots
}

func (h *BufferedLinkHandler) IncrementBackpressure() {
	h.totalBackpressure++
}

func (h *BufferedLinkHandler) CanSendPacket(cycle int, downstreamReady bool) bool {
	return downstreamReady
}
