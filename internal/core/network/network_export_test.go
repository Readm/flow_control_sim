package network_test

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/state"
)

func TestNetwork_ExportState(t *testing.T) {
	// 1. Setup Network
	net := network.New()

	// Add Node 1
	n1 := node.NewWorkerNode(1)
	handle1 := &network.NodeHandle{
		Node: n1,
	}
	net.AddNode(handle1)

	// Add Node 2
	n2 := node.NewWorkerNode(2)
	handle2 := &network.NodeHandle{
		Node: n2,
	}
	net.AddNode(handle2)

	// 2. Export
	cfg := state.ExportConfig{DetailLevel: state.DetailLevelSummary}
	ns := net.ExportState(cfg)

	// 3. Verify
	if len(ns.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(ns.Nodes))
	}

	// Verify sorting (1 then 2)
	if ns.Nodes[0].ID != 1 {
		t.Errorf("Expected first node ID 1, got %d", ns.Nodes[0].ID)
	}
	if ns.Nodes[1].ID != 2 {
		t.Errorf("Expected second node ID 2, got %d", ns.Nodes[1].ID)
	}

	// Verify Type presence (WorkerNode uses BaseNode default currently, or check internal type)
	// BaseNode export uses fmt.Sprintf("%T", n.handler).
	// WorkerNode handler is *node.WorkerNodeHandler (likely, or similar).
	// Let's just check it's not empty.
	if ns.Nodes[0].Type == "" {
		t.Error("Expected Node Type to be set")
	}
}
