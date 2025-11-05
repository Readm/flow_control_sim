package main

import (
	"flag"
	"fmt"
	"time"
)

func main() {
	var headless = flag.Bool("headless", false, "Run in headless mode (no GUI)")
	var benchmark = flag.Bool("benchmark", false, "Run performance benchmark test")
	var configName = flag.String("config", "", "Predefined configuration name (e.g., 'backpressure_test', 'multi_master_multi_slave')")
	flag.Parse()

	// If benchmark mode, run benchmark suite
	if *benchmark {
		RunBenchmarkSuite()
		return
	}

	// Use predefined configuration
	configs := GetPredefinedConfigs()
	var cfg *Config
	
	// If config name is specified, use it; otherwise use first config
	selectedConfigName := *configName
	if selectedConfigName == "" && len(configs) > 0 {
		selectedConfigName = configs[0].Name
	}
	
	if selectedConfigName != "" {
		cfg = GetConfigByName(selectedConfigName)
		if cfg == nil {
			fmt.Printf("Warning: Configuration '%s' not found, using default\n", selectedConfigName)
		} else {
			// Override Headless and VisualMode based on flag
			cfg.Headless = *headless
			cfg.VisualMode = "web"
		}
	}
	
	if cfg == nil {
		// Fallback to default if GetConfigByName fails or no configs available
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
			RequestRateConfig:  0.8,
			BandwidthLimit:     1,
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
