package dram

import (
	"fmt"
	"math/bits"
)

// AddressMapping DRAM地址映射
//
// 对应 ChampSim 的 DRAM_ADDRESS_MAPPING
//
// 地址分解：
// ┌────────┬──────────┬───────────┬──────┬────────┬──────┬────────┐
// │  Row   │  Rank    │  Column   │ Bank │ BkGrp  │ Chan │ Offset │
// └────────┴──────────┴───────────┴──────┴────────┴──────┴────────┘
//
// 注意：与Cache地址分解不同，DRAM的地址分解更复杂，
// 需要考虑Channel、Rank、BankGroup、Bank、Row、Column等
type AddressMapping struct {
	// ==================== 位数配置 ====================

	offsetBits    uint32 // Offset位数 (log2(ChannelWidth))
	channelBits   uint32 // Channel位数 (log2(Channels))
	bankgroupBits uint32 // BankGroup位数 (log2(BankGroups))
	bankBits      uint32 // Bank位数 (log2(Banks))
	columnBits    uint32 // Column位数 (log2(Columns))
	rankBits      uint32 // Rank位数 (log2(Ranks))
	rowBits       uint32 // Row位数 (log2(Rows))

	// ==================== 掩码 ====================

	channelMask   uint64 // Channel掩码
	bankgroupMask uint64 // BankGroup掩码
	bankMask      uint64 // Bank掩码
	columnMask    uint64 // Column掩码
	rankMask      uint64 // Rank掩码
	rowMask       uint64 // Row掩码

	// ==================== 参数 ====================

	channels   uint32
	ranks      uint32
	bankgroups uint32
	banks      uint32
	rows       uint32
	columns    uint32
}

// NewAddressMapping 创建地址映射
//
// 对应 ChampSim 的 DRAM_ADDRESS_MAPPING 构造函数
func NewAddressMapping(config DRAMConfig) (*AddressMapping, error) {
	// 验证配置都是2的幂
	if !isPowerOfTwo(config.Channels) {
		return nil, fmt.Errorf("Channels must be power of 2, got %d", config.Channels)
	}
	if !isPowerOfTwo(config.Ranks) {
		return nil, fmt.Errorf("Ranks must be power of 2, got %d", config.Ranks)
	}
	if !isPowerOfTwo(config.BankGroups) {
		return nil, fmt.Errorf("BankGroups must be power of 2, got %d", config.BankGroups)
	}
	if !isPowerOfTwo(config.Banks) {
		return nil, fmt.Errorf("Banks must be power of 2, got %d", config.Banks)
	}
	if !isPowerOfTwo(config.Rows) {
		return nil, fmt.Errorf("Rows must be power of 2, got %d", config.Rows)
	}
	if !isPowerOfTwo(config.Columns) {
		return nil, fmt.Errorf("Columns must be power of 2, got %d", config.Columns)
	}
	if !isPowerOfTwo(config.ChannelWidth) {
		return nil, fmt.Errorf("ChannelWidth must be power of 2, got %d", config.ChannelWidth)
	}

	// 计算位数
	offsetBits := bits.TrailingZeros32(config.ChannelWidth)
	channelBits := bits.TrailingZeros32(config.Channels)
	bankgroupBits := bits.TrailingZeros32(config.BankGroups)
	bankBits := bits.TrailingZeros32(config.Banks)
	columnBits := bits.TrailingZeros32(config.Columns)
	rankBits := bits.TrailingZeros32(config.Ranks)
	rowBits := bits.TrailingZeros32(config.Rows)

	// 计算掩码
	channelMask := uint64(config.Channels - 1)
	bankgroupMask := uint64(config.BankGroups - 1)
	bankMask := uint64(config.Banks - 1)
	columnMask := uint64(config.Columns - 1)
	rankMask := uint64(config.Ranks - 1)
	rowMask := uint64(config.Rows - 1)

	return &AddressMapping{
		offsetBits:    uint32(offsetBits),
		channelBits:   uint32(channelBits),
		bankgroupBits: uint32(bankgroupBits),
		bankBits:      uint32(bankBits),
		columnBits:    uint32(columnBits),
		rankBits:      uint32(rankBits),
		rowBits:       uint32(rowBits),

		channelMask:   channelMask,
		bankgroupMask: bankgroupMask,
		bankMask:      bankMask,
		columnMask:    columnMask,
		rankMask:      rankMask,
		rowMask:       rowMask,

		channels:   config.Channels,
		ranks:      config.Ranks,
		bankgroups: config.BankGroups,
		banks:      config.Banks,
		rows:       config.Rows,
		columns:    config.Columns,
	}, nil
}

// ==================== 地址分解 ====================

// GetChannel 获取Channel索引
//
// Address Layout (from LSB to MSB):
// [Offset | Channel | BankGroup | Bank | Column | Rank | Row]
func (m *AddressMapping) GetChannel(addr uint64) uint64 {
	shift := m.offsetBits
	return (addr >> shift) & m.channelMask
}

// GetBankGroup 获取BankGroup索引
func (m *AddressMapping) GetBankGroup(addr uint64) uint64 {
	shift := m.offsetBits + m.channelBits
	return (addr >> shift) & m.bankgroupMask
}

// GetBank 获取Bank索引
func (m *AddressMapping) GetBank(addr uint64) uint64 {
	shift := m.offsetBits + m.channelBits + m.bankgroupBits
	return (addr >> shift) & m.bankMask
}

// GetColumn 获取Column地址
func (m *AddressMapping) GetColumn(addr uint64) uint64 {
	shift := m.offsetBits + m.channelBits + m.bankgroupBits + m.bankBits
	return (addr >> shift) & m.columnMask
}

// GetRank 获取Rank索引
func (m *AddressMapping) GetRank(addr uint64) uint64 {
	shift := m.offsetBits + m.channelBits + m.bankgroupBits + m.bankBits + m.columnBits
	return (addr >> shift) & m.rankMask
}

// GetRow 获取Row地址
func (m *AddressMapping) GetRow(addr uint64) uint64 {
	shift := m.offsetBits + m.channelBits + m.bankgroupBits + m.bankBits + m.columnBits + m.rankBits
	return (addr >> shift) & m.rowMask
}

// ==================== Bank索引计算 ====================

// GetBankIndex 计算Bank在BankRequest数组中的索引
//
// 对应 ChampSim 的 bank_request_index()
//
// Index = Rank * BankGroups * Banks + BankGroup * Banks + Bank
func (m *AddressMapping) GetBankIndex(addr uint64) uint64 {
	rank := m.GetRank(addr)
	bankgroup := m.GetBankGroup(addr)
	bank := m.GetBank(addr)

	return rank*uint64(m.bankgroups)*uint64(m.banks) +
		bankgroup*uint64(m.banks) +
		bank
}

// GetBankGroupIndex 计算BankGroup在数组中的索引
//
// 对应 ChampSim 的 bankgroup_request_index()
//
// Index = Rank * BankGroups + BankGroup
func (m *AddressMapping) GetBankGroupIndex(addr uint64) uint64 {
	rank := m.GetRank(addr)
	bankgroup := m.GetBankGroup(addr)

	return rank*uint64(m.bankgroups) + bankgroup
}

// ==================== 辅助方法 ====================

// TotalBanks 返回总Bank数量
func (m *AddressMapping) TotalBanks() uint32 {
	return m.ranks * m.bankgroups * m.banks
}

// TotalBankGroups 返回总BankGroup数量
func (m *AddressMapping) TotalBankGroups() uint32 {
	return m.ranks * m.bankgroups
}

// Channels 返回Channel数量
func (m *AddressMapping) Channels() uint32 {
	return m.channels
}

// Ranks 返回Rank数量
func (m *AddressMapping) Ranks() uint32 {
	return m.ranks
}

// BankGroups 返回BankGroup数量
func (m *AddressMapping) BankGroups() uint32 {
	return m.bankgroups
}

// Banks 返回Bank数量
func (m *AddressMapping) Banks() uint32 {
	return m.banks
}

// Rows 返回Row数量
func (m *AddressMapping) Rows() uint32 {
	return m.rows
}

// Columns 返回Column数量
func (m *AddressMapping) Columns() uint32 {
	return m.columns
}

// isPowerOfTwo 检查是否为2的幂
func isPowerOfTwo(n uint32) bool {
	return n > 0 && (n&(n-1)) == 0
}
