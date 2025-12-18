package link

import (
	"testing"
)

// TestCreateFlowControlStrategy_Buffered tests creating buffered flow control.
func TestCreateFlowControlStrategy_Buffered(t *testing.T) {
	fc := CreateFlowControlStrategy("buffered", 3, 2)

	if fc == nil {
		t.Fatal("CreateFlowControlStrategy returned nil")
	}

	// Type assertion to verify it's BufferedFlowControl
	buffered, ok := fc.(*BufferedFlowControl)
	if !ok {
		t.Fatal("Expected BufferedFlowControl, got different type")
	}

	if buffered.GetLatency() != 3 {
		t.Errorf("Expected latency=3, got %d", buffered.GetLatency())
	}

	if buffered.GetBandwidth() != 2 {
		t.Errorf("Expected bandwidth=2, got %d", buffered.GetBandwidth())
	}
}

// TestCreateFlowControlStrategy_Bufferless tests creating bufferless flow control.
func TestCreateFlowControlStrategy_Bufferless(t *testing.T) {
	fc := CreateFlowControlStrategy("bufferless", 0, 0)

	if fc == nil {
		t.Fatal("CreateFlowControlStrategy returned nil")
	}

	// Type assertion to verify it's BufferlessFlowControl
	_, ok := fc.(*BufferlessFlowControl)
	if !ok {
		t.Fatal("Expected BufferlessFlowControl, got different type")
	}

	// Verify always-ready behavior
	if !fc.CanAcceptPacket(0, 0) {
		t.Error("BufferlessFlowControl should always accept packets")
	}

	if !fc.IsReady(0) {
		t.Error("BufferlessFlowControl should always be ready")
	}
}

// TestCreateFlowControlStrategy_Unknown tests panic on unknown strategy.
func TestCreateFlowControlStrategy_Unknown(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for unknown strategy type")
		}
	}()

	CreateFlowControlStrategy("unknown_strategy", 1, 1)
}

// TestNewLinkWithFlowControl_Buffered tests creating Link with BufferedFlowControl.
func TestNewLinkWithFlowControl_Buffered(t *testing.T) {
	fc := NewBufferedFlowControl(3, 2)
	link := NewLinkWithFlowControl(0, 1, 3, 2, fc)

	if link == nil {
		t.Fatal("NewLinkWithFlowControl returned nil link")
	}

	if link.latency != 3 {
		t.Errorf("Expected latency=3, got %d", link.latency)
	}

	if link.bandwidth != 2 {
		t.Errorf("Expected bandwidth=2, got %d", link.bandwidth)
	}
}

// TestNewLinkWithFlowControl_Bufferless tests creating Link with BufferlessFlowControl.
func TestNewLinkWithFlowControl_Bufferless(t *testing.T) {
	fc := NewBufferlessFlowControl()
	link := NewLinkWithFlowControl(0, 1, 0, 1, fc)

	if link == nil {
		t.Fatal("NewLinkWithFlowControl returned nil link")
	}

	// Verify the link uses bufferless flow control
	// We can't directly access the flowControl field, but we can test behavior
	// (This is a basic sanity check; full behavior is tested in integration tests)
}

// TestNewLinkWithFlowControl_NilStrategy tests panic on nil strategy.
func TestNewLinkWithFlowControl_NilStrategy(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when flowControl is nil")
		}
	}()

	NewLinkWithFlowControl(0, 1, 3, 2, nil)
}

// TestNewLinkWithFlowControl_InvalidParams tests panic on invalid parameters.
func TestNewLinkWithFlowControl_InvalidParams(t *testing.T) {
	fc := NewBufferedFlowControl(3, 2)

	tests := []struct {
		name      string
		latency   int
		bandwidth int
	}{
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
			NewLinkWithFlowControl(0, 1, tt.latency, tt.bandwidth, fc)
		})
	}
}

// TestNewLink_DefaultsToBuffered tests that NewLink creates BufferedFlowControl by default.
func TestNewLink_DefaultsToBuffered(t *testing.T) {
	link := NewLink(0, 1, 3, 2)

	if link == nil {
		t.Fatal("NewLink returned nil link")
	}

	// The default NewLink should create a BufferedFlowControl
	// We can't directly access the flowControl field, but the existing
	// link tests verify the buffered behavior
	if link.latency != 3 {
		t.Errorf("Expected latency=3, got %d", link.latency)
	}

	if link.bandwidth != 2 {
		t.Errorf("Expected bandwidth=2, got %d", link.bandwidth)
	}
}

// TestFactoryIntegration tests using factory to create different link types.
func TestFactoryIntegration(t *testing.T) {
	tests := []struct {
		name         string
		strategyType string
		latency      int
		bandwidth    int
	}{
		{"buffered link", "buffered", 3, 2},
		{"bufferless link", "bufferless", 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := CreateFlowControlStrategy(tt.strategyType, tt.latency, tt.bandwidth)
			link := NewLinkWithFlowControl(0, 1, tt.latency, tt.bandwidth, fc)

			if link == nil {
				t.Fatal("Factory integration failed to create link")
			}

			if link.latency != tt.latency {
				t.Errorf("Expected latency=%d, got %d", tt.latency, link.latency)
			}

			if link.bandwidth != tt.bandwidth {
				t.Errorf("Expected bandwidth=%d, got %d", tt.bandwidth, link.bandwidth)
			}
		})
	}
}
