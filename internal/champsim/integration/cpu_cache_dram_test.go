package integration

import (
	"os"
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/cache"
	"github.com/Readm/flow_sim/internal/champsim/cpu"
	"github.com/Readm/flow_sim/internal/champsim/dram"
	"github.com/Readm/flow_sim/internal/champsim/memory"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	compcache "github.com/Readm/flow_sim/internal/components/cache"
)

// Test_CPU_Cache_DRAM_Integration 端到端集成测试
//
// 测试完整的 CPU → L1D Cache → DRAM 流程
func Test_CPU_Cache_DRAM_Integration(t *testing.T) {
	// Use environment variable if provided, otherwise fallback to repo's small trace
	traceFile := os.Getenv("CHAMPSIM_TRACE_PERLBENCH")
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
		t.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE_PERLBENCH=%s)", err, traceFile)
	}
	defer traceReader.Close()

	// ==================== 创建组件 ====================

	// 1. 创建DRAM
	dramConfig := dram.DefaultDRAMConfig()
	dramChannel, err := dram.NewDRAMChannel(dramConfig)
	if err != nil {
		t.Fatalf("Failed to create DRAM: %v", err)
	}

	// 2. 创建DRAM适配器
	dramAdapter := memory.NewDRAMAdapter(dramChannel)

	// 3. 创建L1D Cache
	cacheConfig := compcache.DefaultL1DConfig()
	l1dCache, err := cache.NewSetAssociativeCache(cacheConfig)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// 4. 连接Cache到DRAM
	l1dCache.SetLowerLevel(dramAdapter)

	// 5. 创建CPU
	cpuConfig := cpu.DefaultO3CPUConfig()
	o3cpu := cpu.NewO3CPU(traceReader, cpuConfig)
	o3cpu.SetStandaloneMode(false) // 关闭standalone模式

	// 6. 连接CPU到Cache
	o3cpu.SetL1DCache(l1dCache)

	t.Logf("\n========== CPU+Cache+DRAM 集成测试 ==========")
	t.Logf("配置:")
	t.Logf("  CPU: O3 (ROB=%d, LQ=%d, SQ=%d)",
		cpuConfig.ROBSize, cpuConfig.LQSize, cpuConfig.SQSize)
	t.Logf("  L1D: %d sets × %d ways = %d KB",
		cacheConfig.NumSets, cacheConfig.NumWays,
		cacheConfig.NumSets*cacheConfig.NumWays*cacheConfig.BlockSize/1024)
	t.Logf("  DRAM: %d Ranks × %d BankGroups × %d Banks",
		dramConfig.Ranks, dramConfig.BankGroups, dramConfig.Banks)
	t.Logf("")

	// ==================== 运行仿真 ====================

	const maxCycles = 1000

	for cycle := uint64(0); cycle < maxCycles; cycle++ {
		// 推进CPU时钟
		o3cpu.Tick()

		// 推进Cache时钟（Cache内部不主动tick，由CPU驱动）
		// Cache的currentCycle由Access()调用时更新

		// 推进DRAM时钟
		dramAdapter.Tick()

		// 每100周期输出一次进度
		if (cycle+1)%100 == 0 {
			cpuStats := o3cpu.GetStats()
			cacheStatsInterface := l1dCache.GetStats()
			cacheStats, _ := cacheStatsInterface.(cache.CacheStats)
			dramStats := dramChannel.GetStats()

			t.Logf("Cycle %d: Instructions=%d, IPC=%.2f, CacheHit=%.1f%%, DRAMReq=%d",
				cycle+1,
				cpuStats.TotalInstructions,
				float64(cpuStats.TotalInstructions)/float64(cycle+1),
				cacheStats.HitRate()*100,
				dramStats.RQAccesses,
			)
		}
	}

	// ==================== 验证结果 ====================

	cpuStats := o3cpu.GetStats()
	cacheStatsInterface := l1dCache.GetStats()
	cacheStats, ok := cacheStatsInterface.(cache.CacheStats)
	if !ok {
		t.Fatal("Failed to get cache stats")
	}
	dramStats := dramChannel.GetStats()

	t.Logf("\n========== 最终统计 ==========")
	t.Logf("CPU:")
	t.Logf("  总周期: %d", maxCycles)
	t.Logf("  退休指令: %d", cpuStats.TotalInstructions)
	t.Logf("  IPC: %.2f", float64(cpuStats.TotalInstructions)/float64(maxCycles))

	t.Logf("\nL1D Cache:")
	t.Logf("  总访问: %d", cacheStats.Accesses)
	t.Logf("  命中: %d (%.2f%%)", cacheStats.Hits, cacheStats.HitRate()*100)
	t.Logf("  未命中: %d (%.2f%%)", cacheStats.Misses, cacheStats.MissRate()*100)
	t.Logf("  Loads: %d (Hits=%d, Misses=%d)",
		cacheStats.Loads, cacheStats.LoadHits, cacheStats.LoadMisses)
	t.Logf("  Stores: %d (Hits=%d, Misses=%d)",
		cacheStats.Stores, cacheStats.StoreHits, cacheStats.StoreMisses)
	t.Logf("  Writebacks: %d", cacheStats.Writebacks)
	t.Logf("  MSHR Full: %d", cacheStats.MSHRFull)

	mshrSize, mshrCapacity := l1dCache.GetMSHRStats()
	t.Logf("  MSHR: %d/%d", mshrSize, mshrCapacity)

	t.Logf("\nDRAM:")
	t.Logf("  RQ访问: %d", dramStats.RQAccesses)
	t.Logf("  WQ访问: %d", dramStats.WQAccesses)
	t.Logf("  Row Buffer Hits: %d (%.2f%%)",
		dramStats.RQRowBufferHit, dramStats.RowBufferHitRate()*100)
	t.Logf("  Row Buffer Misses: %d", dramStats.RQRowBufferMiss)
	t.Logf("  RQ Full: %d", dramStats.RQFull)
	t.Logf("  WQ Full: %d", dramStats.WQFull)

	t.Logf("\n========== 验证 ==========")

	// 验证CPU有指令退休
	if cpuStats.TotalInstructions == 0 {
		t.Error("❌ 没有指令退休")
	} else {
		t.Logf("✅ CPU运行正常: %d instructions", cpuStats.TotalInstructions)
	}

	// 验证Cache有访问
	if cacheStats.Accesses == 0 {
		t.Error("❌ Cache没有访问")
	} else {
		t.Logf("✅ Cache运行正常: %d accesses", cacheStats.Accesses)
	}

	// 验证DRAM有访问
	if dramStats.RQAccesses == 0 {
		t.Log("⚠️  DRAM没有访问（可能Cache命中率100%）")
	} else {
		t.Logf("✅ DRAM运行正常: %d requests", dramStats.RQAccesses)
	}

	// 验证数据一致性: Cache Miss应该导致DRAM请求
	// 注意：由于DRAM延迟，某些miss可能还在队列中
	if cacheStats.Misses > 0 && dramStats.RQAccesses == 0 {
		t.Error("❌ 数据不一致: Cache有miss但DRAM无请求")
	} else if cacheStats.Misses > 0 {
		t.Logf("✅ Cache与DRAM联动正常: %d misses → %d DRAM requests",
			cacheStats.Misses, dramStats.RQAccesses)
	}

	// 验证IPC在合理范围
	ipc := float64(cpuStats.TotalInstructions) / float64(maxCycles)
	if ipc < 0.1 || ipc > 4.0 {
		t.Errorf("❌ IPC异常: %.2f (期望 0.1-4.0)", ipc)
	} else {
		t.Logf("✅ IPC正常: %.2f", ipc)
	}

	t.Logf("\n🎉 CPU+Cache+DRAM 端到端集成测试完成！")
}

// Test_CPU_Cache_DRAM_Performance 性能对比测试
//
// 对比有DRAM和standalone模式的性能差异
func Test_CPU_Cache_DRAM_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	const maxCycles = 10000
	traceFile := os.Getenv("CHAMPSIM_TRACE_PERLBENCH")
	if traceFile == "" {
		largeTrace := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
		if _, err := os.Stat(largeTrace); err == nil {
			traceFile = largeTrace
		} else {
			traceFile = "../../../testdata/traces/small.champsimtrace"
		}
	}

	// ========== 测试1: Standalone模式（baseline） ==========
	t.Log("\n========== Baseline: Standalone模式 ==========")

	traceReader1, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE_PERLBENCH=%s)", err, traceFile)
	}
	defer traceReader1.Close()

	cpu1 := cpu.NewO3CPU(traceReader1, cpu.DefaultO3CPUConfig())
	cpu1.SetStandaloneMode(true)

	cache1, _ := cache.NewSetAssociativeCache(compcache.DefaultL1DConfig())
	cpu1.SetL1DCache(cache1)

	for cycle := uint64(0); cycle < maxCycles; cycle++ {
		cpu1.Tick()
	}

	stats1 := cpu1.GetStats()
	ipc1 := float64(stats1.TotalInstructions) / float64(maxCycles)

	t.Logf("Standalone: Instructions=%d, IPC=%.2f",
		stats1.TotalInstructions, ipc1)

	// ========== 测试2: 完整DRAM模式 ==========
	t.Log("\n========== Complete: CPU+Cache+DRAM ==========")

	traceReader2, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE_PERLBENCH=%s)", err, traceFile)
	}
	defer traceReader2.Close()

	dramChannel2, _ := dram.NewDRAMChannel(dram.DefaultDRAMConfig())
	dramAdapter2 := memory.NewDRAMAdapter(dramChannel2)

	cache2, _ := cache.NewSetAssociativeCache(compcache.DefaultL1DConfig())
	cache2.SetLowerLevel(dramAdapter2)

	cpu2 := cpu.NewO3CPU(traceReader2, cpu.DefaultO3CPUConfig())
	cpu2.SetStandaloneMode(false)
	cpu2.SetL1DCache(cache2)

	for cycle := uint64(0); cycle < maxCycles; cycle++ {
		cpu2.Tick()
		dramAdapter2.Tick()
	}

	stats2 := cpu2.GetStats()
	ipc2 := float64(stats2.TotalInstructions) / float64(maxCycles)

	cacheStats2 := cache2.GetStats().(cache.CacheStats)
	dramStats2 := dramChannel2.GetStats()

	t.Logf("Complete: Instructions=%d, IPC=%.2f, CacheHit=%.1f%%, DRAMReq=%d",
		stats2.TotalInstructions, ipc2,
		cacheStats2.HitRate()*100, dramStats2.RQAccesses)

	// ========== 性能对比 ==========
	t.Log("\n========== 性能对比 ==========")
	t.Logf("IPC变化: %.2f → %.2f (%.1f%%)",
		ipc1, ipc2, (ipc2-ipc1)/ipc1*100)

	// 由于DRAM延迟，完整模式的IPC可能稍低
	// 但不应该差异太大（Cache应该能吸收大部分延迟）
	if ipc2 < ipc1*0.5 {
		t.Logf("⚠️  IPC下降较多，可能需要优化")
	} else {
		t.Logf("✅ 性能表现正常")
	}
}
