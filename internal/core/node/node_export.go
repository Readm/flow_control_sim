package node

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/core/state"
)

// ExportState exports the state of the Node.
func (n *BaseNode) ExportState(cfg state.ExportConfig) state.NodeState {
	n.dataMu.RLock()
	defer n.dataMu.RUnlock()

	ns := state.NodeState{
		ID:           n.id,
		Type:         fmt.Sprintf("%T", n.handler), // Use handler type as node type
		CurrentCycle: int(n.currentCycle),
		Inputs:       make([]state.QueueState, 0, len(n.inputs)),
		Outputs:      make([]state.QueueState, 0, len(n.outputs)),
		Caches:       make([]state.CacheState, 0, len(n.caches)),
		Directories:  make([]state.DirectoryState, 0, len(n.directories)),
		CustomData:   make(map[string]interface{}),
	}

	// Copy CustomData
	for k, v := range n.data {
		ns.CustomData[k] = v
	}

	// Export Input Queues
	for _, input := range n.inputs {
		if exporter, ok := input.(state.Exporter[state.QueueState]); ok {
			ns.Inputs = append(ns.Inputs, exporter.ExportState(cfg))
		} else {
			// Fallback or empty if not exportable
			ns.Inputs = append(ns.Inputs, state.QueueState{Type: "Input (Unknown)"})
		}
	}

	// Export Output Queues
	for _, output := range n.outputs {
		if exporter, ok := output.(state.Exporter[state.QueueState]); ok {
			ns.Outputs = append(ns.Outputs, exporter.ExportState(cfg))
		} else {
			ns.Outputs = append(ns.Outputs, state.QueueState{Type: "Output (Unknown)"})
		}
	}

	// Export Caches
	for _, c := range n.caches {
		if exporter, ok := c.(state.Exporter[state.CacheState]); ok {
			ns.Caches = append(ns.Caches, exporter.ExportState(cfg))
		} else {
			ns.Caches = append(ns.Caches, state.CacheState{})
		}
	}

	// Export Directories
	for _, d := range n.directories {
		if exporter, ok := d.(state.Exporter[state.DirectoryState]); ok {
			ns.Directories = append(ns.Directories, exporter.ExportState(cfg))
		} else {
			ns.Directories = append(ns.Directories, state.DirectoryState{})
		}
	}

	return ns
}
