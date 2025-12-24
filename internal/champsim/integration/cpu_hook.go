package integration

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/champsim/cpu"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	"github.com/Readm/flow_sim/internal/dataflow"
	"github.com/Readm/flow_sim/internal/dataflow/message"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
	"github.com/Readm/flow_sim/internal/dataflow/transaction"
)

// CPUIncentiveHook 实现 IncentiveHook 接口
//
// 将 ChampSim O3CPU 集成到框架中：
// - 每个周期调用 CPU Tick
// - 从 LSQ 获取准备好的内存请求
// - 生成 CHI/AXI Message
// - 返回包含这些 Message 的 Transaction
type CPUIncentiveHook struct {
	// cpu O3 CPU 实例
	cpu *cpu.O3CPU

	// nodeID 当前 CPU 对应的节点 ID
	nodeID int

	// protocol 使用的协议 (AXI 或 CHI)
	protocol transaction.Protocol

	// txnIDCounter Transaction ID 计数器
	txnIDCounter int

	// msgIDCounter Message ID 计数器
	msgIDCounter int

	// pendingResponses 等待响应的请求
	// key: Message ID, value: LSQ Entry ID
	pendingResponses map[dataflow.MessageID]uint64

	// targetNodeID 内存控制器节点 ID
	targetNodeID int
}

// NewCPUIncentiveHook 创建新的 CPU Incentive Hook
//
// 参数：
//   - traceReader: Trace 文件读取器
//   - config: CPU 配置
//   - nodeID: CPU 节点 ID
//   - targetNodeID: 内存控制器节点 ID
//   - protocol: 使用的协议 (AXI 或 CHI)
func NewCPUIncentiveHook(
	traceReader trace.TraceReader,
	config cpu.O3CPUConfig,
	nodeID int,
	targetNodeID int,
	protocol transaction.Protocol,
) *CPUIncentiveHook {
	cpuInstance := cpu.NewO3CPU(traceReader, config)
	// 设置为集成模式（不自动完成内存操作）
	cpuInstance.SetStandaloneMode(false)

	return &CPUIncentiveHook{
		cpu:              cpuInstance,
		nodeID:           nodeID,
		protocol:         protocol,
		txnIDCounter:     0,
		msgIDCounter:     0,
		pendingResponses: make(map[dataflow.MessageID]uint64),
		targetNodeID:     targetNodeID,
	}
}

// ShouldCreateTransaction 判断是否应该创建 Transaction
//
// CPU 每个周期都可能产生内存请求，所以总是返回 true
func (h *CPUIncentiveHook) ShouldCreateTransaction(nodeID int, cycle uint64) bool {
	// 只响应自己的节点
	if nodeID != h.nodeID {
		return false
	}

	// CPU 每个周期都运行
	return true
}

// CreateTransaction 创建 Transaction
//
// 流程：
// 1. 执行 CPU Tick (推进一个周期)
// 2. 从 LSQ 获取准备好的 load/store 请求
// 3. 为每个请求创建 Message
// 4. 返回包含这些 Message 的 Transaction
func (h *CPUIncentiveHook) CreateTransaction(nodeID int, cycle uint64) (*transaction.Transaction, error) {
	// 只处理自己的节点
	if nodeID != h.nodeID {
		return nil, nil
	}

	// 执行 CPU Tick
	h.cpu.Tick()

	// 从 LSQ 获取准备好的内存请求
	readyLoads := h.cpu.GetReadyLoads(cycle)
	readyStores := h.cpu.GetReadyStores(cycle)

	// 如果没有请求，返回 nil
	if len(readyLoads) == 0 && len(readyStores) == 0 {
		return nil, nil
	}

	// 创建 Transaction
	txnID := dataflow.TransactionID{
		NodeID: h.nodeID,
		TxnID:  h.txnIDCounter,
	}
	h.txnIDCounter++

	txn := &transaction.Transaction{
		ID:              txnID,
		Protocol:        h.protocol,
		Type:            0, // 协议特定
		InitiatorNodeID: h.nodeID,
		State:           transaction.TransactionStatePending,
		CreatedCycle:    cycle,
		Messages:        make([]*message.Message, 0),
		Events:          make([]transaction.Event, 0),
	}

	// 为每个 load 请求创建 Message
	for _, loadEntry := range readyLoads {
		msg := h.createReadRequest(txn.ID, loadEntry, cycle)
		txn.Messages = append(txn.Messages, msg)

		// 标记为已发出
		loadEntry.FetchIssued = true

		// 记录等待响应
		h.pendingResponses[msg.ID] = loadEntry.InstrID
	}

	// 为每个 store 请求创建 Message
	for _, storeEntry := range readyStores {
		msg := h.createWriteRequest(txn.ID, storeEntry, cycle)
		txn.Messages = append(txn.Messages, msg)

		// 标记为已发出
		storeEntry.FetchIssued = true

		// 记录等待响应
		h.pendingResponses[msg.ID] = storeEntry.InstrID
	}

	// 记录事件
	txn.Events = append(txn.Events, transaction.Event{
		Cycle:     cycle,
		NodeID:    h.nodeID,
		EventType: "Created",
		Details:   fmt.Sprintf("Created transaction with %d messages", len(txn.Messages)),
	})

	return txn, nil
}

// createReadRequest 创建读请求 Message
func (h *CPUIncentiveHook) createReadRequest(
	txnID dataflow.TransactionID,
	loadEntry *cpu.LSQEntry,
	cycle uint64,
) *message.Message {
	msgID := dataflow.MessageID{
		NodeID:    h.nodeID,
		MessageID: h.msgIDCounter,
	}
	h.msgIDCounter++

	// 根据协议创建不同的 Message
	var channel message.Channel
	var msgType int

	if h.protocol == transaction.ProtocolCHI {
		channel = message.ChannelREQ
		msgType = 0x00 // CHI ReadNoSnp (示例)
	} else { // AXI
		channel = message.ChannelREQ
		msgType = 0x00 // AXI Read (示例)
	}

	return &message.Message{
		ID:            msgID,
		TransactionID: txnID,
		Channel:       channel,
		Type:          msgType,
		SourceNodeID:  h.nodeID,
		TargetNodeID:  h.targetNodeID,
		Payload: &MemoryRequestPayload{
			Address:  loadEntry.VirtualAddr,
			Size:     64, // 假设 cache line 大小
			IsWrite:  false,
			InstrID:  loadEntry.InstrID,
		},
		Packets:       make([]packet.Packet, 0),
		CreatedCycle:  cycle,
		ProcessedInfo: make([]message.ProcessedInfo, 0),
	}
}

// createWriteRequest 创建写请求 Message
func (h *CPUIncentiveHook) createWriteRequest(
	txnID dataflow.TransactionID,
	storeEntry *cpu.LSQEntry,
	cycle uint64,
) *message.Message {
	msgID := dataflow.MessageID{
		NodeID:    h.nodeID,
		MessageID: h.msgIDCounter,
	}
	h.msgIDCounter++

	// 根据协议创建不同的 Message
	var channel message.Channel
	var msgType int

	if h.protocol == transaction.ProtocolCHI {
		channel = message.ChannelREQ
		msgType = 0x10 // CHI WriteNoSnpFull (示例)
	} else { // AXI
		channel = message.ChannelREQ
		msgType = 0x01 // AXI Write (示例)
	}

	return &message.Message{
		ID:            msgID,
		TransactionID: txnID,
		Channel:       channel,
		Type:          msgType,
		SourceNodeID:  h.nodeID,
		TargetNodeID:  h.targetNodeID,
		Payload: &MemoryRequestPayload{
			Address:  storeEntry.VirtualAddr,
			Size:     64, // 假设 cache line 大小
			IsWrite:  true,
			InstrID:  storeEntry.InstrID,
		},
		Packets:       make([]packet.Packet, 0),
		CreatedCycle:  cycle,
		ProcessedInfo: make([]message.ProcessedInfo, 0),
	}
}

// HandleResponse 处理内存响应
//
// 当收到来自内存系统的响应时调用，更新 LSQ 状态
func (h *CPUIncentiveHook) HandleResponse(msgID dataflow.MessageID, cycle uint64) error {
	// 查找对应的请求
	instrID, exists := h.pendingResponses[msgID]
	if !exists {
		return fmt.Errorf("received response for unknown message: Node%d-Msg%d", msgID.NodeID, msgID.MessageID)
	}

	// 更新 LSQ
	// 尝试作为 load 响应
	if h.cpu.HandleLoadResponse(instrID, cycle) {
		delete(h.pendingResponses, msgID)
		return nil
	}

	// 尝试作为 store 响应
	if h.cpu.HandleStoreResponse(instrID, cycle) {
		delete(h.pendingResponses, msgID)
		return nil
	}

	return fmt.Errorf("failed to handle response for instr %d", instrID)
}

// GetCPU 返回 CPU 实例（用于测试和调试）
func (h *CPUIncentiveHook) GetCPU() *cpu.O3CPU {
	return h.cpu
}

// GetStats 返回 CPU 统计信息
func (h *CPUIncentiveHook) GetStats() cpu.O3CPUStats {
	return h.cpu.GetStats()
}

// MemoryRequestPayload 内存请求 Payload
type MemoryRequestPayload struct {
	// Address 内存地址
	Address uint64

	// Size 请求大小（字节）
	Size int

	// IsWrite 是否为写请求
	IsWrite bool

	// InstrID 对应的指令 ID
	InstrID uint64

	// Data 写数据（仅用于写请求）
	Data []byte
}
