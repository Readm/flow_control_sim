package network

import (
	"sort"

	"github.com/Readm/flow_sim/internal/core/state"
)

// ExportState exports the state of the Network.
func (n *Network) ExportState(cfg state.ExportConfig) state.NetworkState {
	// Simple assumption: Static time export, so no lock on network structure
	// but components might need internal locks (handled by their ExportState)

	ns := state.NetworkState{
		CurrentCycle: n.currentCycle,
		Nodes:        make([]state.NodeState, 0, len(n.nodes)),
		Links:        make([]state.LinkState, 0, len(n.links)),
	}

	// Export Nodes
	for _, handle := range n.nodes {
		// handle.Node is an interface. We try to assert it implements Exporter.
		if exporter, ok := handle.Node.(state.Exporter[state.NodeState]); ok {
			ns.Nodes = append(ns.Nodes, exporter.ExportState(cfg))
		} else {
			// Fallback placeholder
			ns.Nodes = append(ns.Nodes, state.NodeState{
				ID:   handle.Node.ID(),
				Type: "Unknown (No ExportState)",
			})
		}
	}

	// Sort nodes by ID for deterministic output
	sort.Slice(ns.Nodes, func(i, j int) bool {
		return ns.Nodes[i].ID < ns.Nodes[j].ID
	})

	// Export Links
	for _, l := range n.links {
		// l is *link.Link, which implements ExportState directly
		ns.Links = append(ns.Links, l.ExportState(cfg))
	}

	// Sort links (optional, maybe by SourceID then TargetID)
	// Currently not enforcing sort, but good for output stability
	sort.Slice(ns.Links, func(i, j int) bool {
		if ns.Links[i].SourceID != ns.Links[j].SourceID {
			return ns.Links[i].SourceID < ns.Links[j].SourceID
		}
		return ns.Links[i].TargetID < ns.Links[j].TargetID
	})

	return ns
}
