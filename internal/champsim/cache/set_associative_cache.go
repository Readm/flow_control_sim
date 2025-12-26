package cache

import (
	"fmt"
	"math/bits"
)

// SetAssociativeCache Set-Associative Cache
//
// 对应 ChampSim 的 CACHE 类
//
// 结构：
// - NUM_SET 个 set
// - 每个 set 有 NUM_WAY 个 way
// - 使用 LRU 替换策略
//
// 地址分解：
// ┌─────────────┬──────────────┬──────────────┐
// │    Tag      │  Set Index   │    Offset    │
// └─────────────┴──────────────┴──────────────┘
type SetAssociativeCache struct {
	// ==================== 配置 ====================

	config CacheConfig

	// ==================== Cache 存储 ====================

	// blocks: 二维数组 [set][way]
	// 总共 NumSets * NumWays 个 blocks
	blocks [][]CacheBlock

	// ==================== 地址分解参数 ====================

	// offsetBits: Offset 位数 = log2(BlockSize)
	offsetBits uint32

	// setIndexBits: Set Index 位数 = log2(NumSets)
	setIndexBits uint32

	// setMask: Set Index 掩码
	setMask uint64

	// tagShift: Tag 右移位数 = offsetBits + setIndexBits
	tagShift uint32

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
func NewSetAssociativeCache(config CacheConfig) (*SetAssociativeCache, error) {
	// 验证配置
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	// 计算地址分解参数
	offsetBits := bits.TrailingZeros32(config.BlockSize)
	setIndexBits := bits.TrailingZeros32(config.NumSets)
	setMask := uint64(config.NumSets - 1)
	tagShift := uint32(offsetBits + setIndexBits)

	// 分配 blocks 数组
	blocks := make([][]CacheBlock, config.NumSets)
	for i := range blocks {
		blocks[i] = make([]CacheBlock, config.NumWays)
	}

	cache := &SetAssociativeCache{
		config:         config,
		blocks:         blocks,
		offsetBits:     uint32(offsetBits),
		setIndexBits:   uint32(setIndexBits),
		setMask:        setMask,
		tagShift:       tagShift,
		currentCycle:   0,
		standaloneMode: true, // 默认standalone模式
		mshr:           NewMSHRQueue(int(config.MSHRSize)),
		stats:          CacheStats{},
	}

	return cache, nil
}

// validateConfig 验证配置的合法性
func validateConfig(config CacheConfig) error {
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

// ==================== 地址分解 ====================

// getSetIndex 获取 Set Index
//
// 地址分解：
// ┌─────────────┬──────────────┬──────────────┐
// │    Tag      │  Set Index   │    Offset    │
// └─────────────┴──────────────┴──────────────┘
//
// Set Index = (addr >> offsetBits) & setMask
func (c *SetAssociativeCache) getSetIndex(addr uint64) uint32 {
	return uint32((addr >> c.offsetBits) & c.setMask)
}

// getTag 获取 Tag
//
// Tag = addr >> (offsetBits + setIndexBits)
func (c *SetAssociativeCache) getTag(addr uint64) uint64 {
	return addr >> c.tagShift
}

// getBlockAddr 获取 Block 对齐的地址
//
// Block Addr = (addr >> offsetBits) << offsetBits
func (c *SetAssociativeCache) getBlockAddr(addr uint64) uint64 {
	return (addr >> c.offsetBits) << c.offsetBits
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
}

// ==================== 查找操作 ====================

// findBlock 在指定 set 中查找 block
//
// 返回：
// - way: 找到的 way 索引（如果 hit）
// - hit: 是否命中
func (c *SetAssociativeCache) findBlock(setIndex uint32, tag uint64) (way uint32, hit bool) {
	set := c.blocks[setIndex]

	for i := uint32(0); i < c.config.NumWays; i++ {
		block := &set[i]
		if block.Valid && c.getTag(block.Address) == tag {
			return i, true
		}
	}

	return 0, false
}

// ==================== LRU 操作 ====================

// findVictim 使用 LRU 策略查找 victim way
//
// 返回应该被替换的 way 索引
func (c *SetAssociativeCache) findVictim(setIndex uint32) uint32 {
	set := c.blocks[setIndex]

	// 首先查找无效的 way
	for i := uint32(0); i < c.config.NumWays; i++ {
		if !set[i].Valid {
			return i
		}
	}

	// 如果所有 way 都有效，查找 LRU 最小的 way
	victimWay := uint32(0)
	minLRU := set[0].LRU

	for i := uint32(1); i < c.config.NumWays; i++ {
		if set[i].LRU < minLRU {
			minLRU = set[i].LRU
			victimWay = i
		}
	}

	return victimWay
}

// updateLRU 更新 LRU 计数器
//
// 将访问的 way 的 LRU 设置为当前周期
func (c *SetAssociativeCache) updateLRU(setIndex uint32, way uint32) {
	c.blocks[setIndex][way].LRU = c.currentCycle
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
	accessType uint8,
	cycle uint64,
) (hit bool, readyCycle uint64, mshrIndex int) {
	// 转换为AccessType
	at := AccessType(accessType)
	c.currentCycle = cycle

	// 对齐到block边界
	blockAddr := c.getBlockAddr(addr)

	// 获取set和tag
	setIndex := c.getSetIndex(blockAddr)
	tag := c.getTag(blockAddr)

	// 更新统计
	c.stats.Accesses++
	if at == AccessLoad {
		c.stats.Loads++
	} else if at == AccessStore {
		c.stats.Stores++
	} else if at == AccessPrefetch {
		c.stats.Prefetches++
	}

	// 查找block
	way, hit := c.findBlock(setIndex, tag)

	if hit {
		// Hit: 更新LRU和统计
		c.updateLRU(setIndex, way)

		c.stats.Hits++
		if at == AccessLoad {
			c.stats.LoadHits++
		} else if at == AccessStore {
			c.stats.StoreHits++
			// Store hit: 标记为dirty
			c.blocks[setIndex][way].Dirty = true
		}

		// 数据在HitLatency后就绪
		readyCycle = cycle + c.config.HitLatency
		return true, readyCycle, -1
	}

	// Miss: 处理miss
	c.stats.Misses++
	if at == AccessLoad {
		c.stats.LoadMisses++
	} else if at == AccessStore {
		c.stats.StoreMisses++
	}

	return c.handleMiss(blockAddr, vaddr, instrID, at, cycle)
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
	accessType AccessType,
	cycle uint64,
) (hit bool, readyCycle uint64, mshrIndex int) {
	// 检查MSHR中是否已有相同地址的请求
	existingIndex, found := c.mshr.Find(addr)

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
		Address:          addr,
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
	if c.lowerLevel != nil {
		// 创建请求（使用interface{}的map结构，避免类型断言问题）
		req := map[string]interface{}{
			"Address":  addr,
			"VAddress": vaddr,
			"InstrID":  instrID,
			"IsWrite":  (accessType == AccessStore),
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
		c.HandleFill(addr, 0, cycle+c.config.FillLatency)
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
	blockAddr := c.getBlockAddr(addr)

	// 从MSHR中移除
	entry := c.mshr.RemoveByAddress(blockAddr)
	if entry == nil {
		// MSHR中没有对应条目，可能是预取
		// 仍然尝试填充cache
	}

	// 获取set和tag
	setIndex := c.getSetIndex(blockAddr)
	_ = c.getTag(blockAddr) // tag已经包含在blockAddr中

	// 查找victim way
	way := c.findVictim(setIndex)

	// 检查是否需要写回
	block := &c.blocks[setIndex][way]
	if block.Valid && block.Dirty {
		// 需要写回dirty block
		c.stats.Writebacks++
		// 实际系统中，这里会向下级发送writeback请求
	}

	// 填充新数据
	block.Valid = true
	block.Address = blockAddr
	block.VAddress = 0
	if entry != nil {
		block.VAddress = entry.VAddress
		block.Prefetch = (entry.Type == AccessPrefetch)
	}
	block.Data = data
	block.Dirty = false
	block.PfMetadata = 0

	// 更新LRU
	c.updateLRU(setIndex, way)

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
