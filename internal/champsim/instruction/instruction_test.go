package instruction

import (
	"testing"
)

// TestBranchTypeString 测试分支类型的字符串表示
func TestBranchTypeString(t *testing.T) {
	tests := []struct {
		bt       BranchType
		expected string
	}{
		{BranchDirectJump, "BRANCH_DIRECT_JUMP"},
		{BranchIndirect, "BRANCH_INDIRECT"},
		{BranchConditional, "BRANCH_CONDITIONAL"},
		{BranchDirectCall, "BRANCH_DIRECT_CALL"},
		{BranchIndirectCall, "BRANCH_INDIRECT_CALL"},
		{BranchReturn, "BRANCH_RETURN"},
		{BranchOther, "BRANCH_OTHER"},
		{NotBranch, "NOT_BRANCH"},
	}

	for _, tt := range tests {
		if got := tt.bt.String(); got != tt.expected {
			t.Errorf("BranchType(%d).String() = %s, want %s", tt.bt, got, tt.expected)
		}
	}
}

// TestBranchTypeIsBranch 测试分支类型判断
func TestBranchTypeIsBranch(t *testing.T) {
	tests := []struct {
		bt       BranchType
		expected bool
	}{
		{BranchDirectJump, true},
		{BranchConditional, true},
		{NotBranch, false},
	}

	for _, tt := range tests {
		if got := tt.bt.IsBranch(); got != tt.expected {
			t.Errorf("BranchType(%s).IsBranch() = %v, want %v", tt.bt, got, tt.expected)
		}
	}
}

// TestPhysicalRegisterIDValid 测试寄存器 ID 有效性
func TestPhysicalRegisterIDValid(t *testing.T) {
	tests := []struct {
		id       PhysicalRegisterID
		expected bool
	}{
		{InvalidRegister, false},
		{-1, false},
		{0, true},
		{10, true},
	}

	for _, tt := range tests {
		if got := tt.id.IsValid(); got != tt.expected {
			t.Errorf("PhysicalRegisterID(%d).IsValid() = %v, want %v", tt.id, got, tt.expected)
		}
	}
}

// TestIdentifyBranchType_DirectJump 测试识别直接跳转
func TestIdentifyBranchType_DirectJump(t *testing.T) {
	// JMP target: 写 IP，不读任何特殊寄存器
	instr := NewOOOModelInstrFromInput(
		0, // cpuID
		0x1000, // ip
		0, // isBranch
		0, // branchTaken
		[]uint8{RegInstructionPointer, 0},                            // 写 IP
		[]uint8{1, 2, 0, 0},                                           // 读普通寄存器 (会被过滤因为导致 readsOther=true... 等等)
		[]uint64{0, 0},                                                // 无内存写
		[]uint64{0, 0, 0, 0},                                          // 无内存读
	)

	// 修正：直接跳转不应读其他寄存器
	instr = NewOOOModelInstrFromInput(
		0,
		0x1000,
		0,
		0,
		[]uint8{RegInstructionPointer, 0},
		[]uint8{0, 0, 0, 0}, // 不读任何寄存器
		[]uint64{0, 0},
		[]uint64{0, 0, 0, 0},
	)

	if instr.BranchType != BranchDirectJump {
		t.Errorf("Expected BranchDirectJump, got %s", instr.BranchType)
	}
	if !instr.IsBranch {
		t.Error("Expected IsBranch to be true")
	}
	if !instr.BranchTaken {
		t.Error("Expected BranchTaken to be true for direct jump")
	}
}

// TestIdentifyBranchType_Conditional 测试识别条件分支
func TestIdentifyBranchType_Conditional(t *testing.T) {
	// JZ target: 读 IP 和 Flags，写 IP
	instr := NewOOOModelInstrFromInput(
		0,
		0x2000,
		1, // trace 说这是分支
		1, // trace 说跳转了
		[]uint8{RegInstructionPointer, 0},
		[]uint8{RegInstructionPointer, RegFlags, 0, 0}, // 读 IP 和 Flags
		[]uint64{0, 0},
		[]uint64{0, 0, 0, 0},
	)

	if instr.BranchType != BranchConditional {
		t.Errorf("Expected BranchConditional, got %s", instr.BranchType)
	}
	if !instr.IsBranch {
		t.Error("Expected IsBranch to be true")
	}
	// BranchTaken 应保持 trace 的值
	if !instr.BranchTaken {
		t.Error("Expected BranchTaken to be true (from trace)")
	}
}

// TestIdentifyBranchType_DirectCall 测试识别直接调用
func TestIdentifyBranchType_DirectCall(t *testing.T) {
	// CALL target: 读写 SP 和 IP，不读其他
	instr := NewOOOModelInstrFromInput(
		0,
		0x3000,
		0,
		0,
		[]uint8{RegStackPointer, RegInstructionPointer}, // 写 SP 和 IP
		[]uint8{RegStackPointer, RegInstructionPointer, 0, 0}, // 读 SP 和 IP
		[]uint64{0, 0},
		[]uint64{0, 0, 0, 0},
	)

	if instr.BranchType != BranchDirectCall {
		t.Errorf("Expected BranchDirectCall, got %s", instr.BranchType)
	}
}

// TestIdentifyBranchType_Return 测试识别返回
func TestIdentifyBranchType_Return(t *testing.T) {
	// RET: 读 SP，写 SP 和 IP，不读 IP
	instr := NewOOOModelInstrFromInput(
		0,
		0x4000,
		0,
		0,
		[]uint8{RegStackPointer, RegInstructionPointer}, // 写 SP 和 IP
		[]uint8{RegStackPointer, 0, 0, 0},                // 只读 SP，不读 IP
		[]uint64{0, 0},
		[]uint64{0, 0, 0, 0},
	)

	if instr.BranchType != BranchReturn {
		t.Errorf("Expected BranchReturn, got %s", instr.BranchType)
	}
}

// TestIdentifyBranchType_NotBranch 测试识别非分支指令
func TestIdentifyBranchType_NotBranch(t *testing.T) {
	// ADD reg1, reg2: 普通算术指令
	instr := NewOOOModelInstrFromInput(
		0,
		0x5000,
		0,
		0,
		[]uint8{1, 0},    // 写普通寄存器
		[]uint8{2, 3, 0, 0}, // 读普通寄存器
		[]uint64{0, 0},
		[]uint64{0, 0, 0, 0},
	)

	if instr.BranchType != NotBranch {
		t.Errorf("Expected NotBranch, got %s", instr.BranchType)
	}
	if instr.IsBranch {
		t.Error("Expected IsBranch to be false")
	}
}

// TestOOOModelInstr_MemoryOps 测试内存操作统计
func TestOOOModelInstr_MemoryOps(t *testing.T) {
	// Load 指令: MOV reg, [addr]
	loadInstr := NewOOOModelInstrFromInput(
		0, 0x6000, 0, 0,
		[]uint8{1, 0},
		[]uint8{2, 0, 0, 0},
		[]uint64{0, 0},                // 无内存写
		[]uint64{0x7000, 0x8000, 0, 0}, // 读两个内存地址
	)

	if !loadInstr.IsLoad() {
		t.Error("Expected IsLoad() to be true")
	}
	if loadInstr.IsStore() {
		t.Error("Expected IsStore() to be false")
	}
	if loadInstr.NumMemOps() != 2 {
		t.Errorf("Expected 2 memory ops, got %d", loadInstr.NumMemOps())
	}

	// Store 指令: MOV [addr], reg
	storeInstr := NewOOOModelInstrFromInput(
		0, 0x6100, 0, 0,
		[]uint8{0, 0},
		[]uint8{1, 0, 0, 0},
		[]uint64{0x9000, 0}, // 写一个内存地址
		[]uint64{0, 0, 0, 0},   // 无内存读
	)

	if storeInstr.IsLoad() {
		t.Error("Expected IsLoad() to be false")
	}
	if !storeInstr.IsStore() {
		t.Error("Expected IsStore() to be true")
	}
	if storeInstr.NumMemOps() != 1 {
		t.Errorf("Expected 1 memory op, got %d", storeInstr.NumMemOps())
	}
}

// TestOOOModelInstr_ProgramOrder 测试程序顺序比较
func TestOOOModelInstr_ProgramOrder(t *testing.T) {
	instr1 := &OOOModelInstr{InstrID: 100}
	instr2 := &OOOModelInstr{InstrID: 200}

	if !instr1.ProgramOrder(instr2) {
		t.Error("Expected instr1 to precede instr2")
	}
	if instr2.ProgramOrder(instr1) {
		t.Error("Expected instr2 not to precede instr1")
	}
}

// TestOOOModelInstr_RegisterFiltering 测试寄存器过滤（0 值）
func TestOOOModelInstr_RegisterFiltering(t *testing.T) {
	instr := NewOOOModelInstrFromInput(
		0, 0x7000, 0, 0,
		[]uint8{1, 0},          // 第二个是 0，应被过滤
		[]uint8{2, 0, 3, 0},    // 0 值应被过滤
		[]uint64{0, 0},
		[]uint64{0, 0, 0, 0},
	)

	if len(instr.DestRegisters) != 1 {
		t.Errorf("Expected 1 dest register, got %d", len(instr.DestRegisters))
	}
	if len(instr.SrcRegisters) != 2 {
		t.Errorf("Expected 2 src registers, got %d", len(instr.SrcRegisters))
	}
}

// TestOOOModelInstr_MemoryFiltering 测试内存地址过滤（0 值）
func TestOOOModelInstr_MemoryFiltering(t *testing.T) {
	instr := NewOOOModelInstrFromInput(
		0, 0x8000, 0, 0,
		[]uint8{0, 0},
		[]uint8{0, 0, 0, 0},
		[]uint64{0x1000, 0},             // 第二个是 0，应被过滤
		[]uint64{0x2000, 0, 0x3000, 0}, // 0 值应被过滤
	)

	if len(instr.DestMemory) != 1 {
		t.Errorf("Expected 1 dest memory, got %d", len(instr.DestMemory))
	}
	if instr.DestMemory[0] != 0x1000 {
		t.Errorf("Expected dest memory 0x1000, got 0x%x", instr.DestMemory[0])
	}

	if len(instr.SrcMemory) != 2 {
		t.Errorf("Expected 2 src memory, got %d", len(instr.SrcMemory))
	}
}
