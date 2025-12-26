package cache

// MSHREntry MSHR (Miss Status Holding Register) 条目
//
// 对应 ChampSim 的 mshr_type
//
// MSHR 用于跟踪未完成的 miss 请求：
// - 当 cache miss 时，分配一个 MSHR 条目
// - 如果多个请求访问同一地址，合并到同一个 MSHR
// - 当 fill 到达时，从 MSHR 中移除
type MSHREntry struct {
	// ==================== 地址信息 ====================

	// Address 物理地址（block对齐）
	Address uint64

	// VAddress 虚拟地址
	VAddress uint64

	// IP 指令地址
	IP uint64

	// InstrID 指令ID
	InstrID uint64

	// ==================== 请求信息 ====================

	// CPU CPU ID
	CPU uint32

	// Type 访问类型（Load/Store/Prefetch）
	Type AccessType

	// PrefetchFromThis 是否从这个级别预取
	PrefetchFromThis bool

	// ==================== 时间信息 ====================

	// EnqueueCycle 入队周期
	EnqueueCycle uint64

	// ReadyCycle 数据就绪周期
	// 在 fill 完成后设置
	ReadyCycle uint64

	// ==================== 依赖跟踪 ====================

	// InstrDependOnMe 依赖这个MSHR的指令ID列表
	// 用于在fill完成时唤醒依赖指令
	InstrDependOnMe []uint64

	// ==================== 合并信息 ====================

	// Merged 是否已合并到其他MSHR
	Merged bool

	// MergedWith 合并到哪个MSHR的索引
	MergedWith int
}

// MSHRQueue MSHR队列
type MSHRQueue struct {
	// entries MSHR条目列表
	entries []*MSHREntry

	// maxSize 最大容量
	maxSize int
}

// NewMSHRQueue 创建新的MSHR队列
func NewMSHRQueue(maxSize int) *MSHRQueue {
	return &MSHRQueue{
		entries: make([]*MSHREntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// IsFull 检查MSHR是否已满
func (mq *MSHRQueue) IsFull() bool {
	return len(mq.entries) >= mq.maxSize
}

// Size 返回当前MSHR数量
func (mq *MSHRQueue) Size() int {
	return len(mq.entries)
}

// Find 查找指定地址的MSHR
//
// 返回：
// - index: MSHR在队列中的索引
// - found: 是否找到
func (mq *MSHRQueue) Find(addr uint64) (index int, found bool) {
	for i, entry := range mq.entries {
		if entry.Address == addr && !entry.Merged {
			return i, true
		}
	}
	return -1, false
}

// Allocate 分配新的MSHR条目
//
// 返回：
// - index: 新MSHR的索引
// - success: 是否成功分配
func (mq *MSHRQueue) Allocate(entry *MSHREntry) (index int, success bool) {
	if mq.IsFull() {
		return -1, false
	}

	mq.entries = append(mq.entries, entry)
	return len(mq.entries) - 1, true
}

// Merge 合并请求到已存在的MSHR
//
// 将新请求的依赖信息添加到已存在的MSHR中
func (mq *MSHRQueue) Merge(index int, instrID uint64) {
	if index < 0 || index >= len(mq.entries) {
		return
	}

	entry := mq.entries[index]

	// 添加到依赖列表（避免重复）
	for _, id := range entry.InstrDependOnMe {
		if id == instrID {
			return // 已存在
		}
	}

	entry.InstrDependOnMe = append(entry.InstrDependOnMe, instrID)
}

// Remove 移除MSHR条目
//
// 返回被移除的MSHR条目
func (mq *MSHRQueue) Remove(index int) *MSHREntry {
	if index < 0 || index >= len(mq.entries) {
		return nil
	}

	entry := mq.entries[index]

	// 从队列中移除
	mq.entries = append(mq.entries[:index], mq.entries[index+1:]...)

	return entry
}

// RemoveByAddress 根据地址移除MSHR条目
//
// 返回被移除的MSHR条目
func (mq *MSHRQueue) RemoveByAddress(addr uint64) *MSHREntry {
	index, found := mq.Find(addr)
	if !found {
		return nil
	}

	return mq.Remove(index)
}

// GetAll 获取所有MSHR条目
func (mq *MSHRQueue) GetAll() []*MSHREntry {
	return mq.entries
}

// Clear 清空MSHR队列
func (mq *MSHRQueue) Clear() {
	mq.entries = mq.entries[:0]
}
