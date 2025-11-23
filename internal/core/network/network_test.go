package network

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/cycle_port"
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/dataflow/flow"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

func TestNetworkNodesExchangePacketsThroughLink(t *testing.T) {
	t.Parallel()

	const (
		cycles      = 6 // Increased to account for dispatch queue and routing delay
		linkCycles  = 2 // Account for both link latency and routing delay
		nodeWork    = 35 * time.Millisecond
		testTimeout = time.Second
		mailboxSize = 4
	)

	tracker := &concurrencyTracker{}
	EnableMockDelay(5 * time.Millisecond)
	defer DisableMockDelay()

	nodeA := newFlowNode(0, 1, mailboxSize, tracker, nodeWork, cycles)
	nodeB := newFlowNode(1, 0, mailboxSize, tracker, nodeWork, cycles)

	// Create output ports for flows
	flowAOutPort := cycle_port.NewCyclePort(mailboxSize)
	flowBOutPort := cycle_port.NewCyclePort(mailboxSize)

	// Connect flows to output ports
	nodeA.Flows()[0].AddOutPort(flowAOutPort)
	nodeB.Flows()[0].AddOutPort(flowBOutPort)

	// Create links with CyclePort
	linkAB := link.NewLink(nodeA.ID(), nodeB.ID(), []cycle_port.CyclePort{flowAOutPort}, nodeB.Flows()[0].InPort(), linkCycles, 1)
	linkBA := link.NewLink(nodeB.ID(), nodeA.ID(), []cycle_port.CyclePort{flowBOutPort}, nodeA.Flows()[0].InPort(), linkCycles, 1)

	// Initialize downstream ready state
	if flowAInPortImpl, ok := nodeA.Flows()[0].InPort().(*cycle_port.CyclePortImpl); ok {
		flowAInPortImpl.SetReadyUntil(int(cycles) + 10)
	}
	if flowBInPortImpl, ok := nodeB.Flows()[0].InPort().(*cycle_port.CyclePortImpl); ok {
		flowBInPortImpl.SetReadyUntil(int(cycles) + 10)
	}
	flowAOutPort.SetReadyUntil(int(cycles) + 10)
	flowBOutPort.SetReadyUntil(int(cycles) + 10)

	// Initialize upstream DoneUntil for outPorts (allows Link to start processing)
	// Link needs upstream DoneUntil >= cycle, so we initialize to 0
	flowAOutPort.SetDoneUntil(0)
	flowBOutPort.SetDoneUntil(0)

	// Initialize upstream DoneUntil for flows (no upstream initially)
	nodeA.Flows()[0].InPort().SetDoneUntil(0)
	nodeB.Flows()[0].InPort().SetDoneUntil(0)

	graph := map[int][]*link.Link{
		nodeA.ID(): {linkAB},
		nodeB.ID(): {linkBA},
	}

	nodes := []node.Node{nodeA, nodeB}
	manager, err := NewManager(nodes, graph)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	start := time.Now()
	if err := manager.Run(ctx, cycles); err != nil {
		t.Fatalf("run manager: %v", err)
	}
	duration := time.Since(start)

	perCycleDelay := mockDelay
	serialEstimate := time.Duration(cycles) * time.Duration(len(nodes)) * nodeWork
	serialEstimate += time.Duration(cycles-1) * perCycleDelay

	if duration >= serialEstimate {
		t.Fatalf("expected runtime < %v, got %v", serialEstimate, duration)
	}

	expectedPackets := int(cycles - linkCycles)
	if got := nodeA.processedCount(); got != expectedPackets {
		t.Fatalf("nodeA processed %d packets, want %d", got, expectedPackets)
	}
	if got := nodeB.processedCount(); got != expectedPackets {
		t.Fatalf("nodeB processed %d packets, want %d", got, expectedPackets)
	}

	if tracker.maxActive() != len(nodes) {
		t.Fatalf("expected max concurrency %d, got %d", len(nodes), tracker.maxActive())
	}
}

type flowNode struct {
	id          int
	peerID      int
	flow        flow.Flow
	tracker     *concurrencyTracker
	workload    time.Duration
	totalCycles uint64
}

func newFlowNode(id, peerID int, mailboxSize int, tracker *concurrencyTracker, workload time.Duration, totalCycles uint64) *flowNode {
	f := flow.NewFIFO(id, mailboxSize)
	return &flowNode{
		id:          id,
		peerID:      peerID,
		flow:        f,
		tracker:     tracker,
		workload:    workload,
		totalCycles: totalCycles,
	}
}

func (n *flowNode) ID() int {
	return n.id
}

func (n *flowNode) Flows() []flow.Flow {
	return []flow.Flow{n.flow}
}

func (n *flowNode) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
	if n.tracker != nil {
		n.tracker.enter()
		defer n.tracker.exit()
	}

	// Send packet to peer if not the last cycle (before processing, so it's included in this cycle)
	if cycle+1 < n.totalCycles {
		payload := fmt.Sprintf("node-%d-cycle-%d", n.id, cycle)
		n.flow.Emit(packet.Packet{
			SourceID: n.id,
			TargetID: n.peerID,
			Payload:  payload,
		})
	}

	// Initialize upstream DoneUntil if needed (no upstream initially)
	if n.flow.InPort().GetDoneUntil() < 0 {
		n.flow.InPort().SetDoneUntil(int(cycle))
	}

	// Process incoming packets
	if err := n.flow.ProcessCycle(int(cycle)); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(n.workload):
		return nil
	}
}

func (n *flowNode) processedCount() int {
	return n.flow.ProcessedCount()
}

type concurrencyTracker struct {
	mu     sync.Mutex
	active int
	max    int
}

func (t *concurrencyTracker) enter() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active++
	if t.active > t.max {
		t.max = t.active
	}
}

func (t *concurrencyTracker) exit() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active > 0 {
		t.active--
	}
}

func (t *concurrencyTracker) maxActive() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.max
}
