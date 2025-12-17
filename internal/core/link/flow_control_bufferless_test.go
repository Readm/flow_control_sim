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

// TestBufferlessFlowControl_IsReady tests ready signal.
func TestBufferlessFlowControl_IsReady(t *testing.T) {
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
			got := fc.IsReady(tt.cycle)
			if !got {
				t.Errorf("IsReady(%d) = false, want true (bufferless should always be ready)",
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

	if !fc.IsReady(0) {
		t.Error("IsReady should still return true after reset")
	}
}

// TestBufferlessFlowControl_Stateless tests that the strategy truly has no state.
func TestBufferlessFlowControl_Stateless(t *testing.T) {
	fc := NewBufferlessFlowControl()

	// Call Reset and verify queries still return the same results
	fc.Reset()

	// All queries should still return the same results
	if !fc.CanAcceptPacket(0, 0) {
		t.Error("CanAcceptPacket should always return true")
	}

	if !fc.CanAcceptPacket(100, 200) {
		t.Error("CanAcceptPacket should always return true regardless of cycles")
	}

	if !fc.IsReady(50) {
		t.Error("IsReady should always return true")
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
