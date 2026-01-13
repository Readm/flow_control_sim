package queue

import (
	"context"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestNewInputQueue tests InputQueue creation.
func TestNewInputQueue(t *testing.T) {
	t.Parallel()

	iq := NewInputQueue(10, 2)
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

	iq := NewInputQueue(10, 2)

	// Create upstream mock and connect
	upstream := newMockUpstream()
	ahead_port.Connect(upstream, iq)

	// Declare InputQueue ready for cycle 1 (downstream signals this)
	iq.fromUpstream.UpdateReady(1, true)

	// Send packet from upstream for cycle 1
	pkt := packet.Packet{SourceID: 1, TargetID: 2, Metadata: map[string]interface{}{"payload": "test"}}

	// Run upstream operations in goroutine
	go func() {
		if !upstream.SendPacket(1, pkt) {
			t.Error("Failed to send packet")
		}
		upstream.MarkDone(1)
	}()

	// Process cycle 1 (will wait for upstream done on cycle 0, which is satisfied)
	done := make(chan error, 1)
	go func() {
		// First tick cycle 0 (no packets expected)
		_ = iq.Tick(0)
		// Then tick cycle 1 (receives packet)
		done <- iq.Tick(1)
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

	iq := NewInputQueue(10, 2)

	// Manually inject packet into storage for testing Pick logic directly
	// Note: In real usage, Tick populates this.
	iq.arrayMu.Lock()
	iq.slots[0] = packet.PacketWithCycle{Cycle: 3, Packet: packet.Packet{SourceID: 1}}
	iq.freeBitmap[0] = false
	iq.blockReasons[0] = 0
	iq.length++ // 更新计数器
	iq.slots[1] = packet.PacketWithCycle{Cycle: 5, Packet: packet.Packet{SourceID: 2}}
	iq.freeBitmap[1] = false
	iq.blockReasons[1] = 0
	iq.length++ // 更新计数器
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
