package flowsim

// cpu_node.go 实现 CPU+Cache Node 适配器
//
// 将 ChampSim 的 O3CPU 和 L1D Cache 包装成 flow_sim 的 Node

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/capabilities/cache"
	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// CPUNodeHandler 实现 NodeHandler 接口
//
// 处理逻辑：
// 1. 接收来自DRAM的响应包，填充到Cache
// 2. 执行CPU的Tick，产生新的load/store请求
// 3. 从MemoryAdapter获取Cache miss请求，发送到DRAM
type CPUNodeHandler struct {
	// ChampSim 组件
	cpu      *cpu.O3CPU
	l1dCache *cache.SetAssociativeCache

	// Memory Adapter（连接Cache和网络）
	memoryAdapter *FlowSimMemoryAdapter

	// Node ID（用于创建Packet）
	nodeID int
	dramID int // DRAM节点的ID

	// 输出队列（发送到DRAM）
	outputQueue *queue.OutputQueue
}

// NewCPUNodeHandler 创建 CPU Node Handler
//
// 参数：
// - nodeID: CPU节点ID
// - dramID: DRAM节点ID
// - cpu: O3CPU实例
// - l1dCache: L1D Cache实例
// - memoryAdapter: FlowSim Memory Adapter（连接Cache和网络）
// - outputQueue: 输出队列（发送到DRAM）
func NewCPUNodeHandler(
	nodeID, dramID int,
	cpu *cpu.O3CPU,
	l1dCache *cache.SetAssociativeCache,
	memoryAdapter *FlowSimMemoryAdapter,
	outputQueue *queue.OutputQueue,
) *CPUNodeHandler {
	return &CPUNodeHandler{
		cpu:           cpu,
		l1dCache:      l1dCache,
		memoryAdapter: memoryAdapter,
		nodeID:        nodeID,
		dramID:        dramID,
		outputQueue:   outputQueue,
	}
}

// Process 实现 NodeHandler.Process
//
// 处理流程：
// 1. 处理输入队列（DRAM响应）
// 2. 执行CPU Tick
// 3. 从MemoryAdapter获取Cache miss请求并发送到DRAM
func (h *CPUNodeHandler) Process(cycle uint64, inputs [][]queue.PacketRef) error {
	// 1. 处理来自DRAM的响应
	if len(inputs) > 0 {
		for _, ref := range inputs[0] {
			if err := h.handleMemoryResponse(cycle, ref.Packet); err != nil {
				return fmt.Errorf("failed to handle memory response: %w", err)
			}
			// 释放输入队列槽位
			ref.Queue.Free(ref.Slot)
		}
	}

	// 2. 执行CPU Tick
	// CPU会访问Cache，Cache miss时会通过memoryAdapter记录请求
	h.cpu.Tick()

	// 3. 从MemoryAdapter获取pending requests并发送到DRAM
	if err := h.sendPendingRequests(cycle); err != nil {
		return fmt.Errorf("failed to send pending requests: %w", err)
	}

	return nil
}

// handleMemoryResponse 处理来自DRAM的内存响应
//
// 流程：
// 1. 解析响应包
// 2. 填充到Cache
// 3. 通知CPU load/store完成
func (h *CPUNodeHandler) handleMemoryResponse(cycle uint64, pkt packet.Packet) error {
	// 解析响应payload
	payload, err := ParseMemoryResponsePayload(pkt)
	if err != nil {
		return err
	}

	// 填充到Cache
	// 注意：HandleFill会从MSHR中移除对应条目
	h.l1dCache.HandleFill(payload.Address, payload.Data, cycle)

	// 通知CPU操作完成
	// 使用响应包中的InstrID
	h.cpu.HandleLoadResponse(payload.InstrID, cycle)

	return nil
}

// sendPendingRequests 发送待处理的请求到DRAM
//
// 从MemoryAdapter获取所有pending requests并发送到网络
func (h *CPUNodeHandler) sendPendingRequests(cycle uint64) error {
	// 从MemoryAdapter获取pending requests
	requests := h.memoryAdapter.GetPendingRequests()

	if len(requests) == 0 {
		return nil
	}

	// 将所有requests转换为Packet并发送
	packets := make([]packet.Packet, 0, len(requests))

	for _, req := range requests {
		pkt := NewMemoryRequestPacket(
			h.nodeID,
			h.dramID,
			req.Address,
			req.VAddress,
			req.InstrID,
			req.IsWrite,
			req.Data,
		)
		packets = append(packets, pkt)
	}

	// 注入到输出队列
	h.outputQueue.InjectPackets(int(cycle), packets)

	return nil
}

// GetCPUStats 返回 CPU 统计信息
func (h *CPUNodeHandler) GetCPUStats() cpu.O3CPUStats {
	return h.cpu.GetStats()
}

// GetCacheStats 返回 Cache 统计信息
func (h *CPUNodeHandler) GetCacheStats() interface{} {
	return h.l1dCache.GetStats()
}

// ExportStats implements StatsExporter interface.
// Returns runtime statistics matching OpenAPI CPUConfig schema fields.
func (h *CPUNodeHandler) ExportStats() map[string]interface{} {
	cpuStats := h.cpu.GetStats()
	cacheStats := h.l1dCache.GetStats()

	stats := map[string]interface{}{
		// CPU 核心统计
		"ipc":                  cpuStats.IPC,
		"total_instructions":   cpuStats.TotalInstructions,
		"total_cycles":         cpuStats.TotalCycles,
		"branch_mispredictions": cpuStats.BranchMispredictions,
		"total_branches":       cpuStats.TotalBranches,

		// 流水线停顿统计
		"fetch_stalls":    cpuStats.FetchStalls,
		"decode_stalls":   cpuStats.DecodeStalls,
		"dispatch_stalls": cpuStats.DispatchStalls,
		"execute_stalls":  cpuStats.ExecuteStalls,

		// L1D Cache 统计
		"l1d_cache_stats": cacheStats,
	}

	return stats
}
