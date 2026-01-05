package benchmarks

import (
	"os"
	"runtime"
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/flowsim"
	"github.com/Readm/flow_sim/internal/champsim/trace"
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

		// var totalCycles uint64 // Metric unused
		var lastNet *network.Network

		// Run benchmark and accumulate actual cycles
		for iteration := 0; iteration < b.N; iteration++ {
			// Build system and warmup OUTSIDE of timing
			b.StopTimer()
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
			b.StartTimer()
			// iterStart := monitor.GetCPUCycles() // Metric unused
			if err := net.AdvanceTo(int(maxCycles - 1)); err != nil {
				handlers.Cleanup()
				b.Fatalf("Simulation failed: %v", err)
			}
			// iterEnd := monitor.GetCPUCycles()

			// totalCycles += (iterEnd - iterStart) // Metric unused
			lastNet = net

			// Cleanup after each iteration
			b.StopTimer()
			handlers.Cleanup()
			b.StartTimer()
		}

		// Calculate performance metrics
		// Calculate sim_Hz
		// b.N iterations, each advanced 'maxCycles' (minus 1, plus 1? Logic seems to use maxCycles-1)
		// In loop: AdvanceTo(maxCycles - 1).
		// AdvanceTo logic: if Current=0, AdvanceTo(999) simulates 999 cycles? Or 1000?
		// Usually inclusive/excessive. Let's assume approx maxCycles.
		// Actually, let's look at loop: totalCycles += (iterEnd - iterStart). No, that's host cycles.
		// Simulated cycles per iter: maxCycles (approx).

		elapsedSec := b.Elapsed().Seconds()
		if elapsedSec == 0 {
			elapsedSec = 1e-9
		}
		totalSimCycles := float64(b.N) * float64(maxCycles)
		simHz := totalSimCycles / elapsedSec

		b.ReportMetric(simHz, "sim_Hz")

		return lastNet
	})
}

func Benchmark_ChampSim_Baseline_1CPU(b *testing.B) {
	// 1. Trace finding logic (same as 64CPU)
	traceFile := os.Getenv("CHAMPSIM_TRACE")
	if traceFile == "" {
		largeTrace := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
		if _, err := os.Stat(largeTrace); err == nil {
			traceFile = largeTrace
		} else {
			traceFile = "../../testdata/traces/small.champsimtrace"
		}
	}

	var finalNet *network.Network
	// Capture statistics for the Baseline run
	baselineStats := network.CollectGlobalRuntimeStats()

	b.Run("SingleCore_Baseline", func(b *testing.B) {
		// Force single thread execution for baseline
		oldMaxProcs := runtime.GOMAXPROCS(1)
		defer runtime.GOMAXPROCS(oldMaxProcs)

		const maxCycles = 1000
		var totalCycles uint64
		var lastNet *network.Network

		for i := 0; i < b.N; i++ {
			// 2. Build system per iteration (Outside Timer)
			b.StopTimer()
			net, handlers, err := flowsim.BuildChampSimSingleCoreSystem(traceFile)
			if err != nil {
				b.Fatalf("Failed to build system: %v", err)
			}

			// Warmup trace readers
			for idx, reader := range handlers.TraceReaders {
				if err := reader.Warmup(); err != nil {
					handlers.Cleanup()
					b.Fatalf("Failed to warmup trace reader %d: %v", idx, err)
				}
			}

			// 3. Execution (Timed)
			b.StartTimer()
			if err := net.AdvanceTo(maxCycles); err != nil {
				handlers.Cleanup()
				b.Fatalf("Simulation failed: %v", err)
			}
			// iterDuration no longer needed here

			totalCycles += maxCycles
			lastNet = net

			// Cleanup (Outside Timer)
			b.StopTimer()
			handlers.Cleanup()
			b.StartTimer()
		}

		// Metric Calculation using standard b.Elapsed()
		elapsedSec := b.Elapsed().Seconds()
		if elapsedSec == 0 {
			elapsedSec = 1e-9 // Avoid div zero
		}
		totalSimCycles := float64(b.N) * float64(maxCycles)
		simHz := totalSimCycles / elapsedSec

		b.ReportMetric(simHz, "sim_Hz")

		// Capture for printing later
		finalNet = lastNet
	})

	// Print global stats only ONCE after benchmark completes
	if finalNet != nil {
		finalNet.PrintGlobalPerformanceSummary(&baselineStats)
	}
}
