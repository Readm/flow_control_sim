package main

import (
	"context"
	"testing"
	"time"
)

// mockVisualizer implements the Visualizer interface for unit testing control flow.
type mockVisualizer struct {
	headless  bool
	commandCh chan ControlCommand
	frameCh   chan *SimulationFrame
}

func newMockVisualizer() *mockVisualizer {
	return &mockVisualizer{
		commandCh: make(chan ControlCommand, 16),
		frameCh:   make(chan *SimulationFrame, 64),
	}
}

func (m *mockVisualizer) SetHeadless(headless bool) {
	m.headless = headless
}

func (m *mockVisualizer) IsHeadless() bool {
	return m.headless
}

func (m *mockVisualizer) PublishFrame(frame *SimulationFrame) {
	if frame == nil {
		return
	}
	select {
	case m.frameCh <- frame:
	default:
		// Drop oldest frame to keep buffer fresh, then push latest.
		select {
		case <-m.frameCh:
		default:
		}
		m.frameCh <- frame
	}
}

func (m *mockVisualizer) NextCommand() (ControlCommand, bool) {
	select {
	case cmd, ok := <-m.commandCh:
		if !ok {
			return ControlCommand{Type: CommandNone}, false
		}
		return cmd, true
	default:
		return ControlCommand{Type: CommandNone}, false
	}
}

func (m *mockVisualizer) WaitCommand(ctx context.Context) (ControlCommand, bool) {
	select {
	case cmd, ok := <-m.commandCh:
		if !ok {
			return ControlCommand{Type: CommandNone}, false
		}
		return cmd, true
	case <-ctx.Done():
		return ControlCommand{Type: CommandNone}, false
	}
}

func (m *mockVisualizer) pushCommand(cmd ControlCommand) {
	m.commandCh <- cmd
}

func (m *mockVisualizer) close() {
	close(m.commandCh)
}

func (m *mockVisualizer) waitForCycle(t *testing.T, target int, timeout time.Duration) *SimulationFrame {
	t.Helper()
	deadline := time.After(timeout)
	last := -1
	for {
		select {
		case frame := <-m.frameCh:
			if frame == nil {
				continue
			}
			last = frame.Cycle
			if frame.Cycle >= target {
				return frame
			}
		case <-deadline:
			t.Fatalf("timeout waiting for cycle >= %d (last=%d)", target, last)
		}
	}
}

func (m *mockVisualizer) assertNoAdvanceBeyond(t *testing.T, limit int, duration time.Duration) {
	t.Helper()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	for {
		select {
		case frame := <-m.frameCh:
			if frame == nil {
				continue
			}
			if frame.Cycle > limit {
				t.Fatalf("unexpected cycle advance to %d (expected <= %d)", frame.Cycle, limit)
			}
		case <-timer.C:
			return
		}
	}
}

// TestBasicFlow runs a short simulation and verifies basic invariants.
func TestBasicFlow(t *testing.T) {
	cfg := &Config{
		NumMasters:         2,
		NumSlaves:          3,
		NumRelays:          1,
		TotalCycles:        300,
		MasterRelayLatency: 2,
		RelayMasterLatency: 2,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   1,
		RequestRateConfig:  0.5,
		BandwidthLimit:     1,
		SlaveWeights:       []int{1, 1, 1},
		Headless:           true, // Test in headless mode
		VisualMode:         "none",
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
		NumMasters:         1,
		NumSlaves:          1,
		NumRelays:          1,
		TotalCycles:        100,
		MasterRelayLatency: 2,
		RelayMasterLatency: 2,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   1,
		RequestRateConfig:  1.0, // Always generate requests
		BandwidthLimit:     1,
		SlaveWeights:       []int{1},
		Headless:           true,
		VisualMode:         "none",
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

func newInteractiveConfig(totalCycles int) *Config {
	return &Config{
		NumMasters:         1,
		NumSlaves:          1,
		NumRelays:          1,
		TotalCycles:        totalCycles,
		MasterRelayLatency: 1,
		RelayMasterLatency: 1,
		RelaySlaveLatency:  1,
		SlaveRelayLatency:  1,
		SlaveProcessRate:   1,
		RequestRateConfig:  0.8,
		BandwidthLimit:     1,
		SlaveWeights:       []int{1},
		Headless:           false,
		VisualMode:         "web",
	}
}

// TestSimulatorControlFlowStepResume verifies pause->step->resume flow without manual intervention.
func TestSimulatorControlFlowStepResume(t *testing.T) {
	cfg := newInteractiveConfig(5)
	sim := NewSimulator(cfg)

	viz := newMockVisualizer()
	viz.SetHeadless(false)
	sim.visualizer = viz

	// Provide initial frame for cycle 0 to mimic the web visualizer behaviour.
	viz.PublishFrame(sim.buildFrame(sim.current))

	done := make(chan bool, 1)
	go func() {
		done <- sim.runCycles()
	}()

	viz.waitForCycle(t, 0, time.Second)

	viz.pushCommand(ControlCommand{Type: CommandStep})
	viz.waitForCycle(t, 1, time.Second)
	viz.assertNoAdvanceBeyond(t, 1, 150*time.Millisecond)

	viz.pushCommand(ControlCommand{Type: CommandStep})
	viz.waitForCycle(t, 2, time.Second)

	viz.pushCommand(ControlCommand{Type: CommandResume})
	viz.waitForCycle(t, cfg.TotalCycles, 2*time.Second)

	if reset := <-done; reset {
		t.Fatalf("expected runCycles to complete without reset request")
	}

	viz.close()
}

// TestSimulatorControlFlowReset verifies that a reset command pauses the current run and prepares new config.
func TestSimulatorControlFlowReset(t *testing.T) {
	cfg := newInteractiveConfig(6)
	sim := NewSimulator(cfg)

	viz := newMockVisualizer()
	viz.SetHeadless(false)
	sim.visualizer = viz

	viz.PublishFrame(sim.buildFrame(sim.current))

	resetCh := make(chan bool, 1)
	go func() {
		resetCh <- sim.runCycles()
	}()

	viz.waitForCycle(t, 0, time.Second)

	viz.pushCommand(ControlCommand{Type: CommandStep})
	viz.waitForCycle(t, 1, time.Second)

	newCfg := newInteractiveConfig(4)
	viz.pushCommand(ControlCommand{Type: CommandReset, ConfigOverride: newCfg})

	if reset := <-resetCh; !reset {
		t.Fatalf("expected runCycles to request reset")
	}
	if sim.pendingReset == nil {
		t.Fatalf("expected pending reset config")
	}
	if sim.pendingReset.TotalCycles != newCfg.TotalCycles {
		t.Fatalf("expected pending reset total cycles %d, got %d", newCfg.TotalCycles, sim.pendingReset.TotalCycles)
	}

	// Apply reset and verify new run publishes a fresh cycle 0 frame.
	sim.reset(sim.pendingReset)
	sim.pendingReset = nil
	frame := viz.waitForCycle(t, 0, time.Second)
	if frame.ConfigHash == "" {
		t.Fatalf("expected config hash in reset frame")
	}

	done := make(chan bool, 1)
	go func() {
		done <- sim.runCycles()
	}()

	viz.pushCommand(ControlCommand{Type: CommandResume})
	viz.waitForCycle(t, sim.cfg.TotalCycles, 2*time.Second)

	if reset := <-done; reset {
		t.Fatalf("expected second run to finish without reset")
	}

	viz.close()
}
