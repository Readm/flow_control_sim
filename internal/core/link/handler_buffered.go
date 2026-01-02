package link

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// BufferedLinkType implements buffered flow control with backpressure.
//
// Mechanism:
// - Ring buffer (slots) stores packets during transit (latency cycles)
// - Backpressure counter tracks accumulated delays when downstream is not ready
// - Bandwidth limit enforces maximum packets per slot
type BufferedLinkType struct {
	// Ring buffer slots - one slot per latency cycle
	// Each slot stores packets destined for that cycle
	slots [][]ahead_port.PacketWithCycle

	// Backpressure counter - increments when downstream is not ready
	// This shifts the slot index to delay packet transmission
	totalBackpressure int

	// Configuration
	latency   int // Number of slots in ring buffer
	bandwidth int // Maximum packets per slot

	// pendingPackets stores packets that couldn't fit in the window or were delayed.
	// Previously in Link, now owned by Handler.
	pendingPackets []ahead_port.PacketWithCycle
}

// NewBufferedLinkType creates a BufferedLinkType.
func NewBufferedLinkType(latency, bandwidth int) *BufferedLinkType {
	if latency <= 0 {
		panic("latency must be positive for BufferedLinkType")
	}
	if bandwidth <= 0 {
		panic("bandwidth must be positive")
	}

	slots := make([][]ahead_port.PacketWithCycle, latency)
	return &BufferedLinkType{
		slots:             slots,
		latency:           latency,
		bandwidth:         bandwidth,
		totalBackpressure: 0,
		pendingPackets:    make([]ahead_port.PacketWithCycle, 0),
	}
}

// Process implements the LinkHandler interface for BufferedLinkType.
func (h *BufferedLinkType) Process(l *Link, cycle int, targetCycle int, incoming []packet.Packet) error {
	// 1. Process pending packets (those that couldn't fit in the window or were delayed)
	// These are stored in h.pendingPackets
	currentPending := h.pendingPackets
	h.pendingPackets = make([]ahead_port.PacketWithCycle, 0)

	for _, pkt := range currentPending {
		targetCycle := pkt.Cycle
		if targetCycle < cycle {
			targetCycle = cycle
			pkt.Cycle = cycle
		}

		if h.CanAcceptPacket(cycle, targetCycle) {
			h.AddToSlot(pkt, targetCycle)
		} else {
			h.pendingPackets = append(h.pendingPackets, pkt)
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
			h.pendingPackets = append(h.pendingPackets, ahead_port.PacketWithCycle{
				Cycle:  targetCycle,
				Packet: pkt,
			})
		}
	}

	// 3. Try to send packets from current slot to downstream
	// Check downstream readiness first (this provides synchronization)
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
func (h *BufferedLinkType) Reset() {
	h.totalBackpressure = 0
	for i := range h.slots {
		h.slots[i] = nil
	}
}

// ReadyDepth returns the number of cycles to pre-mark as ready for bootstrapping.
// For buffered links, we need to fill the pipeline (latency) plus one for cycle 0.
func (h *BufferedLinkType) Init(l *Link) {
	depth := h.latency + 1
	for i := 0; i < depth; i++ {
		l.UpdateUpstreamReady(i, true)
	}
}

func (h *BufferedLinkType) CanAcceptPacket(cycle int, targetCycle int) bool {
	if targetCycle-cycle >= h.latency {
		return false
	}
	targetSlotIndex := ((targetCycle-h.totalBackpressure)%h.latency + h.latency) % h.latency
	return len(h.slots[targetSlotIndex]) < h.bandwidth
}

func (h *BufferedLinkType) CheckSpace(cycle int) bool {
	targetSlotIndex := ((cycle-h.totalBackpressure)%h.latency + h.latency) % h.latency
	return len(h.slots[targetSlotIndex]) < h.bandwidth
}

func (h *BufferedLinkType) AddToSlot(pkt ahead_port.PacketWithCycle, targetCycle int) {
	targetSlotIndex := ((targetCycle-h.totalBackpressure)%h.latency + h.latency) % h.latency

	// Sanity check
	if len(h.slots[targetSlotIndex]) >= h.bandwidth {
		panic("BufferedLinkType.AddToSlot: slot is full (bandwidth limit exceeded)")
	}

	h.slots[targetSlotIndex] = append(h.slots[targetSlotIndex], pkt)
}

func (h *BufferedLinkType) GetSlot(cycle int) []ahead_port.PacketWithCycle {
	slotIndex := ((cycle-h.totalBackpressure)%h.latency + h.latency) % h.latency
	return h.slots[slotIndex]
}

func (h *BufferedLinkType) ClearSlot(cycle int) {
	slotIndex := ((cycle-h.totalBackpressure)%h.latency + h.latency) % h.latency
	h.slots[slotIndex] = nil
}

func (h *BufferedLinkType) UpdateSlot(cycle int, packets []ahead_port.PacketWithCycle) {
	slotIndex := ((cycle-h.totalBackpressure)%h.latency + h.latency) % h.latency
	h.slots[slotIndex] = packets
}

func (h *BufferedLinkType) GetLatency() int {
	return h.latency
}

func (h *BufferedLinkType) GetBandwidth() int {
	return h.bandwidth
}

func (h *BufferedLinkType) GetTotalBackpressure() int {
	return h.totalBackpressure
}

func (h *BufferedLinkType) GetSlots() [][]ahead_port.PacketWithCycle {
	return h.slots
}

func (h *BufferedLinkType) IncrementBackpressure() {
	h.totalBackpressure++
}

func (h *BufferedLinkType) CanSendPacket(cycle int, downstreamReady bool) bool {
	return downstreamReady
}

// GetOccupancy returns the pending packet count per slot for buffered links.
func (h *BufferedLinkType) GetOccupancy(currentCycle int) []int {
	slots := h.GetSlots()
	occupancy := make([]int, len(slots))
	for i, slot := range slots {
		occupancy[i] = len(slot)
	}
	return occupancy
}

// NewBufferedLinkHandler is deprecated. Use NewBufferedLinkType instead.
// Deprecated: Use NewBufferedLinkType.
func NewBufferedLinkHandler(latency, bandwidth int) *BufferedLinkType {
	return NewBufferedLinkType(latency, bandwidth)
}
