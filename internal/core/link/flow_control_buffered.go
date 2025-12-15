package link

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
)

// BufferedFlowControl implements buffered flow control with backpressure.
// This is extracted from the current Link implementation and maintains identical behavior.
//
// Mechanism:
// - Ring buffer (slots) stores packets during transit (latency cycles)
// - Backpressure counter tracks accumulated delays when downstream is not ready
// - Bandwidth limit enforces maximum packets per slot
//
// Key Invariants:
// - Ring buffer has exactly 'latency' slots
// - Each slot can hold up to 'bandwidth' packets
// - Slot index is adjusted by totalBackpressure to handle delays
type BufferedFlowControl struct {
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

// NewBufferedFlowControl creates a BufferedFlowControl strategy.
//
// Parameters:
//   latency: number of cycles for packet delivery (must be > 0)
//   bandwidth: maximum packets per cycle (must be > 0)
func NewBufferedFlowControl(latency, bandwidth int) *BufferedFlowControl {
	if latency <= 0 {
		panic("latency must be positive for BufferedFlowControl")
	}
	if bandwidth <= 0 {
		panic("bandwidth must be positive")
	}

	slots := make([][]ahead_port.PacketWithCycle, latency)
	return &BufferedFlowControl{
		slots:             slots,
		latency:           latency,
		bandwidth:         bandwidth,
		totalBackpressure: 0,
	}
}

// CanAcceptPacket checks if a packet can be accepted into the ring buffer.
// This implements the windowing logic from Link.ProcessPackets.
func (fc *BufferedFlowControl) CanAcceptPacket(cycle int, targetCycle int) bool {
	// Check if packet fits in current window
	// The ring buffer has 'latency' slots, covering [cycle, cycle + latency - 1]
	// If targetCycle >= cycle + latency, it doesn't fit in current window
	if targetCycle-cycle >= fc.latency {
		return false // Outside window - should be kept as pending
	}

	// Calculate target slot index with backpressure adjustment
	// Handle negative modulo: ((a % b) + b) % b ensures non-negative result
	targetSlotIndex := ((targetCycle - fc.totalBackpressure) % fc.latency + fc.latency) % fc.latency

	// Check bandwidth limit for the target slot
	return len(fc.slots[targetSlotIndex]) < fc.bandwidth
}

// OnPacketAccepted is called after a packet is added to a slot.
// In the buffered strategy, slot management is handled externally,
// so this is a no-op. The packet is added via AddToSlot().
func (fc *BufferedFlowControl) OnPacketAccepted(cycle int, targetCycle int) {
	// No-op - slot management handled by AddToSlot
}

// OnPacketBlocked is called when a packet cannot be accepted.
// In the buffered strategy, backpressure is only incremented when
// downstream is not ready (in CanSendPacket), not when accepting packets.
func (fc *BufferedFlowControl) OnPacketBlocked(cycle int, targetCycle int) {
	// No-op - backpressure handled in CanSendPacket
}

// CanSendPacket checks if packets should be sent to downstream.
// Returns true only if downstream is ready.
// If false, the caller should increment backpressure via IncrementBackpressure().
func (fc *BufferedFlowControl) CanSendPacket(cycle int, downstreamReady bool) bool {
	return downstreamReady
}

// OnPacketSent is called after packets are sent.
// Slot clearing is handled externally.
func (fc *BufferedFlowControl) OnPacketSent(cycle int) {
	// No-op - slot clearing handled externally
}

// GetReadyForCycle returns the ready state for upstream.
// In the current implementation, this checks downstream readiness.
// For now, we return true (will be refined in Phase 3).
func (fc *BufferedFlowControl) GetReadyForCycle(cycle int) bool {
	// TODO: Refine this logic in Phase 3
	// Current implementation checks downstream readiness
	return true
}

// Reset resets the flow control state.
// Clears all slots and resets backpressure counter.
func (fc *BufferedFlowControl) Reset() {
	fc.totalBackpressure = 0
	for i := range fc.slots {
		fc.slots[i] = nil
	}
}

// ===== Helper Methods (used by Link.ProcessPackets) =====

// AddToSlot adds a packet to the appropriate slot.
// This is called after CanAcceptPacket returns true.
//
// Parameters:
//   pkt: packet to add
//   targetCycle: the cycle when the packet should arrive
//
// Panics if slot is full (should not happen if CanAcceptPacket is checked first).
func (fc *BufferedFlowControl) AddToSlot(pkt ahead_port.PacketWithCycle, targetCycle int) {
	targetSlotIndex := ((targetCycle - fc.totalBackpressure) % fc.latency + fc.latency) % fc.latency

	// Sanity check (should not happen if CanAcceptPacket is called first)
	if len(fc.slots[targetSlotIndex]) >= fc.bandwidth {
		panic("BufferedFlowControl.AddToSlot: slot is full (bandwidth limit exceeded)")
	}

	fc.slots[targetSlotIndex] = append(fc.slots[targetSlotIndex], pkt)
}

// GetSlot returns the packets in the slot for the given cycle.
// This is used for sending packets.
//
// Parameters:
//   cycle: the current cycle
//
// Returns:
//   packets in the slot (may be nil or empty)
func (fc *BufferedFlowControl) GetSlot(cycle int) []ahead_port.PacketWithCycle {
	slotIndex := ((cycle - fc.totalBackpressure) % fc.latency + fc.latency) % fc.latency
	return fc.slots[slotIndex]
}

// ClearSlot clears the slot for the given cycle after packets are sent.
//
// Parameters:
//   cycle: the current cycle
func (fc *BufferedFlowControl) ClearSlot(cycle int) {
	slotIndex := ((cycle - fc.totalBackpressure) % fc.latency + fc.latency) % fc.latency
	fc.slots[slotIndex] = nil
}

// IncrementBackpressure increments the backpressure counter.
// This is called when downstream is not ready.
func (fc *BufferedFlowControl) IncrementBackpressure() {
	fc.totalBackpressure++
}

// GetTotalBackpressure returns the current backpressure count.
// This is useful for debugging and monitoring.
func (fc *BufferedFlowControl) GetTotalBackpressure() int {
	return fc.totalBackpressure
}

// GetLatency returns the configured latency.
func (fc *BufferedFlowControl) GetLatency() int {
	return fc.latency
}

// GetBandwidth returns the configured bandwidth.
func (fc *BufferedFlowControl) GetBandwidth() int {
	return fc.bandwidth
}
