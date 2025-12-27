package flowsim

// l2_cache_node_v2.go
// 基于 flow_sim components 的 L2 Cache 节点实现（支持 MESI 协议）
//
// 架构说明：
// - CPU Node：继续使用 ChampSim O3CPU + ChampSim L1D Cache
// - L2 Node：使用 flow_sim components/cache + directory（本文件）
// - Memory Controller & DRAM：保持原样

import (
	"github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/components/directory"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// L2CacheNodeHandlerV2 使用 flow_sim components 的 L2 Cache 节点
type L2CacheNodeHandlerV2 struct {
	// Node ID
	nodeID int

	// 上游 CPU 节点 IDs
	cpuNodeIDs []int

	// 下游 Memory Controller ID
	memCtrlID int

	// flow_sim Cache（共享 L2）- 使用 components
	l2Cache cache.Cache

	// flow_sim Directory（MESI 协议管理）- 使用 components
	directory directory.Directory

	// 输出队列
	// outputQueues[0..n-1]: 发送到各个 CPU
	// outputQueues[n]: 发送到 Memory Controller
	outputQueues []*queue.OutputQueue

	// Pending requests tracking
	// 用于跟踪哪个 CPU 请求了哪个地址
	pendingRequests map[uint64]*L2PendingRequest

	// 统计信息
	stats L2CacheStatsV2
}

// L2PendingRequest 跟踪待处理的请求
type L2PendingRequest struct {
	RequesterCPUID int    // 请求者 CPU ID
	RequesterIndex int    // 请求者在 cpuNodeIDs 中的索引
	Address        uint64
	InstrID        uint64
	IsWrite        bool
	Cycle          uint64
}

// L2CacheStatsV2 L2 Cache 统计
type L2CacheStatsV2 struct {
	RequestsFromCPU   uint64
	HitsFromCPU       uint64
	MissesFromCPU     uint64
	InvalidatesSent   uint64
	ForwardsRequested uint64
	MemoryRequests    uint64
	ResponsesFromMem  uint64
	ResponsesToCPU    uint64
}

// NewL2CacheNodeHandlerV2 创建基于 components 的 L2 Node Handler
func NewL2CacheNodeHandlerV2(
	nodeID int,
	cpuNodeIDs []int,
	memCtrlID int,
	outputQueues []*queue.OutputQueue,
) *L2CacheNodeHandlerV2 {
	// 创建 L2 Cache（使用 flow_sim components/cache）
	// 配置：512 sets, 16 ways, 64-byte blocks = 512KB
	l2Cache := cache.NewSetAssociativeCache(512, 16, 64)

	// 创建 Directory（追踪共享者，使用 flow_sim components/directory）
	dir := directory.NewFullyAssociativeDirectory(1024)

	return &L2CacheNodeHandlerV2{
		nodeID:          nodeID,
		cpuNodeIDs:      cpuNodeIDs,
		memCtrlID:       memCtrlID,
		l2Cache:         l2Cache,
		directory:       dir,
		outputQueues:    outputQueues,
		pendingRequests: make(map[uint64]*L2PendingRequest),
	}
}

// Process 实现 NodeHandler.Process
func (h *L2CacheNodeHandlerV2) Process(cycle uint64, inputs [][]queue.PacketRef) error {
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

// handleCPURequest 处理来自 CPU 的请求（来自 ChampSim CPU Node）
func (h *L2CacheNodeHandlerV2) handleCPURequest(cycle uint64, cpuNodeID int, cpuIndex int, pkt packet.Packet) error {
	payload, err := ParseMemoryRequestPayload(pkt)
	if err != nil {
		return err
	}

	h.stats.RequestsFromCPU++

	// 使用 flow_sim Directory 处理 MESI 请求
	action := h.directory.HandleMESIRequest(payload.Address, cpuNodeID, payload.IsWrite)

	// 发送 invalidate 消息到其他 CPU
	if len(action.InvalidateList) > 0 {
		h.sendInvalidates(cycle, action.InvalidateList, payload.Address)
		h.stats.InvalidatesSent += uint64(len(action.InvalidateList))
	}

	// 使用 flow_sim Cache 的 Access 方法检查 L2
	result := h.l2Cache.Access(payload.Address, payload.IsWrite)

	if result.Hit {
		// L2 命中（使用 flow_sim components/cache）
		h.stats.HitsFromCPU++

		// 直接发送响应给 CPU
		h.sendResponseToCPU(cycle, cpuIndex, payload.Address, payload.InstrID, result.Data)
		h.stats.ResponsesToCPU++
	} else {
		// L2 缺失
		h.stats.MissesFromCPU++

		// 记录 pending request
		h.pendingRequests[payload.Address] = &L2PendingRequest{
			RequesterCPUID: cpuNodeID,
			RequesterIndex: cpuIndex,
			Address:        payload.Address,
			InstrID:        payload.InstrID,
			IsWrite:        payload.IsWrite,
			Cycle:          cycle,
		}

		// 检查是否需要从内存获取
		if action.NeedMemory {
			// 发送请求到 Memory Controller
			h.sendRequestToMemory(cycle, payload)
			h.stats.MemoryRequests++
		} else if action.ForwarderID >= 0 {
			// 需要从其他 CPU 转发数据（cache-to-cache transfer）
			// TODO: 实现完整的 cache-to-cache transfer
			// 简化处理：还是从内存获取
			h.sendRequestToMemory(cycle, payload)
			h.stats.MemoryRequests++
			h.stats.ForwardsRequested++
		}
	}

	return nil
}

// handleMemoryResponse 处理来自 Memory 的响应
func (h *L2CacheNodeHandlerV2) handleMemoryResponse(cycle uint64, pkt packet.Packet) error {
	payload, err := ParseMemoryResponsePayload(pkt)
	if err != nil {
		return err
	}

	h.stats.ResponsesFromMem++

	// 使用 flow_sim Cache 的 Fill 方法填充到 L2
	data := uint64ToBytes(payload.Data)
	_, _ = h.l2Cache.Fill(payload.Address, data, cache.StateExclusive)

	// 查找对应的 pending request
	pending, exists := h.pendingRequests[payload.Address]
	if !exists {
		// 没有对应的请求，可能已经被处理了
		return nil
	}

	// 转发给请求的 CPU
	h.sendResponseToCPU(cycle, pending.RequesterIndex, payload.Address, pending.InstrID, data)
	h.stats.ResponsesToCPU++

	// 删除 pending request
	delete(h.pendingRequests, payload.Address)

	return nil
}

// sendResponseToCPU 发送响应给 CPU（ChampSim CPU Node）
func (h *L2CacheNodeHandlerV2) sendResponseToCPU(cycle uint64, cpuIndex int, address, instrID uint64, data []byte) {
	dataValue := bytesToUint64(data)

	responsePkt := NewMemoryResponsePacket(
		h.nodeID,
		h.cpuNodeIDs[cpuIndex],
		address,
		dataValue,
		instrID,
		cycle,
	)

	h.outputQueues[cpuIndex].InjectPackets(int(cycle), []packet.Packet{responsePkt})
}

// sendRequestToMemory 发送请求到 Memory Controller
func (h *L2CacheNodeHandlerV2) sendRequestToMemory(cycle uint64, payload *MemoryRequestPayload) {
	requestPkt := NewMemoryRequestPacket(
		h.nodeID,
		h.memCtrlID,
		payload.Address,
		payload.VAddress,
		payload.InstrID,
		payload.IsWrite,
		payload.Data,
	)

	memCtrlQueueIndex := len(h.cpuNodeIDs)
	h.outputQueues[memCtrlQueueIndex].InjectPackets(int(cycle), []packet.Packet{requestPkt})
}

// sendInvalidates 发送 invalidate 消息到其他 CPU
func (h *L2CacheNodeHandlerV2) sendInvalidates(cycle uint64, nodeIDs []int, address uint64) {
	// TODO: 实现完整的 invalidate 消息协议
	// 需要：
	// 1. 定义 InvalidatePacket 类型
	// 2. CPU Node 需要处理 invalidate 消息
	// 3. 更新 L1 Cache 状态
	//
	// 简化处理：当前仅在 directory 中记录，不发送实际消息
	// 未来可以扩展支持完整的 invalidate 协议
}

// GetCacheStats 获取 L2 Cache 统计（flow_sim components）
func (h *L2CacheNodeHandlerV2) GetCacheStats() cache.CacheStats {
	return h.l2Cache.GetStats()
}

// GetStats 获取节点统计
func (h *L2CacheNodeHandlerV2) GetStats() L2CacheStatsV2 {
	return h.stats
}

// GetDirectorySharers 获取某个地址的共享者列表（用于调试）
func (h *L2CacheNodeHandlerV2) GetDirectorySharers(addr uint64) []int {
	return h.directory.GetSharers(addr)
}

// GetDirectoryState 获取某个地址的 Directory 状态（用于调试）
func (h *L2CacheNodeHandlerV2) GetDirectoryState(addr uint64) directory.State {
	return h.directory.GetState(addr)
}

// 辅助函数：uint64 转 []byte
func uint64ToBytes(val uint64) []byte {
	data := make([]byte, 8)
	for i := 0; i < 8; i++ {
		data[i] = byte(val >> (i * 8))
	}
	return data
}

// 辅助函数：[]byte 转 uint64
func bytesToUint64(data []byte) uint64 {
	if len(data) == 0 {
		return 0
	}
	val := uint64(0)
	for i := 0; i < 8 && i < len(data); i++ {
		val |= uint64(data[i]) << (i * 8)
	}
	return val
}
