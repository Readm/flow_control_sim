package visualization_test

import (
	"encoding/json"
	"testing"

	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/core/visualization"
)

func TestStateToFlowSimNetwork(t *testing.T) {
	// 1. Construct Mock Backend State
	ns := state.NetworkState{
		CurrentCycle: 100,
		Nodes: []state.NodeState{
			{ID: 1, Type: "WorkerNode"},
			{ID: 2, Type: "HubNode"},
		},
		Links: []state.LinkState{
			{
				SourceID:  1,
				TargetID:  2,
				Latency:   10,
				Bandwidth: 1,
				Occupancy: []int{0, 1, 0, 5},
			},
		},
	}

	// 2. Convert to Frontend Protocol (FlowSimNetwork)
	flowNet := visualization.StateToFlowSimNetwork(ns)

	// 3. Verify Structure
	if len(flowNet.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(flowNet.Nodes))
	}
	if len(flowNet.Edges) != 1 {
		t.Errorf("Expected 1 edge, got %d", len(flowNet.Edges))
	}

	// 4. Verify Cycle
	if flowNet.Cycle == nil || *flowNet.Cycle != 100 {
		t.Errorf("Expected cycle 100, got %v", flowNet.Cycle)
	}

	// 5. Verify Node Details
	node1 := flowNet.Nodes[0]
	if node1.NodeId != 1 {
		t.Errorf("Node1 ID mismatch: %d", node1.NodeId)
	}
	if node1.Data.Id == "" {
		t.Errorf("Node1 data.id is empty")
	}
	// Check layout generation (basic check)
	if node1.Position.X == 0 && node1.Position.Y == 0 {
		t.Log("Warning: Node1 position is 0,0 (might be coincidentally correct or uninitialized)")
	}

	// 6. Verify Edge LinkStatus (Traffic)
	edge1 := flowNet.Edges[0]
	if edge1.LinkStatus == nil || len(*edge1.LinkStatus) == 0 {
		t.Errorf("Edge LinkStatus empty")
	} else {
		status := (*edge1.LinkStatus)[0]
		if status.Name != "occupancy" {
			t.Errorf("LinkStatus name expected 'occupancy', got '%s'", status.Name)
		}
		if len(status.Values) != 4 || status.Values[3] != 5 {
			t.Errorf("LinkStatus values mismatch: %v", status.Values)
		}
	}

	// 7. JSON Serialization Check
	bytes, err := json.MarshalIndent(flowNet, "", "  ")
	if err != nil {
		t.Fatalf("JSON Marshal failed: %v", err)
	}
	t.Logf("JSON Output:\n%s", string(bytes))
}
