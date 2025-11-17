package app

import (
	"testing"
	"time"
)

func runSimulatorWithTimeout(t *testing.T, cfg *Config, timeout time.Duration) *Simulator {
	t.Helper()

	sim := NewSimulator(cfg)
	done := make(chan struct{})

	go func() {
		sim.Run()
		close(done)
	}()

	select {
	case <-done:
		return sim
	case <-time.After(timeout):
		target, max, progress := sim.coordinator.SnapshotProgress()
		t.Fatalf("simulator run exceeded timeout %s (target=%d max=%d progress=%v)", timeout, target, max, progress)
	}

	return sim
}

func saturatedConfig(totalCycles int) *Config {
	cfg := &Config{
		NumMasters:         1,
		NumSlaves:          1,
		NumRelays:          1,
		TotalCycles:        totalCycles,
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
	cfg.Graph = buildSingleRelayGraph(cfg)
	return cfg
}

func responsiveConfig(totalCycles int) *Config {
	cfg := &Config{
		NumMasters:         1,
		NumSlaves:          1,
		NumRelays:          1,
		TotalCycles:        totalCycles,
		MasterRelayLatency: 1,
		RelayMasterLatency: 1,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   4,
		RequestRateConfig:  0.4,
		BandwidthLimit:     1,
		SlaveWeights:       []int{1},
		Headless:           true,
		VisualMode:         "none",
	}
	cfg.Graph = buildSingleRelayGraph(cfg)
	return cfg
}

func buildSingleRelayGraph(cfg *Config) *GraphConfig {
	return &GraphConfig{
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
}

func TestBackpressureHistoryWhenDownstreamSaturated(t *testing.T) {
	sim := runSimulatorWithTimeout(t, saturatedConfig(80), 5*time.Second)

	if len(sim.Slaves) == 0 || sim.Relay == nil {
		t.Fatalf("expected at least one slave and one relay")
	}

	edgeKey := EdgeKey{FromID: sim.Relay.ID, ToID: sim.Slaves[0].ID}
	history := sim.Chan.BackpressureHistory(edgeKey)
	trueCount := 0
	for _, bp := range history {
		if bp {
			trueCount++
		}
	}
	if trueCount == 0 {
		t.Fatalf("expected backpressure events but none were recorded")
	}
}

func TestBackpressureHistoryWhenDownstreamResponsive(t *testing.T) {
	sim := runSimulatorWithTimeout(t, responsiveConfig(60), 5*time.Second)

	edgeKey := EdgeKey{FromID: sim.Relay.ID, ToID: sim.Slaves[0].ID}
	history := sim.Chan.BackpressureHistory(edgeKey)
	for cycle, bp := range history {
		if bp {
			t.Fatalf("unexpected backpressure at cycle %d", cycle)
		}
	}
}
