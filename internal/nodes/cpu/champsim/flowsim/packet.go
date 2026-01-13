package flowsim

// packet.go 定义 ChampSim 内存系统在 flow_sim 框架中使用的 Packet 类型

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Packet 类型常量
const (
	PacketTypeMemoryRequest  = 1 // 内存请求（Load/Store）
	PacketTypeMemoryResponse = 2 // 内存响应（Fill）
)

// Op constants for MemoryRequest
const (
	OpRead  = 0
	OpWrite = 1
)

// NewMemoryRequestPacket 创建内存请求 Packet
//
// 参数：
// - sourceID: 源节点ID（CPU节点）
// - targetID: 目标节点ID（DRAM节点）
// - addr: 物理地址
// - vaddr: 虚拟地址
// - instrID: 指令ID
// - isWrite: 是否为写操作
// - data: 数据（仅Store时有效）
func NewMemoryRequestPacket(
	sourceID, targetID int,
	addr, vaddr, instrID uint64,
	isWrite bool,
	data uint64,
) packet.Packet {
	op := OpRead
	if isWrite {
		op = OpWrite
	}

	return packet.Packet{
		SourceID: sourceID,
		TargetID: targetID,
		Type:     PacketTypeMemoryRequest,

		// Use native fields
		Addr:    addr,
		VAddr:   vaddr,
		InstrID: instrID,
		Op:      op,
		Data:    data,
	}
}

// NewMemoryResponsePacket 创建内存响应 Packet
//
// 参数：
// - sourceID: 源节点ID（DRAM节点）
// - targetID: 目标节点ID（CPU节点）
// - addr: 物理地址
// - data: 返回的数据
// - instrID: 指令ID
// - cycle: 完成周期
func NewMemoryResponsePacket(
	sourceID, targetID int,
	addr, data, instrID, cycle uint64,
) packet.Packet {
	return packet.Packet{
		SourceID: sourceID,
		TargetID: targetID,
		Type:     PacketTypeMemoryResponse,

		// Use native fields
		Addr:    addr,
		Data:    data,
		InstrID: instrID,
		Cycle:   cycle,
	}
}

// ParseMemoryRequestPayload is deprecated, fields are now accessed natively.
// Kept temporarily if needed for refactoring but errors if called.
func ParseMemoryRequestPayload(pkt packet.Packet) (packet.Packet, error) {
	if pkt.Type != PacketTypeMemoryRequest {
		return pkt, fmt.Errorf("packet type is not MemoryRequest: %d", pkt.Type)
	}
	return pkt, nil
}

// ParseMemoryResponsePayload is deprecated, fields are now accessed natively.
// Kept temporarily if needed for refactoring but errors if called.
func ParseMemoryResponsePayload(pkt packet.Packet) (packet.Packet, error) {
	if pkt.Type != PacketTypeMemoryResponse {
		return pkt, fmt.Errorf("packet type is not MemoryResponse: %d", pkt.Type)
	}
	return pkt, nil
}
