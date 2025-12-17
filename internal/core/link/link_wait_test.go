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

	link, linkIn, linkOut := NewLink(0, 1, 3, 1) // latency=3, bandwidth=1
	downstreamInPort, upstreamOutPort := createTestPorts(8)
	linkIn.Plug(upstreamOutPort)
	linkOut.Plug(downstreamInPort)

	// Type assert to access SetDone for testing
	mockOut := upstreamOutPort.(*mockOutPort)

	sendPacketToOutPort(t, upstreamOutPort, 0, packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"})
	mockOut.SetDone(2)

	start := time.Now()
	if err := link.Tick(2); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Errorf("Link.Tick(2) should not block when upstream done >= -1")
	}

	sendPacketToOutPort(t, upstreamOutPort, 1, packet.Packet{SourceID: 0, TargetID: 1, Payload: "wait"})
	mockOut.SetDone(0)

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

	mockOut.SetDone(5)
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Link.Tick(5) did not finish after upstream Done updated")
	}
}

// TestLinkWaitLogicBoundary tests boundary cases for the wait logic.
func TestLinkWaitLogicBoundary(t *testing.T) {
	t.Parallel()

	link, linkIn, linkOut := NewLink(0, 1, 5, 1) // latency=5, bandwidth=1
	downstreamInPort, upstreamOutPort := createTestPorts(8)
	linkIn.Plug(upstreamOutPort)
	linkOut.Plug(downstreamInPort)

	// Type assert to access SetDone for testing
	mockOut := upstreamOutPort.(*mockOutPort)

	mockOut.SetDone(1)
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

	link, linkIn, linkOut := NewLink(0, 1, 4, 1) // latency=4, bandwidth=1
	downstreamInPort, upstreamOutPort := createTestPorts(8)
	linkIn.Plug(upstreamOutPort)
	linkOut.Plug(downstreamInPort)

	// Type assert to access SetDone for testing
	mockOut := upstreamOutPort.(*mockOutPort)

	sendPacketToOutPort(t, upstreamOutPort, 0, packet.Packet{SourceID: 0, TargetID: 1, Payload: "early"})
	mockOut.SetDone(2)

	if err := link.Tick(2); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}

	// New design: packet is sent immediately with targetCycle=6 (2+4)
	// So we can receive it now (it's in the channel, labeled as cycle 6)
	received := receivePacketsFromInPort(t, downstreamInPort, 1)
	if received[0].Packet.Payload != "early" {
		t.Fatalf("expected payload 'early', got %q", received[0].Packet.Payload)
	}
	if received[0].Cycle != 6 {
		t.Fatalf("expected cycle 6, got %d", received[0].Cycle)
	}

	ensureNoAdditionalPacketsInPort(t, downstreamInPort)
}
