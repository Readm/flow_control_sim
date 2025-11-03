package main

import (
	"flag"
)

func main() {
	var headless = flag.Bool("headless", false, "Run in headless mode (no GUI)")
	flag.Parse()

	cfg := &Config{
		NumMasters:          3,
		NumSlaves:           2,
		NumRelays:           1,
		TotalCycles:         1000,
		MasterRelayLatency:  2,
		RelayMasterLatency:  2,
		RelaySlaveLatency:   1,
		SlaveRelayLatency:   1,
		SlaveProcessRate:    2,
		RequestRate:         0.3,
		SlaveWeights:        []int{1, 1},
		Headless:            *headless,
		VisualMode:          "gui",
	}

	sim := NewSimulator(cfg)

	if !cfg.Headless && cfg.VisualMode == "gui" {
		// Run simulator in a goroutine
		go func() {
			sim.Run()
			// Close visualization window when simulation completes
			if fviz, ok := sim.visualizer.(*FyneVisualizer); ok {
				fviz.Close()
			}
		}()

		// Run GUI (blocking)
		if fviz, ok := sim.visualizer.(*FyneVisualizer); ok {
			fviz.ShowAndRun()
		}
	} else {
		// Headless mode - just run the simulation
		sim.Run()

		// Print statistics
		stats := sim.CollectStats()
		if stats != nil {
			PrintStats(stats)
		}
	}
}

