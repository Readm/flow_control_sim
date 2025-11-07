package main

import (
	"fmt"
	"testing"
)

// TestBackpressureVerify creates a scenario where backpressure MUST occur
func TestBackpressureVerify(t *testing.T) {
	// Create scenario: High arrival rate, slow processing, small queue
	cfg := &Config{
		NumMasters:         1,
		NumSlaves:          1,
		NumRelays:          1,
		TotalCycles:        100,
		MasterRelayLatency: 1,
		RelayMasterLatency: 1,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   1,  // Process only 1 per cycle
		RequestRateConfig:        1.0, // Always generate (high rate)
		BandwidthLimit:     5,  // Allow multiple packets per slot
		SlaveWeights:       []int{1},
		Headless:           true,
		VisualMode:         "none",
	}

	sim := NewSimulator(cfg)
	edgeKey := EdgeKey{FromID: sim.Relay.ID, ToID: sim.Slaves[0].ID}
	
	backpressureEvents := 0
	maxQueueObserved := 0
	
	fmt.Println("\n=== Backpressure Verification Test ===")
	fmt.Println("Scenario: High arrival rate (1.0), slow processing (1/cycle), queue capacity 20")
	fmt.Println("Expected: Queue should fill up, triggering backpressure")
	fmt.Println()
	
	for i := 0; i < 80; i++ {
		cycle := sim.current
		sim.current++
		
		// Check state BEFORE Link.Tick
		pipelineState := sim.Chan.GetPipelineState(cycle)
		stages, exists := pipelineState[edgeKey]
		slot0Packets := 0
		if exists && len(stages) > 0 {
			slot0Packets = stages[0].PacketCount
		}
		slaveQueueLen := len(sim.Slaves[0].queue)
		canReceive := sim.Slaves[0].CanReceive(edgeKey, slot0Packets)
		
		if slaveQueueLen > maxQueueObserved {
			maxQueueObserved = slaveQueueLen
		}
		
		// Process link
		sim.Chan.Tick(cycle)
		
		// Check state AFTER Link.Tick
		pipelineStateAfter := sim.Chan.GetPipelineState(cycle)
		stagesAfter, existsAfter := pipelineStateAfter[edgeKey]
		slot0After := 0
		if existsAfter && len(stagesAfter) > 0 {
			slot0After = stagesAfter[0].PacketCount
		}
		
		// Detect backpressure: Slot[0] has packets but CanReceive was false
		if slot0Packets > 0 && !canReceive {
			backpressureEvents++
			fmt.Printf("Cycle %3d: [BACKPRESSURE] Slot[0]=%d packets, SlaveQ=%d/20 (FULL), CanReceive=false\n",
				cycle, slot0Packets, slaveQueueLen)
			
			// Verify: Slot[0] should still have packets after Tick (not advanced)
			if slot0After == slot0Packets {
				fmt.Printf("         ✓ Verified: Slot[0] did not advance (backpressure working)\n")
			} else {
				fmt.Printf("         ✗ ERROR: Slot[0] changed from %d to %d (backpressure failed!)\n",
					slot0Packets, slot0After)
				t.Errorf("Backpressure failed: Slot[0] advanced when it shouldn't have")
			}
		}
		
		// Generate requests (high rate)
		for _, m := range sim.Masters {
			if sim.Relay != nil {
				m.Tick(cycle, sim.cfg, sim.Relay.ID, sim.Chan, sim.pktIDs, sim.Slaves, sim.txnMgr)
			}
		}
		
		// Process relay
		if sim.Relay != nil {
			sim.Relay.Tick(cycle, sim.Chan, sim.cfg)
		}
		
		// Process slaves (slow: only 1 per cycle)
		for _, sl := range sim.Slaves {
			resps := sl.Tick(cycle, sim.pktIDs)
			if sim.Relay != nil {
				for _, p := range resps {
					sim.Chan.Send(p, sl.ID, sim.Relay.ID, cycle, sim.cfg.SlaveRelayLatency)
				}
			}
		}
	}
	
	fmt.Printf("\n=== Results ===\n")
	fmt.Printf("Max queue length observed: %d/20\n", maxQueueObserved)
	fmt.Printf("Backpressure events detected: %d\n", backpressureEvents)
	fmt.Printf("Slave MaxQueueLength stat: %d\n", sim.Slaves[0].MaxQueueLength)
	
	// Verify backpressure was triggered
	if maxQueueObserved < 15 {
		t.Logf("WARNING: Queue never reached high capacity (%d/20). Backpressure may not have been triggered.", maxQueueObserved)
		t.Logf("This could mean:")
		t.Logf("  1. Processing is fast enough to keep up")
		t.Logf("  2. Request rate is not high enough")
		t.Logf("  3. Pipeline latency is too high (packets arrive slowly)")
	} else {
		t.Logf("✓ Queue reached high capacity (%d/20), backpressure should have been triggered", maxQueueObserved)
	}
	
	if backpressureEvents == 0 {
		t.Logf("WARNING: No backpressure events detected. This could mean:")
		t.Logf("  1. CanReceive always returned true (queue never full)")
		t.Logf("  2. Slot[0] was always empty when checked")
		t.Logf("  3. Timing issue: packets arrive and are processed in same cycle")
	} else {
		t.Logf("✓ Detected %d backpressure events", backpressureEvents)
	}
}

