package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/Readm/flow_sim/configs"
	app "github.com/Readm/flow_sim/framework/app"
)

func main() {
	app.SetConfigProvider(configs.Provider())

	var headless = flag.Bool("headless", false, "Run in headless mode (no GUI)")
	var benchmark = flag.Bool("benchmark", false, "Run performance benchmark test")
	var configName = flag.String("config", "", "Predefined configuration name (e.g., 'backpressure_test', 'multi_master_multi_slave')")
	var traceEvents = flag.Bool("trace-events", false, "Enable verbose packet event tracing for debugging")
	flag.Parse()

	// If benchmark mode, run benchmark suite
	if *benchmark {
		app.RunBenchmarkSuite()
		return
	}

	// Use predefined configuration
	available := app.GetPredefinedConfigs()
	var cfg *app.Config

	// If config name is specified, use it; otherwise use first config
	selectedConfigName := *configName
	if selectedConfigName == "" && len(available) > 0 {
		selectedConfigName = available[0].Name
	}

	if selectedConfigName != "" {
		cfg = app.GetConfigByName(selectedConfigName)
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
		cfg = &app.Config{
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

	if *traceEvents {
		app.SetPacketEventTrace(true)
		app.GetLogger().Infof("Packet event tracing enabled. Logs will include TRACE entries.")
	}

	sim := app.NewSimulator(cfg)

	if *headless {
		// Headless mode: run simulation and exit
		sim.Run()
		stats := sim.CollectStats()
		if stats != nil {
			app.PrintStats(stats)
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
