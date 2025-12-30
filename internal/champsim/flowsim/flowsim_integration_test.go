package flowsim

import (
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/cache"
	"github.com/Readm/flow_sim/internal/champsim/cpu"
	"github.com/Readm/flow_sim/internal/champsim/dram"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	compcache "github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
)

// Test_FlowSim_CPU_DRAM_Integration 测试使用flow_sim框架的CPU+DRAM集成
func Test_FlowSim_CPU_DRAM_Integration(t *testing.T) {
	// ========== 1. 创建ChampSim组件 ==========

	// 创建trace reader
	traceFile := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Skipf("Skipping test, trace file not available: %v", err)
	}
	defer traceReader.Close()

	// 创建CPU
	o3cpu := cpu.NewO3CPU(traceReader, cpu.DefaultO3CPUConfig())
	o3cpu.SetStandaloneMode(false) // 非standalone模式

	// 创建L1D Cache
	l1dCache, err := cache.NewSetAssociativeCache(compcache.DefaultL1DConfig())
	if err != nil {
		t.Fatalf("Failed to create L1D cache: %v", err)
	}

	// 创建Memory Adapter
	memoryAdapter := NewFlowSimMemoryAdapter()

	// 连接Cache到Adapter
	l1dCache.SetLowerLevel(memoryAdapter)

	// 连接CPU到Cache
	o3cpu.SetL1DCache(l1dCache)

	// 创建DRAM
	dramChannel, err := dram.NewDRAMChannel(dram.DefaultDRAMConfig())
	if err != nil {
		t.Fatalf("Failed to create DRAM: %v", err)
	}

	// ========== 2. 创建flow_sim节点 ==========

	// 节点ID
	cpuNodeID := 0
	dramNodeID := 1

	// 创建CPU Node的输入输出队列
	cpuInputQueue := queue.NewInputQueue(8, 1)   // capacity=8, inBandwidth=1
	cpuOutputQueue := queue.NewOutputQueue(8, 1) // capacity=8, outBandwidth=1

	// 创建DRAM Node的输入输出队列
	dramInputQueue := queue.NewInputQueue(8, 1)
	dramOutputQueue := queue.NewOutputQueue(8, 1)

	// 创建CPU Node Handler
	cpuHandler := NewCPUNodeHandler(
		cpuNodeID,
		dramNodeID,
		o3cpu,
		l1dCache,
		memoryAdapter,
		cpuOutputQueue,
	)

	// 创建DRAM Node Handler
	dramHandler := NewDRAMNodeHandler(
		dramNodeID,
		cpuNodeID,
		dramChannel,
		dramOutputQueue,
	)

	// 创建Worker Nodes
	cpuNode := node.NewWorkerNode(cpuNodeID)
	cpuNode.SetProcessHook(cpuHandler.Process)

	// 关键：必须调用AddInputQueue/AddOutputQueue！
	if err := cpuNode.AddInputQueue(cpuInputQueue); err != nil {
		t.Fatalf("Failed to add CPU input queue: %v", err)
	}
	if err := cpuNode.AddOutputQueue(cpuOutputQueue); err != nil {
		t.Fatalf("Failed to add CPU output queue: %v", err)
	}

	dramNode := node.NewWorkerNode(dramNodeID)
	dramNode.SetProcessHook(dramHandler.Process)

	// 关键：必须调用AddInputQueue/AddOutputQueue！
	if err := dramNode.AddInputQueue(dramInputQueue); err != nil {
		t.Fatalf("Failed to add DRAM input queue: %v", err)
	}
	if err := dramNode.AddOutputQueue(dramOutputQueue); err != nil {
		t.Fatalf("Failed to add DRAM output queue: %v", err)
	}

	// 创建NodeHandles
	cpuNodeHandle := &network.NodeHandle{
		Node:    cpuNode,
		Inputs:  []*queue.InputQueue{cpuInputQueue},
		Outputs: []*queue.OutputQueue{cpuOutputQueue},
	}

	dramNodeHandle := &network.NodeHandle{
		Node:    dramNode,
		Inputs:  []*queue.InputQueue{dramInputQueue},
		Outputs: []*queue.OutputQueue{dramOutputQueue},
	}

	// ========== 3. 创建Network并连接 ==========

	net := network.New()

	// 添加节点
	net.AddNode(cpuNodeHandle)
	net.AddNode(dramNodeHandle)

	// 连接: CPU(output 0) -> DRAM(input 0)
	linkCPUtoDRAM, err := net.Connect(
		cpuNodeID, 0, // CPU node, output 0
		dramNodeID, 0, // DRAM node, input 0
		1, // latency = 1 cycle
		1, // bandwidth = 1 packet/cycle
	)
	if err != nil {
		t.Fatalf("Failed to connect CPU to DRAM: %v", err)
	}
	t.Logf("Created link CPU->DRAM: %v", linkCPUtoDRAM)

	// 连接: DRAM(output 0) -> CPU(input 0)
	linkDRAMtoCPU, err := net.Connect(
		dramNodeID, 0, // DRAM node, output 0
		cpuNodeID, 0, // CPU node, input 0
		1, // latency = 1 cycle
		1, // bandwidth = 1 packet/cycle
	)
	if err != nil {
		t.Fatalf("Failed to connect DRAM to CPU: %v", err)
	}
	t.Logf("Created link DRAM->CPU: %v", linkDRAMtoCPU)

	// ========== 4. 运行仿真 ==========

	t.Log("\n========== 开始flow_sim仿真 ==========")

	maxCycles := 1000 // 恢复正常的周期数
	t.Logf("Advancing network to cycle %d...", maxCycles-1)

	// 一次性推进所有周期（正确的用法）
	if err := net.AdvanceTo(maxCycles - 1); err != nil {
		t.Fatalf("Failed to advance network: %v", err)
	}

	t.Logf("Simulation completed: %d cycles executed", maxCycles)

	// ========== 5. 验证结果 ==========

	t.Log("\n========== 最终统计 ==========")

	cpuStats := o3cpu.GetStats()
	cacheStats := l1dCache.GetStats().(cache.CacheStats)
	dramStats := dramChannel.GetStats()

	t.Logf("CPU:")
	t.Logf("  总周期: %d", maxCycles)
	t.Logf("  退休指令: %d", cpuStats.TotalInstructions)
	t.Logf("  IPC: %.2f", float64(cpuStats.TotalInstructions)/float64(maxCycles))

	t.Logf("\nL1D Cache:")
	t.Logf("  总访问: %d", cacheStats.Accesses)
	cacheHitRate := 0.0
	if cacheStats.Accesses > 0 {
		cacheHitRate = float64(cacheStats.Hits) / float64(cacheStats.Accesses) * 100
	}
	t.Logf("  命中: %d (%.2f%%)", cacheStats.Hits, cacheHitRate)
	t.Logf("  未命中: %d (%.2f%%)", cacheStats.Misses, 100-cacheHitRate)

	t.Logf("\nDRAM:")
	t.Logf("  RQ访问: %d", dramStats.RQAccesses)
	t.Logf("  WQ访问: %d", dramStats.WQAccesses)
	rbHitRate := 0.0
	totalDRAMAccess := dramStats.RQRowBufferHit + dramStats.RQRowBufferMiss
	if totalDRAMAccess > 0 {
		rbHitRate = float64(dramStats.RQRowBufferHit) / float64(totalDRAMAccess) * 100
	}
	t.Logf("  Row Buffer Hits: %d (%.2f%%)", dramStats.RQRowBufferHit, rbHitRate)

	// 验证
	t.Log("\n========== 验证 ==========")

	// 验证CPU运行正常
	if cpuStats.TotalInstructions == 0 {
		t.Error("❌ CPU没有退休任何指令")
	} else {
		t.Logf("✅ CPU运行正常: %d instructions", cpuStats.TotalInstructions)
	}

	// 验证Cache运行正常
	if cacheStats.Accesses == 0 {
		t.Error("❌ Cache没有任何访问")
	} else {
		t.Logf("✅ Cache运行正常: %d accesses", cacheStats.Accesses)
	}

	// 验证DRAM运行正常
	if dramStats.RQAccesses == 0 && cacheStats.Misses > 0 {
		t.Error("❌ Cache有miss但DRAM没有接收到请求")
	} else if dramStats.RQAccesses > 0 {
		t.Logf("✅ DRAM运行正常: %d requests", dramStats.RQAccesses)
	}

	// 验证数据一致性
	if cacheStats.Misses > 0 && dramStats.RQAccesses == 0 {
		t.Error("❌ 数据不一致: Cache有miss但DRAM没有请求")
	} else {
		t.Logf("✅ Cache与DRAM联动正常: %d misses → %d DRAM requests",
			cacheStats.Misses, dramStats.RQAccesses)
	}

	// 验证IPC
	ipc := float64(cpuStats.TotalInstructions) / float64(maxCycles)
	if ipc < 0.1 || ipc > 4.0 {
		t.Errorf("❌ IPC异常: %.2f (期望 0.1-4.0)", ipc)
	} else {
		t.Logf("✅ IPC正常: %.2f", ipc)
	}

	t.Log("\n🎉 FlowSim CPU+DRAM 集成测试完成！")
}
