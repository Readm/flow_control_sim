package cpu

import (
	"testing"

	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/instruction"
)

// TestROB_BasicAddAndRetire 测试基本的添加和退休
func TestROB_BasicAddAndRetire(t *testing.T) {
	rob := NewReorderBuffer(10)

	// 初始时应该为空
	if !rob.IsEmpty() {
		t.Error("ROB should be empty initially")
	}

	// 添加一条指令
	instr := &instruction.OOOModelInstr{
		InstrID:   1,
		Completed: false,
	}
	if err := rob.Add(instr); err != nil {
		t.Fatalf("Failed to add instruction: %v", err)
	}

	if rob.Size() != 1 {
		t.Errorf("Expected size 1, got %d", rob.Size())
	}

	// 尝试退休，应该失败（未完成）
	if rob.Retire() != nil {
		t.Error("Should not retire incomplete instruction")
	}

	// 标记为完成
	instr.Completed = true

	// 现在应该可以退休
	retired := rob.Retire()
	if retired == nil {
		t.Fatal("Should retire completed instruction")
	}

	if retired.InstrID != 1 {
		t.Errorf("Expected instr ID 1, got %d", retired.InstrID)
	}

	if !rob.IsEmpty() {
		t.Error("ROB should be empty after retire")
	}
}

// TestROB_InOrderRetire 测试按序退休
func TestROB_InOrderRetire(t *testing.T) {
	rob := NewReorderBuffer(10)

	// 添加 3 条指令
	instr1 := &instruction.OOOModelInstr{InstrID: 1, Completed: false}
	instr2 := &instruction.OOOModelInstr{InstrID: 2, Completed: true}
	instr3 := &instruction.OOOModelInstr{InstrID: 3, Completed: true}

	rob.Add(instr1)
	rob.Add(instr2)
	rob.Add(instr3)

	// 即使 instr2 和 instr3 完成了，也不能退休（instr1 在前面）
	if rob.Retire() != nil {
		t.Error("Should not retire when head instruction is not completed")
	}

	// 完成 instr1
	instr1.Completed = true

	// 现在应该可以按序退休
	retired := rob.Retire()
	if retired == nil || retired.InstrID != 1 {
		t.Error("Should retire instr1 first")
	}

	retired = rob.Retire()
	if retired == nil || retired.InstrID != 2 {
		t.Error("Should retire instr2 second")
	}

	retired = rob.Retire()
	if retired == nil || retired.InstrID != 3 {
		t.Error("Should retire instr3 third")
	}

	if !rob.IsEmpty() {
		t.Error("ROB should be empty")
	}
}

// TestROB_Full 测试 ROB 满的情况
func TestROB_Full(t *testing.T) {
	rob := NewReorderBuffer(3)

	// 填满 ROB
	for i := 0; i < 3; i++ {
		instr := &instruction.OOOModelInstr{InstrID: uint64(i), Completed: false}
		if err := rob.Add(instr); err != nil {
			t.Fatalf("Failed to add instruction %d: %v", i, err)
		}
	}

	if !rob.IsFull() {
		t.Error("ROB should be full")
	}

	// 尝试再添加应该失败
	instr := &instruction.OOOModelInstr{InstrID: 100, Completed: false}
	if err := rob.Add(instr); err == nil {
		t.Error("Should fail to add to full ROB")
	}

	// 统计应该增加
	stats := rob.GetStats()
	if stats.ROBFull != 1 {
		t.Errorf("Expected ROBFull count 1, got %d", stats.ROBFull)
	}
}

// TestROB_CircularBuffer 测试环形缓冲区
func TestROB_CircularBuffer(t *testing.T) {
	rob := NewReorderBuffer(3)

	// 添加 3 条指令
	for i := 0; i < 3; i++ {
		instr := &instruction.OOOModelInstr{InstrID: uint64(i), Completed: true}
		rob.Add(instr)
	}

	// 退休 2 条
	rob.Retire()
	rob.Retire()

	// 现在应该有空间
	if rob.AvailableSpace() != 2 {
		t.Errorf("Expected 2 available spaces, got %d", rob.AvailableSpace())
	}

	// 添加 2 条新指令（测试环形）
	rob.Add(&instruction.OOOModelInstr{InstrID: 10, Completed: true})
	rob.Add(&instruction.OOOModelInstr{InstrID: 11, Completed: true})

	// 应该有 3 条指令
	if rob.Size() != 3 {
		t.Errorf("Expected size 3, got %d", rob.Size())
	}

	// 按序退休
	retired := rob.Retire()
	if retired.InstrID != 2 {
		t.Errorf("Expected instr ID 2, got %d", retired.InstrID)
	}

	retired = rob.Retire()
	if retired.InstrID != 10 {
		t.Errorf("Expected instr ID 10, got %d", retired.InstrID)
	}

	retired = rob.Retire()
	if retired.InstrID != 11 {
		t.Errorf("Expected instr ID 11, got %d", retired.InstrID)
	}
}

// TestROB_Head 测试 Head 方法
func TestROB_Head(t *testing.T) {
	rob := NewReorderBuffer(10)

	// 空 ROB
	if rob.Head() != nil {
		t.Error("Head should be nil for empty ROB")
	}

	// 添加指令
	instr := &instruction.OOOModelInstr{InstrID: 1, Completed: false}
	rob.Add(instr)

	// Head 应该返回第一条指令
	head := rob.Head()
	if head == nil || head.InstrID != 1 {
		t.Error("Head should return first instruction")
	}

	// Head 不应该移除指令
	if rob.Size() != 1 {
		t.Error("Head should not remove instruction")
	}
}

// TestROB_FindByInstrID 测试通过 ID 查找
func TestROB_FindByInstrID(t *testing.T) {
	rob := NewReorderBuffer(10)

	// 添加几条指令
	rob.Add(&instruction.OOOModelInstr{InstrID: 1, Completed: false})
	rob.Add(&instruction.OOOModelInstr{InstrID: 2, Completed: false})
	rob.Add(&instruction.OOOModelInstr{InstrID: 3, Completed: false})

	// 查找存在的指令
	found := rob.FindByInstrID(2)
	if found == nil || found.InstrID != 2 {
		t.Error("Should find instruction with ID 2")
	}

	// 查找不存在的指令
	if rob.FindByInstrID(100) != nil {
		t.Error("Should not find non-existent instruction")
	}
}

// TestROB_GetAllInstructions 测试获取所有指令
func TestROB_GetAllInstructions(t *testing.T) {
	rob := NewReorderBuffer(10)

	// 空 ROB
	if rob.GetAllInstructions() != nil {
		t.Error("Should return nil for empty ROB")
	}

	// 添加几条指令
	rob.Add(&instruction.OOOModelInstr{InstrID: 1, Completed: false})
	rob.Add(&instruction.OOOModelInstr{InstrID: 2, Completed: false})
	rob.Add(&instruction.OOOModelInstr{InstrID: 3, Completed: false})

	// 获取所有指令
	instrs := rob.GetAllInstructions()
	if len(instrs) != 3 {
		t.Errorf("Expected 3 instructions, got %d", len(instrs))
	}

	// 检查顺序
	for i, instr := range instrs {
		if instr.InstrID != uint64(i+1) {
			t.Errorf("Expected instr ID %d, got %d", i+1, instr.InstrID)
		}
	}
}

// TestROB_Flush 测试分支预测错误后的清空
func TestROB_Flush(t *testing.T) {
	rob := NewReorderBuffer(10)

	// 添加几条指令
	rob.Add(&instruction.OOOModelInstr{InstrID: 1, Completed: false})
	rob.Add(&instruction.OOOModelInstr{InstrID: 2, Completed: false}) // 分支指令
	rob.Add(&instruction.OOOModelInstr{InstrID: 3, Completed: false})
	rob.Add(&instruction.OOOModelInstr{InstrID: 4, Completed: false})

	// 清空 ID=2 之后的所有指令
	flushed := rob.Flush(2)

	if flushed != 2 {
		t.Errorf("Expected to flush 2 instructions, flushed %d", flushed)
	}

	// ROB 应该只剩 2 条指令
	if rob.Size() != 2 {
		t.Errorf("Expected size 2 after flush, got %d", rob.Size())
	}

	// 检查剩余指令
	instrs := rob.GetAllInstructions()
	if len(instrs) != 2 || instrs[0].InstrID != 1 || instrs[1].InstrID != 2 {
		t.Error("Should keep instructions 1 and 2")
	}
}

// TestROB_FlushNotFound 测试 Flush 不存在的指令
func TestROB_FlushNotFound(t *testing.T) {
	rob := NewReorderBuffer(10)

	rob.Add(&instruction.OOOModelInstr{InstrID: 1, Completed: false})
	rob.Add(&instruction.OOOModelInstr{InstrID: 2, Completed: false})

	// Flush 不存在的指令 ID
	flushed := rob.Flush(100)

	if flushed != 0 {
		t.Errorf("Expected 0 flushed instructions, got %d", flushed)
	}

	// ROB 大小不变
	if rob.Size() != 2 {
		t.Errorf("Expected size 2, got %d", rob.Size())
	}
}

// TestROB_FlushAll 测试完全清空
func TestROB_FlushAll(t *testing.T) {
	rob := NewReorderBuffer(10)

	// 添加几条指令
	rob.Add(&instruction.OOOModelInstr{InstrID: 1, Completed: false})
	rob.Add(&instruction.OOOModelInstr{InstrID: 2, Completed: false})
	rob.Add(&instruction.OOOModelInstr{InstrID: 3, Completed: false})

	rob.FlushAll()

	// ROB 应该为空
	if !rob.IsEmpty() {
		t.Error("ROB should be empty after FlushAll")
	}

	if rob.Size() != 0 {
		t.Errorf("Expected size 0, got %d", rob.Size())
	}

	// head 和 tail 应该重置
	if rob.head != 0 || rob.tail != 0 {
		t.Error("head and tail should be reset to 0")
	}
}

// TestROB_Statistics 测试统计信息
func TestROB_Statistics(t *testing.T) {
	rob := NewReorderBuffer(10)

	// 添加并退休几条指令
	for i := 0; i < 5; i++ {
		instr := &instruction.OOOModelInstr{InstrID: uint64(i), Completed: true}
		rob.Add(instr)
		rob.Retire()
	}

	stats := rob.GetStats()
	if stats.TotalRetired != 5 {
		t.Errorf("Expected 5 retired instructions, got %d", stats.TotalRetired)
	}
}
