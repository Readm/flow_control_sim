package cpu

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/champsim/instruction"
)

// ReorderBuffer (ROB) 重排序缓冲区
//
// ROB 是乱序执行 CPU 的核心组件：
// - 跟踪所有在执行中的指令
// - 保证按程序顺序退休（Retire）
// - 处理分支预测错误时的恢复
//
// ROB 使用环形缓冲区实现，有 head 和 tail 指针：
// - tail: 新指令插入位置（分配）
// - head: 下一个要退休的指令（退休）
type ReorderBuffer struct {
	// buffer 环形缓冲区，存储指令指针
	buffer []*instruction.OOOModelInstr

	// maxSize ROB 最大容量
	maxSize int

	// head 指向最老的指令（下一个要退休的）
	head int

	// tail 指向下一个空闲位置（新指令插入处）
	tail int

	// size 当前指令数量
	size int

	// stats 统计信息
	stats ROBStats
}

// ROBStats ROB 统计信息
type ROBStats struct {
	TotalRetired uint64 // 总退休指令数
	ROBFull      uint64 // ROB 满的次数
}

// NewReorderBuffer 创建新的 ROB
//
// 参数：
//   - maxSize: ROB 最大容量（典型值：128-256）
func NewReorderBuffer(maxSize int) *ReorderBuffer {
	return &ReorderBuffer{
		buffer:  make([]*instruction.OOOModelInstr, maxSize),
		maxSize: maxSize,
		head:    0,
		tail:    0,
		size:    0,
		stats:   ROBStats{},
	}
}

// ==================== 添加和移除 ====================

// Add 添加指令到 ROB（在 tail 位置）
//
// 返回错误如果 ROB 已满。
func (rob *ReorderBuffer) Add(instr *instruction.OOOModelInstr) error {
	if rob.IsFull() {
		rob.stats.ROBFull++
		return fmt.Errorf("ROB is full")
	}

	// 在 tail 位置插入
	rob.buffer[rob.tail] = instr
	rob.tail = (rob.tail + 1) % rob.maxSize
	rob.size++

	return nil
}

// Retire 退休 head 位置的指令
//
// 只有当 head 指令完成时才能退休。
// 返回退休的指令，如果不能退休则返回 nil。
func (rob *ReorderBuffer) Retire() *instruction.OOOModelInstr {
	if rob.IsEmpty() {
		return nil
	}

	// 获取 head 指令
	instr := rob.buffer[rob.head]

	// 检查是否完成（所有操作都完成）
	if !instr.Completed {
		return nil
	}

	// 退休指令
	rob.buffer[rob.head] = nil // 清空引用
	rob.head = (rob.head + 1) % rob.maxSize
	rob.size--
	rob.stats.TotalRetired++

	return instr
}

// ==================== 查询操作 ====================

// Head 返回 head 位置的指令（不移除）
func (rob *ReorderBuffer) Head() *instruction.OOOModelInstr {
	if rob.IsEmpty() {
		return nil
	}
	return rob.buffer[rob.head]
}

// FindByInstrID 通过指令 ID 查找 ROB 中的指令
func (rob *ReorderBuffer) FindByInstrID(instrID uint64) *instruction.OOOModelInstr {
	if rob.IsEmpty() {
		return nil
	}

	// 从 head 遍历到 tail
	idx := rob.head
	for i := 0; i < rob.size; i++ {
		if rob.buffer[idx].InstrID == instrID {
			return rob.buffer[idx]
		}
		idx = (idx + 1) % rob.maxSize
	}

	return nil
}

// GetAllInstructions 返回 ROB 中所有指令（按程序顺序）
//
// 用于调试和统计。
func (rob *ReorderBuffer) GetAllInstructions() []*instruction.OOOModelInstr {
	if rob.IsEmpty() {
		return nil
	}

	instrs := make([]*instruction.OOOModelInstr, 0, rob.size)
	idx := rob.head
	for i := 0; i < rob.size; i++ {
		instrs = append(instrs, rob.buffer[idx])
		idx = (idx + 1) % rob.maxSize
	}

	return instrs
}

// ==================== 状态查询 ====================

// IsFull 检查 ROB 是否已满
func (rob *ReorderBuffer) IsFull() bool {
	return rob.size >= rob.maxSize
}

// IsEmpty 检查 ROB 是否为空
func (rob *ReorderBuffer) IsEmpty() bool {
	return rob.size == 0
}

// Size 返回当前 ROB 中的指令数
func (rob *ReorderBuffer) Size() int {
	return rob.size
}

// MaxSize 返回 ROB 的最大容量
func (rob *ReorderBuffer) MaxSize() int {
	return rob.maxSize
}

// PeekAt 返回 ROB 中第 i 个位置的指令（不移除）
//
// 参数：
//   - i: 从 head 开始的偏移量（0 表示 head）
//
// 返回：
//   - 指令指针，如果索引越界则返回 nil
//
// 用于 complete_inflight_instruction() 扫描整个 ROB
func (rob *ReorderBuffer) PeekAt(i int) *instruction.OOOModelInstr {
	if i < 0 || i >= rob.size {
		return nil
	}

	// 计算实际索引（循环缓冲区）
	idx := (rob.head + i) % rob.maxSize
	return rob.buffer[idx]
}

// AvailableSpace 返回 ROB 中剩余空间
func (rob *ReorderBuffer) AvailableSpace() int {
	return rob.maxSize - rob.size
}

// ==================== 分支预测错误处理 ====================

// Flush 清空 ROB（分支预测错误时使用）
//
// 保留 head 到 branchInstrID 之间的指令，
// 清空 branchInstrID 之后的所有指令。
//
// 返回被清空的指令数量。
func (rob *ReorderBuffer) Flush(branchInstrID uint64) int {
	if rob.IsEmpty() {
		return 0
	}

	flushedCount := 0

	// 查找分支指令的位置
	branchIdx := -1
	idx := rob.head
	for i := 0; i < rob.size; i++ {
		if rob.buffer[idx].InstrID == branchInstrID {
			branchIdx = idx
			break
		}
		idx = (idx + 1) % rob.maxSize
	}

	if branchIdx == -1 {
		// 分支指令不在 ROB 中，不做任何操作
		return 0
	}

	// 清空 branchIdx 之后的所有指令
	nextIdx := (branchIdx + 1) % rob.maxSize
	for nextIdx != rob.tail {
		rob.buffer[nextIdx] = nil
		nextIdx = (nextIdx + 1) % rob.maxSize
		flushedCount++
	}

	// 更新 tail 和 size
	rob.tail = (branchIdx + 1) % rob.maxSize
	rob.size -= flushedCount

	return flushedCount
}

// FlushAll 清空整个 ROB（用于异常处理或重置）
func (rob *ReorderBuffer) FlushAll() {
	for i := 0; i < rob.maxSize; i++ {
		rob.buffer[i] = nil
	}
	rob.head = 0
	rob.tail = 0
	rob.size = 0
}

// ==================== 统计信息 ====================

// GetStats 返回统计信息
func (rob *ReorderBuffer) GetStats() ROBStats {
	return rob.stats
}

// ResetStats 重置统计信息（不清空 ROB）
func (rob *ReorderBuffer) ResetStats() {
	rob.stats = ROBStats{}
}

// ==================== 常量 ====================

const (
	// DefaultROBSize 默认 ROB 大小
	// Intel Skylake: 224 entries
	// AMD Zen 2: 224 entries
	DefaultROBSize = 224
)
