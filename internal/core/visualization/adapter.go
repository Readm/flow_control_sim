package visualization

import (
	"fmt"
	"math"

	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

// StateToCyNetwork converts the backend NetworkState into the frontend's CyNetwork protocol format.
func StateToCyNetwork(ns state.NetworkState) protocol.CyNetwork {
	cyNet := protocol.CyNetwork{
		Version: "1.0.0",
		Nodes:   make([]protocol.CyNode, 0, len(ns.Nodes)),
		Edges:   make([]protocol.CyEdge, 0, len(ns.Links)),
		Cycle:   ns.CurrentCycle,
	}

	// Helper for layout
	nodeCount := len(ns.Nodes)
	radius := 200.0
	centerX, centerY := 400.0, 300.0

	// Map Nodes
	nodeIDMap := make(map[int]bool) // For fast lookup if needed
	for i, n := range ns.Nodes {
		nodeIDMap[n.ID] = true

		// Simple Circular Layout
		angle := 2 * math.Pi * float64(i) / float64(nodeCount)
		x := centerX + radius*math.Cos(angle)
		y := centerY + radius*math.Sin(angle)

		cyNode := protocol.CyNode{
			NodeID:       n.ID,
			NodeName:     fmt.Sprintf("Node %d", n.ID),
			NodeFeatures: []string{n.Type},
			Display: protocol.CyNodeDisplay{
				ID:     fmt.Sprintf("node-%d", n.ID),
				Type:   "round-rectangle", // Default shape
				Name:   fmt.Sprintf("N%d", n.ID),
				Resize: true,
				Width:  60,
				Height: 60,
				Bg:     getNodeColor(n.Type),
				Position: protocol.CyPosition{
					X: x,
					Y: y,
				},
			},
		}

		// Optional: Map Queues to InPorts/OutPorts or descriptions if needed
		// For now, we stick to basic topology.

		cyNet.Nodes = append(cyNet.Nodes, cyNode)
	}

	// Map Edges (Links)
	for i, l := range ns.Links {
		// Ensure source/target nodes exist (they should)
		if !nodeIDMap[l.SourceID] || !nodeIDMap[l.TargetID] {
			continue // Skip partial links from export filtering
		}

		cyEdge := protocol.CyEdge{
			EdgeID:    i + 1, // Generate a 1-based ID
			SrcNodeID: l.SourceID,
			SrcPortID: 0, // Default
			DstNodeID: l.TargetID,
			DstPortID: 0, // Default
			Display: protocol.CyEdgeDisplay{
				Data: protocol.CyEdgeData{
					ID:       fmt.Sprintf("edge-%d-%d", l.SourceID, l.TargetID),
					Source:   fmt.Sprintf("node-%d", l.SourceID),
					Target:   fmt.Sprintf("node-%d", l.TargetID),
					LineType: "bezier",
				},
				LinkStatus: []protocol.LinkStatus{
					{
						Name:   "occupancy",
						Values: l.Occupancy,
					},
				},
			},
		}
		cyNet.Edges = append(cyNet.Edges, cyEdge)
	}

	return cyNet
}

func getNodeColor(nodeType string) string {
	switch nodeType {
	case "WorkerNode":
		return "#1890FF" // Blue
	case "HubNode", "CentralSwitch":
		return "#5CDBD3" // Cyan
	default:
		return "#999999" // Grey
	}
}
