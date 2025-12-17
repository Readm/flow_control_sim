package queue

import (
	"context"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestNewInputQueue tests InputQueue creation.
func TestNewInputQueue(t *testing.T) {
	t.Parallel()

	iq := NewInputQueue(10, 2, 3)
	if iq == nil {
		t.Fatal("NewInputQueue returned nil")
	}

	if iq.Capacity() != 10 {
		t.Fatalf("expected capacity 10, got %d", iq.Capacity())
	}

	if iq.Length() != 0 {
		t.Fatalf("expected initial length 0, got %d", iq.Length())
	}
}

// TestInputQueueReceive tests receiving packets via Tick.
func TestInputQueueReceive(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	iq := NewInputQueue(10, 2, 2)

	// Create upstream mock
	_, upstreamOutPort := createTestPorts(8)
	// InputQueue's InPort connects to Upstream's OutPort
	iq.QueueInPort().Plug(upstreamOutPort)

	mockOut := upstreamOutPort.(*mockOutPort)

	// Send packet from upstream
	pkt := packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"}
	select {
	case <-ctx.Done():
		t.Fatal("timeout before sending packet")
	default:
		sendPacketToOutPort(t, upstreamOutPort, 0, pkt)
		mockOut.SetDone(0)
	}

	// Process cycle 0
	done := make(chan error, 1)
	go func() {
		done <- iq.Tick(0)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Tick failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Tick timed out")
	}

	// Verify packet stored
	if iq.Length() != 1 {
		t.Fatalf("expected length 1, got %d", iq.Length())
	}

	received := iq.GetReceivedPackets()
	if len(received) != 1 {
		t.Fatalf("expected 1 received packet, got %d", len(received))
	}
}

// TestInputQueuePick tests picking packets from storage.
func TestInputQueuePick(t *testing.T) {
	t.Parallel()

	iq := NewInputQueue(10, 2, 2)

	// Manually inject packet into storage for testing Pick logic directly
	// Note: In real usage, Tick populates this.
	iq.arrayMu.Lock()
	iq.slots[0] = PacketWithCycle{Cycle: 3, Packet: packet.Packet{SourceID: 1}}
	iq.freeBitmap[0] = false
	iq.blockReasons[0] = 0
	iq.slots[1] = PacketWithCycle{Cycle: 5, Packet: packet.Packet{SourceID: 2}}
	iq.freeBitmap[1] = false
	iq.blockReasons[1] = 0
	iq.arrayMu.Unlock()

	picked := iq.Pick()
	if len(picked) != 2 {
		t.Fatalf("expected 2 picked packets, got %d", len(picked))
	}

	// Check sorting (Cycle 3 before 5)
	if picked[0].SourceID != 1 {
		t.Errorf("expected first packet SourceID 1 (Cycle 3), got %d", picked[0].SourceID)
	}

	if iq.Length() != 0 {
		t.Errorf("expected queue to be empty after pick, got %d", iq.Length())
	}
}

// Helper to simulate upstream behavior
// createTestPorts is in queue_test_helpers_test.go
