package benchmarks

import (
	"os"
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/flowsim"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	"github.com/Readm/flow_sim/internal/core/monitor"
	"github.com/Readm/flow_sim/internal/core/network"
)

// Benchmark_ChampSim_64CPU benchmarks 64-CPU ChampSim system with varying physical core counts
func Benchmark_ChampSim_64CPU(b *testing.B) {
	RunScalingBenchmark(b, "ChampSim_64CPU", func(b *testing.B, coreCount int) *network.Network {
		const numSimCPUs = 64
		const maxCycles = 1000

		// Use environment variable if provided, otherwise fallback to repo's small trace
		traceFile := os.Getenv("CHAMPSIM_TRACE")
		if traceFile == "" {
			// Check if large trace exists locally (not in git)
			largeTrace := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
			if _, err := os.Stat(largeTrace); err == nil {
				traceFile = largeTrace
			} else {
				// Fallback to the small trace snippet provided for CI
				// Note: Adjusted path relative to internal/benchmarks
				traceFile = "../../testdata/traces/small.champsimtrace"
			}
		}

		// Check if trace file is available
		testReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
		if err != nil {
			b.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE=%s)", err, traceFile)
		}
		testReader.Close()

		const cpusPerL2 = 2
		const l2sPerL3 = 4
		const numL3s = 8 // 8
		const numMemCtrls = 8
		const numDRAMs = 8
		const numRingRouters = 16

		var totalCycles uint64
		var lastNet *network.Network

		// Run benchmark and accumulate actual cycles
		for iteration := 0; iteration < b.N; iteration++ {
			// Build system and warmup OUTSIDE of timing
			net, handlers, err := flowsim.BuildChampSimSystem(numSimCPUs, traceFile)
			if err != nil {
				b.Fatalf("Failed to build system: %v", err)
			}

			// Warmup trace readers
			for i, reader := range handlers.TraceReaders {
				if err := reader.Warmup(); err != nil {
					handlers.Cleanup()
					b.Fatalf("Failed to warmup trace reader %d: %v", i, err)
				}
			}

			// Start timing ONLY for simulation execution
			iterStart := monitor.GetCPUCycles()
			if err := net.AdvanceTo(int(maxCycles - 1)); err != nil {
				handlers.Cleanup()
				b.Fatalf("Simulation failed: %v", err)
			}
			iterEnd := monitor.GetCPUCycles()

			totalCycles += (iterEnd - iterStart)
			lastNet = net

			// Cleanup after each iteration
			handlers.Cleanup()
		}

		// Calculate performance metrics
		actualCyclesPerOp := float64(totalCycles) / float64(b.N)

		// NOTE: In this new structure, calculating exact "Speedup" relative to Core_1 is harder
		// because we are inside a specific Core_X run.
		// But we can report Efficiency if we knew SingleCoreCycles.
		// For now, let's just report ActualCycles/Op which is the raw truth.
		// The user can compare the numbers.

		b.ReportMetric(actualCyclesPerOp, "actual_cycles/op")

		return lastNet
	})
}
