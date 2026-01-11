package dram

// DRAMConfig DRAM配置
//
// 对应 ChampSim 的 DRAM_CHANNEL 构造函数参数
//
// 默认配置基于 DDR4-2400 标准
type DRAMConfig struct {
	// ==================== 容量配置 ====================

	// Channels 通道数 (暂时固定为1)
	Channels uint32

	// Ranks Rank数量 (通常为1或2)
	Ranks uint32

	// BankGroups Bank组数量 (DDR4引入，通常为4)
	BankGroups uint32

	// Banks 每组的Bank数量 (通常为4)
	Banks uint32

	// Rows 行数 (32K或64K)
	Rows uint32

	// Columns 列数 (通常为1K)
	Columns uint32

	// ==================== 队列大小 ====================

	// RQSize Read Queue大小
	RQSize uint32

	// WQSize Write Queue大小
	WQSize uint32

	// ==================== 延迟参数 (以cycles为单位) ====================

	// TRP Row Precharge time (关闭行延迟)
	// DDR4-2400: 15ns ≈ 15 cycles
	TRP uint64

	// TRCD RAS to CAS Delay (激活到读写延迟)
	// DDR4-2400: 15ns ≈ 15 cycles
	TRCD uint64

	// TCAS CAS Latency (列访问延迟)
	// DDR4-2400: 15ns ≈ 15 cycles
	TCAS uint64

	// TRAS Row Active time (行激活时间)
	// DDR4-2400: 35ns ≈ 35 cycles
	TRAS uint64

	// ==================== 数据配置 ====================

	// ChannelWidth 通道宽度 (bytes)
	// 通常为8字节 (64位)
	ChannelWidth uint32

	// Name DRAM名称
	Name string
}

// DefaultDRAMConfig 返回默认DRAM配置 (DDR4-2400)
//
// 参数说明:
// - 1 Channel
// - 1 Rank
// - 4 BankGroups × 4 Banks = 16 Banks
// - 32K Rows × 1K Columns
// - Total: ~2GB per channel
func DefaultDRAMConfig() DRAMConfig {
	return DRAMConfig{
		// 容量: 1 × 1 × 4 × 4 × 32K × 1K × 64B = 2GB
		Channels:   1,
		Ranks:      1,
		BankGroups: 4,
		Banks:      4,
		Rows:       32768, // 32K
		Columns:    1024,  // 1K

		// 队列
		RQSize: 64,
		WQSize: 64,

		// 延迟 (DDR4-2400 @ 1GHz CPU clock)
		TRP:  15, // 15 cycles
		TRCD: 15, // 15 cycles
		TCAS: 15, // 15 cycles
		TRAS: 35, // 35 cycles

		// 数据宽度
		ChannelWidth: 8, // 8 bytes = 64 bits

		Name: "DRAM",
	}
}

// DRAMPacket DRAM请求包
//
// 对应 ChampSim 的 DRAM_CHANNEL::request_type
type DRAMPacket struct {
	// ==================== 地址信息 ====================

	// Address 物理地址
	Address uint64

	// VAddress 虚拟地址
	VAddress uint64

	// Data 数据内容
	Data uint64

	// ==================== 请求信息 ====================

	// InstrID 指令ID
	InstrID uint64

	// IsWrite 是否为写请求
	IsWrite bool

	// Scheduled 是否已被调度
	Scheduled bool

	// ReadyTime 就绪时间 (最早可以被调度的时间)
	ReadyTime uint64

	// ==================== 依赖跟踪 ====================

	// InstrDependOnMe 依赖这个请求的指令ID列表
	InstrDependOnMe []uint64

	// ==================== 回调 ====================

	// Callback 请求完成时的回调函数
	// 参数: (address, data, cycle)
	Callback func(addr uint64, data uint64, cycle uint64)
}

// BankRequest Bank请求状态
//
// 对应 ChampSim 的 DRAM_CHANNEL::BANK_REQUEST
type BankRequest struct {
	// Valid 是否有有效的请求正在处理
	Valid bool

	// RowBufferHit 是否为Row Buffer命中
	RowBufferHit bool

	// NeedRefresh 是否需要refresh
	NeedRefresh bool

	// UnderRefresh 是否正在refresh
	UnderRefresh bool

	// OpenRow 当前打开的行号
	// nil表示没有打开的行
	OpenRow *uint64

	// ReadyTime 就绪时间 (请求完成时间)
	ReadyTime uint64

	// Pkt 关联的请求包
	Pkt *DRAMPacket
}

// DRAMStats DRAM统计信息
//
// 对应 ChampSim 的 dram_stats
type DRAMStats struct {
	// ==================== 访问统计 ====================

	// RQAccesses Read Queue访问次数
	RQAccesses uint64

	// WQAccesses Write Queue访问次数
	WQAccesses uint64

	// ==================== Row Buffer统计 ====================

	// RQRowBufferHit Read Queue Row Buffer命中次数
	RQRowBufferHit uint64

	// RQRowBufferMiss Read Queue Row Buffer未命中次数
	RQRowBufferMiss uint64

	// WQRowBufferHit Write Queue Row Buffer命中次数
	WQRowBufferHit uint64

	// WQRowBufferMiss Write Queue Row Buffer未命中次数
	WQRowBufferMiss uint64

	// ==================== 队列统计 ====================

	// RQFull Read Queue满的次数
	RQFull uint64

	// WQFull Write Queue满的次数
	WQFull uint64

	// ==================== 数据总线统计 ====================

	// DBusCongested 数据总线拥塞次数
	DBusCongested uint64

	// DBusCycles 数据总线拥塞周期数
	DBusCycles uint64

	// ==================== Refresh统计 ====================

	// RefreshCycles Refresh周期数
	RefreshCycles uint64
}

// RowBufferHitRate 计算Row Buffer命中率
func (s *DRAMStats) RowBufferHitRate() float64 {
	total := s.RQRowBufferHit + s.RQRowBufferMiss
	if total == 0 {
		return 0
	}
	return float64(s.RQRowBufferHit) / float64(total)
}

// WriteRowBufferHitRate 计算写Row Buffer命中率
func (s *DRAMStats) WriteRowBufferHitRate() float64 {
	total := s.WQRowBufferHit + s.WQRowBufferMiss
	if total == 0 {
		return 0
	}
	return float64(s.WQRowBufferHit) / float64(total)
}

// TotalAccesses 总访问次数
func (s *DRAMStats) TotalAccesses() uint64 {
	return s.RQAccesses + s.WQAccesses
}
