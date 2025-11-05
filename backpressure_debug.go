package main

import (
	"fmt"
)

// DebugBackpressure runs a simulation with detailed logging to verify backpressure
func DebugBackpressure() {
	cfg := &Config{
		NumMasters:         1,
		NumSlaves:          1,
		NumRelays:          1,
		TotalCycles:        100,
		MasterRelayLatency: 2,
		RelayMasterLatency: 2,
		RelaySlaveLatency:  1,  // Latency 1 means packets arrive quickly
		SlaveRelayLatency:  1,
		SlaveProcessRate:   1,  // Process only 1 per cycle
		RequestRate:        1.0,
		BandwidthLimit:     1,
		SlaveWeights:       []int{1},
		Headless:           true,
		VisualMode:         "none",
	}

	sim := NewSimulator(cfg)
	
	fmt.Println("\n=== Backpressure Debug ===")
	fmt.Println("Configuration:")
	fmt.Printf("  SlaveProcessRate: %d (processes 1 packet per cycle)\n", cfg.SlaveProcessRate)
	fmt.Printf("  RelaySlaveLatency: %d (packets arrive after 1 cycle)\n", cfg.RelaySlaveLatency)
	fmt.Printf("  RequestRate: %.1f (always generates requests)\n", cfg.RequestRate)
	fmt.Println()
	
	edgeKey := EdgeKey{FromID: sim.Relay.ID, ToID: sim.Slaves[0].ID}
	
	for i := 0; i < 30; i++ {
		cycle := sim.current
		sim.current++
		
		// State BEFORE Channel.Tick
		pipelineState := sim.Chan.GetPipelineState(cycle)
		stages, _ := pipelineState[edgeKey]
		slot0Before := 0
		if len(stages) > 0 {
			slot0Before = stages[0].PacketCount
		}
		slaveQueueBefore := len(sim.Slaves[0].queue)
		canReceive := sim.Slaves[0].CanReceive(edgeKey, 1)
		
		// Process channel
		sim.Chan.Tick(cycle)
		
		// State AFTER Channel.Tick
		slaveQueueAfter := len(sim.Slaves[0].queue)
		pipelineStateAfter := sim.Chan.GetPipelineState(cycle)
		stagesAfter, _ := pipelineStateAfter[edgeKey]
		slot0After := 0
		if len(stagesAfter) > 0 {
			slot0After = stagesAfter[0].PacketCount
		}
		
		// Generate requests
		for _, m := range sim.Masters {
			if sim.Relay != nil {
				m.Tick(cycle, sim.cfg, sim.Relay.ID, sim.Chan, sim.rng, sim.pktIDs, sim.Slaves)
			}
		}
		
		// Process relay
		if sim.Relay != nil {
			sim.Relay.Tick(cycle, sim.Chan, sim.cfg)
		}
		
		// Process slaves BEFORE their Tick
		slaveQueueBeforeTick := len(sim.Slaves[0].queue)
		
		// Process slaves
		for _, sl := range sim.Slaves {
			resps := sl.Tick(cycle, sim.pktIDs)
			if sim.Relay != nil {
				for _, p := range resps {
					sim.Chan.Send(p, sl.ID, sim.Relay.ID, cycle, sim.cfg.SlaveRelayLatency)
				}
			}
		}
		
		slaveQueueAfterTick := len(sim.Slaves[0].queue)
		
		// Log interesting cycles
		if cycle >= 5 && (slot0Before > 0 || slaveQueueBefore > 0 || slaveQueueAfter > 0) {
			fmt.Printf("Cycle %2d: ", cycle)
			fmt.Printf("Slot[0]: %d->%d, ", slot0Before, slot0After)
			fmt.Printf("SlaveQ: %d->%d->%d, ", slaveQueueBefore, slaveQueueAfter, slaveQueueBeforeTick)
			fmt.Printf("AfterTick: %d, ", slaveQueueAfterTick)
			fmt.Printf("CanReceive: %v", canReceive)
			
			// Check for backpressure
			if slot0Before > 0 && !canReceive {
				fmt.Printf(" [BACKPRESSURE!]")
			} else if slot0Before > 0 && canReceive && slot0After == 0 && slaveQueueAfter > slaveQueueBefore {
				fmt.Printf(" [Received]")
			} else if slot0Before > 0 && canReceive && slot0After == slot0Before {
				fmt.Printf(" [Stuck?]")
			}
			fmt.Println()
		}
	}
	
	fmt.Println("\nFinal State:")
	fmt.Printf("  Slave queue: %d/20\n", len(sim.Slaves[0].queue))
	fmt.Printf("  Slave MaxQueueLength: %d\n", sim.Slaves[0].MaxQueueLength)
	
	// Check pipeline
	pipelineState := sim.Chan.GetPipelineState(30)
	stages, exists := pipelineState[edgeKey]
	if exists {
		fmt.Printf("  Pipeline stages: ")
		for i, s := range stages {
			if s.PacketCount > 0 {
				fmt.Printf("Slot[%d]=%d ", i, s.PacketCount)
			}
		}
		fmt.Println()
	}
	fmt.Println("=== End Debug ===")
	fmt.Println()
}

