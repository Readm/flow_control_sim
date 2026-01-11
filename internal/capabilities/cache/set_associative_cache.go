package cache

import (
	"fmt"

	compcache "github.com/Readm/flow_sim/internal/components/cache"
)

// SetAssociativeCache Set-Associative Cache
//
// 对应 ChampSim 的 CACHE 类 (Wrapper around components/cache)
type SetAssociativeCache struct {
	// ==================== 配置 ====================

	config compcache.CacheConfig

	// ==================== Core Cache Component ====================

	core *compcache.SetAssociativeCache

	// ==================== 运行时状态 ====================

	// currentCycle 当前周期
	currentCycle uint64

	// standaloneMode 独立模式（自动完成miss，用于测试）
	standaloneMode bool

	// ==================== MSHR ====================

	// mshr MSHR队列
	mshr *MSHRQueue

	// ==================== 下级存储 ====================

	// lowerLevel 下级存储接口（DRAM或L2 Cache）
	// nil表示没有下级（standalone模式会自动fill）
	lowerLevel interface {
		SendRequest(req interface{}) bool
		Tick()
		SetCycle(cycle uint64)
	}

	// ==================== 统计信息 ====================

	stats CacheStats
}

// NewSetAssociativeCache 创建新的 Set-Associative Cache
func NewSetAssociativeCache(config compcache.CacheConfig) (*SetAssociativeCache, error) {
	// 验证配置
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	// Initialize Core Cache
	core := compcache.NewSetAssociativeCache(int(config.NumSets), int(config.NumWays), uint64(config.BlockSize))

	cache := &SetAssociativeCache{
		config:         config,
		core:           core,
		currentCycle:   0,
		standaloneMode: true, // 默认standalone模式
		mshr:           NewMSHRQueue(int(config.MSHRSize)),
		stats:          CacheStats{},
	}

	return cache, nil
}

// validateConfig 验证配置的合法性
func validateConfig(config compcache.CacheConfig) error {
	// NumSets 必须是2的幂
	if config.NumSets == 0 || (config.NumSets&(config.NumSets-1)) != 0 {
		return fmt.Errorf("NumSets must be power of 2, got %d", config.NumSets)
	}

	// NumWays 必须 > 0
	if config.NumWays == 0 {
		return fmt.Errorf("NumWays must be > 0, got %d", config.NumWays)
	}

	// BlockSize 必须是2的幂
	if config.BlockSize == 0 || (config.BlockSize&(config.BlockSize-1)) != 0 {
		return fmt.Errorf("BlockSize must be power of 2, got %d", config.BlockSize)
	}

	// MSHRSize 必须 > 0
	if config.MSHRSize == 0 {
		return fmt.Errorf("MSHRSize must be > 0, got %d", config.MSHRSize)
	}

	return nil
}

// ==================== 基本操作 ====================

// SetStandaloneMode 设置独立模式
//
// standalone=true: Miss自动立即完成（用于测试）
// standalone=false: Miss等待Fill（用于集成）
func (c *SetAssociativeCache) SetStandaloneMode(standalone bool) {
	c.standaloneMode = standalone
}

// SetLowerLevel 设置下级存储
//
// 参数：
// - lowerLevel: 实现MemoryInterface的下级存储（DRAM或L2 Cache）
//
// 设置后，Cache miss时会自动向下级发送请求
func (c *SetAssociativeCache) SetLowerLevel(lowerLevel interface {
	SendRequest(req interface{}) bool
	Tick()
	SetCycle(cycle uint64)
}) {
	c.lowerLevel = lowerLevel
	// 设置下级后，自动关闭standalone模式
	if lowerLevel != nil {
		c.standaloneMode = false
	}
}

// SetCycle 设置当前周期
func (c *SetAssociativeCache) SetCycle(cycle uint64) {
	c.currentCycle = cycle
}

// GetStats 获取统计信息
func (c *SetAssociativeCache) GetStats() interface{} {
	return c.stats
}

// ResetStats 重置统计信息
func (c *SetAssociativeCache) ResetStats() {
	c.stats = CacheStats{}
	c.core.ResetStats()
}

// ==================== 统计信息 ====================

// CacheStats Cache统计信息
type CacheStats struct {
	// 访问统计
	Accesses   uint64 // 总访问次数
	Hits       uint64 // 命中次数
	Misses     uint64 // 未命中次数
	Prefetches uint64 // 预取次数

	// 写回统计
	Writebacks uint64 // 写回次数（dirty block被替换）

	// MSHR 统计
	MSHRFull uint64 // MSHR满的次数

	// Load/Store 统计
	Loads       uint64 // Load访问次数
	Stores      uint64 // Store访问次数
	LoadHits    uint64 // Load命中次数
	StoreHits   uint64 // Store命中次数
	LoadMisses  uint64 // Load未命中次数
	StoreMisses uint64 // Store未命中次数
}

// HitRate 计算命中率
func (s *CacheStats) HitRate() float64 {
	if s.Accesses == 0 {
		return 0
	}
	return float64(s.Hits) / float64(s.Accesses)
}

// MissRate 计算未命中率
func (s *CacheStats) MissRate() float64 {
	if s.Accesses == 0 {
		return 0
	}
	return float64(s.Misses) / float64(s.Accesses)
}

// ==================== Access 和 Fill 接口 ====================

// Access 处理访问请求
//
// 参数：
// - addr: 访问地址
// - vaddr: 虚拟地址
// - instrID: 指令ID
// - accessType: 访问类型（0=Load, 1=Store, 2=Prefetch）
// - cycle: 当前周期
//
// 返回：
// - hit: 是否命中
// - readyCycle: 数据就绪周期
// - mshrIndex: MSHR索引（如果miss）
func (c *SetAssociativeCache) Access(
	addr uint64,
	vaddr uint64,
	instrID uint64,
	at compcache.AccessType,
	cycle uint64,
) (hit bool, readyCycle uint64, mshrIndex int) {

	c.currentCycle = cycle

	// 更新统计
	c.stats.Accesses++
	if at == compcache.AccessLoad {
		c.stats.Loads++
	} else if at == compcache.AccessStore {
		c.stats.Stores++
	} else if at == compcache.AccessPrefetch {
		c.stats.Prefetches++
	}

	// 使用 Core Access
	isWrite := (at == compcache.AccessStore)
	result := c.core.Access(addr, isWrite)

	if result.Hit {
		// Hit
		c.stats.Hits++
		if at == compcache.AccessLoad {
			c.stats.LoadHits++
		} else if at == compcache.AccessStore {
			c.stats.StoreHits++
		}

		// 数据在HitLatency后就绪
		readyCycle = cycle + c.config.HitLatency
		return true, readyCycle, -1
	}

	// Miss: 处理miss
	c.stats.Misses++
	if at == compcache.AccessLoad {
		c.stats.LoadMisses++
	} else if at == compcache.AccessStore {
		c.stats.StoreMisses++
	}

	// 这里的 handleMiss 和以前一样调用
	return c.handleMiss(addr, vaddr, instrID, at, cycle)
}

// handleMiss 处理 miss
//
// 返回：
// - hit: false（miss）
// - readyCycle: 数据就绪周期
// - mshrIndex: MSHR索引
func (c *SetAssociativeCache) handleMiss(
	addr uint64,
	vaddr uint64,
	instrID uint64,
	accessType compcache.AccessType,
	cycle uint64,
) (hit bool, readyCycle uint64, mshrIndex int) {
	// Re-alignment logic just to be safe
	blockSize := uint64(c.config.BlockSize)
	blockAddr := (addr / blockSize) * blockSize

	existingIndex, found := c.mshr.Find(blockAddr)

	if found {
		// 合并到已存在的MSHR
		c.mshr.Merge(existingIndex, instrID)

		// 返回已存在MSHR的就绪时间
		entry := c.mshr.GetAll()[existingIndex]
		return false, entry.ReadyCycle, existingIndex
	}

	// 检查MSHR是否已满
	if c.mshr.IsFull() {
		c.stats.MSHRFull++
		// MSHR满时，返回一个较晚的就绪时间
		// 在实际系统中，这会导致请求被阻塞
		return false, cycle + c.config.HitLatency + c.config.FillLatency, -1
	}

	// 分配新的MSHR条目
	entry := &MSHREntry{
		Address:          blockAddr,
		VAddress:         vaddr,
		InstrID:          instrID,
		CPU:              c.config.CPU,
		Type:             accessType,
		EnqueueCycle:     cycle,
		ReadyCycle:       cycle + c.config.FillLatency,
		InstrDependOnMe:  []uint64{instrID},
		Merged:           false,
		MergedWith:       -1,
		PrefetchFromThis: false,
	}

	index, success := c.mshr.Allocate(entry)
	if !success {
		// 分配失败（不应该发生，因为已经检查了IsFull）
		return false, cycle + c.config.HitLatency + c.config.FillLatency, -1
	}

	// 向下级发送请求（如果有下级）
	// 注意：发送给下级的应该是 Block Address
	if c.lowerLevel != nil {
		// 创建请求（使用interface{}的map结构，避免类型断言问题）
		req := map[string]interface{}{
			"Address":  blockAddr,
			"VAddress": vaddr,
			"InstrID":  instrID,
			"IsWrite":  (accessType == compcache.AccessStore),
			"Data":     uint64(0),
			"Callback": func(fillAddr uint64, fillData uint64, fillCycle uint64) {
				// 下级返回数据时，填充到Cache
				c.HandleFill(fillAddr, fillData, fillCycle)
			},
		}

		// 发送请求
		c.lowerLevel.SendRequest(req)
	} else if c.standaloneMode {
		// Standalone模式：自动完成fill（用于测试）
		c.HandleFill(blockAddr, 0, cycle+c.config.FillLatency)
	}

	return false, entry.ReadyCycle, index
}

// HandleFill 处理 Fill 响应（来自下级cache或内存）
//
// 参数：
// - addr: 填充地址
// - data: 数据（可选）
// - cycle: 当前周期
//
// 返回：
// - success: 是否成功填充
func (c *SetAssociativeCache) HandleFill(addr uint64, data uint64, cycle uint64) bool {
	c.currentCycle = cycle

	// 对齐到block边界
	blockSize := uint64(c.config.BlockSize)
	blockAddr := (addr / blockSize) * blockSize

	// 从MSHR中移除
	entry := c.mshr.RemoveByAddress(blockAddr)
	if entry == nil {
		// MSHR中没有对应条目，可能是预取
		// 仍然尝试填充cache
	}

	// 准备数据
	dataBytes := make([]byte, 8)
	// (Binary encoding omitted for brevity, passing empty or mock)

	// Fill Core Cache
	// State 默认为 Shared 或 Exclusive?
	// 简化起见，使用 Exclusive
	_, needWriteback := c.core.Fill(blockAddr, dataBytes, compcache.StateExclusive)

	if needWriteback {
		c.stats.Writebacks++
		// 实际系统中，这里会向下级发送writeback请求
		// TODO: Implement Writeback to lower level if needed
	}

	// 更新MSHR的就绪时间
	if entry != nil {
		entry.ReadyCycle = cycle
	}

	return true
}

// GetMSHRStats 获取MSHR统计信息
func (c *SetAssociativeCache) GetMSHRStats() (size int, capacity int) {
	return c.mshr.Size(), c.mshr.maxSize
}
