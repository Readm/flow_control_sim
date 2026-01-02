package champsim

import (
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/cache"
	"github.com/Readm/flow_sim/internal/champsim/cpu"
	"github.com/Readm/flow_sim/internal/champsim/dram"
	"github.com/Readm/flow_sim/internal/champsim/flowsim"
	"github.com/Readm/flow_sim/internal/champsim/memory"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	compcache "github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
)

// Test_Comparison_RealTrace_DirectVsFlowSim 对比直接集成和 flow_sim 集成
// 使用相同的 trace 文件，确保结果可比
func Test_Comparison_RealTrace_DirectVsFlowSim(t *testing.T) {
	traceFile := "../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
	maxCycles := 10000 // 运行 10000 个周期

	t.Logf("\n========== 使用实际 Trace 对比两种集成方案 ==========")
	t.Logf("Trace 文件: %s", traceFile)
	t.Logf("仿真周期: %d", maxCycles)
	t.Logf("")

	// ========== 方案 1: 直接集成 ==========
	t.Log("========== 方案 1: 直接集成 ==========")
	directStats := runDirectIntegration(t, traceFile, maxCycles)

	// ========== 方案 2: Flow_sim 集成 ==========
	t.Log("\n========== 方案 2: Flow_sim 集成 ==========")
	flowsimStats := runFlowSimIntegration(t, traceFile, maxCycles)

	// ========== 结果对比 ==========
	t.Log("\n========== 结果对比 ==========")
	t.Logf("| 指标 | 直接集成 | Flow_sim 集成 | 差异 |")
	t.Logf("|------|---------|--------------|------|")

	// IPC
	ipcDiff := (flowsimStats.IPC - directStats.IPC) / directStats.IPC * 100
	t.Logf("| IPC | %.3f | %.3f | %+.2f%% |", directStats.IPC, flowsimStats.IPC, ipcDiff)

	// 退休指令数
	instrDiff := (float64(flowsimStats.RetiredInstrs) - float64(directStats.RetiredInstrs)) / float64(directStats.RetiredInstrs) * 100
	t.Logf("| 退休指令 | %d | %d | %+.2f%% |", directStats.RetiredInstrs, flowsimStats.RetiredInstrs, instrDiff)

	// Cache 命中率
	cacheHitDiff := flowsimStats.CacheHitRate - directStats.CacheHitRate
	t.Logf("| Cache 命中率 | %.2f%% | %.2f%% | %+.2f%% |",
		directStats.CacheHitRate, flowsimStats.CacheHitRate, cacheHitDiff)

	// DRAM 请求数
	dramDiff := (float64(flowsimStats.DRAMRequests) - float64(directStats.DRAMRequests)) / float64(directStats.DRAMRequests) * 100
	t.Logf("| DRAM 请求 | %d | %d | %+.2f%% |", directStats.DRAMRequests, flowsimStats.DRAMRequests, dramDiff)

	t.Log("\n========== 分析 ==========")
	if absFloat(ipcDiff) < 1.0 {
		t.Logf("✅ IPC 差异极小 (%.2f%%)，两种方案性能一致", ipcDiff)
	} else if absFloat(ipcDiff) < 5.0 {
		t.Logf("⚠️  IPC 有轻微差异 (%.2f%%)，可能是框架开销", ipcDiff)
	} else {
		t.Logf("❌ IPC 差异较大 (%.2f%%)，需要进一步调查", ipcDiff)
	}

	if absFloat(instrDiff) < 1.0 {
		t.Logf("✅ 退休指令数一致，功能正确性验证通过")
	} else {
		t.Logf("⚠️  退休指令数有差异 (%.2f%%)，可能存在行为不一致", instrDiff)
	}
}

type SimulationStats struct {
	RetiredInstrs uint64
	IPC           float64
	CacheHits     uint64
	CacheMisses   uint64
	CacheHitRate  float64
	DRAMRequests  uint64
}

// runDirectIntegration 运行直接集成方案
func runDirectIntegration(t *testing.T, traceFile string, maxCycles int) SimulationStats {
	// 创建 Trace Reader
	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Failed to open trace: %v", err)
	}
	defer traceReader.Close()

	// 创建 DRAM
	dramChannel, err := dram.NewDRAMChannel(dram.DefaultDRAMConfig())
	if err != nil {
		t.Fatalf("Failed to create DRAM: %v", err)
	}

	// 创建 DRAMAdapter
	dramAdapter := memory.NewDRAMAdapter(dramChannel)

	// 创建 L1D Cache
	cacheConfig := compcache.DefaultL1DConfig()
	l1dCache, err := cache.NewSetAssociativeCache(cacheConfig)
	if err != nil {
		t.Fatalf("Failed to create L1D cache: %v", err)
	}
	l1dCache.SetLowerLevel(dramAdapter)

	// 创建 CPU
	cpuConfig := cpu.DefaultO3CPUConfig()
	o3cpu := cpu.NewO3CPU(traceReader, cpuConfig)
	o3cpu.SetStandaloneMode(false)
	o3cpu.SetL1DCache(l1dCache)

	// 运行仿真
	for cycle := 0; cycle < maxCycles; cycle++ {
		o3cpu.Tick()
		dramAdapter.Tick()

		if cycle%1000 == 999 {
			stats := o3cpu.GetStats()
			ipc := float64(stats.TotalInstructions) / float64(cycle+1)
			t.Logf("  Cycle %d: Retired=%d, IPC=%.3f", cycle+1, stats.TotalInstructions, ipc)
		}
	}

	// 收集统计
	cpuStats := o3cpu.GetStats()
	cacheStatsInterface := l1dCache.GetStats()
	cacheStats, _ := cacheStatsInterface.(cache.CacheStats)
	dramStats := dramChannel.GetStats()

	hitRate := 0.0
	if cacheStats.Accesses > 0 {
		hitRate = float64(cacheStats.Hits) * 100.0 / float64(cacheStats.Accesses)
	}

	return SimulationStats{
		RetiredInstrs: cpuStats.TotalInstructions,
		IPC:           float64(cpuStats.TotalInstructions) / float64(maxCycles),
		CacheHits:     cacheStats.Hits,
		CacheMisses:   cacheStats.Misses,
		CacheHitRate:  hitRate,
		DRAMRequests:  dramStats.RQAccesses,
	}
}

// runFlowSimIntegration 运行 flow_sim 集成方案
func runFlowSimIntegration(t *testing.T, traceFile string, maxCycles int) SimulationStats {
	// 创建 Trace Reader
	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Failed to open trace: %v", err)
	}
	defer traceReader.Close()

	// Node IDs
	cpuNodeID := 0
	dramNodeID := 1

	// 创建队列
	cpuOutputQueue := queue.NewOutputQueue(128, 1)
	cpuInputQueue := queue.NewInputQueue(128, 1)
	dramOutputQueue := queue.NewOutputQueue(128, 1)
	dramInputQueue := queue.NewInputQueue(128, 1)

	// 创建 DRAM Channel
	dramChannel, err := dram.NewDRAMChannel(dram.DefaultDRAMConfig())
	if err != nil {
		t.Fatalf("Failed to create DRAM: %v", err)
	}

	// 创建 CPU
	o3cpu := cpu.NewO3CPU(traceReader, cpu.DefaultO3CPUConfig())
	o3cpu.SetStandaloneMode(false)

	// 创建 L1D Cache
	l1dCache, err := cache.NewSetAssociativeCache(compcache.DefaultL1DConfig())
	if err != nil {
		t.Fatalf("Failed to create L1D cache: %v", err)
	}

	// 创建 Memory Adapter
	memoryAdapter := flowsim.NewFlowSimMemoryAdapter()

	// 连接
	l1dCache.SetLowerLevel(memoryAdapter)
	o3cpu.SetL1DCache(l1dCache)

	// 创建 CPU Handler
	cpuHandler := flowsim.NewCPUNodeHandler(
		cpuNodeID,
		dramNodeID,
		o3cpu,
		l1dCache,
		memoryAdapter,
		cpuOutputQueue,
	)

	// 创建 DRAM Handler
	dramHandler := flowsim.NewDRAMNodeHandler(
		dramNodeID,
		cpuNodeID,
		dramChannel,
		dramOutputQueue,
	)

	// 创建 Nodes
	cpuNode := node.NewWorkerNode(cpuNodeID)
	cpuNode.SetProcessHook(cpuHandler.Process)
	if err := cpuNode.AddInputQueue(cpuInputQueue); err != nil {
		t.Fatalf("Failed to add CPU input queue: %v", err)
	}
	if err := cpuNode.AddOutputQueue(cpuOutputQueue); err != nil {
		t.Fatalf("Failed to add CPU output queue: %v", err)
	}

	dramNode := node.NewWorkerNode(dramNodeID)
	dramNode.SetProcessHook(dramHandler.Process)
	if err := dramNode.AddInputQueue(dramInputQueue); err != nil {
		t.Fatalf("Failed to add DRAM input queue: %v", err)
	}
	if err := dramNode.AddOutputQueue(dramOutputQueue); err != nil {
		t.Fatalf("Failed to add DRAM output queue: %v", err)
	}

	// 创建 Network
	net := network.New()
	net.AddNode(&network.NodeHandle{
		Node:    cpuNode,
		Inputs:  []*queue.InputQueue{cpuInputQueue},
		Outputs: []*queue.OutputQueue{cpuOutputQueue},
	})
	net.AddNode(&network.NodeHandle{
		Node:    dramNode,
		Inputs:  []*queue.InputQueue{dramInputQueue},
		Outputs: []*queue.OutputQueue{dramOutputQueue},
	})

	// 连接节点
	net.ConnectNodes(cpuNode, 0, dramNode, 0, 1, 1) // CPU -> DRAM
	net.ConnectNodes(dramNode, 0, cpuNode, 0, 1, 1) // DRAM -> CPU

	// 运行仿真
	if err := net.AdvanceTo(maxCycles - 1); err != nil {
		t.Fatalf("Failed to advance network: %v", err)
	}

	// 收集统计
	cpuStats := cpuHandler.GetCPUStats()
	cacheStatsInterface := cpuHandler.GetCacheStats()
	cacheStats := cacheStatsInterface.(cache.CacheStats)
	dramStats := dramChannel.GetStats()

	hitRate := 0.0
	if cacheStats.Accesses > 0 {
		hitRate = float64(cacheStats.Hits) * 100.0 / float64(cacheStats.Accesses)
	}

	t.Logf("  Retired=%d, IPC=%.3f", cpuStats.TotalInstructions,
		float64(cpuStats.TotalInstructions)/float64(maxCycles))

	return SimulationStats{
		RetiredInstrs: cpuStats.TotalInstructions,
		IPC:           float64(cpuStats.TotalInstructions) / float64(maxCycles),
		CacheHits:     cacheStats.Hits,
		CacheMisses:   cacheStats.Misses,
		CacheHitRate:  hitRate,
		DRAMRequests:  dramStats.RQAccesses,
	}
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
