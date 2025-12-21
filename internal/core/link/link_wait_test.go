package link

import (
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestLinkWaitLogic tests that Link waits for upstream Done correctly.
// This test verifies the wait logic with latency.
func TestLinkWaitLogic(t *testing.T) {
	t.Parallel()

	// Create Link (latency=3, bandwidth=1)
	link := NewLink(0, 1, 3, 1)

	// Create mock components
	upstream := newMockUpstream()
	downstream := newMockDownstream()

	// Connect
	ahead_port.Connect(upstream, link)
	ahead_port.Connect(link, downstream)

	// Link needs to be ready to accept packet at cycle 0
	link.fromUpstream.UpdateReady(0, true)
	link.fromUpstream.UpdateReady(1, true)
	// Declare downstream ready
	downstream.UpdateReady(2, true)
	downstream.UpdateReady(5, true)

	// Send packet at cycle 0
	if !upstream.SendPacket(0, packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}) {
		t.Fatal("Failed to send packet")
	}
	upstream.MarkDone(0)

	// Tick at cycle 2 should not block (waitCycle = 2-3 = -1, no wait needed)
	start := time.Now()
	if err := link.Tick(2, 2); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Errorf("Link.Tick(2) should not block when waitCycle < 0")
	}

	// Send packet at cycle 1 but don't mark done yet
	downstream.UpdateReady(5, true)
	if !upstream.SendPacket(1, packet.Packet{SourceID: 0, TargetID: 1, Payload: "wait"}) {
		t.Fatal("Failed to send packet")
	}

	// Tick at cycle 5 should wait for upstream Done(2) since waitCycle = 5-3 = 2
	done := make(chan error, 1)
	go func() {
		done <- link.Tick(5, 5)
	}()

	// Should block initially
	select {
	case <-done:
		t.Fatal("Link.Tick(5) returned before upstream Done(2) satisfied")
	case <-time.After(100 * time.Millisecond):
		// Expected to block
	}

	// Mark upstream done for cycle 2
	upstream.MarkDone(2)

	// Should now complete
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Link.Tick(5) failed: %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Link.Tick(5) did not finish after upstream Done(2) updated")
	}
}

// TestLinkWaitLogicBoundary tests boundary cases for the wait logic.
func TestLinkWaitLogicBoundary(t *testing.T) {
	t.Parallel()

	// Create Link (latency=5, bandwidth=1)
	link := NewLink(0, 1, 5, 1)

	// Create mock components
	upstream := newMockUpstream()
	downstream := newMockDownstream()

	// Connect
	ahead_port.Connect(upstream, link)
	ahead_port.Connect(link, downstream)

	// Link needs to be ready (though no packets will be sent)
	link.fromUpstream.UpdateReady(0, true)
	// Declare downstream ready
	downstream.UpdateReady(2, true)

	// Mark upstream done for cycle 0
	upstream.MarkDone(0)

	// Tick at cycle 2 should not block (waitCycle = 2-5 = -3, negative)
	done := make(chan error, 1)
	go func() {
		done <- link.Tick(2, 2)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Link.Tick(2) failed: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Link.Tick(2) should complete when wait cycle is negative")
	}
}

// TestLinkWaitLogicEarlyProcessing tests that Link processes packets with correct timing.
func TestLinkWaitLogicEarlyProcessing(t *testing.T) {
	t.Parallel()

	// Create Link (latency=4, bandwidth=1)
	link := NewLink(0, 1, 4, 1)

	// Create mock components
	upstream := newMockUpstream()
	downstream := newMockDownstream()

	// Connect
	ahead_port.Connect(upstream, link)
	ahead_port.Connect(link, downstream)

	// Link needs to be ready to accept packet at cycle 0
	link.fromUpstream.UpdateReady(0, true)
	// Declare downstream ready for cycle 4 (packet sent at cycle 0 with latency 4 arrives at cycle 4)
	downstream.UpdateReady(4, true)

	// Send packet at cycle 0
	if !upstream.SendPacket(0, packet.Packet{SourceID: 0, TargetID: 1, Payload: "early"}) {
		t.Fatal("Failed to send packet")
	}
	upstream.MarkDone(0)

	// Tick at cycle 4 (waitCycle = 4-4 = 0, waits for upstream Done(0))
	if err := link.Tick(4, 4); err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Wait for downstream and receive
	downstream.WaitDone(4)
	received := downstream.ReceivePackets(4)

	if len(received) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(received))
	}
	if received[0].Payload != "early" {
		t.Fatalf("expected payload 'early', got %q", received[0].Payload)
	}
}
