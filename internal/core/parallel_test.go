package core

import (
	"context"
	"sync"
	"testing"

	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/dataflow/flow"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestIndependentFlowParallelAdvance tests that independent flows can advance to different cycles in parallel.
func TestIndependentFlowParallelAdvance(t *testing.T) {
	t.Parallel()

	// Create independent flows with different links
	f1 := flow.NewFIFO(1, 8, 0, 0, nil, 0)
	f2 := flow.NewFIFO(2, 8, 0, 0, nil, 0)
	f3 := flow.NewFIFO(3, 8, 0, 0, nil, 0)

	link1 := link.NewLink(0, f1, nil, 0, 1, 1, 0)
	link2 := link.NewLink(0, f2, nil, 0, 2, 1, 0)
	link3 := link.NewLink(0, f3, nil, 0, 3, 1, 0)

	// Set noBackpressureUntil for all links
	link1.SetNoBackpressureUntil(20)
	link2.SetNoBackpressureUntil(20)
	link3.SetNoBackpressureUntil(20)

	// Send packets
	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 2, Payload: "test2"}
	pkt3 := packet.Packet{SourceID: 0, TargetID: 3, Payload: "test3"}

	link1.Transmit(0, pkt1)
	link2.Transmit(0, pkt2)
	link3.Transmit(0, pkt3)

	// Parallel advance flows to different cycles
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		link1SFC := link1.SendFinishedCycle()
		f1.AdvanceTo(5, link1SFC+10) // Can advance to 5
	}()

	go func() {
		defer wg.Done()
		link2SFC := link2.SendFinishedCycle()
		f2.AdvanceTo(8, link2SFC+10) // Can advance to 8
	}()

	go func() {
		defer wg.Done()
		link3SFC := link3.SendFinishedCycle()
		f3.AdvanceTo(10, link3SFC+10) // Can advance to 10
	}()

	wg.Wait()

	// Verify flows advanced independently
	if f1.CurrentCycle() < 1 {
		t.Errorf("expected f1 to advance, got cycle %d", f1.CurrentCycle())
	}
	if f2.CurrentCycle() < 1 {
		t.Errorf("expected f2 to advance, got cycle %d", f2.CurrentCycle())
	}
	if f3.CurrentCycle() < 1 {
		t.Errorf("expected f3 to advance, got cycle %d", f3.CurrentCycle())
	}
}

// TestBidirectionalLinkParallel tests bidirectional links advancing in parallel.
func TestBidirectionalLinkParallel(t *testing.T) {
	t.Parallel()

	// Node A -> Node B (Link AB, latency L1=2)
	// Node B -> Node A (Link BA, latency L2=3)
	fA := flow.NewFIFO(1, 8, 0, 0, nil, 0)
	fB := flow.NewFIFO(2, 8, 0, 0, nil, 0)

	linkAB := link.NewLink(1, fB, nil, 0, 2, 1, 0) // A->B, latency 2, bandwidth 1
	linkBA := link.NewLink(2, fA, nil, 0, 3, 1, 0) // B->A, latency 3, bandwidth 1

	linkAB.SetNoBackpressureUntil(20)
	linkBA.SetNoBackpressureUntil(20)

	// Flow A1 processes A->B, can advance to cycle N
	// Flow B1 processes A->B, can advance to N + L1
	// Flow B2 processes B->A, can advance to cycle M
	// Flow A2 processes B->A, can advance to M + L2

	pktAB := packet.Packet{SourceID: 1, TargetID: 2, Payload: "A->B"}
	pktBA := packet.Packet{SourceID: 2, TargetID: 1, Payload: "B->A"}

	linkAB.Transmit(0, pktAB)
	linkBA.Transmit(0, pktBA)

	// Parallel advance
	var wg sync.WaitGroup
	wg.Add(2)

	// Flow B can advance to cycle 2 (0 + latency 2)
	go func() {
		defer wg.Done()
		linkABSFC := linkAB.SendFinishedCycle()
		fB.AdvanceTo(5, linkABSFC+10)
	}()

	// Flow A can advance to cycle 3 (0 + latency 3)
	go func() {
		defer wg.Done()
		linkBASFC := linkBA.SendFinishedCycle()
		fA.AdvanceTo(6, linkBASFC+10)
	}()

	wg.Wait()

	// Verify both flows advanced
	if fA.CurrentCycle() < 1 {
		t.Errorf("expected fA to advance, got cycle %d", fA.CurrentCycle())
	}
	if fB.CurrentCycle() < 1 {
		t.Errorf("expected fB to advance, got cycle %d", fB.CurrentCycle())
	}
}

// TestSFCBasedAdvance tests that flows advance based on SFC + Link Delay.
func TestSFCBasedAdvance(t *testing.T) {
	t.Parallel()

	f := flow.NewFIFO(1, 8, 0, 0, nil, 0)
	l := link.NewLink(0, f, nil, 0, 2, 1, 0) // latency = 2, bandwidth = 1
	l.SetNoBackpressureUntil(20)

	// Send packet at cycle 0, SFC should be 2 (0 + latency)
	pkt := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}
	l.Transmit(0, pkt)

	// Flow can advance to SFC + some margin
	linkSFC := l.SendFinishedCycle()
	maxAdvanceCycle := linkSFC + 5

	err := f.AdvanceTo(maxAdvanceCycle, linkSFC+10)
	if err != nil {
		t.Fatalf("AdvanceTo failed: %v", err)
	}

	// Verify flow advanced
	if f.CurrentCycle() < linkSFC {
		t.Errorf("expected flow to advance to at least %d, got %d", linkSFC, f.CurrentCycle())
	}
}

// TestBackpressureSignalMechanism tests that Flow calculates and notifies Link about noBackpressureUntil.
func TestBackpressureSignalMechanism(t *testing.T) {
	t.Parallel()

	f := flow.NewFIFO(1, 5, 0, 0, nil, 0) // Small mailbox capacity
	l := link.NewLink(0, f, nil, 0, 1, 1, 0)

	// Set upstream link
	f.SetUpstreamLink(l)

	// Advance flow
	ctx := context.Background()
	f.Tick(ctx, 1)

	// Calculate noBackpressureUntil (based on mailbox capacity)
	remainingCapacity := cap(f.Mailbox()) - len(f.Mailbox())
	noBPUntil := f.CurrentCycle() + uint64(remainingCapacity)

	// Notify link
	f.SetNoBackpressureUntil(noBPUntil)

	// Verify link received the signal
	if l.NoBackpressureUntil() != noBPUntil {
		t.Errorf("expected link noBackpressureUntil %d, got %d", noBPUntil, l.NoBackpressureUntil())
	}
}

// TestBackpressureParallel tests that one link backpressure doesn't block others.
func TestBackpressureParallel(t *testing.T) {
	t.Parallel()

	f1 := flow.NewFIFO(1, 2, 0, 0, nil, 0) // Small capacity, will backpressure
	f2 := flow.NewFIFO(2, 8, 0, 0, nil, 0) // Normal capacity

	l1 := link.NewLink(0, f1, nil, 0, 1, 1, 0)
	l2 := link.NewLink(0, f2, nil, 0, 1, 1, 0)

	// Set noBackpressureUntil for f1 to a small value (will backpressure soon)
	f1.SetNoBackpressureUntil(2)
	l1.SetNoBackpressureUntil(2)

	// Set noBackpressureUntil for f2 to a large value (no backpressure)
	f2.SetNoBackpressureUntil(20)
	l2.SetNoBackpressureUntil(20)

	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 2, Payload: "test2"}

	// Transmit packets
	l1.Transmit(0, pkt1)
	l2.Transmit(0, pkt2)

	// Try to advance both links
	l1.Advance(3) // Should be blocked (3 > 2)
	l2.Advance(3) // Should proceed (3 <= 20)

	// Verify l1 didn't advance (backpressured)
	if l1.CurrentCycle() > 2 {
		t.Errorf("expected l1 to be blocked at cycle <= 2, got %d", l1.CurrentCycle())
	}

	// Verify l2 advanced (not backpressured)
	if l2.CurrentCycle() != 3 {
		t.Errorf("expected l2 to advance to cycle 3, got %d", l2.CurrentCycle())
	}
}

