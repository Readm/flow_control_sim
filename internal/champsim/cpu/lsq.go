package cpu

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/champsim/instruction"
)

// LoadStoreQueue 管理 Load 和 Store 操作
//
// LSQ 的核心功能：
// 1. 内存顺序保证：确保 load/store 按程序顺序执行
// 2. Store-to-Load Forwarding：如果 load 的地址与前面的 store 匹配，直接转发数据
// 3. 依赖跟踪：跟踪哪些 load 依赖于哪些 store
type LoadStoreQueue struct {
	// Load Queue：存储待执行和执行中的 load 操作
	loadQueue []*LSQEntry

	// Store Queue：存储待执行和执行中的 store 操作
	storeQueue []*LSQEntry

	// 容量限制
	maxLQSize int // Load Queue 最大大小
	maxSQSize int // Store Queue 最大大小

	// 统计信息
	stats LSQStats
}

// LSQStats LSQ 统计信息
type LSQStats struct {
	TotalLoads          uint64 // 总 load 数
	TotalStores         uint64 // 总 store 数
	ForwardedLoads      uint64 // 被转发的 load 数
	LoadQueueFull       uint64 // LQ 满的次数
	StoreQueueFull      uint64 // SQ 满的次数
	AverageLoadLatency  float64
	AverageStoreLatency float64
}

// NewLoadStoreQueue 创建新的 LSQ
func NewLoadStoreQueue(maxLQSize, maxSQSize int) *LoadStoreQueue {
	return &LoadStoreQueue{
		loadQueue:  make([]*LSQEntry, 0, maxLQSize),
		storeQueue: make([]*LSQEntry, 0, maxSQSize),
		maxLQSize:  maxLQSize,
		maxSQSize:  maxSQSize,
		stats:      LSQStats{},
	}
}

// ==================== Load Queue 操作 ====================

// AddLoad 添加 load 操作到 LQ
func (lsq *LoadStoreQueue) AddLoad(entry *LSQEntry) error {
	if lsq.IsLoadQueueFull() {
		lsq.stats.LoadQueueFull++
		return fmt.Errorf("load queue is full")
	}

	lsq.loadQueue = append(lsq.loadQueue, entry)
	lsq.stats.TotalLoads++
	return nil
}

// RemoveLoad 从 LQ 移除 load（当指令退休时）
func (lsq *LoadStoreQueue) RemoveLoad(instrID uint64) bool {
	for i, entry := range lsq.loadQueue {
		if entry.InstrID == instrID {
			// 从切片中删除
			lsq.loadQueue = append(lsq.loadQueue[:i], lsq.loadQueue[i+1:]...)
			return true
		}
	}
	return false
}

// IsLoadQueueFull 检查 LQ 是否已满
func (lsq *LoadStoreQueue) IsLoadQueueFull() bool {
	return len(lsq.loadQueue) >= lsq.maxLQSize
}

// ==================== Store Queue 操作 ====================

// AddStore 添加 store 操作到 SQ
func (lsq *LoadStoreQueue) AddStore(entry *LSQEntry) error {
	if lsq.IsStoreQueueFull() {
		lsq.stats.StoreQueueFull++
		return fmt.Errorf("store queue is full")
	}

	lsq.storeQueue = append(lsq.storeQueue, entry)
	lsq.stats.TotalStores++
	return nil
}

// RemoveStore 从 SQ 移除 store（当指令退休时）
func (lsq *LoadStoreQueue) RemoveStore(instrID uint64) bool {
	for i, entry := range lsq.storeQueue {
		if entry.InstrID == instrID {
			// 从切片中删除
			lsq.storeQueue = append(lsq.storeQueue[:i], lsq.storeQueue[i+1:]...)
			return true
		}
	}
	return false
}

// IsStoreQueueFull 检查 SQ 是否已满
func (lsq *LoadStoreQueue) IsStoreQueueFull() bool {
	return len(lsq.storeQueue) >= lsq.maxSQSize
}

// ==================== Store-to-Load Forwarding ====================

// CheckStoreToLoadForwarding 检查是否可以从 SQ 转发数据给 load
//
// 返回：
//   - 如果可以转发，返回 true 和转发的 store 条目
//   - 如果不能转发，返回 false
func (lsq *LoadStoreQueue) CheckStoreToLoadForwarding(loadEntry *LSQEntry) (bool, *LSQEntry) {
	// 从后向前遍历 SQ（程序顺序）
	// 查找地址匹配的 store
	for i := len(lsq.storeQueue) - 1; i >= 0; i-- {
		storeEntry := lsq.storeQueue[i]

		// 检查 store 是否在 load 之前（程序顺序）
		if storeEntry.InstrID >= loadEntry.InstrID {
			continue
		}

		// 检查地址是否匹配
		if storeEntry.VirtualAddr == loadEntry.VirtualAddr {
			// 检查 store 是否已准备好（地址和数据都计算完成）
			if storeEntry.ReadyTime <= loadEntry.ReadyTime {
				// 可以转发！
				lsq.stats.ForwardedLoads++
				return true, storeEntry
			}
		}
	}

	return false, nil
}

// ==================== 内存请求调度 ====================

// GetReadyLoads 返回准备好发送到内存系统的 load 请求
func (lsq *LoadStoreQueue) GetReadyLoads(currentCycle uint64) []*LSQEntry {
	var readyLoads []*LSQEntry

	for _, entry := range lsq.loadQueue {
		if entry.IsReady(currentCycle) {
			// 检查是否可以从 SQ 转发
			canForward, _ := lsq.CheckStoreToLoadForwarding(entry)
			if !canForward {
				// 不能转发，需要发送到内存
				readyLoads = append(readyLoads, entry)
			} else {
				// 可以转发，直接标记为完成
				entry.Completed = true
				entry.CompleteCycle = currentCycle
				entry.FetchIssued = true // 标记已处理
			}
		}
	}

	return readyLoads
}

// GetReadyStores 返回准备好发送到内存系统的 store 请求
func (lsq *LoadStoreQueue) GetReadyStores(currentCycle uint64) []*LSQEntry {
	var readyStores []*LSQEntry

	for _, entry := range lsq.storeQueue {
		if entry.IsReady(currentCycle) {
			readyStores = append(readyStores, entry)
		}
	}

	return readyStores
}

// ==================== 响应处理 ====================

// HandleLoadResponse 处理 load 响应
func (lsq *LoadStoreQueue) HandleLoadResponse(instrID uint64, cycle uint64) bool {
	// 注意：一条指令可能有多个 load 操作，需要找到第一个未完成的
	for _, entry := range lsq.loadQueue {
		if entry.InstrID == instrID && !entry.Completed {
			entry.Completed = true
			entry.CompleteCycle = cycle
			return true // 只完成一个未完成的条目
		}
	}
	return false
}

// HandleStoreResponse 处理 store 响应
func (lsq *LoadStoreQueue) HandleStoreResponse(instrID uint64, cycle uint64) bool {
	// 注意：一条指令可能有多个 store 操作，需要找到第一个未完成的
	for _, entry := range lsq.storeQueue {
		if entry.InstrID == instrID && !entry.Completed {
			entry.Completed = true
			entry.CompleteCycle = cycle
			return true // 只完成一个未完成的条目
		}
	}
	return false
}

// ==================== 查找和访问 ====================

// FindLoadByInstrID 通过指令 ID 查找 load 条目
func (lsq *LoadStoreQueue) FindLoadByInstrID(instrID uint64) *LSQEntry {
	for _, entry := range lsq.loadQueue {
		if entry.InstrID == instrID {
			return entry
		}
	}
	return nil
}

// FindStoreByInstrID 通过指令 ID 查找 store 条目
func (lsq *LoadStoreQueue) FindStoreByInstrID(instrID uint64) *LSQEntry {
	for _, entry := range lsq.storeQueue {
		if entry.InstrID == instrID {
			return entry
		}
	}
	return nil
}

// ==================== 内存一致性检查 ====================

// CheckMemoryOrdering 检查内存顺序冲突
//
// 确保没有违反内存顺序的情况：
// - Load 不能越过同地址的 Store
// - Store 不能越过同地址的 Load 或 Store
func (lsq *LoadStoreQueue) CheckMemoryOrdering(instr *instruction.OOOModelInstr) bool {
	if instr.IsLoad() {
		// 检查 load 是否可以安全执行
		for _, loadAddr := range instr.SrcMemory {
			// 检查 SQ 中是否有同地址的 store 在前面
			for _, storeEntry := range lsq.storeQueue {
				if storeEntry.InstrID < instr.InstrID && // 程序顺序更早
					storeEntry.VirtualAddr == loadAddr && // 同地址
					!storeEntry.Completed { // 尚未完成
					// 必须等待这个 store 完成
					return false
				}
			}
		}
	}

	if instr.IsStore() {
		// 检查 store 是否可以安全执行
		for _, storeAddr := range instr.DestMemory {
			// 检查 LQ 中是否有同地址的 load 在前面
			for _, loadEntry := range lsq.loadQueue {
				if loadEntry.InstrID < instr.InstrID && // 程序顺序更早
					loadEntry.VirtualAddr == storeAddr && // 同地址
					!loadEntry.Completed { // 尚未完成
					// 必须等待这个 load 完成
					return false
				}
			}

			// 检查 SQ 中是否有同地址的 store 在前面
			for _, storeEntry := range lsq.storeQueue {
				if storeEntry.InstrID < instr.InstrID && // 程序顺序更早
					storeEntry.VirtualAddr == storeAddr && // 同地址
					!storeEntry.Completed { // 尚未完成
					// 必须等待这个 store 完成
					return false
				}
			}
		}
	}

	return true
}

// ==================== 状态查询 ====================

// LoadQueueSize 返回 LQ 当前大小
func (lsq *LoadStoreQueue) LoadQueueSize() int {
	return len(lsq.loadQueue)
}

// StoreQueueSize 返回 SQ 当前大小
func (lsq *LoadStoreQueue) StoreQueueSize() int {
	return len(lsq.storeQueue)
}

// HasPendingMemoryRequest 检查是否有待处理的内存请求
func (lsq *LoadStoreQueue) HasPendingMemoryRequest() bool {
	for _, entry := range lsq.loadQueue {
		if !entry.FetchIssued && !entry.Completed {
			return true
		}
	}
	for _, entry := range lsq.storeQueue {
		if !entry.FetchIssued && !entry.Completed {
			return true
		}
	}
	return false
}

// GetStats 返回统计信息
func (lsq *LoadStoreQueue) GetStats() LSQStats {
	return lsq.stats
}

// Reset 重置 LSQ（用于测试）
func (lsq *LoadStoreQueue) Reset() {
	lsq.loadQueue = make([]*LSQEntry, 0, lsq.maxLQSize)
	lsq.storeQueue = make([]*LSQEntry, 0, lsq.maxSQSize)
	lsq.stats = LSQStats{}
}

// ==================== 常量 ====================

const (
	// DefaultLQSize 默认 Load Queue 大小
	DefaultLQSize = 128

	// DefaultSQSize 默认 Store Queue 大小
	DefaultSQSize = 72
)
