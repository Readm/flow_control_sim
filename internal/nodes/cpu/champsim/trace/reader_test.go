package trace

import (
	"compress/gzip"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/instruction"
)

// TestTraceFormat_InstrSize 测试指令大小计算
func TestTraceFormat_InstrSize(t *testing.T) {
	tests := []struct {
		format   TraceFormat
		expected int
	}{
		{FormatStandard, 64},
		{FormatCloudSuite, 84},
	}

	for _, tt := range tests {
		if got := tt.format.InstrSize(); got != tt.expected {
			t.Errorf("TraceFormat(%s).InstrSize() = %d, want %d",
				tt.format, got, tt.expected)
		}
	}
}

// createTestTraceFile 创建测试用的 trace 文件
func createTestTraceFile(t *testing.T, instrs []InputInstr, compress bool) string {
	t.Helper()

	// 创建临时文件
	tmpDir := t.TempDir()
	var filename string
	if compress {
		filename = filepath.Join(tmpDir, "test.champsimtrace.gz")
	} else {
		filename = filepath.Join(tmpDir, "test.champsimtrace")
	}

	f, err := os.Create(filename)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer f.Close()

	var writer io.Writer = f
	var gzWriter *gzip.Writer

	if compress {
		gzWriter = gzip.NewWriter(f)
		writer = gzWriter
	}

	// 写入指令
	for _, instr := range instrs {
		if err := binary.Write(writer, binary.LittleEndian, &instr); err != nil {
			t.Fatalf("Failed to write instruction: %v", err)
		}
	}

	if compress {
		if err := gzWriter.Close(); err != nil {
			t.Fatalf("Failed to close gzip writer: %v", err)
		}
	}

	return filename
}

// TestBulkTraceReader_BasicRead 测试基本读取功能
func TestBulkTraceReader_BasicRead(t *testing.T) {
	// 创建测试数据：3 条简单指令
	testInstrs := []InputInstr{
		{
			IP:       0x1000,
			IsBranch: 0,
			DestRegisters: [2]uint8{1, 0},
			SrcRegisters:  [4]uint8{2, 3, 0, 0},
		},
		{
			IP:       0x1010,
			IsBranch: 1,
			BranchTaken: 1,
			DestRegisters: [2]uint8{instruction.RegInstructionPointer, 0},
			SrcRegisters:  [4]uint8{instruction.RegInstructionPointer, instruction.RegFlags, 0, 0},
		},
		{
			IP:       0x2000,
			IsBranch: 0,
			DestRegisters: [2]uint8{4, 0},
			SrcRegisters:  [4]uint8{5, 0, 0, 0},
		},
	}

	filename := createTestTraceFile(t, testInstrs, false)

	// 创建 reader
	reader, err := NewTraceReader(filename, 0, FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	// 读取所有指令
	var readInstrs []*instruction.OOOModelInstr
	for {
		instr, err := reader.ReadInstruction()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read instruction: %v", err)
		}
		readInstrs = append(readInstrs, instr)
	}

	// 验证读取的指令数量
	if len(readInstrs) != len(testInstrs) {
		t.Fatalf("Expected %d instructions, got %d", len(testInstrs), len(readInstrs))
	}

	// 验证指令 ID 递增
	for i, instr := range readInstrs {
		if instr.InstrID != uint64(i) {
			t.Errorf("Instruction %d: expected InstrID %d, got %d", i, i, instr.InstrID)
		}
	}

	// 验证 IP
	if readInstrs[0].IP != 0x1000 {
		t.Errorf("Instruction 0: expected IP 0x1000, got 0x%x", readInstrs[0].IP)
	}

	// 验证分支识别
	if readInstrs[1].BranchType != instruction.BranchConditional {
		t.Errorf("Instruction 1: expected BranchConditional, got %s", readInstrs[1].BranchType)
	}

	// 验证 EOF
	if !reader.EOF() {
		t.Error("Expected EOF to be true after reading all instructions")
	}
}

// TestBulkTraceReader_BranchTargets 测试分支目标设置
func TestBulkTraceReader_BranchTargets(t *testing.T) {
	// 创建包含分支的测试数据
	testInstrs := []InputInstr{
		{
			IP:       0x1000,
			IsBranch: 0,
		},
		{
			IP:            0x1010,
			IsBranch:      1,
			BranchTaken:   1, // 跳转
			DestRegisters: [2]uint8{instruction.RegInstructionPointer, 0},
			SrcRegisters:  [4]uint8{0, 0, 0, 0},
		},
		{
			IP:       0x2000, // 分支目标
			IsBranch: 0,
		},
		{
			IP:       0x2010,
			IsBranch: 0,
		},
	}

	filename := createTestTraceFile(t, testInstrs, false)
	reader, err := NewTraceReader(filename, 0, FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	// 读取所有指令
	var readInstrs []*instruction.OOOModelInstr
	for {
		instr, err := reader.ReadInstruction()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read instruction: %v", err)
		}
		readInstrs = append(readInstrs, instr)
	}

	// 验证分支目标
	branchInstr := readInstrs[1]
	if !branchInstr.IsBranch {
		t.Fatal("Instruction 1 should be a branch")
	}
	if branchInstr.BranchTarget != 0x2000 {
		t.Errorf("Branch target: expected 0x2000, got 0x%x", branchInstr.BranchTarget)
	}
}

// TestBulkTraceReader_GzipCompressed 测试 gzip 压缩格式
func TestBulkTraceReader_GzipCompressed(t *testing.T) {
	testInstrs := []InputInstr{
		{IP: 0x1000, IsBranch: 0},
		{IP: 0x1010, IsBranch: 0},
	}

	filename := createTestTraceFile(t, testInstrs, true)

	reader, err := NewTraceReader(filename, 0, FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create reader for gzip file: %v", err)
	}
	defer reader.Close()

	// 读取指令
	count := 0
	for {
		_, err := reader.ReadInstruction()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read instruction: %v", err)
		}
		count++
	}

	if count != len(testInstrs) {
		t.Errorf("Expected %d instructions, got %d", len(testInstrs), count)
	}
}

// TestBulkTraceReader_LargeTrace 测试大量指令（触发多次缓冲刷新）
func TestBulkTraceReader_LargeTrace(t *testing.T) {
	// 创建 200 条指令（超过默认缓冲区大小 128）
	const numInstrs = 200
	testInstrs := make([]InputInstr, numInstrs)
	for i := 0; i < numInstrs; i++ {
		testInstrs[i] = InputInstr{
			IP:       uint64(0x1000 + i*16),
			IsBranch: 0,
		}
	}

	filename := createTestTraceFile(t, testInstrs, false)
	reader, err := NewTraceReader(filename, 0, FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	// 读取所有指令
	count := 0
	for {
		_, err := reader.ReadInstruction()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read instruction: %v", err)
		}
		count++
	}

	if count != numInstrs {
		t.Errorf("Expected %d instructions, got %d", numInstrs, count)
	}
}

// TestBulkTraceReader_EmptyFile 测试空文件
func TestBulkTraceReader_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "empty.champsimtrace")

	// 创建空文件
	f, err := os.Create(filename)
	if err != nil {
		t.Fatalf("Failed to create empty file: %v", err)
	}
	f.Close()

	reader, err := NewTraceReader(filename, 0, FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	// 尝试读取
	_, err = reader.ReadInstruction()
	if err != io.EOF {
		t.Errorf("Expected io.EOF for empty file, got %v", err)
	}

	if !reader.EOF() {
		t.Error("Expected EOF() to be true for empty file")
	}
}

// TestBulkTraceReader_CPUIDAndASID 测试 CPUID 和 ASID 设置
func TestBulkTraceReader_CPUIDAndASID(t *testing.T) {
	testInstrs := []InputInstr{
		{IP: 0x1000, IsBranch: 0},
	}

	filename := createTestTraceFile(t, testInstrs, false)
	reader, err := NewTraceReader(filename, 5, FormatStandard) // CPUID = 5
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	instr, err := reader.ReadInstruction()
	if err != nil {
		t.Fatalf("Failed to read instruction: %v", err)
	}

	if instr.CPUID != 5 {
		t.Errorf("Expected CPUID 5, got %d", instr.CPUID)
	}
	if instr.ASID[0] != 5 || instr.ASID[1] != 5 {
		t.Errorf("Expected ASID [5, 5], got [%d, %d]", instr.ASID[0], instr.ASID[1])
	}
}

// TestSetBranchTargets 测试分支目标设置算法
func TestSetBranchTargets(t *testing.T) {
	// 手动创建指令序列
	instrs := []*instruction.OOOModelInstr{
		{IP: 0x1000, IsBranch: false},
		{IP: 0x1010, IsBranch: true, BranchTaken: true, BranchType: instruction.BranchConditional},
		{IP: 0x2000, IsBranch: false},
		{IP: 0x2010, IsBranch: true, BranchTaken: true, BranchType: instruction.BranchDirectJump},
		{IP: 0x3000, IsBranch: false},
	}

	setBranchTargets(instrs)

	// 验证分支目标
	if instrs[1].BranchTarget != 0x2000 {
		t.Errorf("Instruction 1: expected branch target 0x2000, got 0x%x", instrs[1].BranchTarget)
	}
	if instrs[3].BranchTarget != 0x3000 {
		t.Errorf("Instruction 3: expected branch target 0x3000, got 0x%x", instrs[3].BranchTarget)
	}
}

// TestSetBranchTargets_NotTaken 测试不跳转的分支
func TestSetBranchTargets_NotTaken(t *testing.T) {
	instrs := []*instruction.OOOModelInstr{
		{IP: 0x1000, IsBranch: true, BranchTaken: false, BranchType: instruction.BranchConditional},
		{IP: 0x1010, IsBranch: false},
	}

	setBranchTargets(instrs)

	// 不跳转的分支不设置 target
	if instrs[0].BranchTarget != 0 {
		t.Errorf("Not-taken branch should have target 0, got 0x%x", instrs[0].BranchTarget)
	}
}
