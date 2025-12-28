package flowsim

// quad_core_test.go 四核 + MESI + 双通道 DRAM 测试

import (
	"fmt"
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/cache"
	"github.com/Readm/flow_sim/internal/champsim/cpu"
	"github.com/Readm/flow_sim/internal/champsim/dram"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
)

// Test_QuadCore_MESI_DualChannel 四核 + MESI协议 + 双通道DRAM 测试
func Test_QuadCore_MESI_DualChannel(t *testing.T) {
	const (
		numCores    = 4
		numChannels = 2
		maxCycles   = 10000
	)

	// Trace 文件（每个核心一个）
	traceFiles := []string{
		"../../../testdata/traces/400.perlbench-41B.champsimtrace.xz",
		"../../../testdata/traces/400.perlbench-41B.champsimtrace.xz", // 复用同一个 trace
		"../../../testdata/traces/429.mcf-22B.champsimtrace.xz",
		"../../../testdata/traces/429.mcf-22B.champsimtrace.xz",       // 复用
	}

	t.Logf("\n========== 四核 + MESI + 双通道DRAM 测试 ==========")
	t.Logf("配置:")
	t.Logf("  CPU 核心数: %d", numCores)
	t.Logf("  DRAM 通道数: %d", numChannels)
	t.Logf("  仿真周期: %d", maxCycles)
	t.Logf("")

	// ========== 节点 ID 分配 ==========
	cpuNodeIDs := []int{0, 1, 2, 3}
	l2NodeID := 4
	memCtrlNodeID := 5
	dramNodeIDs := []int{6, 7}

	// ========== 创建 CPU 核心 + L1 Cache ==========
	t.Log("创建 CPU 核心和 L1 Caches...")
	var cpuHandlers []*CPUNodeHandler
	var cpuNodes []node.Node
	var cpuNodeHandles []*network.NodeHandle

	for i := 0; i < numCores; i++ {
		// 打开 trace 文件
		traceReader, err := trace.NewTraceReader(traceFiles[i], 0, trace.FormatStandard)
		if err != nil {
			t.Skipf("Skipping test, trace file %s not available: %v", traceFiles[i], err)
		}
		// 使用 t.Cleanup 确保测试结束时关闭
		t.Cleanup(func() { traceReader.Close() })

		// 创建 CPU
		o3cpu := cpu.NewO3CPU(traceReader, cpu.DefaultO3CPUConfig())
		o3cpu.SetStandaloneMode(false)

		// 创建 L1D Cache
		l1dCache, err := cache.NewSetAssociativeCache(cache.DefaultL1DConfig())
		if err != nil {
			t.Fatalf("Failed to create L1D cache for core %d: %v", i, err)
		}

		// 创建 Memory Adapter
		memoryAdapter := NewFlowSimMemoryAdapter()
		l1dCache.SetLowerLevel(memoryAdapter)
		o3cpu.SetL1DCache(l1dCache)

		// 创建输出队列（发送到 L2）
		cpuOutputQueue := queue.NewOutputQueue(128, 1)
		cpuInputQueue := queue.NewInputQueue(128, 1)

		// 创建 CPU Handler
		cpuHandler := NewCPUNodeHandler(
			cpuNodeIDs[i],
			l2NodeID, // 所有 CPU 都连接到 L2
			o3cpu,
			l1dCache,
			memoryAdapter,
			cpuOutputQueue,
			0, // no SpinWait simulation
		)

		// 创建 Worker Node
		cpuNode := node.NewWorkerNode(cpuNodeIDs[i])
		cpuNode.SetProcessHook(cpuHandler.Process)
		if err := cpuNode.AddInputQueue(cpuInputQueue); err != nil {
			t.Fatalf("Failed to add input queue for core %d: %v", i, err)
		}
		if err := cpuNode.AddOutputQueue(cpuOutputQueue); err != nil {
			t.Fatalf("Failed to add output queue for core %d: %v", i, err)
		}

		cpuHandlers = append(cpuHandlers, cpuHandler)
		cpuNodes = append(cpuNodes, cpuNode)
		cpuNodeHandles = append(cpuNodeHandles, &network.NodeHandle{
			Node:    cpuNode,
			Inputs:  []*queue.InputQueue{cpuInputQueue},
			Outputs: []*queue.OutputQueue{cpuOutputQueue},
		})

		t.Logf("  Core %d: OK", i)
	}

	// ========== 创建 L2 Cache (共享，带 MESI) ==========
	t.Log("\n创建共享 L2 Cache (MESI)...")

	// L2 Cache 配置
	l2Config := cache.CacheConfig{
		Name:         "L2",
		NumSets:      512,
		NumWays:      16,
		BlockSize:    64,
		MSHRSize:     32,
		HitLatency:   20,
		FillLatency:  10,
	}
	l2Cache, err := cache.NewSetAssociativeCache(l2Config)
	if err != nil {
		t.Fatalf("Failed to create L2 cache: %v", err)
	}

	// L2 的输出队列
	// 4 个发送到 CPU，1 个发送到 Memory Controller
	l2OutputQueues := make([]*queue.OutputQueue, numCores+1)
	for i := 0; i < numCores+1; i++ {
		l2OutputQueues[i] = queue.NewOutputQueue(128, 1)
	}

	// L2 的输入队列
	// 4 个来自 CPU，1 个来自 Memory Controller
	l2InputQueues := make([]*queue.InputQueue, numCores+1)
	for i := 0; i < numCores+1; i++ {
		l2InputQueues[i] = queue.NewInputQueue(128, 1)
	}

	// 创建 L2 Handler
	l2Handler := NewL2CacheNodeHandler(
		l2NodeID,
		cpuNodeIDs,
		memCtrlNodeID,
		l2Cache,
		l2OutputQueues,
	)

	// 创建 L2 Node
	l2Node := node.NewWorkerNode(l2NodeID)
	l2Node.SetProcessHook(l2Handler.Process)
	for _, q := range l2InputQueues {
		if err := l2Node.AddInputQueue(q); err != nil {
			t.Fatalf("Failed to add L2 input queue: %v", err)
		}
	}
	for _, q := range l2OutputQueues {
		if err := l2Node.AddOutputQueue(q); err != nil {
			t.Fatalf("Failed to add L2 output queue: %v", err)
		}
	}

	t.Log("  L2 Cache: OK")

	// ========== 创建 Memory Controller ==========
	t.Log("\n创建 Memory Controller...")

	// Memory Controller 的输出队列
	// 1 个发送到 L2，2 个发送到 DRAM Channels
	memCtrlOutputQueues := make([]*queue.OutputQueue, 1+numChannels)
	for i := 0; i < 1+numChannels; i++ {
		memCtrlOutputQueues[i] = queue.NewOutputQueue(128, 1)
	}

	// Memory Controller 的输入队列
	// 1 个来自 L2，2 个来自 DRAM Channels
	memCtrlInputQueues := make([]*queue.InputQueue, 1+numChannels)
	for i := 0; i < 1+numChannels; i++ {
		memCtrlInputQueues[i] = queue.NewInputQueue(128, 1)
	}

	// 创建 Memory Controller Handler
	memCtrlHandler := NewMemoryControllerHandler(
		memCtrlNodeID,
		l2NodeID,
		dramNodeIDs,
		memCtrlOutputQueues,
		MappingInterleaved,
	)

	// 创建 Memory Controller Node
	memCtrlNode := node.NewWorkerNode(memCtrlNodeID)
	memCtrlNode.SetProcessHook(memCtrlHandler.Process)
	for _, q := range memCtrlInputQueues {
		if err := memCtrlNode.AddInputQueue(q); err != nil {
			t.Fatalf("Failed to add MemCtrl input queue: %v", err)
		}
	}
	for _, q := range memCtrlOutputQueues {
		if err := memCtrlNode.AddOutputQueue(q); err != nil {
			t.Fatalf("Failed to add MemCtrl output queue: %v", err)
		}
	}

	t.Log("  Memory Controller: OK")

	// ========== 创建 DRAM Channels ==========
	t.Log("\n创建 DRAM Channels...")
	var dramHandlers []*DRAMNodeHandler
	var dramNodeHandles []*network.NodeHandle

	for i := 0; i < numChannels; i++ {
		// 创建 DRAM Channel
		dramChannel, err := dram.NewDRAMChannel(dram.DefaultDRAMConfig())
		if err != nil {
			t.Fatalf("Failed to create DRAM channel %d: %v", i, err)
		}

		// 创建队列
		dramOutputQueue := queue.NewOutputQueue(128, 1)
		dramInputQueue := queue.NewInputQueue(128, 1)

		// 创建 DRAM Handler
		dramHandler := NewDRAMNodeHandler(
			dramNodeIDs[i],
			memCtrlNodeID,
			dramChannel,
			dramOutputQueue,
		)

		// 创建 DRAM Node
		dramNode := node.NewWorkerNode(dramNodeIDs[i])
		dramNode.SetProcessHook(dramHandler.Process)
		if err := dramNode.AddInputQueue(dramInputQueue); err != nil {
			t.Fatalf("Failed to add DRAM input queue %d: %v", i, err)
		}
		if err := dramNode.AddOutputQueue(dramOutputQueue); err != nil {
			t.Fatalf("Failed to add DRAM output queue %d: %v", i, err)
		}

		dramHandlers = append(dramHandlers, dramHandler)
		dramNodeHandles = append(dramNodeHandles, &network.NodeHandle{
			Node:    dramNode,
			Inputs:  []*queue.InputQueue{dramInputQueue},
			Outputs: []*queue.OutputQueue{dramOutputQueue},
		})

		t.Logf("  DRAM Channel %d: OK", i)
	}

	// ========== 创建 Network 并连接节点 ==========
	t.Log("\n创建 Network 拓扑...")
	net := network.New()

	// 添加所有节点
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

	// 连接 CPUs -> L2
	for i := 0; i < numCores; i++ {
		net.Connect(cpuNodeIDs[i], 0, l2NodeID, i, 10, 1)      // CPU -> L2 (latency=10)
		net.Connect(l2NodeID, i, cpuNodeIDs[i], 0, 10, 1)      // L2 -> CPU (latency=10)
	}

	// 连接 L2 <-> Memory Controller
	net.Connect(l2NodeID, numCores, memCtrlNodeID, 0, 50, 1)  // L2 -> MemCtrl (latency=50)
	net.Connect(memCtrlNodeID, 0, l2NodeID, numCores, 50, 1)  // MemCtrl -> L2 (latency=50)

	// 连接 Memory Controller <-> DRAM Channels
	for i := 0; i < numChannels; i++ {
		net.Connect(memCtrlNodeID, i+1, dramNodeIDs[i], 0, 20, 1)  // MemCtrl -> DRAM
		net.Connect(dramNodeIDs[i], 0, memCtrlNodeID, i+1, 20, 1)  // DRAM -> MemCtrl
	}

	t.Log("  Network 拓扑创建完成")

	// ========== 运行仿真 ==========
	t.Log("\n开始四核仿真...")
	t.Logf("运行 %d 个周期...", maxCycles)

	if err := net.AdvanceTo(maxCycles - 1); err != nil {
		t.Fatalf("Simulation failed: %v", err)
	}

	t.Log("仿真完成！")

	// ========== 收集和展示统计信息 ==========
	t.Log("\n========== 统计信息 ==========")

	totalInstructions := uint64(0)
	for i := 0; i < numCores; i++ {
		cpuStats := cpuHandlers[i].GetCPUStats()
		cacheStatsInterface := cpuHandlers[i].GetCacheStats()
		cacheStats, _ := cacheStatsInterface.(cache.CacheStats)

		ipc := float64(cpuStats.TotalInstructions) / float64(maxCycles)
		hitRate := 0.0
		if cacheStats.Accesses > 0 {
			hitRate = float64(cacheStats.Hits) * 100.0 / float64(cacheStats.Accesses)
		}

		t.Logf("Core %d:", i)
		t.Logf("  Instructions: %d", cpuStats.TotalInstructions)
		t.Logf("  IPC: %.3f", ipc)
		t.Logf("  L1D Hit Rate: %.2f%%", hitRate)
		t.Logf("  L1D Misses: %d", cacheStats.Misses)

		totalInstructions += cpuStats.TotalInstructions
	}

	t.Logf("\n总体:")
	t.Logf("  总指令数: %d", totalInstructions)
	t.Logf("  平均 IPC: %.3f", float64(totalInstructions)/float64(maxCycles)/float64(numCores))

	// L2 统计
	l2Stats := l2Handler.GetStats()
	l2HitRate := 0.0
	if l2Stats.Accesses > 0 {
		l2HitRate = float64(l2Stats.Hits) * 100.0 / float64(l2Stats.Accesses)
	}
	t.Logf("\nL2 Cache:")
	t.Logf("  Accesses: %d", l2Stats.Accesses)
	t.Logf("  Hit Rate: %.2f%%", l2HitRate)
	t.Logf("  Invalidates Sent: %d", l2Stats.InvalidatesSent)

	// Memory Controller 统计
	memCtrlStats := memCtrlHandler.GetStats()
	t.Logf("\nMemory Controller:")
	t.Logf("  Total Requests: %d", memCtrlStats.TotalRequests)
	for i := 0; i < numChannels; i++ {
		percentage := 0.0
		if memCtrlStats.TotalRequests > 0 {
			percentage = float64(memCtrlStats.RequestsPerChannel[i]) * 100.0 / float64(memCtrlStats.TotalRequests)
		}
		t.Logf("  Channel %d: %d requests (%.1f%%)",
			i, memCtrlStats.RequestsPerChannel[i], percentage)
	}

	// DRAM 统计
	t.Logf("\nDRAM Channels:")
	for i := 0; i < numChannels; i++ {
		dramStats := dramHandlers[i].GetDRAMStats()
		t.Logf("  Channel %d: RQ=%d, WQ=%d",
			i, dramStats.RQAccesses, dramStats.WQAccesses)
	}

	t.Log("\n========== 测试完成 ==========")
}

// 辅助函数：格式化大数字
func formatNumber(n uint64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000.0)
	} else if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000.0)
	}
	return fmt.Sprintf("%d", n)
}
