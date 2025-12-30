package cache

// AccessType 访问类型
//
// 对应 ChampSim 的 access_type
type AccessType uint8

const (
	// AccessLoad Load访问（读取）
	AccessLoad AccessType = iota

	// AccessStore Store访问（写入）
	AccessStore

	// AccessPrefetch Prefetch访问（预取）
	AccessPrefetch

	// AccessRFO Read-For-Ownership（用于写之前的读取）
	AccessRFO

	// AccessTranslation 地址翻译访问
	AccessTranslation
)

// String 返回访问类型的字符串表示
func (at AccessType) String() string {
	switch at {
	case AccessLoad:
		return "LOAD"
	case AccessStore:
		return "STORE"
	case AccessPrefetch:
		return "PREFETCH"
	case AccessRFO:
		return "RFO"
	case AccessTranslation:
		return "TRANSLATION"
	default:
		return "UNKNOWN"
	}
}

// CacheConfig Cache配置
//
// 对应 ChampSim cache 的配置参数
type CacheConfig struct {
	// ==================== 容量参数 ====================

	// NumSets Set 数量
	// 必须是2的幂
	NumSets uint32

	// NumWays Way 数量（每个Set的关联度）
	// 例如：8-way set-associative cache
	NumWays uint32

	// BlockSize Cache line 大小（字节）
	// 通常是64字节
	// 必须是2的幂
	BlockSize uint32

	// ==================== MSHR 参数 ====================

	// MSHRSize MSHR (Miss Status Holding Registers) 大小
	// 用于跟踪未完成的miss请求
	// 典型值：8-16
	MSHRSize uint32

	// ==================== 延迟参数 ====================

	// HitLatency Hit 延迟（cycles）
	// L1: 通常 4 cycles
	// L2: 通常 10 cycles
	// LLC: 通常 20 cycles
	HitLatency uint64

	// FillLatency Fill 延迟（cycles）
	// 从下级cache/内存填充数据的延迟
	// 通常与 HitLatency 相同或稍长
	FillLatency uint64

	// ==================== 其他参数 ====================

	// Name Cache名称（用于调试和统计）
	Name string

	// CPU CPU ID（用于多核系统）
	CPU uint32

	// PrefetchAsLoad 是否将预取当作Load处理
	// true: 预取miss会阻塞后续请求
	// false: 预取miss不阻塞
	PrefetchAsLoad bool
}

// DefaultL1DConfig 返回默认的 L1D Cache 配置
//
// 参考 ChampSim 默认配置
func DefaultL1DConfig() CacheConfig {
	return CacheConfig{
		NumSets:        64, // 64 sets
		NumWays:        8,  // 8-way
		BlockSize:      64, // 64 bytes
		MSHRSize:       8,  // 8 MSHRs
		HitLatency:     4,  // 4 cycles
		FillLatency:    4,  // 4 cycles
		Name:           "L1D",
		CPU:            0,
		PrefetchAsLoad: false,
	}
}

// DefaultL2CConfig 返回默认的 L2 Cache 配置
func DefaultL2CConfig() CacheConfig {
	return CacheConfig{
		NumSets:        512, // 512 sets
		NumWays:        8,   // 8-way
		BlockSize:      64,  // 64 bytes
		MSHRSize:       16,  // 16 MSHRs
		HitLatency:     10,  // 10 cycles
		FillLatency:    10,  // 10 cycles
		Name:           "L2C",
		CPU:            0,
		PrefetchAsLoad: false,
	}
}

// DefaultLLCConfig 返回默认的 LLC (Last Level Cache) 配置
func DefaultLLCConfig() CacheConfig {
	return CacheConfig{
		NumSets:        2048, // 2048 sets
		NumWays:        16,   // 16-way
		BlockSize:      64,   // 64 bytes
		MSHRSize:       32,   // 32 MSHRs
		HitLatency:     20,   // 20 cycles
		FillLatency:    20,   // 20 cycles
		Name:           "LLC",
		CPU:            0,
		PrefetchAsLoad: false,
	}
}
