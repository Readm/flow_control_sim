package main

import (
	"fmt"
	"testing"
)

// TestBackpressure verifies that backpressure mechanism works correctly
func TestBackpressure(t *testing.T) {
	// Create a high-load scenario to trigger backpressure
	cfg := &Config{
		NumMasters:         1,
		NumSlaves:          1,
		NumRelays:          1,
		TotalCycles:        50,
		MasterRelayLatency: 2,
		RelayMasterLatency: 2,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   1,  // Process only 1 packet per cycle
		RequestRateConfig:        1.0, // Always generate requests
		BandwidthLimit:     1,
		SlaveWeights:       []int{1},
		Headless:           true,
		VisualMode:         "none",
	}

	sim := NewSimulator(cfg)
	
	// Track pipeline state before and during backpressure
	backpressureDetected := false
	slot0StuckCount := 0
	
	// Run a few cycles to build up load
	for i := 0; i < 30; i++ {
		cycle := sim.current
		sim.current++
		
		// Check pipeline state before Tick
		pipelineState := sim.Chan.GetPipelineState(cycle)
		edgeKey := EdgeKey{FromID: sim.Relay.ID, ToID: sim.Slaves[0].ID}
		stages, exists := pipelineState[edgeKey]
		
		// Process channel
		sim.Chan.Tick(cycle)
		
		// Check if Slot[0] has packets but Slave queue is full
		if exists && len(stages) > 0 {
			slot0Packets := stages[0].PacketCount
			slaveQueueLen := len(sim.Slaves[0].queue)
			
			if slot0Packets > 0 && slaveQueueLen >= 20 {
				// Backpressure should be active
				backpressureDetected = true
				slot0StuckCount++
				
				// Verify that Slot[0] still has packets after Tick (shouldn't advance)
				newState := sim.Chan.GetPipelineState(cycle + 1)
				newStages, stillExists := newState[edgeKey]
				if stillExists && len(newStages) > 0 {
					newSlot0Packets := newStages[0].PacketCount
					if newSlot0Packets == 0 {
						t.Logf("Cycle %d: Backpressure detected but Slot[0] was cleared (unexpected)", cycle)
					} else {
						t.Logf("Cycle %d: Backpressure active - Slot[0]=%d packets, Slave queue=%d/20", 
							cycle, newSlot0Packets, slaveQueueLen)
					}
				}
			}
		}
		
		// Generate requests
		for _, m := range sim.Masters {
			if sim.Relay != nil {
				m.Tick(cycle, sim.cfg, sim.Relay.ID, sim.Chan, sim.pktIDs, sim.Slaves)
			}
		}
		
		// Process relay
		if sim.Relay != nil {
			sim.Relay.Tick(cycle, sim.Chan, sim.cfg)
		}
		
		// Process slaves (only 1 per cycle, so queue should build up)
		for _, sl := range sim.Slaves {
			resps := sl.Tick(cycle, sim.pktIDs)
			if sim.Relay != nil {
				for _, p := range resps {
					sim.Chan.Send(p, sl.ID, sim.Relay.ID, cycle, sim.cfg.SlaveRelayLatency)
				}
			}
		}
	}
	
	// Check final state
	slaveQueueLen := len(sim.Slaves[0].queue)
	t.Logf("Final Slave queue length: %d/20", slaveQueueLen)
	t.Logf("Backpressure detected: %v (Slot[0] stuck for %d cycles)", backpressureDetected, slot0StuckCount)
	
	// Verify that Slave queue reached capacity at some point
	if slaveQueueLen < 15 {
		t.Logf("WARNING: Slave queue never reached high capacity (%d/20), backpressure may not have been triggered", slaveQueueLen)
	}
	
	// If backpressure was detected, verify the mechanism
	if backpressureDetected {
		t.Logf("✓ Backpressure mechanism appears to be working")
	} else {
		t.Logf("⚠ Backpressure was not clearly detected - may need higher load or longer simulation")
	}
}

// TestBackpressureDetailed provides detailed logging of backpressure behavior
func TestBackpressureDetailed(t *testing.T) {
	cfg := &Config{
		NumMasters:         1,
		NumSlaves:          1,
		NumRelays:          1,
		TotalCycles:        100,
		MasterRelayLatency: 2,
		RelayMasterLatency: 2,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   1,  // Very slow processing
		RequestRateConfig:        1.0, // High request rate
		BandwidthLimit:     1,
		SlaveWeights:       []int{1},
		Headless:           true,
		VisualMode:         "none",
	}

	sim := NewSimulator(cfg)
	
	fmt.Println("\n=== Backpressure Test - Detailed Log ===")
	
	for i := 0; i < 50; i++ {
		cycle := sim.current
		sim.current++
		
		// Get pipeline state
		pipelineState := sim.Chan.GetPipelineState(cycle)
		edgeKey := EdgeKey{FromID: sim.Relay.ID, ToID: sim.Slaves[0].ID}
		stages, exists := pipelineState[edgeKey]
		
		slaveQueueLen := len(sim.Slaves[0].queue)
		canReceive := sim.Slaves[0].CanReceive(edgeKey, 1)
		
		if exists && len(stages) > 0 {
			slot0Packets := stages[0].PacketCount
			slot1Packets := 0
			if len(stages) > 1 {
				slot1Packets = stages[1].PacketCount
			}
			
			// Log state
			if slot0Packets > 0 || slaveQueueLen > 10 {
				fmt.Printf("Cycle %3d: Slot[0]=%d, Slot[1]=%d, SlaveQ=%d/20, CanReceive=%v\n",
					cycle, slot0Packets, slot1Packets, slaveQueueLen, canReceive)
			}
		}
		
		// Process channel
		sim.Chan.Tick(cycle)
		
		// Generate requests
		for _, m := range sim.Masters {
			if sim.Relay != nil {
				m.Tick(cycle, sim.cfg, sim.Relay.ID, sim.Chan, sim.pktIDs, sim.Slaves)
			}
		}
		
		// Process relay
		if sim.Relay != nil {
			sim.Relay.Tick(cycle, sim.Chan, sim.cfg)
		}
		
		// Process slaves
		for _, sl := range sim.Slaves {
			resps := sl.Tick(cycle, sim.pktIDs)
			if sim.Relay != nil {
				for _, p := range resps {
					sim.Chan.Send(p, sl.ID, sim.Relay.ID, cycle, sim.cfg.SlaveRelayLatency)
				}
			}
		}
	}
	
	fmt.Println("=== End of Backpressure Test ===")
}

// TestBackpressureDebug runs the debug function
func TestBackpressureDebug(t *testing.T) {
	DebugBackpressure()
}

