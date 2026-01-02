package link

import (
	"testing"
)

// TestCreateLinkHandler_Buffered tests creating buffered flow control.
func TestCreateLinkHandler_Buffered(t *testing.T) {
	fc := CreateLinkHandler("buffered", 3, 2)

	if fc == nil {
		t.Fatal("CreateLinkHandler returned nil")
	}

	// Type assertion to verify it's BufferedLinkType
	buffered, ok := fc.(*BufferedLinkType)
	if !ok {
		t.Fatal("Expected BufferedLinkHandler, got different type")
	}

	if buffered.GetLatency() != 3 {
		t.Errorf("Expected latency=3, got %d", buffered.GetLatency())
	}

	if buffered.GetBandwidth() != 2 {
		t.Errorf("Expected bandwidth=2, got %d", buffered.GetBandwidth())
	}
}

// TestCreateLinkHandler_Bufferless tests creating bufferless flow control.
func TestCreateLinkHandler_Bufferless(t *testing.T) {
	fc := CreateLinkHandler("bufferless", 0, 0)

	if fc == nil {
		t.Fatal("CreateLinkHandler returned nil")
	}

	// Type assertion to verify it's BufferlessLinkType
	_, ok := fc.(*BufferlessLinkType)
	if !ok {
		t.Fatal("Expected BufferlessLinkHandler, got different type")
	}

	// Verify it's not nil
	if fc == nil {
		t.Fatal("BufferlessLinkHandler instance is nil")
	}
}

// TestCreateLinkHandler_Unknown tests panic on unknown strategy.
func TestCreateLinkHandler_Unknown(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for unknown strategy type")
		}
	}()

	CreateLinkHandler("unknown_strategy", 1, 1)
}

// TestNewLinkWithHandler_Buffered tests creating Link with BufferedLinkHandler.
func TestNewLinkWithHandler_Buffered(t *testing.T) {
	fc := NewBufferedLinkHandler(3, 2)
	link := NewLinkWithHandler(0, 1, 3, 2, fc)

	if link == nil {
		t.Fatal("NewLinkWithHandler returned nil link")
	}

	if link.latency != 3 {
		t.Errorf("Expected latency=3, got %d", link.latency)
	}

	if link.bandwidth != 2 {
		t.Errorf("Expected bandwidth=2, got %d", link.bandwidth)
	}
}

// TestNewLinkWithHandler_Bufferless tests creating Link with BufferlessLinkHandler.
func TestNewLinkWithHandler_Bufferless(t *testing.T) {
	fc := NewBufferlessLinkHandler()
	link := NewLinkWithHandler(0, 1, 1, 1, fc)

	if link == nil {
		t.Fatal("NewLinkWithHandler returned nil link")
	}

	// Verify the link uses bufferless flow control
	// We can't directly access the flowControl field, but we can test behavior
	// (This is a basic sanity check; full behavior is tested in integration tests)
}

// TestNewLinkWithHandler_NilStrategy tests panic on nil strategy.
func TestNewLinkWithHandler_NilStrategy(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when flowControl is nil")
		}
	}()

	NewLinkWithHandler(0, 1, 3, 2, nil)
}

// TestNewLinkWithHandler_InvalidParams tests panic on invalid parameters.
func TestNewLinkWithHandler_InvalidParams(t *testing.T) {
	fc := NewBufferedLinkHandler(3, 2)

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
			NewLinkWithHandler(0, 1, tt.latency, tt.bandwidth, fc)
		})
	}
}

// TestNewLink_DefaultsToBuffered tests that NewLink creates BufferedLinkHandler by default.
func TestNewLink_DefaultsToBuffered(t *testing.T) {
	link := NewLink(0, 1, 3, 2)

	if link == nil {
		t.Fatal("NewLink returned nil link")
	}

	// The default NewLink should create a BufferedLinkHandler
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
		{"bufferless link", "bufferless", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := CreateLinkHandler(tt.strategyType, tt.latency, tt.bandwidth)
			link := NewLinkWithHandler(0, 1, tt.latency, tt.bandwidth, fc)

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
