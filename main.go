package main

import (
	"flag"
	"time"
)

func main() {
	var headless = flag.Bool("headless", false, "Run in headless mode (no GUI)")
	var benchmark = flag.Bool("benchmark", false, "Run performance benchmark test")
	flag.Parse()

	// If benchmark mode, run benchmark suite
	if *benchmark {
		RunBenchmarkSuite()
		return
	}

	cfg := &Config{
		NumMasters:         3,
		NumSlaves:          2,
		NumRelays:          1,
		TotalCycles:        1000,
		MasterRelayLatency: 2,
		RelayMasterLatency: 2,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   1,   // Reduced to create higher load
		RequestRate:        0.8, // Increased to generate more requests
		SlaveWeights:       []int{1, 1},
		Headless:           *headless,
		VisualMode:         "web",
	}

	sim := NewSimulator(cfg)

	if *headless {
		// Headless mode: run simulation and exit
		sim.Run()
		stats := sim.CollectStats()
		if stats != nil {
			PrintStats(stats)
		}
	} else {
		// Web mode: run simulation in goroutine and keep server alive
		go sim.Run()

		// Keep main thread alive to serve HTTP requests
		// The server is started by WebVisualizer
		for {
			time.Sleep(1 * time.Second)
		}
	}
}
