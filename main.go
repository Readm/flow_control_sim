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

	// Use first predefined configuration as default
	configs := GetPredefinedConfigs()
	var cfg *Config
	if len(configs) > 0 {
		cfg = GetConfigByName(configs[0].Name)
		if cfg == nil {
			// Fallback to default if GetConfigByName fails
			cfg = &Config{
				NumMasters:         3,
				NumSlaves:          2,
				NumRelays:          1,
				TotalCycles:        1000,
				MasterRelayLatency: 2,
				RelayMasterLatency: 2,
				RelaySlaveLatency:  1,
				SlaveRelayLatency:  1,
				SlaveProcessRate:   1,
				RequestRate:        0.8,
				BandwidthLimit:     1,
				SlaveWeights:       []int{1, 1},
				Headless:           *headless,
				VisualMode:         "web",
			}
		} else {
			// Override Headless and VisualMode based on flag
			cfg.Headless = *headless
			cfg.VisualMode = "web"
		}
	} else {
		// Fallback if no predefined configs
		cfg = &Config{
			NumMasters:         3,
			NumSlaves:          2,
			NumRelays:          1,
			TotalCycles:        1000,
			MasterRelayLatency: 2,
			RelayMasterLatency: 2,
			RelaySlaveLatency:  1,
			SlaveRelayLatency:  1,
			SlaveProcessRate:   1,
			RequestRate:        0.8,
			SlaveWeights:       []int{1, 1},
			Headless:           *headless,
			VisualMode:         "web",
		}
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
