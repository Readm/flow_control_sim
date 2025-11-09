package main

import "fmt"

// DebugBackpressure runs a simulation with detailed logging to verify backpressure
func DebugBackpressure() {
	cfg := &Config{
		NumMasters:         1,
		NumSlaves:          1,
		NumRelays:          1,
		TotalCycles:        120,
		MasterRelayLatency: 1,
		RelayMasterLatency: 1,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   0,
		RequestRateConfig:  1.0,
		BandwidthLimit:     2,
		SlaveWeights:       []int{1},
		Headless:           true,
		VisualMode:         "none",
	}

	sim := NewSimulator(cfg)

	fmt.Println("\n=== Backpressure Debug ===")
	fmt.Println("Configuration:")
	fmt.Printf("  SlaveProcessRate: %d (stalls processing to force backpressure)\n", cfg.SlaveProcessRate)
	fmt.Printf("  RelaySlaveLatency: %d\n", cfg.RelaySlaveLatency)
	fmt.Printf("  RequestRateConfig: %.1f\n", cfg.RequestRateConfig)

	fmt.Println("Running simulation...")
	sim.Run()

	fmt.Println("\nFinal State:")
	if len(sim.Slaves) > 0 {
		fmt.Printf("  Slave queue: %d/%d\n", len(sim.Slaves[0].queue), DefaultSlaveQueueCapacity)
		fmt.Printf("  Slave MaxQueueLength: %d\n", sim.Slaves[0].MaxQueueLength)
	}

	edgeKey := EdgeKey{FromID: sim.Relay.ID, ToID: sim.Slaves[0].ID}
	finalCycle := cfg.TotalCycles - 1
	if finalCycle < 0 {
		finalCycle = 0
	}
	pipelineState := sim.Chan.GetPipelineState(finalCycle)
	if stages, exists := pipelineState[edgeKey]; exists {
		fmt.Printf("  Pipeline stages at final cycle: ")
		for i, s := range stages {
			if s.PacketCount > 0 {
				fmt.Printf("Stage %d: %d packets (logicCycle=%d) | ", i, s.PacketCount, s.LogicCycle)
			}
		}
		fmt.Println()
	} else {
		fmt.Println("  Pipeline stages: not found")
	}

	history := sim.Chan.BackpressureHistory(edgeKey)
	if len(history) == 0 {
		fmt.Println("  No backpressure recorded.")
		return
	}

	fmt.Println("  Backpressure detected at cycles:")
	for cycle := 0; cycle < cfg.TotalCycles; cycle++ {
		if history[cycle] {
			fmt.Printf("    Cycle %d\n", cycle)
		}
	}
}
