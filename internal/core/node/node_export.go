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
		Stats:        make(map[string]interface{}),
		Features:     make(map[string]map[string]interface{}),
		DisplayData:  make(map[string]interface{}),
		// 兼容性字段（将在后续版本移除）
		Caches:      make([]state.CacheState, 0, len(n.caches)),
		Directories: make([]state.DirectoryState, 0, len(n.directories)),
		CustomData:  make(map[string]interface{}),
	}

	// Copy CustomData（兼容性）
	for k, v := range n.data {
		ns.CustomData[k] = v
	}

	// Copy Features
	for k, v := range n.features {
		ns.Features[k] = v
	}

	// Copy DisplayData
	for k, v := range n.displayData {
		ns.DisplayData[k] = v
	}

	// Copy CoherenceDomainID
	ns.CoherenceDomainID = n.coherenceDomainID

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

	// Export Caches to Stats
	if len(n.caches) > 0 {
		cacheStats := make([]state.CacheState, 0, len(n.caches))
		for _, c := range n.caches {
			if exporter, ok := c.(state.Exporter[state.CacheState]); ok {
				cacheState := exporter.ExportState(cfg)
				cacheStats = append(cacheStats, cacheState)
				// 兼容性：同时填充到旧字段
				ns.Caches = append(ns.Caches, cacheState)
			} else {
				ns.Caches = append(ns.Caches, state.CacheState{})
			}
		}
		ns.Stats["cache"] = cacheStats
	}

	// Export Directories to Stats
	if len(n.directories) > 0 {
		directoryStats := make([]state.DirectoryState, 0, len(n.directories))
		for _, d := range n.directories {
			if exporter, ok := d.(state.Exporter[state.DirectoryState]); ok {
				directoryState := exporter.ExportState(cfg)
				directoryStats = append(directoryStats, directoryState)
				// 兼容性：同时填充到旧字段
				ns.Directories = append(ns.Directories, directoryState)
			} else {
				ns.Directories = append(ns.Directories, state.DirectoryState{})
			}
		}
		ns.Stats["directory"] = directoryStats
	}

	return ns
}
