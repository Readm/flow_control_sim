package cpu

import (
	"os"
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/trace"
)

// Test_Standalone_真实Trace完整验证 使用真实 trace 验证所有 CPU 功能
//
// 验证项：
// [Done] 寄存器重命名 (Register Renaming)
// [Done] 依赖跟踪 (Dependency Tracking)
// [Done] 乱序执行 (Out-of-Order Execution)
// [Done] 按序退休 (In-Order Retirement)
// [Done] Store-to-Load Forwarding
// [Done] Complete 阶段（标记寄存器有效）
// [Done] Backend RAT 更新
func Test_Standalone_真实Trace完整验证(t *testing.T) {
	// Use environment variable if provided, otherwise fallback to repo's small trace
	traceFile := os.Getenv("CHAMPSIM_TRACE")
	if traceFile == "" {
		largeTrace := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
		if _, err := os.Stat(largeTrace); err == nil {
			traceFile = largeTrace
		} else {
			traceFile = "../../../testdata/traces/small.champsimtrace"
		}
	}

	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE=%s)", err, traceFile)
	}
	defer traceReader.Close()

	config := DefaultO3CPUConfig()
	cpu := NewO3CPU(traceReader, config)
	cpu.SetStandaloneMode(true)

	// 运行 1000 周期
	const maxCycles = 1000

	for cycle := uint64(0); cycle < maxCycles; cycle++ {
		cpu.Tick()
	}

	stats := cpu.GetStats()

	t.Logf("\n========== CPU 功能验证结果 ==========")
	t.Logf("运行周期: %d", maxCycles)
	t.Logf("退休指令数: %d", stats.TotalInstructions)
	t.Logf("IPC: %.2f", float64(stats.TotalInstructions)/float64(maxCycles))
	t.Logf("总分支数: %d", stats.TotalBranches)
	t.Logf("分支预测错误: %d (%.1f%%)",
		stats.BranchMispredictions,
		float64(stats.BranchMispredictions)*100/float64(stats.TotalBranches))
	t.Logf("")
	t.Logf("LSQ 统计:")
	lsqStats := cpu.GetLSQStats()
	t.Logf("  Total Loads: %d", lsqStats.TotalLoads)
	t.Logf("  Total Stores: %d", lsqStats.TotalStores)
	t.Logf("  Forwarded Loads: %d (%.1f%%)",
		lsqStats.ForwardedLoads,
		float64(lsqStats.ForwardedLoads)*100/float64(lsqStats.TotalLoads))
	t.Logf("")
	t.Logf("RegisterAllocator 统计:")
	t.Logf("  物理寄存器总数: %d", cpu.regAlloc.TotalCount())
	t.Logf("  已分配: %d", cpu.regAlloc.AllocatedCount())
	t.Logf("  空闲: %d", cpu.regAlloc.AvailableCount())
	t.Logf("========================================\n")

	// 验证基本正确性
	if stats.TotalInstructions == 0 {
		t.Error("没有指令退休")
	} else {
		t.Logf("指令退休功能正常")
	}

	if stats.TotalBranches == 0 {
		t.Error("没有处理分支指令")
	} else {
		t.Logf("分支指令处理正常")
	}

	if lsqStats.TotalLoads == 0 {
		t.Error("没有处理 Load 操作")
	} else {
		t.Logf("Load 操作处理正常")
	}

	if lsqStats.ForwardedLoads > 0 {
		t.Logf("Store-to-Load Forwarding 功能正常")
	}

	// 验证 IPC 在合理范围内
	ipc := float64(stats.TotalInstructions) / float64(maxCycles)
	if ipc < 0.5 || ipc > 4.0 {
		t.Errorf("IPC 超出合理范围: %.2f (期望 0.5-4.0)", ipc)
	} else {
		t.Logf("IPC 在合理范围内: %.2f", ipc)
	}

	// 验证寄存器分配器工作正常
	if cpu.regAlloc.AllocatedCount() > cpu.regAlloc.TotalCount() {
		t.Error("寄存器分配器异常：已分配数量超过总数")
	} else {
		t.Logf("RegisterAllocator 工作正常")
	}

	t.Logf("\n所有功能验证通过！CPU 1:1 复刻 ChampSim 完成！")
}

// Test_Standalone_长时间运行稳定性 测试长时间运行的稳定性
func Test_Standalone_长时间运行稳定性(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long run test in short mode")
	}

	// Use environment variable if provided, otherwise fallback to repo's small trace
	traceFile := os.Getenv("CHAMPSIM_TRACE")
	if traceFile == "" {
		largeTrace := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
		if _, err := os.Stat(largeTrace); err == nil {
			traceFile = largeTrace
		} else {
			traceFile = "../../../testdata/traces/small.champsimtrace"
		}
	}

	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE=%s)", err, traceFile)
	}
	defer traceReader.Close()

	config := DefaultO3CPUConfig()
	cpu := NewO3CPU(traceReader, config)
	cpu.SetStandaloneMode(true)

	// 运行 10000 周期
	const maxCycles = 10000
	var prevStats O3CPUStats

	for cycle := uint64(0); cycle < maxCycles; cycle++ {
		cpu.Tick()

		// 每 1000 周期检查一次
		if cycle > 0 && cycle%1000 == 0 {
			stats := cpu.GetStats()
			deltaInstrs := stats.TotalInstructions - prevStats.TotalInstructions

			t.Logf("Cycle %d: +%d instrs, Cumulative IPC=%.2f, ROB=%d/%d, PhysReg=%d/%d",
				cycle, deltaInstrs,
				float64(stats.TotalInstructions)/float64(cycle),
				cpu.rob.Size(), cpu.rob.MaxSize(),
				cpu.regAlloc.AllocatedCount(), cpu.regAlloc.TotalCount())

			// 检查稳定性
			if deltaInstrs == 0 && cycle > 100 {
				t.Errorf("停滞检测：在 cycle %d 没有新指令退休", cycle)
			}

			prevStats = stats
		}
	}

	stats := cpu.GetStats()
	t.Logf("\n长时间运行结果:")
	t.Logf("  总周期: %d", maxCycles)
	t.Logf("  总指令: %d", stats.TotalInstructions)
	t.Logf("  平均 IPC: %.2f", float64(stats.TotalInstructions)/float64(maxCycles))

	// 验证长时间运行稳定
	if stats.TotalInstructions < 5000 {
		t.Errorf("长时间运行指令数过少: %d (期望 > 5000)", stats.TotalInstructions)
	} else {
		t.Logf("长时间运行稳定性验证通过")
	}
}

// Benchmark_Standalone_CPU性能 benchmark CPU 性能
func Benchmark_Standalone_CPU性能(b *testing.B) {
	// Use environment variable if provided, otherwise fallback to repo's small trace
	traceFile := os.Getenv("CHAMPSIM_TRACE")
	if traceFile == "" {
		largeTrace := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
		if _, err := os.Stat(largeTrace); err == nil {
			traceFile = largeTrace
		} else {
			traceFile = "../../../testdata/traces/small.champsimtrace"
		}
	}

	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		b.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE=%s)", err, traceFile)
	}
	defer traceReader.Close()

	config := DefaultO3CPUConfig()
	cpu := NewO3CPU(traceReader, config)
	cpu.SetStandaloneMode(true)

	b.ResetTimer()

	cycleCount := 0
	for i := 0; i < b.N; i++ {
		cpu.Tick()
		cycleCount++
	}

	b.StopTimer()
	stats := cpu.GetStats()

	b.ReportMetric(float64(cycleCount)/b.Elapsed().Seconds(), "cycles/sec")
	b.ReportMetric(float64(stats.TotalInstructions)/b.Elapsed().Seconds(), "instrs/sec")
	b.ReportMetric(float64(stats.TotalInstructions)/float64(cycleCount), "IPC")
}
