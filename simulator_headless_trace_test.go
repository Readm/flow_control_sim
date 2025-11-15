package main

import (
	"testing"
	"time"

	"github.com/Readm/flow_sim/visual"
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
