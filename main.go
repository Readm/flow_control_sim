package main

import (
	"flag"
	"time"
)

func main() {
	var headless = flag.Bool("headless", false, "Run in headless mode (no GUI)")
	flag.Parse()

	cfg := &Config{
		NumMasters:         3,
		NumSlaves:          2,
		NumRelays:          1,
		TotalCycles:        1000,
		MasterRelayLatency: 2,
		RelayMasterLatency: 2,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   2,
		RequestRate:        0.3,
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
