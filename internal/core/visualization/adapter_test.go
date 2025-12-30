package visualization_test

import (
	"encoding/json"
	"testing"

	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/core/visualization"
)

func TestStateToCyNetwork(t *testing.T) {
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
				Occupancy: []int{0, 1, 0, 5},
			},
		},
	}

	// 2. Convert to Frontend Protocol
	cyNet := visualization.StateToCyNetwork(ns)

	// 3. Verify Structure
	if len(cyNet.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(cyNet.Nodes))
	}
	if len(cyNet.Edges) != 1 {
		t.Errorf("Expected 1 edge, got %d", len(cyNet.Edges))
	}

	// 4. Verify Node Details
	node1 := cyNet.Nodes[0]
	if node1.NodeID != 1 {
		t.Errorf("Node1 ID mismatch: %d", node1.NodeID)
	}
	if node1.Display.Bg != "#1890FF" {
		t.Errorf("Node1 Color mismatch: %s", node1.Display.Bg)
	}
	// Check layout generation (basic check)
	if node1.Display.Position.X == 0 && node1.Display.Position.Y == 0 {
		t.Log("Warning: Node1 position is 0,0 (might be coincidentally correct or uninitialized)")
	}

	// 5. Verify Edge LinkStatus (Traffic)
	edge1 := cyNet.Edges[0]
	if len(edge1.Display.LinkStatus) == 0 {
		t.Errorf("Edge LinkStatus empty")
	} else {
		status := edge1.Display.LinkStatus[0]
		if status.Name != "occupancy" {
			t.Errorf("LinkStatus name expected 'occupancy', got '%s'", status.Name)
		}
		if len(status.Values) != 4 || status.Values[3] != 5 {
			t.Errorf("LinkStatus values mismatch: %v", status.Values)
		}
	}

	// 6. JSON Serialization Check
	bytes, err := json.MarshalIndent(cyNet, "", "  ")
	if err != nil {
		t.Fatalf("JSON Marshal failed: %v", err)
	}
	t.Logf("JSON Output:\n%s", string(bytes))
}
