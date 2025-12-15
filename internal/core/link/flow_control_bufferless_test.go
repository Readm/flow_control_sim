package link

import (
	"testing"
)

// TestNewBufferlessFlowControl tests creation of bufferless flow control.
func TestNewBufferlessFlowControl(t *testing.T) {
	fc := NewBufferlessFlowControl()

	if fc == nil {
		t.Fatal("NewBufferlessFlowControl returned nil")
	}
}

// TestBufferlessFlowControl_CanAcceptPacket tests that packets are always accepted.
func TestBufferlessFlowControl_CanAcceptPacket(t *testing.T) {
	fc := NewBufferlessFlowControl()

	tests := []struct {
		name        string
		cycle       int
		targetCycle int
	}{
		{"same cycle", 0, 0},
		{"future cycle", 0, 10},
		{"far future cycle", 0, 1000},
		{"negative cycle", -5, 5},
		{"large cycle numbers", 1000000, 1000100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fc.CanAcceptPacket(tt.cycle, tt.targetCycle)
			if !got {
				t.Errorf("CanAcceptPacket(%d, %d) = false, want true (bufferless should always accept)",
					tt.cycle, tt.targetCycle)
			}
		})
	}
}

// TestBufferlessFlowControl_CanSendPacket tests send logic.
func TestBufferlessFlowControl_CanSendPacket(t *testing.T) {
	fc := NewBufferlessFlowControl()

	tests := []struct {
		name            string
		cycle           int
		downstreamReady bool
		want            bool
	}{
		{"downstream ready", 0, true, true},
		{"downstream not ready", 0, false, false},
		{"downstream ready at cycle 100", 100, true, true},
		{"downstream not ready at cycle 100", 100, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fc.CanSendPacket(tt.cycle, tt.downstreamReady)
			if got != tt.want {
				t.Errorf("CanSendPacket(%d, %v) = %v, want %v",
					tt.cycle, tt.downstreamReady, got, tt.want)
			}
		})
	}
}

// TestBufferlessFlowControl_GetReadyForCycle tests ready signal.
func TestBufferlessFlowControl_GetReadyForCycle(t *testing.T) {
	fc := NewBufferlessFlowControl()

	tests := []struct {
		name  string
		cycle int
	}{
		{"cycle 0", 0},
		{"cycle 10", 10},
		{"cycle 1000", 1000},
		{"negative cycle", -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fc.GetReadyForCycle(tt.cycle)
			if !got {
				t.Errorf("GetReadyForCycle(%d) = false, want true (bufferless should always be ready)",
					tt.cycle)
			}
		})
	}
}

// TestBufferlessFlowControl_Reset tests reset functionality.
func TestBufferlessFlowControl_Reset(t *testing.T) {
	fc := NewBufferlessFlowControl()

	// Call Reset (should be no-op, but shouldn't panic)
	fc.Reset()

	// Verify behavior is unchanged
	if !fc.CanAcceptPacket(0, 0) {
		t.Error("CanAcceptPacket should still return true after reset")
	}

	if !fc.GetReadyForCycle(0) {
		t.Error("GetReadyForCycle should still return true after reset")
	}
}

// TestBufferlessFlowControl_OnPacketAccepted tests packet acceptance callback.
func TestBufferlessFlowControl_OnPacketAccepted(t *testing.T) {
	fc := NewBufferlessFlowControl()

	// Call OnPacketAccepted (should be no-op, but shouldn't panic)
	fc.OnPacketAccepted(0, 0)
	fc.OnPacketAccepted(10, 15)

	// Verify behavior is unchanged
	if !fc.CanAcceptPacket(0, 0) {
		t.Error("CanAcceptPacket should still return true after OnPacketAccepted")
	}
}

// TestBufferlessFlowControl_OnPacketBlocked tests packet blocking callback.
func TestBufferlessFlowControl_OnPacketBlocked(t *testing.T) {
	fc := NewBufferlessFlowControl()

	// Call OnPacketBlocked (should be no-op, but shouldn't panic)
	// Note: This should never happen in practice since CanAcceptPacket always returns true
	fc.OnPacketBlocked(0, 0)

	// Verify behavior is unchanged
	if !fc.CanAcceptPacket(0, 0) {
		t.Error("CanAcceptPacket should still return true after OnPacketBlocked")
	}
}

// TestBufferlessFlowControl_OnPacketSent tests packet sent callback.
func TestBufferlessFlowControl_OnPacketSent(t *testing.T) {
	fc := NewBufferlessFlowControl()

	// Call OnPacketSent (should be no-op, but shouldn't panic)
	fc.OnPacketSent(0)
	fc.OnPacketSent(10)

	// Verify behavior is unchanged
	if !fc.CanAcceptPacket(0, 0) {
		t.Error("CanAcceptPacket should still return true after OnPacketSent")
	}
}

// TestBufferlessFlowControl_Stateless tests that the strategy truly has no state.
func TestBufferlessFlowControl_Stateless(t *testing.T) {
	fc := NewBufferlessFlowControl()

	// Call various methods in random order
	fc.OnPacketAccepted(0, 5)
	fc.OnPacketSent(5)
	fc.OnPacketBlocked(10, 15)
	fc.Reset()
	fc.OnPacketAccepted(20, 25)

	// All queries should still return the same results
	if !fc.CanAcceptPacket(0, 0) {
		t.Error("CanAcceptPacket should always return true")
	}

	if !fc.CanAcceptPacket(100, 200) {
		t.Error("CanAcceptPacket should always return true regardless of cycles")
	}

	if !fc.GetReadyForCycle(50) {
		t.Error("GetReadyForCycle should always return true")
	}

	if !fc.CanSendPacket(10, true) {
		t.Error("CanSendPacket should return true when downstream is ready")
	}

	if fc.CanSendPacket(10, false) {
		t.Error("CanSendPacket should return false when downstream is not ready")
	}
}

// TestBufferlessFlowControl_InterfaceCompliance tests that BufferlessFlowControl
// implements the FlowControlStrategy interface.
func TestBufferlessFlowControl_InterfaceCompliance(t *testing.T) {
	var _ FlowControlStrategy = (*BufferlessFlowControl)(nil)
}
