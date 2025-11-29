package link

import (
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestLinkWaitLogic tests that Link waits for Done(cycle+1-latency) instead of Done(cycle).
// This test verifies the optimization that allows Link to process packets earlier.
func TestLinkWaitLogic(t *testing.T) {
	t.Parallel()

	upstream := newTestAheadPort(8)
	downstream := newTestAheadPort(8)

	link := NewLink(0, 1, upstream, downstream, 3, 1)

	sendPacket(t, upstream, 0, packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"})
	upstream.SetDone(2)

	start := time.Now()
	if err := link.Tick(2); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Errorf("Link.Tick(2) should not block when upstream done >= -1")
	}

	sendPacket(t, upstream, 1, packet.Packet{SourceID: 0, TargetID: 1, Payload: "wait"})
	upstream.SetDone(0)

	done := make(chan struct{})
	go func() {
		link.Tick(5)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Link.Tick(5) returned before upstream Done satisfied")
	case <-time.After(100 * time.Millisecond):
	}

	upstream.SetDone(5)
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Link.Tick(5) did not finish after upstream Done updated")
	}
}

// TestLinkWaitLogicBoundary tests boundary cases for the wait logic.
func TestLinkWaitLogicBoundary(t *testing.T) {
	t.Parallel()

	upstream := newTestAheadPort(8)
	downstream := newTestAheadPort(8)

	link := NewLink(0, 1, upstream, downstream, 5, 1)

	upstream.SetDone(1)
	done := make(chan struct{})
	go func() {
		link.Tick(2)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Link.Tick(2) should complete when wait cycle is negative")
	}
}

// TestLinkWaitLogicEarlyProcessing tests that Link can process packets earlier with latency buffer.
func TestLinkWaitLogicEarlyProcessing(t *testing.T) {
	t.Parallel()

	upstream := newTestAheadPort(8)
	downstream := newTestAheadPort(8)

	link := NewLink(0, 1, upstream, downstream, 4, 1)

	sendPacket(t, upstream, 0, packet.Packet{SourceID: 0, TargetID: 1, Payload: "early"})
	upstream.SetDone(2)

	if err := link.Tick(2); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}
	ensureNoAdditionalPackets(t, downstream)

	upstream.SetDone(5)
	if err := link.Tick(4); err != nil {
		t.Fatalf("link.Tick failed at cycle 4: %v", err)
	}

	received := receivePackets(t, downstream, 1)
	if received[0].Packet.Payload != "early" {
		t.Fatalf("expected payload 'early', got %q", received[0].Packet.Payload)
	}
}
