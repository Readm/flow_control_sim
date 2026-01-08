package builder

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

func TestBuildFromFlowSimNetworkBasic(t *testing.T) {
	t.Parallel()

	// Create network with one node
	flow := NewTestNetworkBuilder().
		AddNode(0, 1, 1). // Node 0: 1 input, 1 output
		BuildFlowSimNetwork()

	net, err := BuildFromFlowSimNetwork(flow)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Export state to verify
	cfg := state.ExportConfig{DetailLevel: state.DetailLevelSummary}
	ns := net.ExportState(cfg)

	// Verify node was created
	if len(ns.Nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(ns.Nodes))
	}

	if ns.Nodes[0].ID != 0 {
		t.Fatalf("Expected node ID 0, got %d", ns.Nodes[0].ID)
	}

	// Verify input queue
	if len(ns.Nodes[0].Inputs) != 1 {
		t.Fatalf("Expected 1 input, got %d", len(ns.Nodes[0].Inputs))
	}
	if ns.Nodes[0].Inputs[0].Capacity != 16 {
		t.Fatalf("Expected input capacity 16, got %d", ns.Nodes[0].Inputs[0].Capacity)
	}

	// Verify output queue
	if len(ns.Nodes[0].Outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(ns.Nodes[0].Outputs))
	}
}

func TestBuildFromFlowSimNetworkTwoNodes(t *testing.T) {
	t.Parallel()

	// Build network with two nodes and connection
	flow := NewTestNetworkBuilder().
		AddNode(1, 1, 1). // Node 1: 1 input, 1 output
		AddNode(2, 1, 0). // Node 2: 1 input, 0 output
		AddEdge(1, 1, 0, 2, 0). // Connect Node 1 -> Node 2
		BuildFlowSimNetwork()

	net, err := BuildFromFlowSimNetwork(flow)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	cfg := state.ExportConfig{DetailLevel: state.DetailLevelSummary}
	ns := net.ExportState(cfg)

	// Verify nodes were created
	if len(ns.Nodes) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(ns.Nodes))
	}

	// Verify link was created
	if len(ns.Links) != 1 {
		t.Fatalf("Expected 1 link, got %d", len(ns.Links))
	}

	// Verify link connects correct nodes
	if ns.Links[0].SourceID != 1 || ns.Links[0].TargetID != 2 {
		t.Fatalf("Link should connect 1->2, got %d->%d", ns.Links[0].SourceID, ns.Links[0].TargetID)
	}
}

func TestBuildFromFlowSimNetworkInvalidPort(t *testing.T) {
	t.Parallel()

	// Schema with invalid port ID
	flowNet := protocol.FlowSimNetwork{
		Nodes: []protocol.Node{
			{
				NodeId:   0,
				NodeName: "Node_0",
				Data: protocol.Node_Data{Id: "node-0"},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: 100, Y: 100},
				InPorts: &[]protocol.Port{{PortId: 0, Bandwidth: 1}},
			},
		},
		Edges: []protocol.Edge{
			{
				EdgeId:    1,
				SrcNodeId: 0,
				SrcPortId: intPtr(0),
				DstNodeId: 0,
				DstPortId: intPtr(99), // Invalid
				Data:      protocol.Edge_Data{Id: "edge-1", Source: "node-0", Target: "node-0"},
			},
		},
	}

	_, err := BuildFromFlowSimNetwork(flowNet)
	if err == nil {
		t.Fatalf("Expected error for invalid destination port")
	}
}

func TestBuildFromFlowSimNetworkWithCache(t *testing.T) {
	t.Parallel()

	flow := protocol.FlowSimNetwork{
		Nodes: []protocol.Node{
			{
				NodeId:   0,
				NodeName: "Node_0",
				Data:     protocol.Node_Data{Id: "node-0"},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: 100, Y: 100},
				InPorts:  &[]protocol.Port{{PortId: 0, Bandwidth: 1}},
				OutPorts: &[]protocol.Port{{PortId: 0, Bandwidth: 1}},
				Cache: &protocol.CacheConfig{
					Capacity:          1024,
					NumSets:           16,
					ReplacementPolicy: protocol.LRU,
					States:            "MESI",
				},
			},
		},
		Edges: []protocol.Edge{},
	}

	net, err := BuildFromFlowSimNetwork(flow)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	cfg := state.ExportConfig{DetailLevel: state.DetailLevelSummary}
	ns := net.ExportState(cfg)

	// Verify cache was created
	if ns.Nodes[0].Stats["cache"] == nil {
		t.Fatalf("Expected cache stats to be present")
	}
}

func TestBuildFromFlowSimNetworkAndSimulate(t *testing.T) {
	t.Parallel()

	// Build network: 0 -> 1
	flow := NewTestNetworkBuilder().
		AddNode(0, 0, 1).
		AddNode(1, 1, 0).
		AddEdge(1, 0, 0, 1, 0).
		BuildFlowSimNetwork()

	net, err := BuildFromFlowSimNetwork(flow)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Inject packet (需要访问内部节点，这里简化测试)
	// 直接运行仿真
	if err := net.AdvanceTo(10); err != nil {
		t.Fatalf("AdvanceTo failed: %v", err)
	}

	// Verify cycle advanced (AdvanceTo may advance one more cycle)
	if net.CurrentCycle() < 10 {
		t.Fatalf("Expected cycle >= 10, got %d", net.CurrentCycle())
	}
}

// Helper function
func intPtr(i int) *int {
	return &i
}
