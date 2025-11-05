package main

import (
	"fmt"
	"time"
)

// BenchmarkResult stores performance test results
type BenchmarkResult struct {
	TotalCycles    int
	TotalDuration  time.Duration
	CyclesPerSec   float64
	DurationPerCycle time.Duration
}

// RunBenchmark runs a performance test in headless mode
func RunBenchmark(testCycles int, cfg *Config) *BenchmarkResult {
	// Override config for benchmark
	cfg.Headless = true
	cfg.VisualMode = "none"
	cfg.TotalCycles = testCycles

	// Create simulator
	sim := NewSimulator(cfg)

	// Measure execution time
	startTime := time.Now()
	sim.Run()
	duration := time.Since(startTime)

	// Calculate performance metrics
	cyclesPerSec := float64(testCycles) / duration.Seconds()
	durationPerCycle := duration / time.Duration(testCycles)

	return &BenchmarkResult{
		TotalCycles:      testCycles,
		TotalDuration:    duration,
		CyclesPerSec:     cyclesPerSec,
		DurationPerCycle: durationPerCycle,
	}
}

// RunBenchmarkSuite runs multiple benchmark tests with different configurations
func RunBenchmarkSuite() {
	fmt.Println("=== Headless Mode Performance Benchmark ===")
	fmt.Println()

	// Test configuration
	baseCfg := &Config{
		NumMasters:         3,
		NumSlaves:          2,
		NumRelays:          1,
		MasterRelayLatency: 2,
		RelayMasterLatency: 2,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   1,
		RequestRateConfig:        0.8,
		SlaveWeights:       []int{1, 1},
		Headless:           true,
		VisualMode:         "none",
	}

	// Test with different cycle counts to get accurate measurements
	testSizes := []int{10000, 50000, 100000}
	iterations := 3 // Run each test multiple times and average

	for _, cycles := range testSizes {
		fmt.Printf("Testing with %d cycles (running %d iterations)...\n", cycles, iterations)
		
		var totalCyclesPerSec float64
		var totalDuration time.Duration

		for i := 0; i < iterations; i++ {
			result := RunBenchmark(cycles, baseCfg)
			totalCyclesPerSec += result.CyclesPerSec
			totalDuration += result.TotalDuration
		}

		avgCyclesPerSec := totalCyclesPerSec / float64(iterations)
		avgDuration := totalDuration / time.Duration(iterations)

		fmt.Printf("  Average: %.2f cycles/sec\n", avgCyclesPerSec)
		fmt.Printf("  Average time: %v\n", avgDuration)
		fmt.Printf("  Average time per cycle: %.2f μs\n", float64(avgDuration.Nanoseconds())/float64(cycles)/1000.0)
		fmt.Println()
	}

	// Final comprehensive test
	fmt.Println("Running comprehensive test (1,000,000 cycles)...")
	finalResult := RunBenchmark(1000000, baseCfg)
	fmt.Printf("Result: %.2f cycles/sec\n", finalResult.CyclesPerSec)
	fmt.Printf("Total time: %v\n", finalResult.TotalDuration)
	fmt.Printf("Time per cycle: %.2f μs\n", float64(finalResult.DurationPerCycle.Nanoseconds())/1000.0)
}

