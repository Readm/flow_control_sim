package dram

import "fmt"

// DRAMChannel DRAM通道
//
// 对应 ChampSim 的 DRAM_CHANNEL
//
// 负责管理一个DRAM通道的所有请求和Bank状态
type DRAMChannel struct {
	// ==================== 配置 ====================

	config  DRAMConfig
	mapping *AddressMapping

	// ==================== 队列 ====================

	// RQ Read Queue
	RQ []*DRAMPacket

	// WQ Write Queue
	WQ []*DRAMPacket

	// ==================== Bank状态 ====================

	// bankRequest Bank状态数组
	// 大小 = Ranks * BankGroups * Banks
	bankRequest []*BankRequest

	// activeRequest 当前在数据总线上的请求
	activeRequest *BankRequest

	// ==================== 时序状态 ====================

	// currentCycle 当前周期
	currentCycle uint64

	// writeMode 当前是否为写模式
	writeMode bool

	// dbusAvailable 数据总线可用时间
	dbusAvailable uint64

	// ==================== 统计信息 ====================

	stats DRAMStats
}

// NewDRAMChannel 创建新的DRAM通道
//
// 对应 ChampSim 的 DRAM_CHANNEL 构造函数
func NewDRAMChannel(config DRAMConfig) (*DRAMChannel, error) {
	// 创建地址映射
	mapping, err := NewAddressMapping(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create address mapping: %w", err)
	}

	// 创建队列
	rq := make([]*DRAMPacket, 0, config.RQSize)
	wq := make([]*DRAMPacket, 0, config.WQSize)

	// 创建Bank状态数组
	totalBanks := mapping.TotalBanks()
	bankRequest := make([]*BankRequest, totalBanks)
	for i := range bankRequest {
		bankRequest[i] = &BankRequest{
			Valid:        false,
			RowBufferHit: false,
			NeedRefresh:  false,
			UnderRefresh: false,
			OpenRow:      nil,
			ReadyTime:    0,
			Pkt:          nil,
		}
	}

	return &DRAMChannel{
		config:        config,
		mapping:       mapping,
		RQ:            rq,
		WQ:            wq,
		bankRequest:   bankRequest,
		activeRequest: nil,
		currentCycle:  0,
		writeMode:     false,
		dbusAvailable: 0,
		stats:         DRAMStats{},
	}, nil
}

// ==================== 队列管理 ====================

// AddRequest 添加请求到DRAM
//
// 对应 ChampSim 的 add_rq/add_wq
//
// 返回：
// - success: 是否成功添加
func (dc *DRAMChannel) AddRequest(pkt *DRAMPacket) bool {
	if pkt.IsWrite {
		// Write请求
		if len(dc.WQ) >= int(dc.config.WQSize) {
			dc.stats.WQFull++
			return false
		}

		// 设置就绪时间
		if pkt.ReadyTime == 0 {
			pkt.ReadyTime = dc.currentCycle
		}

		dc.WQ = append(dc.WQ, pkt)
		dc.stats.WQAccesses++
		return true
	}

	// Read请求
	if len(dc.RQ) >= int(dc.config.RQSize) {
		dc.stats.RQFull++
		return false
	}

	// 设置就绪时间
	if pkt.ReadyTime == 0 {
		pkt.ReadyTime = dc.currentCycle
	}

	dc.RQ = append(dc.RQ, pkt)
	dc.stats.RQAccesses++
	return true
}

// ==================== 时钟推进 ====================

// SetCycle 设置当前周期
func (dc *DRAMChannel) SetCycle(cycle uint64) {
	dc.currentCycle = cycle
}

// Tick 时钟推进
func (dc *DRAMChannel) Tick() {
	dc.currentCycle++
	dc.operate()
}

// ==================== 统计信息 ====================

// GetStats 获取统计信息
func (dc *DRAMChannel) GetStats() DRAMStats {
	return dc.stats
}

// ResetStats 重置统计信息
func (dc *DRAMChannel) ResetStats() {
	dc.stats = DRAMStats{}
}

// ==================== 主循环 ====================

// operate() 实现在 operate.go 中
