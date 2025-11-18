//go:build e2e

package e2e_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/tests/e2e/mocks"
	"github.com/Readm/flow_sim/tests/e2e/model"
	"github.com/Readm/flow_sim/tests/e2e/server"
)

func TestFlowView_RodEndToEnd(t *testing.T) {
	t.Parallel()

	mockCtrl := mocks.NewController()
	staticDir := filepath.Join(projectRoot(t), "web", "static")
	srv, err := server.New(server.Options{
		Controller:         mockCtrl,
		StaticDir:          staticDir,
		DefaultConfig:      defaultTestConfig(),
		DefaultTotalCycles: 32,
	})
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go forwardFrames(ctx, mockCtrl)

	if err := mockCtrl.EmitFrame(initialFrame()); err != nil {
		t.Fatalf("emit initial frame: %v", err)
	}

	browser := launchBrowser(t)
	defer browser.MustClose()

	page := browser.MustPage(srv.BaseURL())
	page.MustWaitLoad()

	waitForText(t, page, "#currentCycle", "0")
	waitForText(t, page, "#simStatus", "Status: Paused")

	totalRequests := page.MustElement(".stats-value").MustText()
	if totalRequests == "" {
		t.Fatal("expected stats panel to render global metrics")
	}

	nodeCount := page.MustEval(`() => (window.__flowViewCy ? window.__flowViewCy.nodes().length : 0)`).Int()
	if nodeCount != 2 {
		t.Fatalf("expected 2 nodes rendered, got %d", nodeCount)
	}

	page.MustElement("#runCycleCount").MustSelectAllText().MustInput("3")
	page.MustElement("#btnRun").MustClick()

	waitForCondition(t, 5*time.Second, func() bool {
		return page.MustEval(`() => document.querySelectorAll('#pipelineOverlay circle').length`).Int() >= 4
	})

	page.MustElement("#btnReset").MustClick()
	waitForText(t, page, "#currentCycle", "0")
	waitForCondition(t, 5*time.Second, func() bool {
		return page.MustEval(`() => (window.__flowViewCy ? window.__flowViewCy.nodes().length : 0)`).Int() == 3
	})
}

func launchBrowser(t *testing.T) *rod.Browser {
	t.Helper()
	u := launcher.New().Headless(true).MustLaunch()
	return rod.New().ControlURL(u).MustConnect()
}

func waitForText(t *testing.T, page *rod.Page, selector, expected string) {
	t.Helper()
	waitForCondition(t, 5*time.Second, func() bool {
		el, err := page.Element(selector)
		if err != nil {
			return false
		}
		text, err := el.Text()
		if err != nil {
			return false
		}
		return text == expected
	})
}

func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func forwardFrames(ctx context.Context, ctrl *mocks.Controller) {
	for {
		select {
		case cmd := <-ctrl.Commands():
			switch cmd.Type {
			case "run":
				_ = ctrl.EmitFrame(runFrame())
			case "reset":
				_ = ctrl.EmitFrame(resetFrame())
			}
		case <-ctx.Done():
			return
		}
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func initialFrame() *model.Frame {
	return &model.Frame{
		Cycle:         0,
		Paused:        true,
		InFlightCount: 0,
		ConfigHash:    "cfg-demo-a",
		Nodes: []model.Node{
			{
				ID:    1,
				Label: "Master 1",
				Type:  "master",
				Queues: []model.Queue{{
					Name:     "dispatch",
					Length:   2,
					Capacity: 8,
				}},
			},
			{
				ID:    2,
				Label: "Slave 2",
				Type:  "slave",
				Queues: []model.Queue{{
					Name:     "ingress",
					Length:   0,
					Capacity: 8,
				}},
			},
		},
		Edges: []model.Edge{
			{Source: 1, Target: 2, Label: "Req", Latency: 3, BandwidthLimit: 2},
		},
		Stats: &model.Stats{
			Global: &model.GlobalStats{
				TotalRequests:    4,
				Completed:        2,
				CompletionRate:   50,
				AvgEndToEndDelay: 10.5,
				MaxDelay:         14,
				MinDelay:         8,
			},
		},
	}
}

func runFrame() *model.Frame {
	return &model.Frame{
		Cycle:         5,
		Paused:        false,
		InFlightCount: 3,
		ConfigHash:    "cfg-demo-a",
		Nodes:         initialFrame().Nodes,
		Edges: []model.Edge{
			{
				Source:         1,
				Target:         2,
				Label:          "Req",
				Latency:        3,
				BandwidthLimit: 2,
				PipelineStages: []model.PipelineStage{
					{StageIndex: 0, PacketCount: 2},
					{StageIndex: 1, PacketCount: 1},
					{StageIndex: 2, PacketCount: 0},
				},
			},
		},
	}
}

func resetFrame() *model.Frame {
	return &model.Frame{
		Cycle:         0,
		Paused:        true,
		InFlightCount: 0,
		ConfigHash:    "cfg-demo-b",
		Nodes: []model.Node{
			{ID: 10, Label: "Router 10", Type: "RT"},
			{ID: 11, Label: "Master 11", Type: "master"},
			{ID: 12, Label: "Slave 12", Type: "slave"},
		},
		Edges: []model.Edge{
			{Source: 11, Target: 10, Label: "Req", Latency: 2, BandwidthLimit: 1},
			{Source: 10, Target: 12, Label: "Rsp", Latency: 2, BandwidthLimit: 1},
		},
	}
}

func defaultTestConfig() config.EntityConfig {
	return config.EntityConfig{
		Nodes: []config.NodeConfig{
			{ID: 1},
			{ID: 2},
			{ID: 3},
		},
		Link: config.LinkConfig{},
	}
}
