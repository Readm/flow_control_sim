package integration

import (
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/cpu"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	"github.com/Readm/flow_sim/internal/dataflow/transaction"
)

// TestCPUIncentiveHook_RealTrace_Perlbench 使用真实 Perlbench trace 进行集成测试
func TestCPUIncentiveHook_RealTrace_Perlbench(t *testing.T) {
	traceFile := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"

	// 创建 TraceReader
	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Skipf("Skipping test, trace file not available: %v", err)
	}
	defer traceReader.Close()

	// 创建 CPU 配置
	config := cpu.DefaultO3CPUConfig()
	// 使用默认配置即可

	// 创建 CPUIncentiveHook
	hook := NewCPUIncentiveHook(
		traceReader,
		config,
		0,                       // nodeID
		1,                       // targetNodeID
		transaction.ProtocolCHI, // protocol
	)

	// 运行仿真
	const maxCycles = 1000
	txnCount := 0
	msgCount := 0
	loadCount := 0
	storeCount := 0

	for cycle := uint64(0); cycle < maxCycles; cycle++ {
		// 创建 Transaction
		txn, err := hook.CreateTransaction(0, cycle)
		if err != nil {
			t.Fatalf("Cycle %d: Failed to create transaction: %v", cycle, err)
		}

		if txn != nil {
			txnCount++
			msgCount += len(txn.Messages)

			// 统计 Load/Store 请求
			for _, msg := range txn.Messages {
				if payload, ok := msg.Payload.(*MemoryRequestPayload); ok {
					if payload.IsWrite {
						storeCount++
					} else {
						loadCount++
					}
				}
			}

			// 模拟立即响应所有消息
			for _, msg := range txn.Messages {
				if err := hook.HandleResponse(msg.ID, cycle+1); err != nil {
					t.Errorf("Cycle %d: Failed to handle response for msg %v: %v",
						cycle, msg.ID, err)
				}
			}
		}

		// 每 100 周期打印一次进度
		if cycle > 0 && cycle%100 == 0 {
			stats := hook.GetStats()
			t.Logf("Cycle %d: Instructions=%d, IPC=%.2f, Txns=%d, Msgs=%d (Loads=%d, Stores=%d)",
				cycle, stats.TotalInstructions,
				float64(stats.TotalInstructions)/float64(cycle),
				txnCount, msgCount, loadCount, storeCount)
		}
	}

	// 最终统计
	stats := hook.GetStats()
	t.Logf("\nFinal Statistics after %d cycles:", maxCycles)
	t.Logf("  Instructions Retired: %d", stats.TotalInstructions)
	t.Logf("  IPC: %.2f", float64(stats.TotalInstructions)/float64(maxCycles))
	t.Logf("  Total Branches: %d", stats.TotalBranches)
	t.Logf("  Branch Mispredictions: %d", stats.BranchMispredictions)
	t.Logf("  Transactions: %d", txnCount)
	t.Logf("  Messages: %d (Loads=%d, Stores=%d)", msgCount, loadCount, storeCount)

	// 验证仿真有效性
	if stats.TotalInstructions == 0 {
		t.Error("No instructions retired")
	}
	if msgCount == 0 {
		t.Error("No memory requests generated")
	}
	if stats.TotalBranches == 0 {
		t.Error("No branches processed")
	}

	// 验证 IPC 在合理范围内（0.1 ~ 4.0）
	ipc := float64(stats.TotalInstructions) / float64(maxCycles)
	if ipc < 0.1 || ipc > 4.0 {
		t.Errorf("IPC out of reasonable range: %.2f", ipc)
	}
}

// TestCPUIncentiveHook_RealTrace_MCF 使用真实 MCF trace 进行集成测试
func TestCPUIncentiveHook_RealTrace_MCF(t *testing.T) {
	traceFile := "../../../testdata/traces/429.mcf-22B.champsimtrace.xz"

	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Skipf("Skipping test, trace file not available: %v", err)
	}
	defer traceReader.Close()

	config := cpu.DefaultO3CPUConfig()
	hook := NewCPUIncentiveHook(
		traceReader,
		config,
		0,
		1,
		transaction.ProtocolCHI,
	)

	// 运行较短的仿真（MCF 是内存密集型，可能慢一些）
	const maxCycles = 500
	msgCount := 0

	for cycle := uint64(0); cycle < maxCycles; cycle++ {
		txn, err := hook.CreateTransaction(0, cycle)
		if err != nil {
			t.Fatalf("Cycle %d: Failed to create transaction: %v", cycle, err)
		}

		if txn != nil {
			msgCount += len(txn.Messages)

			// 模拟响应
			for _, msg := range txn.Messages {
				if err := hook.HandleResponse(msg.ID, cycle+1); err != nil {
					t.Errorf("Failed to handle response: %v", err)
				}
			}
		}
	}

	stats := hook.GetStats()
	t.Logf("MCF Statistics after %d cycles:", maxCycles)
	t.Logf("  Instructions Retired: %d", stats.TotalInstructions)
	t.Logf("  IPC: %.2f", float64(stats.TotalInstructions)/float64(maxCycles))
	t.Logf("  Messages: %d", msgCount)

	// MCF 是内存密集型，应该有大量内存请求
	if msgCount == 0 {
		t.Error("MCF should generate memory requests")
	}
}

// TestCPUIncentiveHook_RealTrace_LongRun 长时间运行测试
func TestCPUIncentiveHook_RealTrace_LongRun(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long run test in short mode")
	}

	traceFile := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"

	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Skipf("Skipping test, trace file not available: %v", err)
	}
	defer traceReader.Close()

	config := cpu.DefaultO3CPUConfig()
	hook := NewCPUIncentiveHook(
		traceReader,
		config,
		0,
		1,
		transaction.ProtocolCHI,
	)

	// 运行 10000 周期
	const maxCycles = 10000
	var prevStats cpu.O3CPUStats

	for cycle := uint64(0); cycle < maxCycles; cycle++ {
		txn, err := hook.CreateTransaction(0, cycle)
		if err != nil {
			t.Fatalf("Cycle %d: Failed: %v", cycle, err)
		}

		if txn != nil {
			for _, msg := range txn.Messages {
				hook.HandleResponse(msg.ID, cycle+1)
			}
		}

		// 每 1000 周期检查一次进度
		if cycle > 0 && cycle%1000 == 0 {
			stats := hook.GetStats()
			deltaInstrs := stats.TotalInstructions - prevStats.TotalInstructions

			t.Logf("Cycle %d: +%d instrs, IPC=%.2f, Total Branches=%d",
				cycle, deltaInstrs,
				float64(stats.TotalInstructions)/float64(cycle),
				stats.TotalBranches)

			prevStats = stats
		}
	}

	stats := hook.GetStats()
	t.Logf("\nLong Run Final Statistics:")
	t.Logf("  Cycles: %d", maxCycles)
	t.Logf("  Instructions: %d", stats.TotalInstructions)
	t.Logf("  Average IPC: %.2f", float64(stats.TotalInstructions)/float64(maxCycles))
	t.Logf("  Total Branches: %d", stats.TotalBranches)
	t.Logf("  Mispredictions: %d (%.1f%%)",
		stats.BranchMispredictions,
		float64(stats.BranchMispredictions)*100/float64(stats.TotalBranches))

	// 长时间运行应该有显著的指令退休
	if stats.TotalInstructions < 1000 {
		t.Errorf("Too few instructions retired in long run: %d", stats.TotalInstructions)
	}
}

// BenchmarkCPUIncentiveHook_RealTrace 基准测试真实 trace 的集成性能
func BenchmarkCPUIncentiveHook_RealTrace(b *testing.B) {
	traceFile := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"

	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		b.Skipf("Skipping benchmark, trace file not available: %v", err)
	}
	defer traceReader.Close()

	config := cpu.DefaultO3CPUConfig()
	hook := NewCPUIncentiveHook(
		traceReader,
		config,
		0,
		1,
		transaction.ProtocolCHI,
	)

	b.ResetTimer()

	cycleCount := 0
	for i := 0; i < b.N; i++ {
		txn, err := hook.CreateTransaction(0, uint64(i))
		if err != nil {
			b.Fatal(err)
		}

		if txn != nil {
			for _, msg := range txn.Messages {
				hook.HandleResponse(msg.ID, uint64(i+1))
			}
		}
		cycleCount++
	}

	b.StopTimer()
	stats := hook.GetStats()

	b.ReportMetric(float64(cycleCount)/b.Elapsed().Seconds(), "cycles/sec")
	b.ReportMetric(float64(stats.TotalInstructions)/b.Elapsed().Seconds(), "instrs/sec")
	b.ReportMetric(float64(stats.TotalInstructions)/float64(cycleCount), "IPC")
}
