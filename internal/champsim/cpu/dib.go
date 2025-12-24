package cpu

import (
	"math/bits"
)

// DIB (Decoded Instruction Buffer) 类似于 Intel CPU 的 uop cache
//
// 工作原理：
// - 缓存最近译码的指令，避免重复译码
// - 按指令地址（IP）的高位索引
// - 使用 LRU 替换策略
//
// 命中时：
// - 跳过 Fetch 和 Decode 阶段
// - 直接进入 Dispatch
// - 减少前端延迟
type DIB struct {
	// entries 缓存条目（按 IP 索引）
	// key: IP 的高位（去除偏移）
	// value: DIB 条目
	entries map[uint64]*DIBEntry

	// maxSize 最大条目数
	maxSize int

	// shiftAmount IP 右移位数（用于索引）
	// 例如：shift=6 表示缓存行大小为 64 字节
	shiftAmount uint

	// currentTime 当前时间（用于 LRU）
	currentTime uint64
}

// DIBEntry DIB 缓存条目
type DIBEntry struct {
	// ip 完整的指令地址
	ip uint64

	// valid 条目是否有效
	valid bool

	// lastAccessTime 最后访问时间（LRU）
	lastAccessTime uint64
}

// NewDIB 创建新的 DIB
//
// 参数：
//   - maxSize: 最大条目数（典型值：1024-4096）
//   - shiftAmount: IP 索引位移（典型值：6，对应 64 字节缓存行）
func NewDIB(maxSize int, shiftAmount uint) *DIB {
	return &DIB{
		entries:     make(map[uint64]*DIBEntry, maxSize),
		maxSize:     maxSize,
		shiftAmount: shiftAmount,
		currentTime: 0,
	}
}

// Check 检查指令是否在 DIB 中
//
// 如果命中，更新访问时间。
func (dib *DIB) Check(ip uint64) bool {
	// 计算索引（去除低位偏移）
	index := dib.getIndex(ip)

	// 查找条目
	entry, exists := dib.entries[index]
	if !exists || !entry.valid {
		return false
	}

	// 检查 IP 是否完全匹配（处理别名）
	if entry.ip != ip {
		return false
	}

	// 命中：更新访问时间
	entry.lastAccessTime = dib.currentTime
	dib.currentTime++

	return true
}

// Insert 插入指令到 DIB
//
// 如果 DIB 已满，使用 LRU 策略驱逐最旧的条目。
func (dib *DIB) Insert(ip uint64) {
	index := dib.getIndex(ip)

	// 检查是否已存在
	if entry, exists := dib.entries[index]; exists {
		// 更新现有条目
		entry.ip = ip
		entry.valid = true
		entry.lastAccessTime = dib.currentTime
		dib.currentTime++
		return
	}

	// 检查是否已满
	if len(dib.entries) >= dib.maxSize {
		dib.evict()
	}

	// 插入新条目
	dib.entries[index] = &DIBEntry{
		ip:             ip,
		valid:          true,
		lastAccessTime: dib.currentTime,
	}
	dib.currentTime++
}

// evict 使用 LRU 策略驱逐一个条目
func (dib *DIB) evict() {
	if len(dib.entries) == 0 {
		return
	}

	// 查找最旧的条目
	var oldestIndex uint64
	var oldestTime uint64 = ^uint64(0) // MaxUint64

	for index, entry := range dib.entries {
		if entry.lastAccessTime < oldestTime {
			oldestTime = entry.lastAccessTime
			oldestIndex = index
		}
	}

	// 删除最旧的条目
	delete(dib.entries, oldestIndex)
}

// getIndex 计算 IP 的索引
//
// 使用 IP 的高位作为索引，去除低位偏移。
func (dib *DIB) getIndex(ip uint64) uint64 {
	return ip >> dib.shiftAmount
}

// Invalidate 使某个地址的条目无效
//
// 用于自修改代码或代码重定位。
func (dib *DIB) Invalidate(ip uint64) {
	index := dib.getIndex(ip)
	if entry, exists := dib.entries[index]; exists {
		entry.valid = false
	}
}

// Clear 清空所有条目
func (dib *DIB) Clear() {
	dib.entries = make(map[uint64]*DIBEntry, dib.maxSize)
	dib.currentTime = 0
}

// Size 返回当前条目数
func (dib *DIB) Size() int {
	validCount := 0
	for _, entry := range dib.entries {
		if entry.valid {
			validCount++
		}
	}
	return validCount
}

// HitRate 返回命中率统计（需要配合外部计数器）
type DIBStats struct {
	Hits   uint64
	Misses uint64
}

// CalculateHitRate 计算命中率
func (stats *DIBStats) CalculateHitRate() float64 {
	total := stats.Hits + stats.Misses
	if total == 0 {
		return 0.0
	}
	return float64(stats.Hits) / float64(total)
}

// DefaultDIBSize 默认 DIB 大小
const DefaultDIBSize = 2048

// DefaultDIBShift 默认 DIB 索引位移（对应 64 字节）
const DefaultDIBShift = 6

// CalculateOptimalShift 根据缓存行大小计算最优位移量
func CalculateOptimalShift(cacheLineSize int) uint {
	// 找到大于等于 cacheLineSize 的最小 2 的幂次
	if cacheLineSize <= 0 {
		return DefaultDIBShift
	}
	return uint(bits.Len(uint(cacheLineSize - 1)))
}
