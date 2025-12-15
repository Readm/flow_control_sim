package link

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestNewBufferedFlowControl tests creation with valid parameters.
func TestNewBufferedFlowControl(t *testing.T) {
	fc := NewBufferedFlowControl(3, 2)

	if fc == nil {
		t.Fatal("NewBufferedFlowControl returned nil")
	}

	if fc.GetLatency() != 3 {
		t.Errorf("Expected latency=3, got %d", fc.GetLatency())
	}

	if fc.GetBandwidth() != 2 {
		t.Errorf("Expected bandwidth=2, got %d", fc.GetBandwidth())
	}

	if fc.GetTotalBackpressure() != 0 {
		t.Errorf("Expected initial backpressure=0, got %d", fc.GetTotalBackpressure())
	}

	if len(fc.slots) != 3 {
		t.Errorf("Expected 3 slots, got %d", len(fc.slots))
	}
}

// TestNewBufferedFlowControl_Panics tests panic conditions.
func TestNewBufferedFlowControl_Panics(t *testing.T) {
	tests := []struct {
		name      string
		latency   int
		bandwidth int
	}{
		{"zero latency", 0, 1},
		{"negative latency", -1, 1},
		{"zero bandwidth", 1, 0},
		{"negative bandwidth", 1, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Expected panic for %s", tt.name)
				}
			}()
			NewBufferedFlowControl(tt.latency, tt.bandwidth)
		})
	}
}

// TestBufferedFlowControl_CanAcceptPacket tests the windowing logic.
func TestBufferedFlowControl_CanAcceptPacket(t *testing.T) {
	fc := NewBufferedFlowControl(3, 2) // latency=3, bandwidth=2

	tests := []struct {
		name        string
		cycle       int
		targetCycle int
		want        bool
		reason      string
	}{
		{
			name:        "in window, slot empty",
			cycle:       0,
			targetCycle: 2,
			want:        true,
			reason:      "targetCycle (2) - cycle (0) = 2 < latency (3)",
		},
		{
			name:        "at window edge",
			cycle:       0,
			targetCycle: 2, // targetCycle - cycle = 2 < 3
			want:        true,
			reason:      "exactly at window edge, should accept",
		},
		{
			name:        "outside window",
			cycle:       0,
			targetCycle: 5, // targetCycle - cycle = 5 >= 3
			want:        false,
			reason:      "outside window, should reject",
		},
		{
			name:        "at window boundary",
			cycle:       0,
			targetCycle: 3, // targetCycle - cycle = 3 >= 3
			want:        false,
			reason:      "at boundary (>=), should reject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fc.CanAcceptPacket(tt.cycle, tt.targetCycle)
			if got != tt.want {
				t.Errorf("CanAcceptPacket(%d, %d) = %v, want %v; reason: %s",
					tt.cycle, tt.targetCycle, got, tt.want, tt.reason)
			}
		})
	}
}

// TestBufferedFlowControl_BandwidthLimit tests bandwidth enforcement.
func TestBufferedFlowControl_BandwidthLimit(t *testing.T) {
	fc := NewBufferedFlowControl(3, 2) // bandwidth=2

	// Manually fill slot 0 to capacity
	pkt := ahead_port.PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{},
	}

	// Add 2 packets (up to bandwidth)
	fc.AddToSlot(pkt, 0)
	fc.AddToSlot(pkt, 0)

	// Slot should now be full
	if fc.CanAcceptPacket(0, 0) {
		t.Error("Should not accept packet when slot is full")
	}

	// Different slot should still be available
	if !fc.CanAcceptPacket(0, 1) {
		t.Error("Different slot should be available")
	}
}

// TestBufferedFlowControl_AddToSlot tests adding packets to slots.
func TestBufferedFlowControl_AddToSlot(t *testing.T) {
	fc := NewBufferedFlowControl(3, 2)

	pkt1 := ahead_port.PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{Payload: "pkt1"},
	}
	pkt2 := ahead_port.PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{Payload: "pkt2"},
	}

	// Add packets to slot for targetCycle=0
	fc.AddToSlot(pkt1, 0)
	fc.AddToSlot(pkt2, 0)

	// Retrieve slot
	slot := fc.GetSlot(0)
	if len(slot) != 2 {
		t.Fatalf("Expected 2 packets in slot, got %d", len(slot))
	}

	if slot[0].Packet.Payload != "pkt1" {
		t.Errorf("Expected pkt1, got %v", slot[0].Packet.Payload)
	}
	if slot[1].Packet.Payload != "pkt2" {
		t.Errorf("Expected pkt2, got %v", slot[1].Packet.Payload)
	}
}

// TestBufferedFlowControl_AddToSlot_Panic tests panic when slot is full.
func TestBufferedFlowControl_AddToSlot_Panic(t *testing.T) {
	fc := NewBufferedFlowControl(3, 2)

	pkt := ahead_port.PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{},
	}

	// Fill slot
	fc.AddToSlot(pkt, 0)
	fc.AddToSlot(pkt, 0)

	// Try to add one more (should panic)
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when adding to full slot")
		}
	}()
	fc.AddToSlot(pkt, 0)
}

// TestBufferedFlowControl_GetSlotAndClear tests slot retrieval and clearing.
func TestBufferedFlowControl_GetSlotAndClear(t *testing.T) {
	fc := NewBufferedFlowControl(3, 2)

	pkt := ahead_port.PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{Payload: "test"},
	}
	fc.AddToSlot(pkt, 0)

	// Get slot
	slot := fc.GetSlot(0)
	if len(slot) != 1 {
		t.Fatalf("Expected 1 packet, got %d", len(slot))
	}

	// Clear slot
	fc.ClearSlot(0)

	// Verify cleared
	slot = fc.GetSlot(0)
	if len(slot) != 0 {
		t.Errorf("Expected empty slot after clear, got %d packets", len(slot))
	}
}

// TestBufferedFlowControl_Backpressure tests backpressure mechanism.
func TestBufferedFlowControl_Backpressure(t *testing.T) {
	fc := NewBufferedFlowControl(3, 2)

	// Initial backpressure should be 0
	if fc.GetTotalBackpressure() != 0 {
		t.Errorf("Initial backpressure should be 0, got %d", fc.GetTotalBackpressure())
	}

	// Increment backpressure
	fc.IncrementBackpressure()
	if fc.GetTotalBackpressure() != 1 {
		t.Errorf("Backpressure should be 1, got %d", fc.GetTotalBackpressure())
	}

	// Increment again
	fc.IncrementBackpressure()
	if fc.GetTotalBackpressure() != 2 {
		t.Errorf("Backpressure should be 2, got %d", fc.GetTotalBackpressure())
	}
}

// TestBufferedFlowControl_BackpressureAffectsSlotIndex tests that backpressure shifts slot index.
func TestBufferedFlowControl_BackpressureAffectsSlotIndex(t *testing.T) {
	fc := NewBufferedFlowControl(3, 2)

	pkt := ahead_port.PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{Payload: "test"},
	}

	// Add packet to targetCycle=0 with no backpressure
	// slotIndex = (0 - 0) % 3 = 0
	fc.AddToSlot(pkt, 0)

	slot0 := fc.GetSlot(0)
	if len(slot0) != 1 {
		t.Fatalf("Expected 1 packet in slot 0, got %d", len(slot0))
	}

	// Clear and add backpressure
	fc.ClearSlot(0)
	fc.IncrementBackpressure()

	// Now add packet to targetCycle=1 with backpressure=1
	// slotIndex = (1 - 1) % 3 = 0 (same slot!)
	pkt2 := ahead_port.PacketWithCycle{
		Cycle:  1,
		Packet: packet.Packet{Payload: "test2"},
	}
	fc.AddToSlot(pkt2, 1)

	// GetSlot(1) with backpressure=1 should return slot 0
	// slotIndex = (1 - 1) % 3 = 0
	slot1 := fc.GetSlot(1)
	if len(slot1) != 1 {
		t.Fatalf("Expected 1 packet in slot for cycle 1, got %d", len(slot1))
	}
	if slot1[0].Packet.Payload != "test2" {
		t.Errorf("Expected test2, got %v", slot1[0].Packet.Payload)
	}
}

// TestBufferedFlowControl_CanSendPacket tests send logic.
func TestBufferedFlowControl_CanSendPacket(t *testing.T) {
	fc := NewBufferedFlowControl(3, 2)

	// Should send only if downstream is ready
	if !fc.CanSendPacket(0, true) {
		t.Error("Should send when downstream is ready")
	}

	if fc.CanSendPacket(0, false) {
		t.Error("Should not send when downstream is not ready")
	}
}

// TestBufferedFlowControl_Reset tests reset functionality.
func TestBufferedFlowControl_Reset(t *testing.T) {
	fc := NewBufferedFlowControl(3, 2)

	// Add some state
	pkt := ahead_port.PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{},
	}
	// Add packet before incrementing backpressure
	fc.AddToSlot(pkt, 0) // slotIndex = 0

	// Increment backpressure
	fc.IncrementBackpressure()
	fc.IncrementBackpressure()

	// Verify state is set
	if fc.GetTotalBackpressure() != 2 {
		t.Fatal("Setup failed: backpressure should be 2")
	}

	// Verify slots have packets (check slot 0 directly)
	if len(fc.slots[0]) != 1 {
		t.Fatalf("Setup failed: slot 0 should have 1 packet, got %d", len(fc.slots[0]))
	}

	// Reset
	fc.Reset()

	// Verify state is cleared
	if fc.GetTotalBackpressure() != 0 {
		t.Errorf("Backpressure should be 0 after reset, got %d", fc.GetTotalBackpressure())
	}

	// Verify all slots are empty
	for i := 0; i < 3; i++ {
		if len(fc.slots[i]) != 0 {
			t.Errorf("Slot %d should be empty after reset, got %d packets", i, len(fc.slots[i]))
		}
	}
}

// TestBufferedFlowControl_RingBufferWrapAround tests slot index wrap-around.
func TestBufferedFlowControl_RingBufferWrapAround(t *testing.T) {
	fc := NewBufferedFlowControl(3, 1) // 3 slots

	pkt := ahead_port.PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{Payload: "test"},
	}

	// Add packets to targetCycles 0, 1, 2
	fc.AddToSlot(pkt, 0) // slot 0
	fc.AddToSlot(pkt, 1) // slot 1
	fc.AddToSlot(pkt, 2) // slot 2

	// Verify all slots have packets
	for i := 0; i < 3; i++ {
		slot := fc.GetSlot(i)
		if len(slot) != 1 {
			t.Errorf("Slot %d should have 1 packet, got %d", i, len(slot))
		}
	}

	// Clear slot 0
	fc.ClearSlot(0)

	// Add packet to targetCycle=3 (should wrap to slot 0)
	// slotIndex = (3 - 0) % 3 = 0
	fc.AddToSlot(pkt, 3)

	slot0 := fc.GetSlot(3)
	if len(slot0) != 1 {
		t.Errorf("Slot 0 (wrapped) should have 1 packet, got %d", len(slot0))
	}
}
