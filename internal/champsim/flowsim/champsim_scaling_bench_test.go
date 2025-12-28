package flowsim

import (
	"fmt"
	"runtime"
	"testing"
	"time"

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

// buildChampSimSystem builds a complete ChampSim system with configurable CPU count
// Returns network and handlers for cleanup
func buildChampSimSystem(numCPUs int, traceFile string) (*network.Network, *SystemHandlers, error) {
	const numChannels = 2

	// Node IDs
	cpuNodeIDs := make([]int, numCPUs)
	for i := 0; i < numCPUs; i++ {
		cpuNodeIDs[i] = i
	}
	l2NodeID := numCPUs
	memCtrlNodeID := numCPUs + 1
	dramNodeIDs := make([]int, numChannels)
	for i := 0; i < numChannels; i++ {
		dramNodeIDs[i] = numCPUs + 2 + i
	}

	handlers := &SystemHandlers{}

	// Create CPU cores
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

		cpuHandler := NewCPUNodeHandler(
			cpuNodeIDs[i], l2NodeID,
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

	// Create L2 Cache
	l2Config := cache.CacheConfig{
		Name:        "L2",
		NumSets:     512,
		NumWays:     16,
		BlockSize:   64,
		MSHRSize:    32,
		HitLatency:  20,
		FillLatency: 10,
	}
	l2Cache, _ := cache.NewSetAssociativeCache(l2Config)

	l2OutputQueues := make([]*queue.OutputQueue, numCPUs+1)
	l2InputQueues := make([]*queue.InputQueue, numCPUs+1)
	for i := 0; i < numCPUs+1; i++ {
		l2OutputQueues[i] = queue.NewOutputQueue(128, 1)
		l2InputQueues[i] = queue.NewInputQueue(128, 1)
	}

	l2Handler := NewL2CacheNodeHandler(
		l2NodeID, cpuNodeIDs, memCtrlNodeID,
		l2Cache, l2OutputQueues,
	)

	l2Node := node.NewWorkerNode(l2NodeID)
	l2Node.SetProcessHook(l2Handler.Process)
	for _, q := range l2InputQueues {
		l2Node.AddInputQueue(q)
	}
	for _, q := range l2OutputQueues {
		l2Node.AddOutputQueue(q)
	}

	// Create Memory Controller
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

	// Connect topology
	for i := 0; i < numCPUs; i++ {
		net.Connect(cpuNodeIDs[i], 0, l2NodeID, i, 10, 1)
		net.Connect(l2NodeID, i, cpuNodeIDs[i], 0, 10, 1)
	}
	net.Connect(l2NodeID, numCPUs, memCtrlNodeID, 0, 50, 1)
	net.Connect(memCtrlNodeID, 0, l2NodeID, numCPUs, 50, 1)
	for i := 0; i < numChannels; i++ {
		net.Connect(memCtrlNodeID, i+1, dramNodeIDs[i], 0, 20, 1)
		net.Connect(dramNodeIDs[i], 0, memCtrlNodeID, i+1, 20, 1)
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

	// Calibrate CPU frequency
	cyclesPerUS := node.CalibrateCyclesPerUS(100 * time.Millisecond)
	b.Logf("Calibrated CPU Frequency: %.2f GHz", cyclesPerUS/1000.0)

	numPhysicalCPU := runtime.NumCPU()

	// Generate core count samples: 1, 2, 4, 8... up to 16
	var coreCountSamples []int
	for i := 1; i <= 16 && i <= numPhysicalCPU; i *= 2 {
		coreCountSamples = append(coreCountSamples, i)
	}
	if coreCountSamples[len(coreCountSamples)-1] != 16 && numPhysicalCPU >= 16 {
		coreCountSamples = append(coreCountSamples, 16)
	}

	totalNodes := numSimCPUs + 4 // CPUs + L2 + MemCtrl + 2 DRAM

	b.Logf("Testing 64-CPU ChampSim system: %d total nodes, %d sim cycles", totalNodes, maxCycles)
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

			// Actual CPU cycles used per operation
			actualCyclesPerOp := float64(totalCycles) / float64(b.N)

			// Store single core cycles for efficiency calculation
			if coreCount == 1 {
				singleCoreCycles = actualCyclesPerOp
			}

			// Calculate efficiency: (single_core_cycles / (actual_cycles * cores)) * 100
			efficiencyPct := 0.0
			if actualCyclesPerOp > 0 && singleCoreCycles > 0 {
				efficiencyPct = (singleCoreCycles / (actualCyclesPerOp * float64(coreCount))) * 100
			}

			// Calculate speedup
			speedup := 0.0
			if actualCyclesPerOp > 0 && singleCoreCycles > 0 {
				speedup = singleCoreCycles / actualCyclesPerOp
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
