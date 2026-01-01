package flowsim

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/cache"
	"github.com/Readm/flow_sim/internal/champsim/cpu"
	"github.com/Readm/flow_sim/internal/champsim/dram"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	compcache "github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
)

// SystemHandlers holds all handlers for cleanup
type SystemHandlers struct {
	traceReaders []trace.TraceReader
}

func (h *SystemHandlers) Cleanup() {
	for _, reader := range h.traceReaders {
		reader.Close()
	}
}

// buildChampSimSystem builds a complete ChampSim system with hierarchical cache
// Topology: 64 CPUs -> 32 L2s -> 8 L3s <-> 8 MemCtrls -> 8 DRAMs
// L3s and MemCtrls are connected via a Bufferless Ring (对称设计)
// Returns network and handlers for cleanup
func buildChampSimSystem(numCPUs int, traceFile string) (*network.Network, *SystemHandlers, error) {
	const numChannels = 8   // 8个DRAM通道
	const numMemCtrls = 8   // 8个Memory Controllers (与DRAM一对一)
	const numL3s = 8        // 8个L3 (与MemCtrl数量对称)
	const cpusPerL2 = 2     // 每2个CPU共享1个L2
	const l2sPerL3 = 4      // 每4个L2共享1个L3
	const ringLatency = 5   // Ring链路延迟
	const localLatency = 3  // Local链路延迟（测试：1→3 增加流水线深度）
	const routerBuffer = 16 // Ring路由器缓冲区大小

	numL2s := numCPUs / cpusPerL2                // 32个L2
	numRingNodes := numL3s + numMemCtrls         // Ring上共16个节点（8个L3 + 8个MemCtrl）

	// Node IDs分配
	// 0-63: CPUs (64个)
	// 64-95: L2s (32个)
	// 96-103: L3 Workers (8个，实际的L3 Cache节点)
	// 104-111: MemCtrl Workers (8个，实际的Memory Controller节点)
	// 112-119: DRAMs (8个)
	// 200-215: Ring Routers (16个，交错排列L3和MemCtrl)
	cpuNodeIDs := make([]int, numCPUs)
	for i := 0; i < numCPUs; i++ {
		cpuNodeIDs[i] = i
	}

	l2NodeIDs := make([]int, numL2s)
	for i := 0; i < numL2s; i++ {
		l2NodeIDs[i] = numCPUs + i
	}

	l3WorkerIDs := make([]int, numL3s)
	for i := 0; i < numL3s; i++ {
		l3WorkerIDs[i] = numCPUs + numL2s + i
	}

	memCtrlWorkerIDs := make([]int, numMemCtrls)
	for i := 0; i < numMemCtrls; i++ {
		memCtrlWorkerIDs[i] = numCPUs + numL2s + numL3s + i
	}

	dramNodeIDs := make([]int, numChannels)
	for i := 0; i < numChannels; i++ {
		dramNodeIDs[i] = numCPUs + numL2s + numL3s + numMemCtrls + i
	}

	// Ring Router IDs: 200-215 (交错排列L3和MemCtrl，对称设计)
	// 200: L3_0, 201: MC_0, 202: L3_1, 203: MC_1, 204: L3_2, 205: MC_2, 206: L3_3, 207: MC_3,
	// 208: L3_4, 209: MC_4, 210: L3_5, 211: MC_5, 212: L3_6, 213: MC_6, 214: L3_7, 215: MC_7
	ringRouterIDs := make([]int, numRingNodes)
	for i := 0; i < numRingNodes; i++ {
		ringRouterIDs[i] = 200 + i
	}

	handlers := &SystemHandlers{}

	// Create CPU cores (每个CPU连接到对应的L2)
	var cpuNodeHandles []*network.NodeHandle
	for i := 0; i < numCPUs; i++ {
		traceReader, err := trace.NewSharedTraceReader(traceFile, uint8(i), trace.FormatStandard)
		if err != nil {
			return nil, nil, err
		}
		handlers.traceReaders = append(handlers.traceReaders, traceReader)

		o3cpu := cpu.NewO3CPU(traceReader, cpu.DefaultO3CPUConfig())
		o3cpu.SetStandaloneMode(false)

		l1dCache, _ := cache.NewSetAssociativeCache(compcache.DefaultL1DConfig())
		memoryAdapter := NewFlowSimMemoryAdapter()
		l1dCache.SetLowerLevel(memoryAdapter)
		o3cpu.SetL1DCache(l1dCache)

		cpuOutputQueue := queue.NewOutputQueue(128, 1)
		cpuInputQueue := queue.NewInputQueue(128, 1)

		// 每4个CPU共享1个L2
		myL2NodeID := l2NodeIDs[i/cpusPerL2]

		cpuHandler := NewCPUNodeHandler(
			cpuNodeIDs[i], myL2NodeID,
			o3cpu, l1dCache, memoryAdapter,
			cpuOutputQueue,
		)

		cpuNode := node.NewWorkerNode(cpuNodeIDs[i])
		cpuNode.SetProcessHook(cpuHandler.Process)
		cpuNode.AddInputQueue(cpuInputQueue)
		cpuNode.AddOutputQueue(cpuOutputQueue)

		cpuNodeHandles = append(cpuNodeHandles, &network.NodeHandle{
			Node:    cpuNode,
			Inputs:  []*queue.InputQueue{cpuInputQueue},
			Outputs: []*queue.OutputQueue{cpuOutputQueue},
		})
	}

	// Create L2 Caches (16个，每个服务4个CPU)
	var l2NodeHandles []*network.NodeHandle
	for l2Index := 0; l2Index < numL2s; l2Index++ {
		l2Config := compcache.CacheConfig{
			Name:        fmt.Sprintf("L2_%d", l2Index),
			NumSets:     512,
			NumWays:     16,
			BlockSize:   64,
			MSHRSize:    32,
			HitLatency:  20,
			FillLatency: 10,
		}
		l2Cache, _ := cache.NewSetAssociativeCache(l2Config)

		// 每个L2有4个CPU的输入/输出 + 1个L3的输入/输出
		l2OutputQueues := make([]*queue.OutputQueue, cpusPerL2+1)
		l2InputQueues := make([]*queue.InputQueue, cpusPerL2+1)
		for i := 0; i < cpusPerL2+1; i++ {
			l2OutputQueues[i] = queue.NewOutputQueue(128, 1)
			l2InputQueues[i] = queue.NewInputQueue(128, 1)
		}

		// 这个L2服务的CPU IDs
		startCPU := l2Index * cpusPerL2
		myCPUNodeIDs := cpuNodeIDs[startCPU : startCPU+cpusPerL2]

		// 这个L2连接的L3
		myL3NodeID := l3WorkerIDs[l2Index/l2sPerL3]

		l2Handler := NewL2CacheNodeHandler(
			l2NodeIDs[l2Index], myCPUNodeIDs, myL3NodeID,
			l2Cache, l2OutputQueues,
		)

		l2Node := node.NewWorkerNode(l2NodeIDs[l2Index])
		l2Node.SetProcessHook(l2Handler.Process)
		for _, q := range l2InputQueues {
			l2Node.AddInputQueue(q)
		}
		for _, q := range l2OutputQueues {
			l2Node.AddOutputQueue(q)
		}

		l2NodeHandles = append(l2NodeHandles, &network.NodeHandle{
			Node:    l2Node,
			Inputs:  l2InputQueues,
			Outputs: l2OutputQueues,
		})
	}

	// Create L3 Worker Nodes (4个，每个服务4个L2，通过Ring与MemCtrl通信)
	var l3WorkerHandles []*network.NodeHandle
	for l3Index := 0; l3Index < numL3s; l3Index++ {
		l3Config := compcache.CacheConfig{
			Name:        fmt.Sprintf("L3_%d", l3Index),
			NumSets:     2048,
			NumWays:     16,
			BlockSize:   64,
			MSHRSize:    64,
			HitLatency:  40,
			FillLatency: 20,
		}
		l3Cache, _ := cache.NewSetAssociativeCache(l3Config)

		// 每个L3 Worker有4个L2的输入/输出 + 1个Ring的输入/输出
		l3OutputQueues := make([]*queue.OutputQueue, l2sPerL3+1)
		l3InputQueues := make([]*queue.InputQueue, l2sPerL3+1)
		for i := 0; i < l2sPerL3+1; i++ {
			l3OutputQueues[i] = queue.NewOutputQueue(256, 1)
			l3InputQueues[i] = queue.NewInputQueue(256, 1)
		}

		// 这个L3服务的L2 IDs
		startL2 := l3Index * l2sPerL3
		myL2NodeIDs := l2NodeIDs[startL2 : startL2+l2sPerL3]

		// 注意：L3现在通过Ring与MemCtrl通信，ringNodeID将在后面设置
		// 这里暂时传入一个占位符ID，后续需要修改Handler以支持Ring路由
		l3Handler := NewL2CacheNodeHandler(
			l3WorkerIDs[l3Index], myL2NodeIDs, ringRouterIDs[0], // 暂时用ring router 0
			l3Cache, l3OutputQueues,
		)

		l3Worker := node.NewWorkerNode(l3WorkerIDs[l3Index])
		l3Worker.SetProcessHook(l3Handler.Process)
		for _, q := range l3InputQueues {
			l3Worker.AddInputQueue(q)
		}
		for _, q := range l3OutputQueues {
			l3Worker.AddOutputQueue(q)
		}

		l3WorkerHandles = append(l3WorkerHandles, &network.NodeHandle{
			Node:    l3Worker,
			Inputs:  l3InputQueues,
			Outputs: l3OutputQueues,
		})
	}

	// Create Memory Controller Worker Nodes (8个，每个对应1个DRAM，通过Ring与L3通信)
	var memCtrlWorkerHandles []*network.NodeHandle
	for mcIndex := 0; mcIndex < numMemCtrls; mcIndex++ {
		// 每个MemCtrl Worker有1个Ring的输入/输出 + 1个DRAM的输入/输出
		mcOutputQueues := make([]*queue.OutputQueue, 2)
		mcInputQueues := make([]*queue.InputQueue, 2)
		for i := 0; i < 2; i++ {
			mcOutputQueues[i] = queue.NewOutputQueue(256, 1)
			mcInputQueues[i] = queue.NewInputQueue(256, 1)
		}

		// 每个MemCtrl只连接到1个DRAM
		mcHandler := NewMemoryControllerHandler(
			memCtrlWorkerIDs[mcIndex],
			[]int{ringRouterIDs[0]}, // 暂时用ring router 0作为上游
			[]int{dramNodeIDs[mcIndex]},
			mcOutputQueues,
			MappingInterleaved,
		)

		mcWorker := node.NewWorkerNode(memCtrlWorkerIDs[mcIndex])
		mcWorker.SetProcessHook(mcHandler.Process)
		for _, q := range mcInputQueues {
			mcWorker.AddInputQueue(q)
		}
		for _, q := range mcOutputQueues {
			mcWorker.AddOutputQueue(q)
		}

		memCtrlWorkerHandles = append(memCtrlWorkerHandles, &network.NodeHandle{
			Node:    mcWorker,
			Inputs:  mcInputQueues,
			Outputs: mcOutputQueues,
		})
	}

	// Create DRAM Channels (8个，每个连接到对应的MemCtrl Worker)
	var dramNodeHandles []*network.NodeHandle
	for i := 0; i < numChannels; i++ {
		dramChannel, _ := dram.NewDRAMChannel(dram.DefaultDRAMConfig())
		dramOutputQueue := queue.NewOutputQueue(128, 1)
		dramInputQueue := queue.NewInputQueue(128, 1)

		dramHandler := NewDRAMNodeHandler(
			dramNodeIDs[i], memCtrlWorkerIDs[i], // 每个DRAM连接到对应的MemCtrl
			dramChannel, dramOutputQueue,
		)

		dramNode := node.NewWorkerNode(dramNodeIDs[i])
		dramNode.SetProcessHook(dramHandler.Process)
		dramNode.AddInputQueue(dramInputQueue)
		dramNode.AddOutputQueue(dramOutputQueue)

		dramNodeHandles = append(dramNodeHandles, &network.NodeHandle{
			Node:    dramNode,
			Inputs:  []*queue.InputQueue{dramInputQueue},
			Outputs: []*queue.OutputQueue{dramOutputQueue},
		})
	}

	// Create Ring Routers (16个，对称交错排列L3和MemCtrl)
	// Ring顺序: L3_0(200), MC_0(201), L3_1(202), MC_1(203), ..., L3_7(214), MC_7(215)
	var ringRouterHandles []*network.NodeHandle
	for i := 0; i < numRingNodes; i++ {
		routerID := ringRouterIDs[i]

		// 确定这个router对应的worker ID（对称交错：偶数=L3，奇数=MC）
		var workerID int
		if i%2 == 0 {
			// 偶数索引：L3 router
			l3Index := i / 2
			workerID = l3WorkerIDs[l3Index]
		} else {
			// 奇数索引：MemCtrl router
			mcIndex := (i - 1) / 2
			workerID = memCtrlWorkerIDs[mcIndex]
		}

		router := node.NewBufferlessRingRouter(routerID, workerID, routerBuffer)
		ringInQueue := queue.NewInputQueue(256, 1)
		localInQueue := queue.NewInputQueue(256, 1)
		ringOutQueue := queue.NewOutputQueue(256, 1)
		localOutQueue := queue.NewOutputQueue(256, 1)

		router.AddInputQueue(ringInQueue)
		router.AddInputQueue(localInQueue)
		router.AddOutputQueue(ringOutQueue)
		router.AddOutputQueue(localOutQueue)

		ringRouterHandles = append(ringRouterHandles, &network.NodeHandle{
			Node:    router,
			Inputs:  []*queue.InputQueue{ringInQueue, localInQueue},
			Outputs: []*queue.OutputQueue{ringOutQueue, localOutQueue},
		})
	}

	// Create Network and add all nodes
	net := network.New()

	// Add all CPU nodes
	for _, handle := range cpuNodeHandles {
		net.AddNode(handle)
	}

	// Add all L2 nodes
	for _, handle := range l2NodeHandles {
		net.AddNode(handle)
	}

	// Add all L3 Worker nodes
	for _, handle := range l3WorkerHandles {
		net.AddNode(handle)
	}

	// Add all MemCtrl Worker nodes
	for _, handle := range memCtrlWorkerHandles {
		net.AddNode(handle)
	}

	// Add all DRAM nodes
	for _, handle := range dramNodeHandles {
		net.AddNode(handle)
	}

	// Add all Ring Router nodes
	for _, handle := range ringRouterHandles {
		net.AddNode(handle)
	}

	// Connect topology: CPUs <-> L2s
	for cpuIdx := 0; cpuIdx < numCPUs; cpuIdx++ {
		l2Idx := cpuIdx / cpusPerL2
		portInL2 := cpuIdx % cpusPerL2
		net.Connect(cpuNodeIDs[cpuIdx], 0, l2NodeIDs[l2Idx], portInL2, 10, 1)
		net.Connect(l2NodeIDs[l2Idx], portInL2, cpuNodeIDs[cpuIdx], 0, 10, 1)
	}

	// Connect topology: L2s <-> L3 Workers
	for l2Idx := 0; l2Idx < numL2s; l2Idx++ {
		l3Idx := l2Idx / l2sPerL3
		portInL3 := l2Idx % l2sPerL3
		net.Connect(l2NodeIDs[l2Idx], cpusPerL2, l3WorkerIDs[l3Idx], portInL3, 20, 1)
		net.Connect(l3WorkerIDs[l3Idx], portInL3, l2NodeIDs[l2Idx], cpusPerL2, 20, 1)
	}

	// Connect topology: L3 Workers <-> Ring Routers (local connection，对称设计)
	// L3_0 <-> Router200, L3_1 <-> Router202, ..., L3_7 <-> Router214
	for l3Idx := 0; l3Idx < numL3s; l3Idx++ {
		routerID := ringRouterIDs[l3Idx*2] // 偶数位置的router
		net.ConnectWithHandler(l3WorkerIDs[l3Idx], l2sPerL3, routerID, 1, localLatency, 1,
			link.NewBufferlessLinkHandler())
		net.ConnectWithHandler(routerID, 1, l3WorkerIDs[l3Idx], l2sPerL3, localLatency, 1,
			link.NewBufferlessLinkHandler())
	}

	// Connect topology: MemCtrl Workers <-> Ring Routers (local connection，对称设计)
	// MC_0 <-> Router201, MC_1 <-> Router203, ..., MC_7 <-> Router215
	for mcIdx := 0; mcIdx < numMemCtrls; mcIdx++ {
		routerID := ringRouterIDs[mcIdx*2+1] // 奇数位置的router
		net.ConnectWithHandler(memCtrlWorkerIDs[mcIdx], 0, routerID, 1, localLatency, 1,
			link.NewBufferlessLinkHandler())
		net.ConnectWithHandler(routerID, 1, memCtrlWorkerIDs[mcIdx], 0, localLatency, 1,
			link.NewBufferlessLinkHandler())
	}

	// Connect topology: Ring (使用 BufferlessLinkHandler)
	// Router[i] -> Router[(i+1) % 16]
	for i := 0; i < numRingNodes; i++ {
		nextRouter := (i + 1) % numRingNodes
		net.ConnectWithHandler(ringRouterIDs[i], 0, ringRouterIDs[nextRouter], 0, ringLatency, 1,
			link.NewBufferlessLinkHandler())
	}

	// Connect topology: MemCtrl Workers <-> DRAMs (一对一)
	for mcIdx := 0; mcIdx < numMemCtrls; mcIdx++ {
		net.Connect(memCtrlWorkerIDs[mcIdx], 1, dramNodeIDs[mcIdx], 0, 20, 1)
		net.Connect(dramNodeIDs[mcIdx], 0, memCtrlWorkerIDs[mcIdx], 1, 20, 1)
	}

	return net, handlers, nil
}

// runChampSimBenchmark runs ChampSim simulation using AdvanceTo
func runChampSimBenchmark(b *testing.B, numCPUs int, maxCycles uint64, traceFile string) *network.Network {
	net, handlers, err := buildChampSimSystem(numCPUs, traceFile)
	if err != nil {
		b.Fatalf("Failed to build system: %v", err)
	}
	defer handlers.Cleanup()

	// Run simulation using AdvanceTo
	if err := net.AdvanceTo(int(maxCycles - 1)); err != nil {
		b.Fatalf("Simulation failed: %v", err)
	}

	return net
}

// Benchmark_ChampSim_64CPU benchmarks 64-CPU ChampSim system with varying physical core counts
// This tests how well the framework parallelizes a fixed large-scale workload
//
// Test configuration:
//   - Fixed: 64 simulated CPUs, 2000 simulation cycles
//   - Varying: 1, 2, 4, 8, 16 physical cores (GOMAXPROCS)
//
// Key metrics:
//   - actual_cycles/op: Real CPU cycles consumed
//   - sim_cpus: Number of simulated CPUs (fixed at 64)
//   - total_nodes: Total number of nodes in the system (68)
//   - efficiency_pct: Parallel efficiency = (single_core_cycles / (actual_cycles * cores)) * 100
//
// Usage:
//
//	go test -bench=Benchmark_ChampSim_64CPU -benchmem
func Benchmark_ChampSim_64CPU(b *testing.B) {
	const numSimCPUs = 64
	const maxCycles = 2000
	traceFile := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"

	// Check if trace file is available
	testReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		b.Skipf("Trace file not available: %v (run from repo root or provide trace)", err)
	}
	testReader.Close()

	numPhysicalCPU := runtime.NumCPU()

	// Generate core count samples: 1, 2, 4, 8... up to 16
	var coreCountSamples []int
	for i := 1; i <= 16 && i <= numPhysicalCPU; i *= 2 {
		coreCountSamples = append(coreCountSamples, i)
	}
	if coreCountSamples[len(coreCountSamples)-1] != 16 && numPhysicalCPU >= 16 {
		coreCountSamples = append(coreCountSamples, 16)
	}

	const cpusPerL2 = 2
	const l2sPerL3 = 4
	numL2s := numSimCPUs / cpusPerL2                   // 32
	const numL3s = 8                                   // 8 (对称设计)
	const numMemCtrls = 8
	const numDRAMs = 8
	const numRingRouters = 16
	totalNodes := numSimCPUs + numL2s + numL3s + numMemCtrls + numDRAMs + numRingRouters // 64+32+8+8+8+16=136

	b.Logf("Testing 64-CPU ChampSim system with symmetric Ring: %d CPUs, %d L2s, %d L3s, %d MemCtrls, %d DRAMs, %d Ring Routers, %d total nodes, %d sim cycles",
		numSimCPUs, numL2s, numL3s, numMemCtrls, numDRAMs, numRingRouters, totalNodes, maxCycles)
	b.Logf("Physical core counts: %v (NumCPU=%d)", coreCountSamples, numPhysicalCPU)

	var singleCoreCycles float64
	var profilePrinted bool

	for _, coreCount := range coreCountSamples {
		b.Run(fmt.Sprintf("Cores_%d", coreCount), func(b *testing.B) {
			// Set GOMAXPROCS for this benchmark
			oldMaxProcs := runtime.GOMAXPROCS(coreCount)
			defer runtime.GOMAXPROCS(oldMaxProcs)

			b.ResetTimer()

			var totalCycles uint64
			var lastNet *network.Network

			// Run benchmark and accumulate actual cycles
			for iteration := 0; iteration < b.N; iteration++ {
				iterStart := node.GetCPUCycles()
				lastNet = runChampSimBenchmark(b, numSimCPUs, maxCycles, traceFile)
				iterEnd := node.GetCPUCycles()
				totalCycles += (iterEnd - iterStart)
			}

			b.StopTimer()

			// Print profiling results after first core count test (only once)
			if !profilePrinted && lastNet != nil {
				b.Logf("\n========== Profiling Results (Cores=%d) ==========", coreCount)

				// 1. 同步阻塞 Profiling
				lastNet.PrintSyncProfile()
				lastNet.PrintTopBlockers(10)
				lastNet.PrintBlockingTimeProfile(20) // 新增：阻塞时间 profiling

				// 2. 节点执行时间 Profiling
				lastNet.PrintTopSlowestNodes(20)

				// 3. 三阶段详细时间 Profiling (关键：找出真正瓶颈)
				lastNet.PrintNodeDetailedTimeProfile(150) // 增加到 150 以包含所有节点类型

				profilePrinted = true
			}

			// Calculate performance metrics
			// Actual cycles per op (measured with RDTSC)
			actualCyclesPerOp := float64(totalCycles) / float64(b.N)

			// Store single core baseline for speedup calculation
			if coreCount == 1 {
				singleCoreCycles = actualCyclesPerOp
			}

			// Calculate speedup relative to single core
			speedup := 0.0
			if actualCyclesPerOp > 0 && singleCoreCycles > 0 {
				speedup = singleCoreCycles / actualCyclesPerOp
			}

			// Efficiency = (Speedup / Cores) * 100
			// This shows how well we utilize the available cores
			efficiencyPct := 0.0
			if coreCount > 0 {
				efficiencyPct = (speedup / float64(coreCount)) * 100
			}

			// Report metrics
			b.ReportMetric(float64(numSimCPUs), "sim_cpus")
			b.ReportMetric(float64(totalNodes), "total_nodes")
			b.ReportMetric(actualCyclesPerOp, "actual_cycles/op")
			b.ReportMetric(efficiencyPct, "efficiency_pct")
			b.ReportMetric(speedup, "speedup")
		})
	}
}
