package cache

// CacheBlock 对应 ChampSim 的 champsim::cache_block
//
// Cache block 是 cache 的基本存储单元，包含：
// - 状态标志（valid, prefetch, dirty）
// - 地址信息（物理地址tag，虚拟地址）
// - LRU 替换信息
//
// 对应 ChampSim 源码: inc/block.h
type CacheBlock struct {
	// ==================== 状态标志 ====================

	// Valid 是否有效
	// true: block包含有效数据
	// false: block为空或无效
	Valid bool

	// Prefetch 是否是预取的
	// true: 该block由预取器填充
	// false: 该block由demand请求填充
	Prefetch bool

	// Dirty 是否被修改（需要写回）
	// true: 数据已被修改，需要写回下级
	// false: 数据未被修改，可以直接丢弃
	Dirty bool

	// ==================== 地址信息 ====================

	// Address 物理地址（完整地址，包含tag）
	// ChampSim中是 champsim::address 类型
	Address uint64

	// VAddress 虚拟地址
	// 用于虚拟地址相关的操作
	VAddress uint64

	// Data 数据地址
	// ChampSim中用于存储数据内容的地址
	Data uint64

	// ==================== 预取元数据 ====================

	// PfMetadata 预取元数据
	// 预取器特定的元数据，用于预取器算法
	PfMetadata uint32

	// ==================== LRU 替换信息 ====================

	// LRU LRU计数器
	// 记录最后一次访问的周期数，用于LRU替换策略
	// 值越小表示越久未被访问
	LRU uint64
}

// Invalidate 使cache block无效
func (b *CacheBlock) Invalidate() {
	b.Valid = false
	b.Prefetch = false
	b.Dirty = false
	b.Address = 0
	b.VAddress = 0
	b.Data = 0
	b.PfMetadata = 0
	// LRU 不清零，保留历史信息
}
