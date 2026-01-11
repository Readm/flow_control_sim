package flowsim

// dram_node.go 实现 DRAM Node 适配器
//
// 将 ChampSim 的 DRAM Channel 包装成 flow_sim 的 Node

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/capabilities/memory/dram"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// DRAMNodeHandler 实现 NodeHandler 接口
//
// 处理逻辑：
// 1. 接收来自CPU的内存请求
// 2. 将请求发送到DRAM Channel
// 3. 执行DRAM的Tick
// 4. 收集完成的请求，发送响应包回CPU
type DRAMNodeHandler struct {
	// ChampSim 组件
	dramChannel *dram.DRAMChannel

	// Node ID（用于创建Packet）
	nodeID int
	cpuID  int // CPU节点的ID

	// 输出队列（发送到CPU）
	outputQueue *queue.OutputQueue

	// 待发送的响应队列
	// DRAM完成请求时，通过callback记录在这里
	pendingResponses []MemoryResponsePayload
}

// NewDRAMNodeHandler 创建 DRAM Node Handler
//
// 参数：
// - nodeID: DRAM节点ID
// - cpuID: CPU节点ID
// - dramChannel: DRAM Channel实例
// - outputQueue: 输出队列（发送到CPU）
func NewDRAMNodeHandler(
	nodeID, cpuID int,
	dramChannel *dram.DRAMChannel,
	outputQueue *queue.OutputQueue,
) *DRAMNodeHandler {
	handler := &DRAMNodeHandler{
		dramChannel:      dramChannel,
		nodeID:           nodeID,
		cpuID:            cpuID,
		outputQueue:      outputQueue,
		pendingResponses: make([]MemoryResponsePayload, 0),
	}

	return handler
}

// Process 实现 NodeHandler.Process
//
// 处理流程：
// 1. 处理输入队列（CPU请求）
// 2. 执行DRAM Tick
// 3. 发送完成的响应到CPU
func (h *DRAMNodeHandler) Process(cycle uint64, inputs [][]queue.PacketRef) error {
	// 1. 处理来自CPU的请求
	if len(inputs) > 0 {
		for _, ref := range inputs[0] {
			if err := h.handleMemoryRequest(cycle, ref.Packet); err != nil {
				return fmt.Errorf("failed to handle memory request: %w", err)
			}
			// 释放输入队列槽位
			ref.Queue.Free(ref.Slot)
		}
	}

	// 2. 执行DRAM Tick
	h.dramChannel.Tick()
	h.dramChannel.SetCycle(cycle)

	// 3. 发送pending responses到CPU
	if err := h.sendPendingResponses(cycle); err != nil {
		return fmt.Errorf("failed to send pending responses: %w", err)
	}

	return nil
}

// handleMemoryRequest 处理来自CPU的内存请求
//
// 流程：
// 1. 解析请求包
// 2. 创建DRAM Packet
// 3. 发送到DRAM Channel
func (h *DRAMNodeHandler) handleMemoryRequest(cycle uint64, pkt packet.Packet) error {
	// 解析请求payload
	payload, err := ParseMemoryRequestPayload(pkt)
	if err != nil {
		return err
	}

	// 创建DRAM Packet
	// 设置callback：当DRAM完成时，将响应添加到pending列表
	dramPkt := &dram.DRAMPacket{
		Address:         payload.Address,
		VAddress:        payload.VAddress,
		InstrID:         payload.InstrID,
		IsWrite:         payload.IsWrite,
		Data:            payload.Data,
		Scheduled:       false,
		ReadyTime:       0,
		InstrDependOnMe: nil,
		Callback:        h.createCallback(payload.InstrID),
	}

	// 发送到DRAM
	if !h.dramChannel.AddRequest(dramPkt) {
		return fmt.Errorf("DRAM queue full for address 0x%x", payload.Address)
	}

	return nil
}

// createCallback 创建DRAM完成时的回调函数
//
// 当DRAM完成请求时，此callback会被调用
// 我们将响应添加到pending列表，稍后发送到CPU
func (h *DRAMNodeHandler) createCallback(instrID uint64) func(uint64, uint64, uint64) {
	return func(addr uint64, data uint64, cycle uint64) {
		// 添加到pending responses
		h.pendingResponses = append(h.pendingResponses, MemoryResponsePayload{
			Address: addr,
			Data:    data,
			InstrID: instrID,
			Cycle:   cycle,
		})
	}
}

// sendPendingResponses 发送待处理的响应到CPU
func (h *DRAMNodeHandler) sendPendingResponses(cycle uint64) error {
	// 将所有pending responses转换为Packet并发送
	packets := make([]packet.Packet, 0, len(h.pendingResponses))

	for _, resp := range h.pendingResponses {
		pkt := NewMemoryResponsePacket(
			h.nodeID,
			h.cpuID,
			resp.Address,
			resp.Data,
			resp.InstrID,
			resp.Cycle,
		)
		packets = append(packets, pkt)
	}

	// 注入到输出队列
	if len(packets) > 0 {
		h.outputQueue.InjectPackets(int(cycle), packets)
		// 清空pending responses
		h.pendingResponses = h.pendingResponses[:0]
	}

	return nil
}

// GetDRAMStats 获取 DRAM 统计信息
func (h *DRAMNodeHandler) GetDRAMStats() dram.DRAMStats {
	return h.dramChannel.GetStats()
}

// ExportStats implements StatsExporter interface.
// Returns runtime statistics matching OpenAPI MemoryConfig schema fields.
func (h *DRAMNodeHandler) ExportStats() map[string]interface{} {
	dramStats := h.dramChannel.GetStats()

	stats := map[string]interface{}{
		// 请求统计
		"read_requests":  dramStats.RQAccesses,
		"write_requests": dramStats.WQAccesses,

		// Row Buffer 统计
		"row_buffer_hits":   dramStats.RQRowBufferHit + dramStats.WQRowBufferHit,
		"row_buffer_misses": dramStats.RQRowBufferMiss + dramStats.WQRowBufferMiss,

		// 详细统计
		"rq_row_buffer_hits":   dramStats.RQRowBufferHit,
		"rq_row_buffer_misses": dramStats.RQRowBufferMiss,
		"wq_row_buffer_hits":   dramStats.WQRowBufferHit,
		"wq_row_buffer_misses": dramStats.WQRowBufferMiss,
	}

	return stats
}
