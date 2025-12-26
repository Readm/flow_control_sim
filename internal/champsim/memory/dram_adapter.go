package memory

import (
	"github.com/Readm/flow_sim/internal/champsim/dram"
)

// DRAMAdapter DRAM适配器（实现MemoryInterface）
//
// 将DRAM包装为MemoryInterface，使Cache可以通过统一接口访问
//
// Phase 1: 直接调用DRAM
// Phase 2: 可替换为PortBasedMemoryAdapter（基于flow_sim框架）
type DRAMAdapter struct {
	dram *dram.DRAMChannel
}

// NewDRAMAdapter 创建DRAM适配器
func NewDRAMAdapter(dramChannel *dram.DRAMChannel) *DRAMAdapter {
	return &DRAMAdapter{
		dram: dramChannel,
	}
}

// SendRequest 实现MemoryInterface接口
//
// 将MemoryRequest转换为DRAM的DRAMPacket并发送
func (da *DRAMAdapter) SendRequest(reqInterface interface{}) bool {
	// 从map[string]interface{}提取字段
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

	callback, ok := reqMap["Callback"].(func(uint64, uint64, uint64))
	if !ok {
		return false
	}

	// 转换为DRAMPacket
	pkt := &dram.DRAMPacket{
		Address:         addr,
		VAddress:        vaddr,
		InstrID:         instrID,
		IsWrite:         isWrite,
		Data:            data,
		Scheduled:       false,
		ReadyTime:       0,
		InstrDependOnMe: nil,
		Callback:        callback,
	}

	// 发送到DRAM
	return da.dram.AddRequest(pkt)
}

// Tick 实现MemoryInterface接口
func (da *DRAMAdapter) Tick() {
	da.dram.Tick()
}

// SetCycle 实现MemoryInterface接口
func (da *DRAMAdapter) SetCycle(cycle uint64) {
	da.dram.SetCycle(cycle)
}

// GetDRAM 获取底层DRAM（用于调试和统计）
func (da *DRAMAdapter) GetDRAM() *dram.DRAMChannel {
	return da.dram
}
