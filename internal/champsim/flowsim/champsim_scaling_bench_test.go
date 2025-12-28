package flowsim

import (
	"flag"
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

var (
	benchCPUs   = flag.Int("bench_cpus", 4, "Number of CPUs for ChampSim benchmark")
	benchCycles = flag.Int("bench_cycles", 5000, "Number of cycles to simulate")
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

// buildQuadCoreSystem builds a complete ChampSim system with configurable CPU count
// Returns network and handlers for cleanup
func buildQuadCoreSystem(numCPUs int, traceFile string) (*network.Network, *SystemHandlers, error) {
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

// BenchmarkChampSimCoreScaling benchmarks ChampSim simulation with varying CPU core counts.
// Measures the simulation efficiency (how well we utilize available CPU resources).
//
// This benchmark uses real-time cycle counting instead of calibration for accurate measurement.
//
// Key metrics:
//   - efficiency_pct: Simulation time utilization efficiency
//   - avg_work_per_sim_cycle: Average CPU cycles per simulation cycle (measured)
//   - actual_cycles/op: Real CPU cycles consumed
//   - ideal_cycles_per_core/op: Ideal CPU cycles if perfectly parallelized
//
// Usage:
//
//	go test -bench=BenchmarkChampSimCoreScaling -benchmem -bench_cpus=8 -bench_cycles=5000
func BenchmarkChampSimCoreScaling(b *testing.B) {
	numSimCPUs := *benchCPUs          // Number of simulated CPUs
	maxCycles := uint64(*benchCycles) // Number of simulation cycles
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

	// Generate core count samples: 1, 2, 4, 8... up to NumCPU
	var coreCountSamples []int
	for i := 1; i <= numPhysicalCPU; i *= 2 {
		coreCountSamples = append(coreCountSamples, i)
	}
	if coreCountSamples[len(coreCountSamples)-1] != numPhysicalCPU {
		coreCountSamples = append(coreCountSamples, numPhysicalCPU)
	}

	b.Logf("Testing ChampSim: %d simulated CPUs, %d sim cycles", numSimCPUs, maxCycles)
	b.Logf("Physical core counts: %v (NumCPU=%d)", coreCountSamples, numPhysicalCPU)

	for _, coreCount := range coreCountSamples {
		b.Run(fmt.Sprintf("Cores_%d", coreCount), func(b *testing.B) {
			// Set GOMAXPROCS for this benchmark
			oldMaxProcs := runtime.GOMAXPROCS(coreCount)
			defer runtime.GOMAXPROCS(oldMaxProcs)

			// Keep simulated CPUs fixed to measure framework efficiency
			// The efficiency shows how well the framework parallelizes
			// a FIXED simulation workload across varying core counts
			scaledSimCPUs := numSimCPUs

			// Reset timer before actual benchmark
			b.ResetTimer()

			var totalCycles uint64

			// Run benchmark and accumulate actual cycles
			for iteration := 0; iteration < b.N; iteration++ {
				iterStart := node.GetCPUCycles()
				runChampSimBenchmark(b, scaledSimCPUs, maxCycles, traceFile)
				iterEnd := node.GetCPUCycles()
				totalCycles += (iterEnd - iterStart)
			}

			b.StopTimer()

			// Calculate efficiency metrics (matching network_perf_profile_test.go)

			// 1. Actual CPU cycles used per operation (measured directly)
			actualCyclesPerOp := float64(totalCycles) / float64(b.N)

			// 2. Average work per simulation cycle (from actual measurements)
			//    actualCyclesPerOp = scaledSimCPUs * maxCycles * avgSimWorkCycles
			avgSimWorkCycles := actualCyclesPerOp / (float64(scaledSimCPUs) * float64(maxCycles))

			// 3. Total simulated work per operation
			simWorkPerOpCycles := float64(scaledSimCPUs) * float64(maxCycles) * avgSimWorkCycles

			// 4. Ideal cycles per core (if perfectly parallelized)
			simWorkPerCoreCycles := simWorkPerOpCycles / float64(coreCount)

			// 5. Efficiency: (Ideal Cycles / Actual Cycles) * 100
			efficiencyPct := 0.0
			if actualCyclesPerOp > 0 {
				efficiencyPct = (simWorkPerCoreCycles / actualCyclesPerOp) * 100
			}

			// Report metrics (matching network_perf_profile_test.go format)
			b.ReportMetric(float64(scaledSimCPUs), "sim_cpus")
			b.ReportMetric(avgSimWorkCycles, "avg_work_per_sim_cycle")
			b.ReportMetric(simWorkPerOpCycles, "ideal_sim_work_cycles/op")
			b.ReportMetric(simWorkPerCoreCycles, "ideal_cycles_per_core/op")
			b.ReportMetric(actualCyclesPerOp, "actual_cycles/op")
			b.ReportMetric(efficiencyPct, "efficiency_pct")
		})
	}
}

// runChampSimBenchmark runs ChampSim simulation using AdvanceTo (standard framework usage)
func runChampSimBenchmark(b *testing.B, numCPUs int, maxCycles uint64, traceFile string) {
	net, handlers, err := buildQuadCoreSystem(numCPUs, traceFile)
	if err != nil {
		b.Fatalf("Failed to build system: %v", err)
	}
	defer handlers.Cleanup()

	// Run simulation using AdvanceTo (standard framework usage)
	if err := net.AdvanceTo(int(maxCycles - 1)); err != nil {
		b.Fatalf("Simulation failed: %v", err)
	}
}

// Benchmark_ChampSim_SystemScaling tests system scaling with fixed physical cores
// This shows framework's ability to handle increasing system complexity
func Benchmark_ChampSim_SystemScaling(b *testing.B) {
	maxCycles := uint64(2000)
	traceFile := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"

	// Check trace availability
	testReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		b.Skipf("Trace file not available: %v", err)
	}
	testReader.Close()

	// Use all available cores
	runtime.GOMAXPROCS(runtime.NumCPU())
	cyclesPerUS := node.CalibrateCyclesPerUS(100 * time.Millisecond)
	b.Logf("Using all %d physical cores", runtime.NumCPU())
	b.Logf("Calibrated CPU Frequency: %.2f GHz", cyclesPerUS/1000.0)

	testCases := []struct {
		name    string
		numCPUs int
	}{
		{"CPUs_2", 2},
		{"CPUs_4", 4},
		{"CPUs_8", 8},
		{"CPUs_16", 16},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			var totalCycles uint64

			for i := 0; i < b.N; i++ {
				iterStart := node.GetCPUCycles()
				runChampSimBenchmark(b, tc.numCPUs, maxCycles, traceFile)
				iterEnd := node.GetCPUCycles()
				totalCycles += (iterEnd - iterStart)
			}

			b.StopTimer()

			actualCyclesPerOp := float64(totalCycles) / float64(b.N)
			nodeCount := tc.numCPUs + 4 // CPUs + L2 + MemCtrl + 2 DRAM

			b.ReportMetric(float64(tc.numCPUs), "sim_cpus")
			b.ReportMetric(float64(nodeCount), "total_nodes")
			b.ReportMetric(actualCyclesPerOp, "actual_cycles/op")
		})
	}
}
