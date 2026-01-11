package trace

import (
	"fmt"
	"os"
	"testing"
)

// TestRealTraceReader_Perlbench 测试读取真实的 perlbench trace
func TestRealTraceReader_Perlbench(t *testing.T) {
	// Use environment variable if provided, otherwise fallback to repo's small trace
	traceFile := os.Getenv("CHAMPSIM_TRACE_PERLBENCH")
	if traceFile == "" {
		largeTrace := "../../../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
		if _, err := os.Stat(largeTrace); err == nil {
			traceFile = largeTrace
		} else {
			traceFile = "../../../../../testdata/traces/small.champsimtrace"
		}
	}

	reader, err := NewTraceReader(traceFile, 0, FormatStandard)
	if err != nil {
		t.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE_PERLBENCH=%s)", err, traceFile)
	}
	defer reader.Close()

	// 读取前 100 条指令
	const testCount = 100
	instrCount := 0
	loadCount := 0
	storeCount := 0
	branchCount := 0

	for i := 0; i < testCount; i++ {
		instr, err := reader.ReadInstruction()
		if err != nil {
			t.Fatalf("Failed to read instruction %d: %v", i, err)
		}

		instrCount++

		// 统计指令类型
		if instr.IsLoad() {
			loadCount++
		}
		if instr.IsStore() {
			storeCount++
		}
		if instr.IsBranch {
			branchCount++
		}

		// 验证指令的合理性
		if instr.IP == 0 {
			t.Errorf("Instruction %d has zero IP", i)
		}

		// 打印前 5 条指令的详细信息
		if i < 5 {
			t.Logf("Instruction %d:", i)
			t.Logf("  IP: 0x%x", instr.IP)
			t.Logf("  IsBranch: %v, BranchTaken: %v", instr.IsBranch, instr.BranchTaken)
			t.Logf("  IsLoad: %v, IsStore: %v", instr.IsLoad(), instr.IsStore())
			if instr.IsBranch && instr.BranchTaken {
				t.Logf("  BranchTarget: 0x%x", instr.BranchTarget)
			}
		}
	}

	// 验证统计信息
	t.Logf("\nStatistics for first %d instructions:", testCount)
	t.Logf("  Total instructions: %d", instrCount)
	t.Logf("  Loads: %d (%.1f%%)", loadCount, float64(loadCount)*100/float64(instrCount))
	t.Logf("  Stores: %d (%.1f%%)", storeCount, float64(storeCount)*100/float64(instrCount))
	t.Logf("  Branches: %d (%.1f%%)", branchCount, float64(branchCount)*100/float64(instrCount))

	// 验证读取到了指令
	if instrCount != testCount {
		t.Errorf("Expected to read %d instructions, got %d", testCount, instrCount)
	}

	// 验证有合理的负载/存储/分支比例
	if loadCount == 0 && storeCount == 0 {
		t.Error("No memory operations found, trace may be corrupted")
	}
	if branchCount == 0 {
		t.Error("No branches found, trace may be corrupted")
	}
}

// TestRealTraceReader_MCF 测试读取真实的 mcf trace
func TestRealTraceReader_MCF(t *testing.T) {
	// Use environment variable if provided, otherwise fallback to repo's small trace
	traceFile := os.Getenv("CHAMPSIM_TRACE_MCF")
	if traceFile == "" {
		largeTrace := "../../../../../testdata/traces/429.mcf-22B.champsimtrace.xz"
		if _, err := os.Stat(largeTrace); err == nil {
			traceFile = largeTrace
		} else {
			traceFile = "../../../../../testdata/traces/small_mcf.champsimtrace"
		}
	}

	reader, err := NewTraceReader(traceFile, 0, FormatStandard)
	if err != nil {
		t.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE_MCF=%s)", err, traceFile)
	}
	defer reader.Close()

	// 读取前 100 条指令
	const testCount = 100
	for i := 0; i < testCount; i++ {
		instr, err := reader.ReadInstruction()
		if err != nil {
			t.Fatalf("Failed to read instruction %d: %v", i, err)
		}

		// 验证 IP 不为零
		if instr.IP == 0 {
			t.Errorf("Instruction %d has zero IP", i)
		}

		// 打印第一条指令
		if i == 0 {
			t.Logf("First instruction from MCF trace:")
			t.Logf("  IP: 0x%x", instr.IP)
			t.Logf("  IsBranch: %v", instr.IsBranch)
			t.Logf("  IsLoad: %v, IsStore: %v", instr.IsLoad(), instr.IsStore())
		}
	}

	t.Logf("Successfully read %d instructions from MCF trace", testCount)
}

// TestTraceReaderBinaryLayout 验证二进制布局的正确性
func TestTraceReaderBinaryLayout(t *testing.T) {
	// 验证 FormatStandard 的大小
	expectedSize := 8 + 1 + 1 + 2 + 4 + 16 + 32 // = 64 bytes
	actualSize := FormatStandard.InstrSize()

	if actualSize != expectedSize {
		t.Errorf("FormatStandard size mismatch: expected %d, got %d", expectedSize, actualSize)
	}

	// 验证 CloudSuite 的大小
	expectedCloudSuiteSize := 8 + 1 + 1 + 4 + 4 + 32 + 32 + 2 // = 84 bytes
	actualCloudSuiteSize := FormatCloudSuite.InstrSize()

	if actualCloudSuiteSize != expectedCloudSuiteSize {
		t.Errorf("FormatCloudSuite size mismatch: expected %d, got %d",
			expectedCloudSuiteSize, actualCloudSuiteSize)
	}

	t.Logf("Binary layout verification:")
	t.Logf("  FormatStandard: %d bytes", actualSize)
	t.Logf("  FormatCloudSuite: %d bytes", actualCloudSuiteSize)
}

// BenchmarkRealTraceReader 基准测试真实 trace 读取性能
func BenchmarkRealTraceReader(b *testing.B) {
	// Use environment variable if provided, otherwise fallback to repo's small trace
	traceFile := os.Getenv("CHAMPSIM_TRACE_PERLBENCH")
	if traceFile == "" {
		largeTrace := "../../../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
		if _, err := os.Stat(largeTrace); err == nil {
			traceFile = largeTrace
		} else {
			traceFile = "../../../../../testdata/traces/small.champsimtrace"
		}
	}

	reader, err := NewTraceReader(traceFile, 0, FormatStandard)
	if err != nil {
		b.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE_PERLBENCH=%s)", err, traceFile)
	}
	defer reader.Close()

	b.ResetTimer()

	instrCount := 0
	for i := 0; i < b.N; i++ {
		instr, err := reader.ReadInstruction()
		if err != nil {
			// 重新打开文件继续测试
			reader.Close()
			reader, _ = NewTraceReader(traceFile, 0, FormatStandard)
			continue
		}
		instrCount++

		// 避免编译器优化掉读取操作
		_ = instr.IP
	}

	b.ReportMetric(float64(instrCount)/b.Elapsed().Seconds(), "instrs/sec")
}

// ExampleTraceReader 演示如何使用 TraceReader
func ExampleTraceReader() {
	// 打开 trace 文件
	reader, err := NewTraceReader("trace.champsimtrace.xz", 0, FormatStandard)
	if err != nil {
		fmt.Printf("Failed to open trace: %v\n", err)
		return
	}
	defer reader.Close()

	// 读取指令
	for i := 0; i < 10; i++ {
		instr, err := reader.ReadInstruction()
		if err != nil {
			fmt.Printf("Error reading instruction: %v\n", err)
			break
		}

		fmt.Printf("IP: 0x%x, Branch: %v, Load: %v, Store: %v\n",
			instr.IP, instr.IsBranch, instr.IsLoad(), instr.IsStore())
	}
}
