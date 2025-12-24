package cpu

// LSQEntry 表示 Load/Store Queue 中的一个条目
//
// LSQ (Load-Store Queue) 用于管理内存操作的顺序和依赖关系：
// - Load Queue: 跟踪待执行和执行中的 load 操作
// - Store Queue: 跟踪待执行和执行中的 store 操作
//
// LSQ 的关键功能：
// 1. Store-to-Load Forwarding: 如果 load 地址与之前的 store 地址匹配，
//    可以直接从 store 数据转发，无需等待 store 写入内存
// 2. 内存顺序保证: 确保 load/store 按程序顺序执行
// 3. 依赖跟踪: 跟踪哪些 load 依赖于哪些 store
type LSQEntry struct {
	// ==================== 基本标识 ====================

	// InstrID 对应的指令 ID (与 OOOModelInstr.InstrID 相同)
	// 用于关联 LSQ 条目和 ROB 中的指令
	InstrID uint64

	// VirtualAddr 虚拟内存地址
	// 对于 load: 读取的地址
	// 对于 store: 写入的地址
	VirtualAddr uint64

	// IP 指令地址 (用于调试和统计)
	IP uint64

	// ASID 地址空间标识符
	ASID [2]uint8

	// ==================== 时间和状态 ====================

	// ReadyTime 该内存操作准备好执行的时间
	// 对于 load: 地址计算完成的时间
	// 对于 store: 数据和地址都准备好的时间
	ReadyTime uint64

	// FetchIssued 是否已向内存系统发出请求
	// load: 已发出读请求
	// store: 已发出写请求
	FetchIssued bool

	// Completed 内存操作是否完成
	Completed bool

	// CompleteCycle 完成时的周期数
	CompleteCycle uint64

	// ==================== 依赖关系 ====================

	// ProducerID 产生该地址的指令 ID
	// 如果该 load/store 的地址依赖于前面的指令计算结果，
	// ProducerID 记录那条指令的 ID
	// MaxUint64 表示无依赖
	ProducerID uint64

	// LQDependOnMe Load Queue 中依赖于我的条目列表
	//
	// 对于 Store Queue 中的条目：
	//   记录哪些 load 可能依赖于这个 store (地址匹配或部分重叠)
	//   当 store 完成时，需要检查这些 load 是否可以执行
	//
	// 对于 Load Queue 中的条目：
	//   通常为空，load 不会阻塞其他 load
	LQDependOnMe []*LSQEntry
}

// NewLSQEntry 创建新的 LSQ 条目
func NewLSQEntry(instrID uint64, virtualAddr uint64, ip uint64, asid [2]uint8) *LSQEntry {
	return &LSQEntry{
		InstrID:       instrID,
		VirtualAddr:   virtualAddr,
		IP:            ip,
		ASID:          asid,
		ReadyTime:     0,
		FetchIssued:   false,
		Completed:     false,
		CompleteCycle: 0,
		ProducerID:    ^uint64(0), // MaxUint64 表示无依赖
		LQDependOnMe:  nil,
	}
}

// IsReady 返回该条目是否准备好执行
func (entry *LSQEntry) IsReady(currentCycle uint64) bool {
	return currentCycle >= entry.ReadyTime && !entry.FetchIssued && !entry.Completed
}

// AddDependentLoad 添加一个依赖于该条目的 load
// 用于 Store-to-Load Forwarding 的依赖跟踪
func (entry *LSQEntry) AddDependentLoad(loadEntry *LSQEntry) {
	entry.LQDependOnMe = append(entry.LQDependOnMe, loadEntry)
}

// ClearDependents 清空依赖列表
// 当 store 完成时调用
func (entry *LSQEntry) ClearDependents() {
	entry.LQDependOnMe = nil
}

// HasDependents 返回是否有依赖于该条目的 load
func (entry *LSQEntry) HasDependents() bool {
	return len(entry.LQDependOnMe) > 0
}
