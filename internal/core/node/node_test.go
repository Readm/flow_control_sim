package node

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/dataflow/flow"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// multiFlowNode implements Node with multiple flows that can execute serially or in parallel.
type multiFlowNode struct {
	id       int
	flows    []flow.Flow
	parallel bool
}

func newMultiFlowNode(id int, flowCount int, parallel bool, mailboxSize int, inQueueCapacity int, outQueueCapacity int) *multiFlowNode {
	flows := make([]flow.Flow, flowCount)
	for i := 0; i < flowCount; i++ {
		flows[i] = flow.NewFIFO(id, mailboxSize, inQueueCapacity, outQueueCapacity, nil, 0)
	}
	return &multiFlowNode{
		id:       id,
		flows:    flows,
		parallel: parallel,
	}
}

func (n *multiFlowNode) ID() int {
	return n.id
}

func (n *multiFlowNode) Flows() []flow.Flow {
	return n.flows
}

func (n *multiFlowNode) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
	if n.parallel {
		var wg sync.WaitGroup
		errCh := make(chan error, len(n.flows))
		for _, f := range n.flows {
			wg.Add(1)
			go func(fl flow.Flow) {
				defer wg.Done()
				if err := fl.Tick(ctx, cycle); err != nil {
					select {
					case errCh <- err:
					default:
					}
				}
			}(f)
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			if err != nil {
				return err
			}
		}
	} else {
		for _, f := range n.flows {
			if err := f.Tick(ctx, cycle); err != nil {
				return err
			}
		}
	}
	return nil
}

// Run executes the requested number of cycles, calling Tick for each cycle.
func (n *multiFlowNode) Run(cycles uint64) error {
	ctx := context.Background()
	for i := uint64(0); i < cycles; i++ {
		if err := n.Tick(ctx, i, 0); err != nil {
			return err
		}
	}
	return nil
}

// TestNodeWithSingleFlow tests that a Node can have at least one Flow.
func TestNodeWithSingleFlow(t *testing.T) {
	t.Parallel()

	node := newMultiFlowNode(1, 1, false, 8, 0, 0)
	if len(node.Flows()) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(node.Flows()))
	}

	// Create a link and send a packet
	link := link.NewLink(0, node.Flows()[0], nil, 0, 1, 1, 0)
	pkt := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "test",
	}

	// Transmit and advance
	link.Transmit(0, pkt)
	link.Advance(1)

	// Run one cycle
	if err := node.Run(1); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// Verify packet was processed
	if node.Flows()[0].ProcessedCount() != 1 {
		t.Fatalf("expected 1 processed packet, got %d", node.Flows()[0].ProcessedCount())
	}
}

// TestNodeWithMultipleFlowsSerial tests that multiple flows execute serially.
func TestNodeWithMultipleFlowsSerial(t *testing.T) {
	t.Parallel()

	node := newMultiFlowNode(1, 3, false, 8, 0, 0)
	if len(node.Flows()) != 3 {
		t.Fatalf("expected 3 flows, got %d", len(node.Flows()))
	}

	// Create links for each flow
	links := make([]*link.Link, 3)
	for i := 0; i < 3; i++ {
		links[i] = link.NewLink(0, node.Flows()[i], nil, 0, 1, 1, 0)
		pkt := packet.Packet{
			SourceID: 0,
			TargetID: 1,
			Payload:  "test",
		}
		links[i].Transmit(0, pkt)
		links[i].Advance(1)
	}

	// Run one cycle
	if err := node.Run(1); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// Verify all flows processed packets
	for i, f := range node.Flows() {
		if f.ProcessedCount() != 1 {
			t.Fatalf("flow %d: expected 1 processed packet, got %d", i, f.ProcessedCount())
		}
	}
}

// TestNodeWithMultipleFlowsParallel tests that multiple flows execute in parallel.
func TestNodeWithMultipleFlowsParallel(t *testing.T) {
	t.Parallel()

	node := newMultiFlowNode(1, 3, true, 8, 0, 0)

	// Create links for each flow
	links := make([]*link.Link, 3)
	for i := 0; i < 3; i++ {
		links[i] = link.NewLink(0, node.Flows()[i], nil, 0, 1, 1, 0)
		pkt := packet.Packet{
			SourceID: 0,
			TargetID: 1,
			Payload:  "test",
		}
		links[i].Transmit(0, pkt)
		links[i].Advance(1)
	}

	// Run one cycle
	start := time.Now()
	if err := node.Run(1); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	duration := time.Since(start)

	// Verify all flows processed packets
	for i, f := range node.Flows() {
		if f.ProcessedCount() != 1 {
			t.Fatalf("flow %d: expected 1 processed packet, got %d", i, f.ProcessedCount())
		}
	}

	// Parallel execution should be fast (no significant delay)
	if duration > 100*time.Millisecond {
		t.Logf("parallel execution took %v, which seems slow", duration)
	}
}

// TestNodeRunMultipleCycles tests that Run executes the correct number of cycles.
func TestNodeRunMultipleCycles(t *testing.T) {
	t.Parallel()

	node := newMultiFlowNode(1, 2, false, 8, 0, 0)
	link := link.NewLink(0, node.Flows()[0], nil, 0, 1, 1, 0)

	// Send packets for multiple cycles
	for cycle := uint64(0); cycle < 5; cycle++ {
		pkt := packet.Packet{
			SourceID: 0,
			TargetID: 1,
			Payload:  "test",
		}
		link.Transmit(cycle, pkt)
		link.Advance(cycle + 1)
	}

	// Run 5 cycles
	if err := node.Run(5); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// Verify packets were processed
	if node.Flows()[0].ProcessedCount() != 5 {
		t.Fatalf("expected 5 processed packets, got %d", node.Flows()[0].ProcessedCount())
	}
}

// TestFlowEmitAndDrainDispatchQueue tests that packets can be emitted and routed to dispatch queues.
func TestFlowEmitAndDrainDispatchQueue(t *testing.T) {
	t.Parallel()

	// Create a flow with one dispatch queue
	targetFlow := flow.NewFIFO(2, 8, 0, 0, nil, 0)
	f := flow.NewFIFO(1, 8, 0, 0, []interface{}{targetFlow}, 16)

	// Run Tick to trigger routing (needed for packets to go through the router)
	ctx := context.Background()
	f.Tick(ctx, 0)

	// Emit packets
	pkt1 := packet.Packet{SourceID: 1, TargetID: 2, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 1, TargetID: 2, Payload: "test2"}
	f.Emit(pkt1, pkt2)

	// Run Tick again to trigger routing
	f.Tick(ctx, 1)

	// Drain dispatch queue
	drained := f.DrainDispatchQueue(0)
	if len(drained) != 2 {
		t.Fatalf("expected 2 packets, got %d", len(drained))
	}

	// Verify drained packets
	if drained[0].Payload != "test1" || drained[1].Payload != "test2" {
		t.Fatalf("unexpected packet content")
	}

	// Verify dispatch queue is empty after drain
	drained2 := f.DrainDispatchQueue(0)
	if len(drained2) != 0 {
		t.Fatalf("expected empty queue after drain, got %d packets", len(drained2))
	}
}

// TestBackpressureInQueueFull tests that in_queue full triggers backpressure signal.
func TestBackpressureInQueueFull(t *testing.T) {
	t.Parallel()

	node := newMultiFlowNode(1, 1, false, 2, 2, 0)
	f := node.Flows()[0]
	link := link.NewLink(0, f, nil, 0, 1, 1, 0)

	// Set up backpressure callback
	backpressureTriggered := false
	f.SetUpstreamBackpressureCallback(func() {
		backpressureTriggered = true
		link.SetBackpressure(true)
	})

	// Fill mailbox to capacity
	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test2"}
	link.Transmit(0, pkt1)
	link.Transmit(0, pkt2)
	link.Advance(1)

	// Try to send one more packet (should trigger backpressure)
	pkt3 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test3"}
	link.Transmit(1, pkt3)
	link.Advance(2)

	// Run Tick to check for backpressure
	ctx := context.Background()
	f.Tick(ctx, 2)

	// Verify backpressure was triggered
	if !backpressureTriggered {
		t.Fatalf("expected backpressure to be triggered")
	}

	// Verify link is backpressured
	if !link.IsBackpressured() {
		t.Fatalf("expected link to be backpressured")
	}
}

// TestBackpressureDownstreamBlocksEmit tests that downstream backpressure blocks Emit.
func TestBackpressureDownstreamBlocksEmit(t *testing.T) {
	t.Parallel()

	// Create a flow with one dispatch queue
	targetFlow := flow.NewFIFO(2, 8, 0, 0, nil, 0)
	f := flow.NewFIFO(1, 8, 0, 0, []interface{}{targetFlow}, 16)
	ctx := context.Background()

	// Set downstream backpressure
	f.SetDownstreamBackpressure(true)

	// Try to emit packets
	pkt := packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"}
	f.Emit(pkt)

	// Trigger routing
	f.Tick(ctx, 0)

	// Verify packet was not routed to dispatch_queue (because Emit blocked it)
	drained := f.DrainDispatchQueue(0)
	if len(drained) != 0 {
		t.Fatalf("expected no packets in dispatch_queue when backpressured, got %d", len(drained))
	}

	// Clear backpressure
	f.SetDownstreamBackpressure(false)

	// Emit again
	f.Emit(pkt)

	// Trigger routing
	f.Tick(ctx, 1)

	// Verify packet is now in dispatch_queue
	drained = f.DrainDispatchQueue(0)
	if len(drained) != 1 {
		t.Fatalf("expected 1 packet after clearing backpressure, got %d", len(drained))
	}
}

// TestBackpressureOutQueueFullBlocksProcess tests that out_queue or dispatch queues full blocks processing.
func TestBackpressureOutQueueFullBlocksProcess(t *testing.T) {
	t.Parallel()

	// Create a flow with one dispatch queue (capacity 2)
	targetFlow := flow.NewFIFO(2, 8, 0, 0, nil, 0)
	f := flow.NewFIFO(1, 8, 0, 2, []interface{}{targetFlow}, 2)
	ctx := context.Background()

	// Fill dispatch_queue to capacity
	pkt1 := packet.Packet{SourceID: 1, TargetID: 2, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 1, TargetID: 2, Payload: "test2"}
	f.Emit(pkt1, pkt2)
	f.Tick(ctx, 0) // Route packets to dispatch_queue

	// Verify dispatch_queue is full
	if !f.IsDispatchQueueFull(0) {
		t.Fatalf("expected dispatch_queue to be full")
	}

	// Send packet to mailbox
	link := link.NewLink(0, f, nil, 0, 1, 1, 0)
	pkt3 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test3"}
	link.Transmit(0, pkt3)
	link.Advance(1)

	// Run Tick - processing should be blocked because dispatch_queue is full
	f.Tick(ctx, 1)

	// Verify packet is still in incoming (not processed)
	// We can't directly check incoming, but we can verify it wasn't processed
	if f.ProcessedCount() != 0 {
		t.Fatalf("expected no processed packets when dispatch_queue is full, got %d", f.ProcessedCount())
	}

	// Drain dispatch_queue
	f.DrainDispatchQueue(0)

	// Run Tick again - now processing should work
	f.Tick(ctx, 2)

	// Verify packet was processed
	if f.ProcessedCount() != 1 {
		t.Fatalf("expected 1 processed packet after draining dispatch_queue, got %d", f.ProcessedCount())
	}
}

// TestBackpressureLinkBlocksTransmit tests that backpressured link blocks Transmit.
func TestBackpressureLinkBlocksTransmit(t *testing.T) {
	t.Parallel()

	node := newMultiFlowNode(1, 1, false, 8, 0, 0)
	f := node.Flows()[0]
	link := link.NewLink(0, f, nil, 0, 1, 1, 0)

	// Set link backpressure
	link.SetBackpressure(true)

	// Try to transmit packet
	pkt := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}
	link.Transmit(0, pkt)

	// Advance link
	link.Advance(1)

	// Verify packet was not delivered
	if f.ProcessedCount() != 0 {
		t.Fatalf("expected no processed packets when link is backpressured, got %d", f.ProcessedCount())
	}

	// Clear backpressure
	link.SetBackpressure(false)

	// Transmit again
	link.Transmit(1, pkt)
	link.Advance(2)

	// Run Tick
	ctx := context.Background()
	f.Tick(ctx, 2)

	// Verify packet was processed
	if f.ProcessedCount() != 1 {
		t.Fatalf("expected 1 processed packet after clearing backpressure, got %d", f.ProcessedCount())
	}
}

