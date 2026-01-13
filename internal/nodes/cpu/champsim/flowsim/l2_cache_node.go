package flowsim

// l2_cache_node.go 实现支持 MESI 协议的共享 L2 Cache 节点

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/capabilities/cache"
	compcache "github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// L2CacheNodeHandler 实现共享 L2 Cache 的 NodeHandler
// 支持多个上游 L1 Cache 和 MESI 协议
type L2CacheNodeHandler struct {
	// L2 Cache 实例
	l2Cache *cache.SetAssociativeCache

	// MESI 协议控制器
	mesiController *cache.MESIController

	// Node ID
	nodeID int

	// 上游 CPU/L1 节点的 IDs
	cpuNodeIDs []int

	// 下游 Memory Controller 的 ID
	memCtrlID int

	// 输出队列（发送到 CPUs 和 Memory Controller）
	// outputQueues[i] 对应 cpuNodeIDs[i]
	// outputQueues[len(cpuNodeIDs)] 对应 memCtrlID
	outputQueues []*queue.OutputQueue

	// 统计信息
	stats L2CacheStats

	// Pending coherence transactions
	// address -> list of pending requestor IDs
	pendingInvalidates map[uint64][]int
}

// L2CacheStats L2 Cache 统计信息
type L2CacheStats struct {
	Accesses        uint64
	Hits            uint64
	Misses          uint64
	CoherenceStats  cache.CoherenceStats
	InvalidatesSent uint64
	Writebacks      uint64
}

// NewL2CacheNodeHandler 创建 L2 Cache Node Handler
func NewL2CacheNodeHandler(
	nodeID int,
	cpuNodeIDs []int,
	memCtrlID int,
	l2Cache *cache.SetAssociativeCache,
	outputQueues []*queue.OutputQueue,
) *L2CacheNodeHandler {
	return &L2CacheNodeHandler{
		l2Cache:            l2Cache,
		mesiController:     cache.NewMESIController(),
		nodeID:             nodeID,
		cpuNodeIDs:         cpuNodeIDs,
		memCtrlID:          memCtrlID,
		outputQueues:       outputQueues,
		pendingInvalidates: make(map[uint64][]int),
	}
}

// Process 处理输入包
// inputs[0..n-1]: 来自各个 CPU 的请求
// inputs[n]: 来自 Memory Controller 的响应
func (h *L2CacheNodeHandler) Process(cycle uint64, inputs [][]queue.PacketRef) error {
	// 1. 处理来自 Memory Controller 的响应
	memCtrlInputIndex := len(h.cpuNodeIDs)
	for _, ref := range inputs[memCtrlInputIndex] {
		if err := h.handleMemoryResponse(cycle, ref.Packet); err != nil {
			return err
		}
		ref.Queue.Free(ref.Slot)
	}

	// 2. 处理来自各个 CPU 的请求
	for cpuIndex, cpuInputs := range inputs[:len(h.cpuNodeIDs)] {
		cpuNodeID := h.cpuNodeIDs[cpuIndex]
		for _, ref := range cpuInputs {
			if err := h.handleCPURequest(cycle, cpuNodeID, cpuIndex, ref.Packet); err != nil {
				return err
			}
			ref.Queue.Free(ref.Slot)
		}
	}

	return nil
}

// handleCPURequest 处理来自 CPU 的请求
func (h *L2CacheNodeHandler) handleCPURequest(cycle uint64, cpuNodeID int, cpuIndex int, pkt packet.Packet) error {
	// 1. Check type
	if pkt.Type != PacketTypeMemoryRequest {
		return fmt.Errorf("invalid packet type for L2: %d", pkt.Type)
	}

	h.stats.Accesses++

	// 确定访问类型
	accessType := compcache.AccessLoad // Read
	if pkt.Op == OpWrite {
		accessType = compcache.AccessStore // Write
	}

	// 检查 L2 Cache 是否命中
	hit, _, _ := h.l2Cache.Access(pkt.Addr, pkt.VAddr, pkt.InstrID, accessType, cycle)

	// 简化：假设总是能读取到数据（实际应该从 cache 读取）
	data := pkt.Data

	if hit {
		h.stats.Hits++

		// L2 命中，但还需要检查 coherence
		coherenceMsg := cache.CoherenceMessage{
			Type:        cache.CoherenceRead,
			Address:     pkt.Addr,
			RequestorID: cpuIndex,
		}
		if pkt.Op == OpWrite {
			coherenceMsg.Type = cache.CoherenceWrite
		}

		// 处理 coherence
		coherenceMsgs := h.mesiController.HandleRequest(coherenceMsg)
		if err := h.sendCoherenceMessages(cycle, coherenceMsgs); err != nil {
			return err
		}

		// 如果是写操作，可能需要 invalidate 其他核心
		if pkt.Op == OpWrite {
			h.stats.InvalidatesSent += uint64(len(coherenceMsgs))
		}

		// 发送响应给请求的 CPU
		return h.sendResponseToCPU(cycle, cpuIndex, pkt.Addr, data, pkt.InstrID)
	}

	// L2 Miss - 需要从内存获取
	h.stats.Misses++

	// 发送请求到 Memory Controller
	// We pass the packet itself which contains all info
	return h.sendRequestToMemory(cycle, pkt)
}

// handleMemoryResponse 处理来自 Memory 的响应
func (h *L2CacheNodeHandler) handleMemoryResponse(cycle uint64, pkt packet.Packet) error {
	// 1. Check type
	if pkt.Type != PacketTypeMemoryResponse {
		return fmt.Errorf("invalid response type for L2: %d", pkt.Type)
	}

	// 填充到 L2 Cache
	h.l2Cache.HandleFill(pkt.Addr, pkt.Data, cycle)

	// 转发给请求的 CPU
	// TODO: 需要追踪哪个 CPU 请求了这个地址
	// 这里简化处理，假设 InstrID 编码了 CPU ID
	cpuIndex := 0 // 简化：总是发送给 CPU0，实际应该追踪原始请求者

	return h.sendResponseToCPU(cycle, cpuIndex, pkt.Addr, pkt.Data, pkt.InstrID)
}

// sendResponseToCPU 发送响应给 CPU
func (h *L2CacheNodeHandler) sendResponseToCPU(cycle uint64, cpuIndex int, address, data, instrID uint64) error {
	responsePkt := NewMemoryResponsePacket(
		h.nodeID,
		h.cpuNodeIDs[cpuIndex],
		address,
		data,
		instrID,
		cycle,
	)

	h.outputQueues[cpuIndex].InjectPackets(int(cycle), []packet.Packet{responsePkt})
	return nil
}

// sendRequestToMemory 发送请求到 Memory Controller
func (h *L2CacheNodeHandler) sendRequestToMemory(cycle uint64, pkt packet.Packet) error {
	isWrite := (pkt.Op == OpWrite)

	requestPkt := NewMemoryRequestPacket(
		h.nodeID,
		h.memCtrlID,
		pkt.Addr,
		pkt.VAddr,
		pkt.InstrID,
		isWrite,
		pkt.Data,
	)

	memCtrlQueueIndex := len(h.cpuNodeIDs)
	h.outputQueues[memCtrlQueueIndex].InjectPackets(int(cycle), []packet.Packet{requestPkt})
	return nil
}

// sendCoherenceMessages 发送 coherence 消息到其他 CPU
func (h *L2CacheNodeHandler) sendCoherenceMessages(cycle uint64, msgs []cache.CoherenceMessage) error {
	for _, msg := range msgs {
		if msg.Type == cache.CoherenceInvalidate {
			// 发送 Invalidate 消息到对应的 CPU
			// 这里需要将 coherence message 转换为 packet
			// 简化实现：可以复用 MemoryRequest/Response 结构，或者定义新的 CoherencePacket
			// TODO: 实现 coherence packet 类型
		}
	}
	return nil
}

// GetStats 获取统计信息
func (h *L2CacheNodeHandler) GetStats() L2CacheStats {
	return h.stats
}

// GetCacheStats 获取底层 Cache 统计
func (h *L2CacheNodeHandler) GetCacheStats() interface{} {
	return h.l2Cache.GetStats()
}

// ExportStats implements StatsExporter interface.
// Returns L2 cache statistics.
func (h *L2CacheNodeHandler) ExportStats() map[string]interface{} {
	cacheStats := h.l2Cache.GetStats()

	stats := map[string]interface{}{
		"accesses":         h.stats.Accesses,
		"hits":             h.stats.Hits,
		"misses":           h.stats.Misses,
		"invalidates_sent": h.stats.InvalidatesSent,
		"writebacks":       h.stats.Writebacks,
		"l2_cache_stats":   cacheStats,
		"coherence_stats":  h.stats.CoherenceStats,
	}

	return stats
}
