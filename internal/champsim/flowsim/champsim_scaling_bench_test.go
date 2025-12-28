package flowsim

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/cache"
	"github.com/Readm/flow_sim/internal/champsim/cpu"
	"github.com/Readm/flow_sim/internal/champsim/dram"
	"github.com/Readm/flow_sim/internal/champsim/trace"
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
// Topology: 64 CPUs -> 16 L2s -> 4 L3s -> MemCtrl -> 2 DRAMs
// Returns network and handlers for cleanup
func buildChampSimSystem(numCPUs int, traceFile string) (*network.Network, *SystemHandlers, error) {
	const numChannels = 2
	const cpusPerL2 = 4      // 每4个CPU共享1个L2
	const l2sPerL3 = 4       // 每4个L2共享1个L3

	numL2s := numCPUs / cpusPerL2      // 16个L2
	numL3s := numL2s / l2sPerL3        // 4个L3

	// Node IDs分配
	// 0-63: CPUs
	// 64-79: L2s (16个)
	// 80-83: L3s (4个)
	// 84: MemCtrl
	// 85-86: DRAMs (2个)
	cpuNodeIDs := make([]int, numCPUs)
	for i := 0; i < numCPUs; i++ {
		cpuNodeIDs[i] = i
	}

	l2NodeIDs := make([]int, numL2s)
	for i := 0; i < numL2s; i++ {
		l2NodeIDs[i] = numCPUs + i
	}

	l3NodeIDs := make([]int, numL3s)
	for i := 0; i < numL3s; i++ {
		l3NodeIDs[i] = numCPUs + numL2s + i
	}

	memCtrlNodeID := numCPUs + numL2s + numL3s

	dramNodeIDs := make([]int, numChannels)
	for i := 0; i < numChannels; i++ {
		dramNodeIDs[i] = memCtrlNodeID + 1 + i
	}

	handlers := &SystemHandlers{}

	// Create CPU cores (每个CPU连接到对应的L2)
	var cpuNodeHandles []*network.NodeHandle
	for i := 0; i < numCPUs; i++ {
		traceReader, err := trace.NewTraceReader(traceFile, uint8(i), trace.FormatStandard)
		if err != nil {
			return nil, nil, err
		}
		handlers.traceReaders = append(handlers.traceReaders, traceReader)

		o3cpu := cpu.NewO3CPU(traceReader, cpu.DefaultO3CPUConfig())
		o3cpu.SetStandaloneMode(false)

		l1dCache, _ := cache.NewSetAssociativeCache(cache.DefaultL1DConfig())
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
		l2Config := cache.CacheConfig{
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
		myL3NodeID := l3NodeIDs[l2Index/l2sPerL3]

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

	// Create L3 Caches (4个，每个服务4个L2)
	var l3NodeHandles []*network.NodeHandle
	for l3Index := 0; l3Index < numL3s; l3Index++ {
		l3Config := cache.CacheConfig{
			Name:        fmt.Sprintf("L3_%d", l3Index),
			NumSets:     2048,  // 更大的L3
			NumWays:     16,
			BlockSize:   64,
			MSHRSize:    64,    // 更多的MSHR
			HitLatency:  40,    // 更高的延迟
			FillLatency: 20,
		}
		l3Cache, _ := cache.NewSetAssociativeCache(l3Config)

		// 每个L3有4个L2的输入/输出 + 1个MemCtrl的输入/输出
		l3OutputQueues := make([]*queue.OutputQueue, l2sPerL3+1)
		l3InputQueues := make([]*queue.InputQueue, l2sPerL3+1)
		for i := 0; i < l2sPerL3+1; i++ {
			l3OutputQueues[i] = queue.NewOutputQueue(256, 1)  // 更大的队列
			l3InputQueues[i] = queue.NewInputQueue(256, 1)
		}

		// 这个L3服务的L2 IDs
		startL2 := l3Index * l2sPerL3
		myL2NodeIDs := l2NodeIDs[startL2 : startL2+l2sPerL3]

		l3Handler := NewL2CacheNodeHandler(
			l3NodeIDs[l3Index], myL2NodeIDs, memCtrlNodeID,
			l3Cache, l3OutputQueues,
		)

		l3Node := node.NewWorkerNode(l3NodeIDs[l3Index])
		l3Node.SetProcessHook(l3Handler.Process)
		for _, q := range l3InputQueues {
			l3Node.AddInputQueue(q)
		}
		for _, q := range l3OutputQueues {
			l3Node.AddOutputQueue(q)
		}

		l3NodeHandles = append(l3NodeHandles, &network.NodeHandle{
			Node:    l3Node,
			Inputs:  l3InputQueues,
			Outputs: l3OutputQueues,
		})
	}

	// Create Memory Controller (连接4个L3和2个DRAM)
	memCtrlOutputQueues := make([]*queue.OutputQueue, numL3s+numChannels)
	memCtrlInputQueues := make([]*queue.InputQueue, numL3s+numChannels)
	for i := 0; i < numL3s+numChannels; i++ {
		memCtrlOutputQueues[i] = queue.NewOutputQueue(256, 1)
		memCtrlInputQueues[i] = queue.NewInputQueue(256, 1)
	}

	memCtrlHandler := NewMemoryControllerHandler(
		memCtrlNodeID, l3NodeIDs[0], dramNodeIDs,  // 暂时用第一个L3的ID，实际上需要处理多个L3
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

	// Create DRAM Channels
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

		dramNodeHandles = append(dramNodeHandles, &network.NodeHandle{
			Node:    dramNode,
			Inputs:  []*queue.InputQueue{dramInputQueue},
			Outputs: []*queue.OutputQueue{dramOutputQueue},
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

	// Add all L3 nodes
	for _, handle := range l3NodeHandles {
		net.AddNode(handle)
	}

	// Add Memory Controller
	net.AddNode(&network.NodeHandle{
		Node:    memCtrlNode,
		Inputs:  memCtrlInputQueues,
		Outputs: memCtrlOutputQueues,
	})

	// Add DRAM nodes
	for _, handle := range dramNodeHandles {
		net.AddNode(handle)
	}

	// Connect topology: CPUs -> L2s
	for cpuIdx := 0; cpuIdx < numCPUs; cpuIdx++ {
		l2Idx := cpuIdx / cpusPerL2
		portInL2 := cpuIdx % cpusPerL2

		// CPU -> L2
		net.Connect(cpuNodeIDs[cpuIdx], 0, l2NodeIDs[l2Idx], portInL2, 10, 1)
		// L2 -> CPU
		net.Connect(l2NodeIDs[l2Idx], portInL2, cpuNodeIDs[cpuIdx], 0, 10, 1)
	}

	// Connect topology: L2s -> L3s
	for l2Idx := 0; l2Idx < numL2s; l2Idx++ {
		l3Idx := l2Idx / l2sPerL3
		portInL3 := l2Idx % l2sPerL3

		// L2 -> L3 (L2的最后一个端口连接到L3)
		net.Connect(l2NodeIDs[l2Idx], cpusPerL2, l3NodeIDs[l3Idx], portInL3, 20, 1)
		// L3 -> L2
		net.Connect(l3NodeIDs[l3Idx], portInL3, l2NodeIDs[l2Idx], cpusPerL2, 20, 1)
	}

	// Connect topology: L3s -> MemCtrl
	// 临时方案：所有L3都连接到MemCtrl的端口0（输入）和各自的输出端口
	// TODO: 需要修改MemoryControllerHandler以支持多个上游节点
	for l3Idx := 0; l3Idx < numL3s; l3Idx++ {
		// L3 -> MemCtrl (所有L3都连接到MemCtrl的输入端口0)
		net.Connect(l3NodeIDs[l3Idx], l2sPerL3, memCtrlNodeID, 0, 50, 1)
		// MemCtrl -> L3
		net.Connect(memCtrlNodeID, l3Idx, l3NodeIDs[l3Idx], l2sPerL3, 50, 1)
	}

	// Connect topology: MemCtrl -> DRAMs
	for dramIdx := 0; dramIdx < numChannels; dramIdx++ {
		// MemCtrl -> DRAM
		net.Connect(memCtrlNodeID, numL3s+dramIdx, dramNodeIDs[dramIdx], 0, 20, 1)
		// DRAM -> MemCtrl
		net.Connect(dramNodeIDs[dramIdx], 0, memCtrlNodeID, numL3s+dramIdx, 20, 1)
	}

	return net, handlers, nil
}

// runChampSimBenchmark runs ChampSim simulation using AdvanceTo
func runChampSimBenchmark(b *testing.B, numCPUs int, maxCycles uint64, traceFile string) {
	net, handlers, err := buildChampSimSystem(numCPUs, traceFile)
	if err != nil {
		b.Fatalf("Failed to build system: %v", err)
	}
	defer handlers.Cleanup()

	// Run simulation using AdvanceTo
	if err := net.AdvanceTo(int(maxCycles - 1)); err != nil {
		b.Fatalf("Simulation failed: %v", err)
	}
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
//   go test -bench=Benchmark_ChampSim_64CPU -benchmem
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

	const cpusPerL2 = 4
	const l2sPerL3 = 4
	numL2s := numSimCPUs / cpusPerL2  // 16
	numL3s := numL2s / l2sPerL3        // 4
	totalNodes := numSimCPUs + numL2s + numL3s + 1 + 2 // CPUs + L2s + L3s + MemCtrl + 2 DRAM

	b.Logf("Testing 64-CPU ChampSim system: %d CPUs, %d L2s, %d L3s, %d total nodes, %d sim cycles",
		numSimCPUs, numL2s, numL3s, totalNodes, maxCycles)
	b.Logf("Physical core counts: %v (NumCPU=%d)", coreCountSamples, numPhysicalCPU)

	var singleCoreCycles float64

	for _, coreCount := range coreCountSamples {
		b.Run(fmt.Sprintf("Cores_%d", coreCount), func(b *testing.B) {
			// Set GOMAXPROCS for this benchmark
			oldMaxProcs := runtime.GOMAXPROCS(coreCount)
			defer runtime.GOMAXPROCS(oldMaxProcs)

			b.ResetTimer()

			var totalCycles uint64

			// Run benchmark and accumulate actual cycles
			for iteration := 0; iteration < b.N; iteration++ {
				iterStart := node.GetCPUCycles()
				runChampSimBenchmark(b, numSimCPUs, maxCycles, traceFile)
				iterEnd := node.GetCPUCycles()
				totalCycles += (iterEnd - iterStart)
			}

			b.StopTimer()

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
