package node

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/configconv"
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
func extractCPUConfig(stats map[string]interface{}) map[string]interface{} {
	config := make(map[string]interface{})

	// CPU核心统计
	if v, ok := stats["ipc"]; ok {
		config["ipc"] = v
	}
	if v, ok := stats["total_instructions"]; ok {
		config["total_instructions"] = v
	}
	if v, ok := stats["total_cycles"]; ok {
		config["total_cycles"] = v
	}
	if v, ok := stats["branch_mispredictions"]; ok {
		config["branch_mispredictions"] = v
	}
	if v, ok := stats["total_branches"]; ok {
		config["total_branches"] = v
	}

	// 流水线停顿
	if v, ok := stats["fetch_stalls"]; ok {
		config["fetch_stalls"] = v
	}
	if v, ok := stats["decode_stalls"]; ok {
		config["decode_stalls"] = v
	}
	if v, ok := stats["dispatch_stalls"]; ok {
		config["dispatch_stalls"] = v
	}
	if v, ok := stats["execute_stalls"]; ok {
		config["execute_stalls"] = v
	}

	// L1D Cache统计
	if v, ok := stats["l1d_cache_stats"]; ok {
		config["l1d_cache_stats"] = v
	}

	return config
}

// extractMemoryConfig extracts memory-related fields from Stats into a MemoryConfig map
func extractMemoryConfig(stats map[string]interface{}) map[string]interface{} {
	config := make(map[string]interface{})

	// 请求统计
	if v, ok := stats["read_requests"]; ok {
		config["read_requests"] = v
	}
	if v, ok := stats["write_requests"]; ok {
		config["write_requests"] = v
	}

	// Row Buffer统计
	if v, ok := stats["row_buffer_hits"]; ok {
		config["row_buffer_hits"] = v
	}
	if v, ok := stats["row_buffer_misses"]; ok {
		config["row_buffer_misses"] = v
	}

	// 详细统计
	if v, ok := stats["rq_row_buffer_hits"]; ok {
		config["rq_row_buffer_hits"] = v
	}
	if v, ok := stats["rq_row_buffer_misses"]; ok {
		config["rq_row_buffer_misses"] = v
	}
	if v, ok := stats["wq_row_buffer_hits"]; ok {
		config["wq_row_buffer_hits"] = v
	}
	if v, ok := stats["wq_row_buffer_misses"]; ok {
		config["wq_row_buffer_misses"] = v
	}

	// Memory Controller 统计
	if v, ok := stats["total_requests"]; ok {
		config["total_requests"] = v
	}
	if v, ok := stats["responses"]; ok {
		config["responses"] = v
	}
	if v, ok := stats["requests_per_channel"]; ok {
		config["requests_per_channel"] = v
	}

	return config
}

