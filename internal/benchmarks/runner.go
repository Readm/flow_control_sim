package benchmarks

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/Readm/flow_sim/internal/core/network"
)

// RunScalingBenchmark provides a standardized harness for core scaling benchmarks.
// It iterates from 1 core up to the maximum available cores (power of 2 steps),
// sets GOMAXPROCS, runs the benchmark function, and prints global stats.
func RunScalingBenchmark(b *testing.B, name string, testFunc func(b *testing.B, coreCount int) *network.Network) {
	numCPU := runtime.NumCPU()

	// Generate sample points: 1, 2, 4, 8... up to NumCPU
	var coreCountSamples []int
	for i := 1; i < numCPU; i *= 2 {
		coreCountSamples = append(coreCountSamples, i)
	}
	// Ensure we include NumCPU
	coreCountSamples = append(coreCountSamples, numCPU)
	// Remove duplicates
	if len(coreCountSamples) > 1 && coreCountSamples[len(coreCountSamples)-1] == coreCountSamples[len(coreCountSamples)-2] {
		coreCountSamples = coreCountSamples[:len(coreCountSamples)-1]
	}

	fmt.Printf("Running Scaling Benchmark '%s' with core counts: %v\n", name, coreCountSamples)

	for _, coreCount := range coreCountSamples {
		var lastNet *network.Network
		// Force GC and return memory to OS
		debug.FreeOSMemory()
		// Capture stats start for this core count
		iterationStartStats := network.CollectGlobalRuntimeStats()

		// Check and start profiling if requested
		if profiler, err := StartProfiling(fmt.Sprintf("%s_Cores_%d", name, coreCount)); err == nil && profiler != nil {
			defer profiler.StopAndAnalyze()
		} else if err != nil {
			b.Logf("Failed to start profiling: %v", err)
		}

		b.Run(fmt.Sprintf("Cores_%d", coreCount), func(b *testing.B) {
			// Set GOMAXPROCS for this benchmark
			oldMaxProcs := runtime.GOMAXPROCS(coreCount)
			defer runtime.GOMAXPROCS(oldMaxProcs)

			// Run the actual benchmark logic
			net := testFunc(b, coreCount)

			// Capture the network from the latest iteration
			lastNet = net
		})

		// Print global stats after the sub-benchmark finishes (only once per core count)
		if lastNet != nil {
			lastNet.PrintGlobalPerformanceSummary(&iterationStartStats)
		}
	}
}
