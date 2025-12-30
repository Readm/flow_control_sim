package visualization

import (
	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

// CyNetworkToConfig converts a visualization network protocol object into
// a simulation configuration object.
func CyNetworkToConfig(cyNet protocol.CyNetwork) config.EntityConfig {
	cfg := config.EntityConfig{
		Nodes: make([]config.NodeConfig, 0, len(cyNet.Nodes)),
		Edges: make([]config.EdgeConfig, 0, len(cyNet.Edges)),
		Link: config.LinkConfig{
			// Defaults, can be parametrized later if CyNetwork includes them
			Multiplier: 1,
		},
	}

	for _, n := range cyNet.Nodes {
		nodeType := "WorkerNode" // Default
		// Map features to type. This is a simplification.
		// If "CentralSwitch" is in features (or name), treat as such
		if len(n.NodeFeatures) > 0 {
			if contains(n.NodeFeatures, "CentralSwitch") {
				nodeType = "CentralSwitch"
			} else {
				// If specific type is stored in features, use it
				nodeType = n.NodeFeatures[0]
			}
		}

		cfg.Nodes = append(cfg.Nodes, config.NodeConfig{
			ID:   n.NodeID,
			Type: nodeType,
		})
	}

	for _, e := range cyNet.Edges {
		cfg.Edges = append(cfg.Edges, config.EdgeConfig{
			Src: e.SrcNodeID,
			Dst: e.DstNodeID,
		})
	}

	return cfg
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
