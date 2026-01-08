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

	// Phase 2: 从 configRef 读取 Config 和 Display 数据
	if n.configRef != nil {
		// Copy Features from configRef
		if n.configRef.Cache != nil {
			cacheConfig := map[string]interface{}{
				"capacity":           n.configRef.Cache.Capacity,
				"num_sets":           n.configRef.Cache.NumSets,
				"replacement_policy": string(n.configRef.Cache.ReplacementPolicy),
				"states":             n.configRef.Cache.States,
			}
			ns.Features["cache"] = cacheConfig
		}

		if n.configRef.Directory != nil {
			directoryConfig := map[string]interface{}{
				"capacity":           n.configRef.Directory.Capacity,
				"num_sets":           n.configRef.Directory.NumSets,
				"replacement_policy": n.configRef.Directory.ReplacementPolicy,
				"states":             n.configRef.Directory.States,
			}
			ns.Features["directory"] = directoryConfig
		}

		// Copy DisplayData from configRef
		ns.DisplayData["position"] = n.configRef.Position
		ns.DisplayData["data"] = n.configRef.Data
		if n.configRef.Style != nil {
			ns.DisplayData["style"] = *n.configRef.Style
		}

		// Copy CoherenceDomainID from configRef
		ns.CoherenceDomainID = n.configRef.CoherenceDomainId
	}
	// Phase 2: 移除 fallback 分支,因为 features/displayData/coherenceDomainID 字段已删除
	// 所有数据现在都必须通过 configRef 提供

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
