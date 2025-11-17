package app

import (
	"testing"
)

// TestBackpressureVerify creates a scenario where backpressure MUST occur
func TestBackpressureVerify(t *testing.T) {
	cfg := &Config{
		NumMasters:         1,
		NumSlaves:          1,
		NumRelays:          1,
		TotalCycles:        100,
		MasterRelayLatency: 1,
		RelayMasterLatency: 1,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   1,
		RequestRateConfig:  1.0,
		BandwidthLimit:     5,
		SlaveWeights:       []int{1},
		Headless:           true,
		VisualMode:         "none",
	}
	cfg.Graph = &GraphConfig{
		Nodes: []GraphNode{
			{ID: "rn0", Label: "Request 0", Capabilities: []string{"requester"}},
			{ID: "hn0", Label: "Home", Capabilities: []string{"home_directory"}},
			{ID: "sn0", Label: "Slave 0", Capabilities: []string{"slave_target"}},
		},
		Edges: []GraphEdge{
			{From: "rn0", To: "hn0", Latency: cfg.MasterRelayLatency, Bandwidth: cfg.BandwidthLimit},
			{From: "hn0", To: "rn0", Latency: cfg.RelayMasterLatency, Bandwidth: cfg.BandwidthLimit},
			{From: "hn0", To: "sn0", Latency: cfg.RelaySlaveLatency, Bandwidth: cfg.BandwidthLimit},
			{From: "sn0", To: "hn0", Latency: cfg.SlaveRelayLatency, Bandwidth: cfg.BandwidthLimit},
		},
	}

	sim := NewSimulator(cfg)
	sim.Run()

	if len(sim.Slaves) == 0 || sim.Relay == nil {
		t.Fatalf("expected at least one slave and relay")
	}

	edgeKey := EdgeKey{FromID: sim.Relay.ID, ToID: sim.Slaves[0].ID}
	history := sim.Chan.BackpressureHistory(edgeKey)
	hasBackpressure := false
	for _, bp := range history {
		if bp {
			hasBackpressure = true
			break
		}
	}

	if !hasBackpressure {
		t.Fatalf("expected backpressure events but none were recorded")
	}

	stats := sim.Slaves[0].SnapshotStats()
	if stats.MaxQueueLength < DefaultSlaveQueueCapacity {
		t.Fatalf("expected slave queue to fill, max observed %d", stats.MaxQueueLength)
	}
}
