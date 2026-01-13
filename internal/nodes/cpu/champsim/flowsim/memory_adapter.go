package flowsim

// memory_adapter.go 实现 flow_sim 的 Memory Adapter
//
// 连接 Cache 和 flow_sim 网络框架

import (
	"sync"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// FlowSimMemoryAdapter 实现 MemoryInterface
//
// 将 Cache 的内存请求转换为待发送的网络包
// 不直接操作 DRAM，而是将请求加入队列
type FlowSimMemoryAdapter struct {
	// 请求队列
	requestQueue []packet.Packet
	queueMu      sync.Mutex

	// 周期同步
	currentCycle uint64
	cycleMu      sync.Mutex
}

// NewFlowSimMemoryAdapter 创建 FlowSim Memory Adapter
func NewFlowSimMemoryAdapter() *FlowSimMemoryAdapter {
	return &FlowSimMemoryAdapter{
		requestQueue: make([]packet.Packet, 0),
		currentCycle: 0,
	}
}

// SendRequest 实现 MemoryInterface.SendRequest
//
// 将请求添加到队列，稍后由 CPU Node 发送到网络
func (a *FlowSimMemoryAdapter) SendRequest(reqInterface interface{}) bool {
	// 从 map[string]interface{} 提取字段
	reqMap, ok := reqInterface.(map[string]interface{})
	if !ok {
		return false
	}

	// 提取各个字段
	addr, ok := reqMap["Address"].(uint64)
	if !ok {
		return false
	}

	vaddr, ok := reqMap["VAddress"].(uint64)
	if !ok {
		return false
	}

	instrID, ok := reqMap["InstrID"].(uint64)
	if !ok {
		return false
	}

	isWrite, ok := reqMap["IsWrite"].(bool)
	if !ok {
		return false
	}

	data, ok := reqMap["Data"].(uint64)
	if !ok {
		return false
	}

	// 创建请求 (使用 Packet Native Fields)
	op := OpRead
	if isWrite {
		op = OpWrite
	}

	req := packet.Packet{
		Addr:    addr,
		VAddr:   vaddr,
		InstrID: instrID,
		Op:      op,
		Data:    data,
		Type:    PacketTypeMemoryRequest, // 标记类型
	}

	// 添加到队列
	a.queueMu.Lock()
	a.requestQueue = append(a.requestQueue, req)
	a.queueMu.Unlock()

	return true
}

// Tick 实现 MemoryInterface.Tick
//
// 在 flow_sim 模式下，Tick 由 Network 统一管理
// 这里不需要做任何事情
func (a *FlowSimMemoryAdapter) Tick() {
	// No-op: Tick is managed by Network
}

// SetCycle 实现 MemoryInterface.SetCycle
func (a *FlowSimMemoryAdapter) SetCycle(cycle uint64) {
	a.cycleMu.Lock()
	a.currentCycle = cycle
	a.cycleMu.Unlock()
}

// GetPendingRequests 获取所有待发送的请求
//
// CPU Node 调用此方法来获取需要发送的请求
// 调用后会清空队列
func (a *FlowSimMemoryAdapter) GetPendingRequests() []packet.Packet {
	a.queueMu.Lock()
	defer a.queueMu.Unlock()

	// 复制队列
	requests := make([]packet.Packet, len(a.requestQueue))
	copy(requests, a.requestQueue)

	// 清空队列
	a.requestQueue = a.requestQueue[:0]

	return requests
}

// GetQueueSize 获取当前队列大小（用于调试）
func (a *FlowSimMemoryAdapter) GetQueueSize() int {
	a.queueMu.Lock()
	defer a.queueMu.Unlock()
	return len(a.requestQueue)
}
