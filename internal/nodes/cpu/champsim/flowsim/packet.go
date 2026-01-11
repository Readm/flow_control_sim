package flowsim

// packet.go 定义 ChampSim 内存系统在 flow_sim 框架中使用的 Packet 类型

import (
	"encoding/json"
	"fmt"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Packet 类型常量
const (
	PacketTypeMemoryRequest  = 1 // 内存请求（Load/Store）
	PacketTypeMemoryResponse = 2 // 内存响应（Fill）
)

// MemoryRequestPayload 内存请求的 Payload
//
// 对应 ChampSim 的 DRAM_PACKET 或 Cache 的 miss request
type MemoryRequestPayload struct {
	Address  uint64 `json:"address"`   // 物理地址
	VAddress uint64 `json:"v_address"` // 虚拟地址
	InstrID  uint64 `json:"instr_id"`  // 指令ID
	IsWrite  bool   `json:"is_write"`  // 是否为写操作
	Data     uint64 `json:"data"`      // 数据（仅Store时有效）
}

// MemoryResponsePayload 内存响应的 Payload
//
// 对应 ChampSim 的 fill response
type MemoryResponsePayload struct {
	Address uint64 `json:"address"` // 物理地址
	Data    uint64 `json:"data"`    // 返回的数据
	InstrID uint64 `json:"instr_id"` // 指令ID（用于通知CPU）
	Cycle   uint64 `json:"cycle"`   // 完成周期
}

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
	payload := MemoryRequestPayload{
		Address:  addr,
		VAddress: vaddr,
		InstrID:  instrID,
		IsWrite:  isWrite,
		Data:     data,
	}

	// 序列化为JSON
	payloadJSON, _ := json.Marshal(payload)

	return packet.Packet{
		SourceID: sourceID,
		TargetID: targetID,
		Payload:  string(payloadJSON),
		Type:     PacketTypeMemoryRequest,
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
	payload := MemoryResponsePayload{
		Address: addr,
		Data:    data,
		InstrID: instrID,
		Cycle:   cycle,
	}

	// 序列化为JSON
	payloadJSON, _ := json.Marshal(payload)

	return packet.Packet{
		SourceID: sourceID,
		TargetID: targetID,
		Payload:  string(payloadJSON),
		Type:     PacketTypeMemoryResponse,
	}
}

// ParseMemoryRequestPayload 解析内存请求 Payload
func ParseMemoryRequestPayload(pkt packet.Packet) (*MemoryRequestPayload, error) {
	if pkt.Type != PacketTypeMemoryRequest {
		return nil, fmt.Errorf("packet type is not MemoryRequest: %d", pkt.Type)
	}

	var payload MemoryRequestPayload
	if err := json.Unmarshal([]byte(pkt.Payload), &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal MemoryRequestPayload: %w", err)
	}

	return &payload, nil
}

// ParseMemoryResponsePayload 解析内存响应 Payload
func ParseMemoryResponsePayload(pkt packet.Packet) (*MemoryResponsePayload, error) {
	if pkt.Type != PacketTypeMemoryResponse {
		return nil, fmt.Errorf("packet type is not MemoryResponse: %d", pkt.Type)
	}

	var payload MemoryResponsePayload
	if err := json.Unmarshal([]byte(pkt.Payload), &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal MemoryResponsePayload: %w", err)
	}

	return &payload, nil
}
