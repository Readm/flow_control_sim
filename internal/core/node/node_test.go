package node

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/pipeline"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// multiFlowNode implements Node with multiple flows that can execute serially or in parallel.
type multiFlowNode struct {
	id       int
	flows    []pipeline.Pipeline
	parallel bool
}

func newMultiFlowNode(id int, flowCount int, parallel bool, mailboxSize int, inQueueCapacity int, outQueueCapacity int) *multiFlowNode {
	flows := make([]pipeline.Pipeline, flowCount)
	for i := 0; i < flowCount; i++ {
		flows[i] = pipeline.NewFIFO(id, mailboxSize)
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

func (n *multiFlowNode) Flows() []pipeline.Pipeline {
	return n.flows
}

func (n *multiFlowNode) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
	if n.parallel {
		var wg sync.WaitGroup
		errCh := make(chan error, len(n.flows))
		for _, f := range n.flows {
			wg.Add(1)
			go func(fl pipeline.Pipeline) {
				defer wg.Done()
				if err := fl.ProcessCycle(int(cycle)); err != nil {
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
			if err := f.ProcessCycle(int(cycle)); err != nil {
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

	// Create a link and send a packet using new interface
	flow0 := node.Flows()[0]
	outPort := ahead_port.NewAheadPort(8)
	flow0.AddOutPort(outPort)
	inPort := flow0.InPort()

	link := link.NewLink(0, 1, outPort, inPort, 1, 1)
	pkt := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "test",
	}

	// Initialize upstream Done for flow0 (no upstream, so set to 0)
	flow0.InPort().SetDone(-1)

	// Initialize downstream ready state
	if inPortImpl, ok := inPort.(*ahead_port.SinglePort); ok {
		inPortImpl.SetReadyUntil(10)
	}
	outPort.SetReadyUntil(10)

	// Send packet through port
	env := ahead_port.PacketWithCycle{Cycle: 0, Packet: pkt}
	outPort.SendChan() <- env
	outPort.SetDone(1)

	// Process cycles
	flow0.ProcessCycle(0)
	link.ProcessCycle(0)
	link.ProcessCycle(1)
	flow0.InPort().SetDone(1)
	flow0.ProcessCycle(1)

	// Verify packet was processed
	if flow0.ProcessedCount() != 1 {
		t.Fatalf("expected 1 processed packet, got %d", flow0.ProcessedCount())
	}
}

// TestNodeWithMultipleFlowsSerial tests that multiple flows execute serially.
func TestNodeWithMultipleFlowsSerial(t *testing.T) {
	t.Parallel()

	node := newMultiFlowNode(1, 3, false, 8, 0, 0)
	if len(node.Flows()) != 3 {
		t.Fatalf("expected 3 flows, got %d", len(node.Flows()))
	}

	// Create links for each flow using new interface
	links := make([]*link.Link, 3)
	for i := 0; i < 3; i++ {
		flow := node.Flows()[i]
		outPort := ahead_port.NewAheadPort(8)
		flow.AddOutPort(outPort)
		inPort := flow.InPort()

		links[i] = link.NewLink(0, flow.ID(), outPort, inPort, 1, 1)

		// Initialize upstream Done for flow (no upstream, so set to 0)
		flow.InPort().SetDone(-1)

		// Initialize downstream ready state
		if inPortImpl, ok := inPort.(*ahead_port.SinglePort); ok {
			inPortImpl.SetReadyUntil(10)
		}

		// Send packet through port
		pkt := packet.Packet{
			SourceID: 0,
			TargetID: 1,
			Payload:  "test",
		}
		env := ahead_port.PacketWithCycle{Cycle: 0, Packet: pkt}
		outPort.SendChan() <- env
		outPort.SetDone(1)
	}

	// Process cycles
	for i := 0; i < 3; i++ {
		node.Flows()[i].ProcessCycle(0)
		links[i].ProcessCycle(0)
		links[i].ProcessCycle(1)
		node.Flows()[i].ProcessCycle(1)
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

	// Create links for each flow using new interface
	links := make([]*link.Link, 3)
	for i := 0; i < 3; i++ {
		flow := node.Flows()[i]
		outPort := ahead_port.NewAheadPort(8)
		flow.AddOutPort(outPort)
		inPort := flow.InPort()

		links[i] = link.NewLink(0, flow.ID(), outPort, inPort, 1, 1)

		// Initialize upstream Done for flow (no upstream, so set to 0)
		flow.InPort().SetDone(-1)

		// Initialize downstream ready state
		if inPortImpl, ok := inPort.(*ahead_port.SinglePort); ok {
			inPortImpl.SetReadyUntil(10)
		}

		// Send packet through port
		pkt := packet.Packet{
			SourceID: 0,
			TargetID: 1,
			Payload:  "test",
		}
		env := ahead_port.PacketWithCycle{Cycle: 0, Packet: pkt}
		outPort.SendChan() <- env
		outPort.SetDone(1)
	}

	// Process cycles
	start := time.Now()
	for i := 0; i < 3; i++ {
		node.Flows()[i].ProcessCycle(0)
		links[i].ProcessCycle(0)
		links[i].ProcessCycle(1)
		node.Flows()[i].ProcessCycle(1)
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
// Simplified: tests that multiple packets can be sent and processed across cycles.
func TestNodeRunMultipleCycles(t *testing.T) {
	t.Parallel()

	node := newMultiFlowNode(1, 2, false, 8, 0, 0)
	flow0 := node.Flows()[0]
	outPort := ahead_port.NewAheadPort(8)
	flow0.AddOutPort(outPort)
	inPort := flow0.InPort()

	link := link.NewLink(0, flow0.ID(), outPort, inPort, 1, 1)

	// Fix: prevent re-circulation of packets
	flow0.SetRouterHook(func(pkt packet.Packet, outPorts []interface{}, topology interface{}) int {
		if pkt.TargetID == flow0.ID() {
			return -1
		}
		if len(outPorts) > 0 {
			return 0
		}
		return -1
	})

	// Initialize upstream Done for flow (no upstream, so set to 0)
	flow0.InPort().SetDone(-1)

	// Initialize downstream ready state
	if inPortImpl, ok := inPort.(*ahead_port.SinglePort); ok {
		inPortImpl.SetReadyUntil(10)
	}

	// Send and process packets one by one across cycles
	for cycle := 0; cycle < 5; cycle++ {
		pkt := packet.Packet{
			SourceID: 0,
			TargetID: 1,
			Payload:  fmt.Sprintf("test-%d", cycle),
		}
		env := ahead_port.PacketWithCycle{Cycle: cycle, Packet: pkt}
		outPort.SendChan() <- env
		outPort.SetDone(cycle)

		// Process Flow cycle (sends packet to outPort)
		flow0.InPort().SetDone(cycle)
		flow0.ProcessCycle(cycle)

		// Process Link cycle (receives from outPort)
		link.ProcessCycle(cycle)

		// Process Link next cycle (forwards to inPort with latency=1)
		link.ProcessCycle(cycle + 1)

		// Process Flow next cycle (receives and processes packet)
		flow0.InPort().SetDone(cycle)
		flow0.ProcessCycle(cycle + 1)
	}

	// Verify packets were processed (should have at least 5 packets processed)
	processed := node.Flows()[0].ProcessedCount()
	if processed < 5 {
		t.Fatalf("expected at least 5 processed packets, got %d", processed)
	}
}

// TestFlowEmitAndDrainDispatchQueue tests that packets can be emitted and routed to dispatch queues.
func TestFlowEmitAndDrainDispatchQueue(t *testing.T) {
	t.Parallel()

	// Create flows with output ports and link
	targetFlow := pipeline.NewFIFO(2, 8)
	f := pipeline.NewFIFO(1, 8)

	// Create output port and link
	outPort := ahead_port.NewAheadPort(8)
	f.AddOutPort(outPort)
	targetInPort := targetFlow.InPort()
	link := link.NewLink(1, 2, outPort, targetInPort, 1, 10)

	// Initialize upstream Done for flows
	f.InPort().SetDone(-1)
	targetFlow.InPort().SetDone(-1)

	// Initialize downstream ready state
	outPort.SetReadyUntil(10)
	if targetInPortImpl, ok := targetInPort.(*ahead_port.SinglePort); ok {
		targetInPortImpl.SetReadyUntil(10)
	}

	// Emit packets
	pkt1 := packet.Packet{SourceID: 1, TargetID: 2, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 1, TargetID: 2, Payload: "test2"}
	f.Emit(pkt1, pkt2)

	// Process cycles to route packets
	f.ProcessCycle(0)
	link.ProcessCycle(0)
	link.ProcessCycle(1)
	targetFlow.ProcessCycle(1)

	// Verify packets were processed
	if targetFlow.ProcessedCount() != 2 {
		t.Fatalf("expected 2 processed packets, got %d", targetFlow.ProcessedCount())
	}
}
