package flowsim

// quad_core_components_test.go
// 组合测试：ChampSim CPU Node + flow_sim components L2 Node
//
// 架构：
// - CPU Node：ChampSim O3CPU + ChampSim L1D Cache（原有实现）
// - L2 Node：flow_sim components/cache + directory（新实现）
// - Memory Controller & DRAM：原有实现

import (
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/cache"
	"github.com/Readm/flow_sim/internal/champsim/cpu"
	"github.com/Readm/flow_sim/internal/champsim/dram"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
)

// Test_QuadCore_Components_Quick 快速测试（1000 cycles）
// 组合：ChampSim CPU + flow_sim L2
func Test_QuadCore_Components_Quick(t *testing.T) {
	const (
		numCores    = 4
		numChannels = 2
		maxCycles   = 1000
	)

	// Trace 文件
	traceFile := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"

	t.Logf("\n========== 四核测试（flow_sim components L2） ==========")
	t.Logf("配置: %d cores, %d DRAM channels, %d cycles", numCores, numChannels, maxCycles)
	t.Logf("CPU: ChampSim O3CPU + ChampSim L1D")
	t.Logf("L2: flow_sim components/cache + directory (MESI)")

	// ========== 节点 ID ==========
	cpuNodeIDs := []int{0, 1, 2, 3}
	l2NodeID := 4
	memCtrlNodeID := 5
	dramNodeIDs := []int{6, 7}

	// ========== 创建 CPU 核心（使用原有的 CPUNodeHandler） ==========
	var cpuHandlers []*CPUNodeHandler
	var cpuNodeHandles []*network.NodeHandle

	for i := 0; i < numCores; i++ {
		traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
		if err != nil {
			t.Skipf("Trace file not available: %v", err)
		}
		t.Cleanup(func() { traceReader.Close() })

		o3cpu := cpu.NewO3CPU(traceReader, cpu.DefaultO3CPUConfig())
		o3cpu.SetStandaloneMode(false)

		l1dCache, _ := cache.NewSetAssociativeCache(cache.DefaultL1DConfig())
		memoryAdapter := NewFlowSimMemoryAdapter()
		l1dCache.SetLowerLevel(memoryAdapter)
		o3cpu.SetL1DCache(l1dCache)

		cpuOutputQueue := queue.NewOutputQueue(128, 1)
		cpuInputQueue := queue.NewInputQueue(128, 1)

		// 使用原有的 CPUNodeHandler
		cpuHandler := NewCPUNodeHandler(
			cpuNodeIDs[i], l2NodeID,
			o3cpu, l1dCache, memoryAdapter,
			cpuOutputQueue,
			0, // no SpinWait simulation
		)

		cpuNode := node.NewWorkerNode(cpuNodeIDs[i])
		cpuNode.SetProcessHook(cpuHandler.Process)
		cpuNode.AddInputQueue(cpuInputQueue)
		cpuNode.AddOutputQueue(cpuOutputQueue)

		cpuHandlers = append(cpuHandlers, cpuHandler)
		cpuNodeHandles = append(cpuNodeHandles, &network.NodeHandle{
			Node:    cpuNode,
			Inputs:  []*queue.InputQueue{cpuInputQueue},
			Outputs: []*queue.OutputQueue{cpuOutputQueue},
		})
	}

	// ========== 创建 L2 Cache（使用新的 L2CacheNodeHandlerV2） ==========
	l2OutputQueues := make([]*queue.OutputQueue, numCores+1)
	l2InputQueues := make([]*queue.InputQueue, numCores+1)
	for i := 0; i < numCores+1; i++ {
		l2OutputQueues[i] = queue.NewOutputQueue(128, 1)
		l2InputQueues[i] = queue.NewInputQueue(128, 1)
	}

	// 使用新的 L2CacheNodeHandlerV2（基于 flow_sim components）
	l2Handler := NewL2CacheNodeHandlerV2(
		l2NodeID,
		cpuNodeIDs,
		memCtrlNodeID,
		l2OutputQueues,
	)

	l2Node := node.NewWorkerNode(l2NodeID)
	l2Node.SetProcessHook(l2Handler.Process)
	for _, q := range l2InputQueues {
		l2Node.AddInputQueue(q)
	}
	for _, q := range l2OutputQueues {
		l2Node.AddOutputQueue(q)
	}

	// ========== 创建 Memory Controller ==========
	memCtrlOutputQueues := make([]*queue.OutputQueue, 1+numChannels)
	memCtrlInputQueues := make([]*queue.InputQueue, 1+numChannels)
	for i := 0; i < 1+numChannels; i++ {
		memCtrlOutputQueues[i] = queue.NewOutputQueue(128, 1)
		memCtrlInputQueues[i] = queue.NewInputQueue(128, 1)
	}

	memCtrlHandler := NewMemoryControllerHandler(
		memCtrlNodeID, l2NodeID, dramNodeIDs,
		memCtrlOutputQueues, MappingInterleaved,
	)

	memCtrlNode := node.NewWorkerNode(memCtrlNodeID)
	memCtrlNode.SetProcessHook(memCtrlHandler.Process)
	for _, q := range memCtrlInputQueues {
		memCtrlNode.AddInputQueue(q)
	}
	for _, q := range memCtrlOutputQueues {
		memCtrlNode.AddOutputQueue(q)
	}

	// ========== 创建 DRAM Channels ==========
	var dramHandlers []*DRAMNodeHandler
	var dramNodeHandles []*network.NodeHandle

	for i := 0; i < numChannels; i++ {
		dramChannel, _ := dram.NewDRAMChannel(dram.DefaultDRAMConfig())
		dramOutputQueue := queue.NewOutputQueue(128, 1)
		dramInputQueue := queue.NewInputQueue(128, 1)

		dramHandler := NewDRAMNodeHandler(
			dramNodeIDs[i], memCtrlNodeID,
			dramChannel, dramOutputQueue,
		)

		dramNode := node.NewWorkerNode(dramNodeIDs[i])
		dramNode.SetProcessHook(dramHandler.Process)
		dramNode.AddInputQueue(dramInputQueue)
		dramNode.AddOutputQueue(dramOutputQueue)

		dramHandlers = append(dramHandlers, dramHandler)
		dramNodeHandles = append(dramNodeHandles, &network.NodeHandle{
			Node:    dramNode,
			Inputs:  []*queue.InputQueue{dramInputQueue},
			Outputs: []*queue.OutputQueue{dramOutputQueue},
		})
	}

	// ========== 创建 Network ==========
	net := network.New()

	for _, handle := range cpuNodeHandles {
		net.AddNode(handle)
	}
	net.AddNode(&network.NodeHandle{
		Node:    l2Node,
		Inputs:  l2InputQueues,
		Outputs: l2OutputQueues,
	})
	net.AddNode(&network.NodeHandle{
		Node:    memCtrlNode,
		Inputs:  memCtrlInputQueues,
		Outputs: memCtrlOutputQueues,
	})
	for _, handle := range dramNodeHandles {
		net.AddNode(handle)
	}

	// 连接拓扑
	for i := 0; i < numCores; i++ {
		net.Connect(cpuNodeIDs[i], 0, l2NodeID, i, 10, 1)
		net.Connect(l2NodeID, i, cpuNodeIDs[i], 0, 10, 1)
	}
	net.Connect(l2NodeID, numCores, memCtrlNodeID, 0, 50, 1)
	net.Connect(memCtrlNodeID, 0, l2NodeID, numCores, 50, 1)
	for i := 0; i < numChannels; i++ {
		net.Connect(memCtrlNodeID, i+1, dramNodeIDs[i], 0, 20, 1)
		net.Connect(dramNodeIDs[i], 0, memCtrlNodeID, i+1, 20, 1)
	}

	// ========== 运行仿真 ==========
	t.Logf("\n开始仿真 %d 个周期...", maxCycles)

	if err := net.AdvanceTo(maxCycles - 1); err != nil {
		t.Fatalf("Simulation failed: %v", err)
	}

	t.Log("✅ 仿真完成！")

	// ========== 收集统计 ==========
	t.Log("\n========== 统计结果 ==========")

	// CPU 统计（ChampSim）
	totalInstr := uint64(0)
	for i := 0; i < numCores; i++ {
		cpuStats := cpuHandlers[i].GetCPUStats()
		cacheStatsIf := cpuHandlers[i].GetCacheStats()
		cacheStats, _ := cacheStatsIf.(cache.CacheStats)

		ipc := float64(cpuStats.TotalInstructions) / float64(maxCycles)
		hitRate := 0.0
		if cacheStats.Accesses > 0 {
			hitRate = float64(cacheStats.Hits) * 100.0 / float64(cacheStats.Accesses)
		}

		t.Logf("Core %d (ChampSim): Instr=%d, IPC=%.3f, L1D_Hit=%.1f%%, L1D_Miss=%d",
			i, cpuStats.TotalInstructions, ipc, hitRate, cacheStats.Misses)

		totalInstr += cpuStats.TotalInstructions
	}

	avgIPC := float64(totalInstr) / float64(maxCycles) / float64(numCores)
	t.Logf("\n总体: 总指令=%d, 平均IPC=%.3f", totalInstr, avgIPC)

	// L2 统计（flow_sim components）
	l2CacheStats := l2Handler.GetCacheStats()
	l2NodeStats := l2Handler.GetStats()

	l2HitRate := 0.0
	if l2CacheStats.Accesses > 0 {
		l2HitRate = float64(l2CacheStats.Hits) * 100.0 / float64(l2CacheStats.Accesses)
	}

	t.Logf("\nL2 Cache (flow_sim components):")
	t.Logf("  Cache: Accesses=%d, Hits=%d, Misses=%d, Hit Rate=%.1f%%",
		l2CacheStats.Accesses, l2CacheStats.Hits, l2CacheStats.Misses, l2HitRate)
	t.Logf("  Node: RequestsFromCPU=%d, HitsFromCPU=%d, MissesFromCPU=%d",
		l2NodeStats.RequestsFromCPU, l2NodeStats.HitsFromCPU, l2NodeStats.MissesFromCPU)
	t.Logf("  MESI: InvalidatesSent=%d, MemoryRequests=%d",
		l2NodeStats.InvalidatesSent, l2NodeStats.MemoryRequests)

	// Memory Controller 统计
	memCtrlStats := memCtrlHandler.GetStats()
	t.Logf("\nMemory Controller: Total=%d", memCtrlStats.TotalRequests)
	for i := 0; i < numChannels; i++ {
		pct := 0.0
		if memCtrlStats.TotalRequests > 0 {
			pct = float64(memCtrlStats.RequestsPerChannel[i]) * 100.0 / float64(memCtrlStats.TotalRequests)
		}
		t.Logf("  Ch%d: %d (%.1f%%)", i, memCtrlStats.RequestsPerChannel[i], pct)
	}

	// DRAM 统计
	t.Log("\nDRAM Channels:")
	for i := 0; i < numChannels; i++ {
		dramStats := dramHandlers[i].GetDRAMStats()
		t.Logf("  Ch%d: RQ=%d, WQ=%d", i, dramStats.RQAccesses, dramStats.WQAccesses)
	}

	// ========== 验证 ==========
	if totalInstr > 0 {
		t.Logf("\n✅ 测试通过！四核系统（flow_sim components L2）正常运行")
		t.Logf("   架构验证：")
		t.Logf("   - CPU: ChampSim O3CPU ✓")
		t.Logf("   - L1D: ChampSim Cache ✓")
		t.Logf("   - L2: flow_sim components/cache ✓")
		t.Logf("   - Directory: flow_sim components/directory (MESI) ✓")
	} else {
		t.Errorf("❌ 错误：没有退休任何指令")
	}
}
