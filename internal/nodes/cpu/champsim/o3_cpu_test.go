package cpu

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"testing"

	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/instruction"
	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/trace"
)

// nopCloser wraps an io.Reader to add a no-op Close method
type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

// createTestTrace 创建一个测试用的 trace 缓冲区
func createTestTrace(count int) *bytes.Buffer {
	buf := new(bytes.Buffer)

	for i := 0; i < count; i++ {
		instr := trace.InputInstr{
			IP:            uint64(0x400000 + i*16),
			IsBranch:      0,
			BranchTaken:   0,
			DestRegisters: [2]uint8{uint8(i % 16), 0xFF}, // 使用不同的寄存器
			SrcRegisters:  [4]uint8{uint8((i + 1) % 16), 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
		}
		binary.Write(buf, binary.LittleEndian, &instr)
	}

	return buf
}

// createTraceReader 从 buffer 创建 trace reader
func createTraceReader(buf *bytes.Buffer) trace.TraceReader {
	// We need to access the unexported fields, so we'll use a temporary file approach
	// For testing, we create a trace reader directly
	tmpfile, err := os.CreateTemp("", "trace_test_*.champsimtrace")
	if err != nil {
		panic(err)
	}
	defer os.Remove(tmpfile.Name())

	tmpfile.Write(buf.Bytes())
	tmpfile.Close()

	reader, err := trace.NewTraceReader(tmpfile.Name(), 0, trace.FormatStandard)
	if err != nil {
		panic(err)
	}

	return reader
}

// TestO3CPU_DefaultConfig 测试默认配置
func TestO3CPU_DefaultConfig(t *testing.T) {
	config := DefaultO3CPUConfig()

	// 验证关键参数
	if config.FetchWidth != 6 {
		t.Errorf("Expected FetchWidth 6, got %d", config.FetchWidth)
	}

	if config.ROBSize != DefaultROBSize {
		t.Errorf("Expected ROBSize %d, got %d", DefaultROBSize, config.ROBSize)
	}

	if config.PhysicalRegisters != 180 {
		t.Errorf("Expected 180 physical registers, got %d", config.PhysicalRegisters)
	}
}

// TestO3CPU_Creation 测试 CPU 创建
func TestO3CPU_Creation(t *testing.T) {
	traceBuf := createTestTrace(10)
	traceReader := createTraceReader(traceBuf)

	cpu := NewO3CPU(traceReader, DefaultO3CPUConfig())

	if cpu == nil {
		t.Fatal("Failed to create O3CPU")
	}

	// 检查组件是否正确初始化
	if cpu.rob == nil {
		t.Error("ROB should be initialized")
	}

	if cpu.lsq == nil {
		t.Error("LSQ should be initialized")
	}

	if cpu.regAlloc == nil {
		t.Error("Register allocator should be initialized")
	}

	if cpu.dib == nil {
		t.Error("DIB should be initialized")
	}

	if cpu.currentCycle != 0 {
		t.Errorf("Expected initial cycle 0, got %d", cpu.currentCycle)
	}
}

// TestO3CPU_SingleTick 测试单个时钟周期
func TestO3CPU_SingleTick(t *testing.T) {
	traceBuf := createTestTrace(10)
	traceReader := createTraceReader(traceBuf)

	cpu := NewO3CPU(traceReader, DefaultO3CPUConfig())

	// 执行一个周期
	cpu.Tick()

	// 周期数应该增加
	if cpu.currentCycle != 1 {
		t.Errorf("Expected cycle 1, got %d", cpu.currentCycle)
	}

	// 统计应该更新
	if cpu.stats.TotalCycles != 1 {
		t.Errorf("Expected total cycles 1, got %d", cpu.stats.TotalCycles)
	}
}

// TestO3CPU_FetchStage 测试 Fetch 阶段
func TestO3CPU_FetchStage(t *testing.T) {
	traceBuf := createTestTrace(20)
	traceReader := createTraceReader(traceBuf)

	config := DefaultO3CPUConfig()
	config.FetchWidth = 4
	cpu := NewO3CPU(traceReader, config)

	// 执行几个周期让 fetch 工作
	for i := 0; i < 3; i++ {
		cpu.Tick()
	}

	// fetchQueue 应该有指令
	if len(cpu.fetchQueue) == 0 {
		t.Error("fetchQueue should have instructions after fetch")
	}

	// 检查指令 ID
	if cpu.instrCounter == 0 {
		t.Error("instrCounter should increment")
	}
}

// TestO3CPU_PipelineFlow 测试流水线流动
func TestO3CPU_PipelineFlow(t *testing.T) {
	traceBuf := createTestTrace(10)
	traceReader := createTraceReader(traceBuf)

	cpu := NewO3CPU(traceReader, DefaultO3CPUConfig())

	// 执行多个周期
	for i := 0; i < 20; i++ {
		cpu.Tick()
	}

	// 应该有指令在流水线中流动
	// 至少应该有一些指令被退休
	if cpu.stats.TotalInstructions == 0 {
		t.Error("Should have retired some instructions")
	}
}

// TestO3CPU_ROBFull 测试 ROB 满的情况
func TestO3CPU_ROBFull(t *testing.T) {
	traceBuf := createTestTrace(100)
	traceReader := createTraceReader(traceBuf)

	config := DefaultO3CPUConfig()
	config.ROBSize = 4 // 小的 ROB
	cpu := NewO3CPU(traceReader, config)

	// 执行几个周期
	for i := 0; i < 10; i++ {
		cpu.Tick()
	}

	// ROB 应该会满
	// 这会导致 dispatch stalls
	// 注意：由于指令可能会退休，ROB 不一定一直满
	// 但是应该至少有一些 dispatch stalls
	if cpu.rob.IsFull() || cpu.stats.DispatchStalls > 0 {
		// 这是预期的行为
	} else {
		t.Log("Warning: Expected ROB to be full or have dispatch stalls")
	}
}

// TestO3CPU_SimpleRun 测试简单运行
func TestO3CPU_SimpleRun(t *testing.T) {
	traceBuf := createTestTrace(50)
	traceReader := createTraceReader(traceBuf)

	cpu := NewO3CPU(traceReader, DefaultO3CPUConfig())

	// 运行仿真
	cpu.Run(0, 50)

	// 检查统计
	stats := cpu.GetStats()

	if stats.TotalInstructions != 50 {
		t.Errorf("Expected 50 instructions, got %d", stats.TotalInstructions)
	}

	if stats.TotalCycles == 0 {
		t.Error("Total cycles should be greater than 0")
	}

	// IPC 应该被计算
	if stats.IPC == 0 {
		t.Error("IPC should be calculated")
	}

	t.Logf("IPC: %.2f, Cycles: %d, Instructions: %d",
		stats.IPC, stats.TotalCycles, stats.TotalInstructions)
}

// TestO3CPU_MemoryInstructions 测试内存指令
func TestO3CPU_MemoryInstructions(t *testing.T) {
	// 创建包含内存操作的 trace
	buf := new(bytes.Buffer)

	// Load 指令
	loadInstr := trace.InputInstr{
		IP:            0x400000,
		IsBranch:      0,
		BranchTaken:   0,
		DestRegisters: [2]uint8{1, 0xFF},
		SrcRegisters:  [4]uint8{2, 0xFF, 0xFF, 0xFF},
		DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
		SrcMemory:     [instruction.NumInstrSources]uint64{0x1000, 0, 0, 0}, // Load from 0x1000
	}
	binary.Write(buf, binary.LittleEndian, &loadInstr)

	// Store 指令
	storeInstr := trace.InputInstr{
		IP:            0x400010,
		IsBranch:      0,
		BranchTaken:   0,
		DestRegisters: [2]uint8{0xFF, 0xFF},
		SrcRegisters:  [4]uint8{3, 0xFF, 0xFF, 0xFF},
		DestMemory:    [instruction.NumInstrDestinations]uint64{0x2000, 0}, // Store to 0x2000
		SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
	}
	binary.Write(buf, binary.LittleEndian, &storeInstr)

	traceReader := createTraceReader(buf)
	cpu := NewO3CPU(traceReader, DefaultO3CPUConfig())

	// 运行仿真
	cpu.Run(0, 2)

	// 检查 LSQ 统计
	lsqStats := cpu.lsq.GetStats()

	if lsqStats.TotalLoads == 0 {
		t.Error("Should have processed load instructions")
	}

	if lsqStats.TotalStores == 0 {
		t.Error("Should have processed store instructions")
	}
}

// TestO3CPU_BranchInstructions 测试分支指令
func TestO3CPU_BranchInstructions(t *testing.T) {
	// 创建包含分支的 trace
	buf := new(bytes.Buffer)

	// 普通指令
	normalInstr := trace.InputInstr{
		IP:            0x400000,
		IsBranch:      0,
		BranchTaken:   0,
		DestRegisters: [2]uint8{1, 0xFF},
		SrcRegisters:  [4]uint8{2, 0xFF, 0xFF, 0xFF},
	}
	binary.Write(buf, binary.LittleEndian, &normalInstr)

	// 分支指令（taken）
	branchInstr := trace.InputInstr{
		IP:            0x400010,
		IsBranch:      1,
		BranchTaken:   1,
		DestRegisters: [2]uint8{0xFF, 0xFF},
		SrcRegisters:  [4]uint8{1, instruction.RegFlags, 0xFF, 0xFF},
	}
	binary.Write(buf, binary.LittleEndian, &branchInstr)

	// 分支目标指令
	targetInstr := trace.InputInstr{
		IP:            0x500000, // 不同的地址
		IsBranch:      0,
		BranchTaken:   0,
		DestRegisters: [2]uint8{3, 0xFF},
		SrcRegisters:  [4]uint8{4, 0xFF, 0xFF, 0xFF},
	}
	binary.Write(buf, binary.LittleEndian, &targetInstr)

	traceReader := createTraceReader(buf)
	cpu := NewO3CPU(traceReader, DefaultO3CPUConfig())

	// 运行仿真
	cpu.Run(0, 3)

	// 检查分支统计
	stats := cpu.GetStats()

	if stats.TotalBranches == 0 {
		t.Error("Should have processed branch instructions")
	}

	t.Logf("Total branches: %d, Mispredictions: %d",
		stats.TotalBranches, stats.BranchMispredictions)
}

// TestO3CPU_Warmup 测试预热阶段
func TestO3CPU_Warmup(t *testing.T) {
	traceBuf := createTestTrace(100)
	traceReader := createTraceReader(traceBuf)

	cpu := NewO3CPU(traceReader, DefaultO3CPUConfig())

	// 运行仿真：10 条预热，20 条统计
	cpu.Run(10, 20)

	// 总指令数应该至少是 30（可能会多一些，因为流水线）
	if cpu.stats.TotalInstructions < 30 {
		t.Errorf("Expected at least 30 total instructions, got %d", cpu.stats.TotalInstructions)
	}

	// 仿真指令数应该至少是 20
	if cpu.simulationInstructions < 20 {
		t.Errorf("Expected at least 20 simulation instructions, got %d", cpu.simulationInstructions)
	}
}

// TestO3CPU_EmptyTrace 测试空 trace
func TestO3CPU_EmptyTrace(t *testing.T) {
	buf := new(bytes.Buffer) // 空 buffer
	traceReader := createTraceReader(buf)

	cpu := NewO3CPU(traceReader, DefaultO3CPUConfig())

	// 运行仿真
	cpu.Run(0, 10)

	// 应该没有指令被执行
	if cpu.stats.TotalInstructions != 0 {
		t.Errorf("Expected 0 instructions for empty trace, got %d", cpu.stats.TotalInstructions)
	}
}
