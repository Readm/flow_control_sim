package node

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/configconv"
	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
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

	// Export Handler-specific Stats (Phase 4: Unified Interface)
	// If handler implements StatsExporter, merge its stats into NodeState.Stats
	if statsExporter, ok := n.handler.(StatsExporter); ok {
		handlerStats := statsExporter.ExportStats()
		for k, v := range handlerStats {
			ns.Stats[k] = v
		}
	}

	// Phase 5: Export NodeType and specialized configs
	// Infer node type from handler type name
	handlerType := fmt.Sprintf("%T", n.handler)
	switch {
	case contains(handlerType, "CPUNodeHandler"):
		nodeType := "cpu"
		ns.NodeType = &nodeType
		// Collect CPU-specific stats into CPUConfig
		ns.CPUConfig = extractCPUConfig(ns.Stats)
	case contains(handlerType, "DRAMNodeHandler"), contains(handlerType, "MemoryControllerHandler"):
		nodeType := "memory_controller"
		ns.NodeType = &nodeType
		// Collect memory-specific stats into MemoryConfig
		ns.MemoryConfig = extractMemoryConfig(ns.Stats)
	case contains(handlerType, "L2CacheNodeHandler"):
		// L2 Cache is a generic node with cache capability
		nodeType := "generic"
		ns.NodeType = &nodeType
	default:
		// Generic node
		nodeType := "generic"
		ns.NodeType = &nodeType
	}

	// Phase 5: Also read from configRef if available (configuration, not just stats)
	if n.configRef != nil {
		if n.configRef.NodeType != nil {
			nodeTypeStr := string(*n.configRef.NodeType)
			ns.NodeType = &nodeTypeStr
		}

		// Merge configuration from configRef with stats
		// 配置参数（如 rob_size, TCAS）从 configRef 读取
		// 统计数据（如 total_instructions, read_requests）从 stats 读取
		if n.configRef.CpuConfig != nil && len(ns.CPUConfig) > 0 {
			// Merge config with stats using generic converter
			ns.CPUConfig = configconv.MergeMaps(
				configconv.StructToMap(n.configRef.CpuConfig),
				ns.CPUConfig,
			)
		} else if n.configRef.CpuConfig != nil {
			ns.CPUConfig = configconv.StructToMap(n.configRef.CpuConfig)
		}

		if n.configRef.MemoryConfig != nil && len(ns.MemoryConfig) > 0 {
			// Merge config with stats using generic converter
			ns.MemoryConfig = configconv.MergeMaps(
				configconv.StructToMap(n.configRef.MemoryConfig),
				ns.MemoryConfig,
			)
		} else if n.configRef.MemoryConfig != nil {
			ns.MemoryConfig = configconv.StructToMap(n.configRef.MemoryConfig)
		}
	}

	return ns
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// extractCPUConfig extracts CPU-related fields from Stats into a CPUConfig map
// extractCPUConfig extracts CPU-related fields from Stats into a CPUConfig map
func extractCPUConfig(stats map[string]interface{}) map[string]interface{} {
	// Use configconv to filter fields defined in protocol.CPUConfig
	var config protocol.CPUConfig

	// 1. Filter valid fields into struct (ignore unknown fields)
	if err := configconv.MapToStruct(stats, &config); err != nil {
		// Should not happen for loose map -> struct conversion
		return make(map[string]interface{})
	}

	// 2. Convert back to map (this ensures only defined fields are exported)
	return configconv.StructToMap(&config)
}

// extractMemoryConfig extracts memory-related fields from Stats into a MemoryConfig map
func extractMemoryConfig(stats map[string]interface{}) map[string]interface{} {
	// Use configconv to filter fields defined in protocol.MemoryConfig
	var config protocol.MemoryConfig

	// 1. Filter valid fields into struct
	if err := configconv.MapToStruct(stats, &config); err != nil {
		return make(map[string]interface{})
	}

	// 2. Convert back to map
	return configconv.StructToMap(&config)
}
