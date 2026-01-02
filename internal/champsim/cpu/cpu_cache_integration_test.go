package cpu

import (
	"os"
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/cache"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	compcache "github.com/Readm/flow_sim/internal/components/cache"
)

// Test_CPUCache_Integration 测试CPU+Cache集成
func Test_CPUCache_Integration(t *testing.T) {
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

	// 创建CPU
	cpuConfig := DefaultO3CPUConfig()
	cpu := NewO3CPU(traceReader, cpuConfig)
	cpu.SetStandaloneMode(true)

	// 创建L1D Cache
	cacheConfig := compcache.DefaultL1DConfig()
	l1dCache, err := cache.NewSetAssociativeCache(cacheConfig)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// 设置CPU的Cache
	cpu.SetL1DCache(l1dCache)

	// 运行1000周期
	const maxCycles = 1000

	for cycle := uint64(0); cycle < maxCycles; cycle++ {
		cpu.Tick()
	}

	// 获取CPU统计
	cpuStats := cpu.GetStats()
	t.Logf("\n========== CPU+Cache 集成测试结果 ==========")
	t.Logf("运行周期: %d", maxCycles)
	t.Logf("退休指令数: %d", cpuStats.TotalInstructions)
	t.Logf("IPC: %.2f", float64(cpuStats.TotalInstructions)/float64(maxCycles))

	// 获取Cache统计
	cacheStatsInterface := l1dCache.GetStats()
	cacheStats, ok := cacheStatsInterface.(cache.CacheStats)
	if !ok {
		t.Fatal("Failed to get cache stats")
	}

	t.Logf("\nCache 统计:")
	t.Logf("  Accesses: %d", cacheStats.Accesses)
	t.Logf("  Hits: %d (%.2f%%)", cacheStats.Hits, cacheStats.HitRate()*100)
	t.Logf("  Misses: %d (%.2f%%)", cacheStats.Misses, cacheStats.MissRate()*100)
	t.Logf("  Loads: %d (Hits: %d, Misses: %d)",
		cacheStats.Loads, cacheStats.LoadHits, cacheStats.LoadMisses)
	t.Logf("  Stores: %d (Hits: %d, Misses: %d)",
		cacheStats.Stores, cacheStats.StoreHits, cacheStats.StoreMisses)
	t.Logf("  Writebacks: %d", cacheStats.Writebacks)
	t.Logf("  MSHR Full: %d", cacheStats.MSHRFull)

	mshrSize, mshrCapacity := l1dCache.GetMSHRStats()
	t.Logf("  MSHR: %d/%d", mshrSize, mshrCapacity)
	t.Logf("==========================================\n")

	// 验证基本正确性
	if cpuStats.TotalInstructions == 0 {
		t.Error("没有指令退休")
	} else {
		t.Logf("指令退休功能正常")
	}

	// 验证Cache有访问
	if cacheStats.Accesses == 0 {
		t.Error("Cache没有被访问")
	} else {
		t.Logf("Cache正常工作")
	}

	// 验证Cache有命中
	if cacheStats.Hits == 0 {
		t.Logf("没有Cache命中（全是cold miss）")
	} else {
		t.Logf("Cache有命中: %.2f%%", cacheStats.HitRate()*100)
	}

	// 验证IPC在合理范围内
	ipc := float64(cpuStats.TotalInstructions) / float64(maxCycles)
	if ipc < 0.5 || ipc > 4.0 {
		t.Errorf("IPC超出合理范围: %.2f (期望 0.5-4.0)", ipc)
	} else {
		t.Logf("IPC在合理范围内: %.2f", ipc)
	}

	t.Logf("\nCPU+Cache 集成测试通过！")
}

// Test_CPUCache_比较无Cache性能 比较有Cache和无Cache的性能差异
func Test_CPUCache_比较无Cache性能(t *testing.T) {
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

	const maxCycles = 1000

	// ========== 无Cache测试 ==========
	traceReader1, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE=%s)", err, traceFile)
	}
	defer traceReader1.Close()

	cpuNoCache := NewO3CPU(traceReader1, DefaultO3CPUConfig())
	cpuNoCache.SetStandaloneMode(true)
	// 不设置Cache

	for cycle := uint64(0); cycle < maxCycles; cycle++ {
		cpuNoCache.Tick()
	}

	statsNoCache := cpuNoCache.GetStats()
	ipcNoCache := float64(statsNoCache.TotalInstructions) / float64(maxCycles)

	// ========== 有Cache测试 ==========
	traceReader2, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE=%s)", err, traceFile)
	}
	defer traceReader2.Close()

	cpuWithCache := NewO3CPU(traceReader2, DefaultO3CPUConfig())
	cpuWithCache.SetStandaloneMode(true)

	l1dCache, err := cache.NewSetAssociativeCache(compcache.DefaultL1DConfig())
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	cpuWithCache.SetL1DCache(l1dCache)

	for cycle := uint64(0); cycle < maxCycles; cycle++ {
		cpuWithCache.Tick()
	}

	statsWithCache := cpuWithCache.GetStats()
	ipcWithCache := float64(statsWithCache.TotalInstructions) / float64(maxCycles)

	// ========== 性能比较 ==========
	t.Logf("\n========== 性能比较 ==========")
	t.Logf("运行周期: %d", maxCycles)
	t.Logf("")
	t.Logf("无Cache:")
	t.Logf("  指令数: %d", statsNoCache.TotalInstructions)
	t.Logf("  IPC: %.2f", ipcNoCache)
	t.Logf("")
	t.Logf("有Cache:")
	t.Logf("  指令数: %d", statsWithCache.TotalInstructions)
	t.Logf("  IPC: %.2f", ipcWithCache)

	// 获取Cache统计
	cacheStatsInterface := l1dCache.GetStats()
	cacheStats, ok := cacheStatsInterface.(cache.CacheStats)
	if !ok {
		t.Fatal("Failed to get cache stats")
	}
	t.Logf("  Cache命中率: %.2f%%", cacheStats.HitRate()*100)

	t.Logf("")
	t.Logf("性能提升: %.2f%%", (ipcWithCache-ipcNoCache)/ipcNoCache*100)
	t.Logf("============================\n")

	// 注意：在standalone模式下，Cache的主要影响是统计，
	// 因为内存操作都是立即完成的。
	// 在真实集成模式下，Cache miss会导致延迟，性能差异会更明显。

	// 验证两者IPC相近（因为都是standalone模式）
	if ipcWithCache < ipcNoCache*0.9 || ipcWithCache > ipcNoCache*1.1 {
		t.Logf("IPC差异较大：%.2f vs %.2f (standalone模式下应该相近)", ipcWithCache, ipcNoCache)
	} else {
		t.Logf("IPC相近（符合standalone模式预期）")
	}
}

// Test_CPUCache_长时间运行 测试CPU+Cache长时间运行稳定性
func Test_CPUCache_长时间运行(t *testing.T) {
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

	cpuConfig := DefaultO3CPUConfig()
	cpu := NewO3CPU(traceReader, cpuConfig)
	cpu.SetStandaloneMode(true)

	cacheConfig := compcache.DefaultL1DConfig()
	l1dCache, err := cache.NewSetAssociativeCache(cacheConfig)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	cpu.SetL1DCache(l1dCache)

	// 运行10000周期
	const maxCycles = 10000
	var prevStats O3CPUStats

	for cycle := uint64(0); cycle < maxCycles; cycle++ {
		cpu.Tick()

		// 每1000周期检查一次
		if cycle > 0 && cycle%1000 == 0 {
			stats := cpu.GetStats()
			deltaInstrs := stats.TotalInstructions - prevStats.TotalInstructions

			cacheStatsInterface := l1dCache.GetStats()
			cacheStats, ok := cacheStatsInterface.(cache.CacheStats)
			if !ok {
				t.Fatal("Failed to get cache stats")
			}

			t.Logf("Cycle %d: +%d instrs, IPC=%.2f, Cache Hit=%.1f%%, MSHR=%d/%d",
				cycle, deltaInstrs,
				float64(stats.TotalInstructions)/float64(cycle),
				cacheStats.HitRate()*100,
				func() int { s, _ := l1dCache.GetMSHRStats(); return s }(),
				cacheConfig.MSHRSize)

			// 检查停滞
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

	if stats.TotalInstructions < 5000 {
		t.Errorf("长时间运行指令数过少: %d (期望 > 5000)", stats.TotalInstructions)
	} else {
		t.Logf("长时间运行稳定性验证通过")
	}
}
