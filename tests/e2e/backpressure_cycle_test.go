//go:build e2e

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/pipeline"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
	"github.com/Readm/flow_sim/pkg/visual/frame"
	"github.com/Readm/flow_sim/pkg/visual/recorder"
)

// TestBackpressureCycles tests that backpressure signals only appear when they should
func TestBackpressureCycles(t *testing.T) {
	t.Parallel()

	// Create a real network to test backpressure
	ctx := context.Background()
	mgr, frames := createTestNetwork(t, ctx)

	// Test cycles 0-5: Should have NO backpressure in early cycles
	for cycle := 0; cycle <= 5; cycle++ {
		// Run the cycle
		if err := mgr.RunFrom(ctx, uint64(cycle), 1); err != nil {
			t.Fatalf("run cycle %d: %v", cycle, err)
		}

		// Wait a bit for frame to be produced
		time.Sleep(100 * time.Millisecond)

		// Get frame for this cycle
		var fr *frame.Frame
		select {
		case fr = <-frames:
			if fr == nil {
				t.Fatalf("received nil frame at cycle %d", cycle)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for frame at cycle %d", cycle)
		}

		// Verify frame cycle matches (frame is produced AFTER the cycle, so it should be cycle)
		if fr.Cycle != cycle {
			t.Errorf("expected frame cycle %d, got %d", cycle, fr.Cycle)
		}

		// Check no backpressure in frame data
		checkFrameNoBackpressure(t, fr, cycle)
	}
}

func checkFrameNoBackpressure(t *testing.T, fr *frame.Frame, cycle int) {
	t.Helper()

	// Check all nodes have no backpressure signals
	for _, node := range fr.Nodes {
		if node.InQueueBackpressure {
			t.Errorf("cycle %d: node %d has InQueueBackpressure=true, but should not (queues: %+v)", cycle, node.ID, node.Queues)
		}
		if node.OutQueueBackpressure {
			t.Errorf("cycle %d: node %d has OutQueueBackpressure=true, but should not (queues: %+v)", cycle, node.ID, node.Queues)
		}
		if node.DownstreamBackpressure {
			t.Errorf("cycle %d: node %d has DownstreamBackpressure=true, but should not", cycle, node.ID)
		}
	}

	// Check all edges have no backpressure
	for _, edge := range fr.Edges {
		if edge.Backpressured {
			t.Errorf("cycle %d: edge %d->%d has Backpressured=true, but should not (pipeline stages: %+v)", cycle, edge.Source, edge.Target, edge.PipelineStages)
		}
	}

	// Log frame details for debugging
	t.Logf("cycle %d: frame has %d nodes, %d edges", cycle, len(fr.Nodes), len(fr.Edges))
	for _, node := range fr.Nodes {
		t.Logf("  node %d: InQueueBP=%v, OutQueueBP=%v, DownstreamBP=%v, queues=%+v",
			node.ID, node.InQueueBackpressure, node.OutQueueBackpressure, node.DownstreamBackpressure, node.Queues)
	}
	for _, edge := range fr.Edges {
		t.Logf("  edge %d->%d: Backpressured=%v, pipeline=%+v",
			edge.Source, edge.Target, edge.Backpressured, edge.PipelineStages)
	}
}

func createTestNetwork(t *testing.T, ctx context.Context) (*network.Manager, <-chan *frame.Frame) {
	t.Helper()

	// Create flows similar to main.go
	mailboxSize := 8
	linkLatency := uint64(5)
	linkBandwidth := uint64(1)
	totalCycles := uint64(64)

	// Create two nodes that exchange packets
	node0 := newTestFlowNode(0, 1, totalCycles, mailboxSize)
	node1 := newTestFlowNode(1, 0, totalCycles, mailboxSize)

	nodes := []node.Node{node0, node1}

	// Create bidirectional links
	linkAB := link.NewLink(0, node1.Flows()[0], linkLatency, linkBandwidth, 0)
	linkBA := link.NewLink(1, node0.Flows()[0], linkLatency, linkBandwidth, 0)

	// Set noBackpressureUntil to allow transmission
	linkAB.SetNoBackpressureUntil(totalCycles + 10)
	linkBA.SetNoBackpressureUntil(totalCycles + 10)

	graph := map[int][]*link.Link{
		0: {linkAB},
		1: {linkBA},
	}

	mgr, err := network.NewManager(nodes, graph)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Create recorder
	rec := recorder.New(32)
	rec.SetPaused(false)
	mgr.SetCycleHook(rec)

	// Start frame relay
	frameCh := make(chan *frame.Frame, 32)
	go func() {
		for fr := range rec.Frames() {
			select {
			case frameCh <- fr:
			default:
			}
		}
		close(frameCh)
	}()

	// Don't run cycle 0 here, let the test run it
	return mgr, frameCh
}

type testFlowNode struct {
	id          int
	peerID      int
	flow        pipeline.Pipeline
	totalCycles uint64
}

func newTestFlowNode(id, peerID int, totalCycles uint64, mailboxSize int) *testFlowNode {
	f := pipeline.NewFIFO(id, mailboxSize)
	return &testFlowNode{
		id:          id,
		peerID:      peerID,
		flow:        f,
		totalCycles: totalCycles,
	}
}

func (n *testFlowNode) ID() int {
	return n.id
}

func (n *testFlowNode) Flows() []pipeline.Pipeline {
	return []pipeline.Pipeline{n.flow}
}

func (n *testFlowNode) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
	if err := n.flow.Tick(int(cycle)); err != nil {
		return err
	}
	// Inject packet only if cycle+1 < totalCycles
	if cycle+1 < n.totalCycles {
		if fifo, ok := n.flow.(*pipeline.FIFO); ok {
			fifo.InjectPackets(int(cycle), []packet.Packet{{
				SourceID: n.id,
				TargetID: n.peerID,
				Payload:  "",
			}})
		}
	}
	return nil
}
