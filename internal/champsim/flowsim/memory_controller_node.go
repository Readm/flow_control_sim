package flowsim

// memory_controller_node.go 实现 Memory Controller 节点
// 负责将内存请求路由到多个 DRAM 通道

import (
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// MemoryControllerHandler Memory Controller 节点处理器
type MemoryControllerHandler struct {
	nodeID int

	// 上游 L2 Cache 节点 ID
	l2NodeID int

	// 下游 DRAM Channel 节点 IDs
	dramChannelIDs []int

	// 输出队列
	// outputQueues[0]: 发送到 L2
	// outputQueues[1..n]: 发送到各个 DRAM Channel
	outputQueues []*queue.OutputQueue

	// 地址映射策略
	addressMapping AddressMappingStrategy

	// 统计信息
	stats MemoryControllerStats
}

// MemoryControllerStats Memory Controller 统计
type MemoryControllerStats struct {
	TotalRequests      uint64
	RequestsPerChannel []uint64 // 每个通道的请求数
	Responses          uint64
}

// AddressMappingStrategy 地址映射策略
type AddressMappingStrategy int

const (
	// 使用地址的低位 bits 来选择通道（交错映射）
	MappingInterleaved AddressMappingStrategy = iota

	// 使用地址范围来选择通道
	MappingRanged
)

// NewMemoryControllerHandler 创建 Memory Controller Handler
func NewMemoryControllerHandler(
	nodeID int,
	l2NodeID int,
	dramChannelIDs []int,
	outputQueues []*queue.OutputQueue,
	strategy AddressMappingStrategy,
) *MemoryControllerHandler {
	return &MemoryControllerHandler{
		nodeID:         nodeID,
		l2NodeID:       l2NodeID,
		dramChannelIDs: dramChannelIDs,
		outputQueues:   outputQueues,
		addressMapping: strategy,
		stats: MemoryControllerStats{
			RequestsPerChannel: make([]uint64, len(dramChannelIDs)),
		},
	}
}

// Process 处理输入包
// inputs[0]: 来自 L2 的请求
// inputs[1..n]: 来自各个 DRAM Channel 的响应
func (h *MemoryControllerHandler) Process(cycle uint64, inputs [][]queue.PacketRef) error {
	// 1. 处理来自 DRAM Channels 的响应
	for channelIndex := 1; channelIndex < len(inputs); channelIndex++ {
		for _, ref := range inputs[channelIndex] {
			if err := h.handleDRAMResponse(cycle, ref.Packet); err != nil {
				return err
			}
			ref.Queue.Free(ref.Slot)
		}
	}

	// 2. 处理来自 L2 的请求
	for _, ref := range inputs[0] {
		if err := h.handleL2Request(cycle, ref.Packet); err != nil {
			return err
		}
		ref.Queue.Free(ref.Slot)
	}

	return nil
}

// handleL2Request 处理来自 L2 的请求
func (h *MemoryControllerHandler) handleL2Request(cycle uint64, pkt packet.Packet) error {
	payload, err := ParseMemoryRequestPayload(pkt)
	if err != nil {
		return err
	}

	h.stats.TotalRequests++

	// 根据地址选择 DRAM Channel
	channelIndex := h.selectChannel(payload.Address)
	h.stats.RequestsPerChannel[channelIndex]++

	// 转发请求到选定的 DRAM Channel
	dramNodeID := h.dramChannelIDs[channelIndex]
	requestPkt := NewMemoryRequestPacket(
		h.nodeID,
		dramNodeID,
		payload.Address,
		payload.VAddress,
		payload.InstrID,
		payload.IsWrite,
		payload.Data,
	)

	// outputQueues[0] 是到 L2 的
	// outputQueues[1..n] 是到各个 DRAM 的
	dramQueueIndex := channelIndex + 1
	h.outputQueues[dramQueueIndex].InjectPackets(int(cycle), []packet.Packet{requestPkt})

	return nil
}

// handleDRAMResponse 处理来自 DRAM 的响应
func (h *MemoryControllerHandler) handleDRAMResponse(cycle uint64, pkt packet.Packet) error {
	payload, err := ParseMemoryResponsePayload(pkt)
	if err != nil {
		return err
	}

	h.stats.Responses++

	// 转发响应给 L2
	responsePkt := NewMemoryResponsePacket(
		h.nodeID,
		h.l2NodeID,
		payload.Address,
		payload.Data,
		payload.InstrID,
		cycle,
	)

	// outputQueues[0] 是到 L2 的
	h.outputQueues[0].InjectPackets(int(cycle), []packet.Packet{responsePkt})

	return nil
}

// selectChannel 根据地址选择 DRAM Channel
func (h *MemoryControllerHandler) selectChannel(address uint64) int {
	numChannels := len(h.dramChannelIDs)

	switch h.addressMapping {
	case MappingInterleaved:
		// 使用地址的某些 bits 来交错映射
		// 例如，使用 bits [12:14] (假设 cache line size = 64B = 2^6)
		// 这样每个 cache line 会映射到不同的通道
		blockBits := 6                                        // log2(64)
		channelBits := log2(uint64(numChannels))              // 需要多少位来表示通道数
		channelIndex := (address >> blockBits) & ((1 << channelBits) - 1)
		return int(channelIndex) % numChannels

	case MappingRanged:
		// 按地址范围划分
		// 简单平均分配地址空间
		addressPerChannel := uint64(1) << (64 - log2(uint64(numChannels)))
		channelIndex := address / addressPerChannel
		return int(channelIndex) % numChannels

	default:
		// 默认使用简单的模运算
		return int(address % uint64(numChannels))
	}
}

// GetStats 获取统计信息
func (h *MemoryControllerHandler) GetStats() MemoryControllerStats {
	return h.stats
}

// log2 计算 log2(n)，向上取整
func log2(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	result := uint64(0)
	n--
	for n > 0 {
		n >>= 1
		result++
	}
	return result
}
