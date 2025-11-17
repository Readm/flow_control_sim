package app

import (
	"testing"
	"time"

	"github.com/Readm/flow_sim/framework/app/visual"
)

func TestHeadlessRunThreeCycleChunksWithTrace(t *testing.T) {
	SetPacketEventTrace(true)
	defer SetPacketEventTrace(false)

	originalLogger := GetLogger()
	SetLogger(NewLogger(LogLevelInfo, "[TRACE TEST] "))
	defer SetLogger(originalLogger)

	cfg := &Config{
		NumMasters:         1,
		NumSlaves:          1,
		NumRelays:          1,
		TotalCycles:        10,
		MasterRelayLatency: 1,
		RelayMasterLatency: 1,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   1,
		RequestRateConfig:  1.0,
		BandwidthLimit:     1,
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

	done := make(chan bool, 1)
	go func() {
		done <- sim.runCycles()
	}()

	for i := 0; i < 4; i++ {
		if !sim.handleCommand(visual.ControlCommand{Type: visual.CommandRun, Cycles: 3}) {
			t.Fatalf("handleCommand run cycles failed at iteration %d", i)
		}
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-time.After(3 * time.Second):
		t.Fatal("simulator did not finish within timeout; potential deadlock")
	case resetRequested := <-done:
		if resetRequested {
			t.Fatal("unexpected reset requested during headless run")
		}
	}
}
