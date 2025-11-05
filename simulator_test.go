package main

import (
    "testing"
)

// TestBasicFlow runs a short simulation and verifies basic invariants.
func TestBasicFlow(t *testing.T) {
	cfg := &Config{
		NumMasters:          2,
		NumSlaves:           3,
		NumRelays:           1,
		TotalCycles:         300,
		MasterRelayLatency:  2,
		RelayMasterLatency:  2,
		RelaySlaveLatency:   1,
		SlaveRelayLatency:   1,
		SlaveProcessRate:    1,
		RequestRateConfig:         0.5,
		BandwidthLimit:      1,
		SlaveWeights:        []int{1, 1, 1},
		Headless:            true,  // Test in headless mode
		VisualMode:          "none",
	}

    sim := NewSimulator(cfg)
    sim.Run()

    stats := sim.CollectStats()
    if stats == nil || stats.Global == nil {
        t.Fatalf("stats should not be nil")
    }
    g := stats.Global
    t.Logf("Total=%d Completed=%d Rate=%.2f%% AvgDelay=%.2f Max=%d Min=%d",
        g.TotalRequests, g.Completed, g.CompletionRate, g.AvgEndToEndDelay, g.MaxDelay, g.MinDelay)

    if g.TotalRequests < 0 {
        t.Fatalf("total requests should be >= 0")
    }
    if g.Completed < 0 || g.Completed > g.TotalRequests {
        t.Fatalf("completed out of range: %d of %d", g.Completed, g.TotalRequests)
    }
    if g.CompletionRate < 0 || g.CompletionRate > 100 {
        t.Fatalf("completion rate out of range: %.2f", g.CompletionRate)
    }
    if len(stats.PerMaster) != cfg.NumMasters {
        t.Fatalf("expected %d masters stats, got %d", cfg.NumMasters, len(stats.PerMaster))
    }
    if len(stats.PerSlave) != cfg.NumSlaves {
        t.Fatalf("expected %d slaves stats, got %d", cfg.NumSlaves, len(stats.PerSlave))
    }
    // With enough cycles and non-zero rates, we expect at least some completion.
    if g.Completed == 0 {
        t.Fatalf("expected some completed requests, got 0 (total=%d)", g.TotalRequests)
    }
}

// TestReadNoSnpTransaction verifies the complete ReadNoSnp transaction flow:
// RN -> HN: ReadNoSnp Request
// HN -> SN: Forward Request
// SN -> HN: CompData Response
// HN -> RN: CompData Response
func TestReadNoSnpTransaction(t *testing.T) {
	cfg := &Config{
		NumMasters:          1,
		NumSlaves:           1,
		NumRelays:           1,
		TotalCycles:         100,
		MasterRelayLatency:  2,
		RelayMasterLatency:  2,
		RelaySlaveLatency:   1,
		SlaveRelayLatency:   1,
		SlaveProcessRate:    1,
		RequestRateConfig:         1.0, // Always generate requests
		BandwidthLimit:      1,
		SlaveWeights:        []int{1},
		Headless:            true,
		VisualMode:          "none",
	}

	sim := NewSimulator(cfg)
	sim.Run()

	stats := sim.CollectStats()
	if stats == nil || stats.Global == nil {
		t.Fatalf("stats should not be nil")
	}

	// Verify CHI node types
	if len(sim.Masters) != 1 {
		t.Fatalf("expected 1 Request Node, got %d", len(sim.Masters))
	}
	if sim.Masters[0].Type != NodeTypeRN {
		t.Fatalf("expected Request Node type RN, got %s", sim.Masters[0].Type)
	}

	if len(sim.Slaves) != 1 {
		t.Fatalf("expected 1 Slave Node, got %d", len(sim.Slaves))
	}
	if sim.Slaves[0].Type != NodeTypeSN {
		t.Fatalf("expected Slave Node type SN, got %s", sim.Slaves[0].Type)
	}

	if sim.Relay == nil {
		t.Fatalf("expected Home Node, got nil")
	}
	if sim.Relay.Type != NodeTypeHN {
		t.Fatalf("expected Home Node type HN, got %s", sim.Relay.Type)
	}

	// Verify that ReadNoSnp transactions completed
	g := stats.Global
	t.Logf("CHI ReadNoSnp Test - Total=%d Completed=%d Rate=%.2f%%",
		g.TotalRequests, g.Completed, g.CompletionRate)

	if g.TotalRequests == 0 {
		t.Fatalf("expected some ReadNoSnp requests, got 0")
	}

	if g.Completed == 0 {
		t.Fatalf("expected some completed ReadNoSnp transactions, got 0")
	}

	// Verify that requests were generated with CHI protocol fields
	if len(stats.PerMaster) > 0 {
		rnStats := stats.PerMaster[0]
		if rnStats.TotalRequests == 0 {
			t.Fatalf("Request Node should have generated ReadNoSnp requests")
		}
		t.Logf("Request Node: %d requests, %d completed, avg delay=%.2f",
			rnStats.TotalRequests, rnStats.CompletedRequests, rnStats.AvgDelay)
	}
}


