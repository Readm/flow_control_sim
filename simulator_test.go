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
		RequestRate:         0.5,
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


