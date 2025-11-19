//go:build e2e

package e2e_test

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/pkg/visual/frame"
	"github.com/Readm/flow_sim/tests/e2e/mocks"
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
	_ = mockCtrl.EmitFrame(runFrame())

	waitForCondition(t, 5*time.Second, func() bool {
		return page.MustEval(`() => document.querySelectorAll('#pipelineOverlay circle').length`).Int() >= 4
	})

	page.MustElement("#btnReset").MustClick()
	_ = mockCtrl.EmitFrame(resetFrame())
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

func projectRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func initialFrame() *frame.Frame {
	return &frame.Frame{
		Cycle:         0,
		Paused:        true,
		InFlightCount: 0,
		ConfigHash:    "cfg-demo-a",
		Nodes: []frame.Node{
			{
				ID:    1,
				Label: "Master 1",
				Type:  "master",
				Queues: []frame.Queue{{
					Name:     "dispatch",
					Length:   2,
					Capacity: 8,
				}},
			},
			{
				ID:    2,
				Label: "Slave 2",
				Type:  "slave",
				Queues: []frame.Queue{{
					Name:     "ingress",
					Length:   0,
					Capacity: 8,
				}},
			},
		},
		Edges: []frame.Edge{
			{Source: 1, Target: 2, Label: "Req", Latency: 3, BandwidthLimit: 2},
		},
		Stats: &frame.Stats{
			Global: &frame.GlobalStats{
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

func runFrame() *frame.Frame {
	return &frame.Frame{
		Cycle:         5,
		Paused:        false,
		InFlightCount: 3,
		ConfigHash:    "cfg-demo-a",
		Nodes:         initialFrame().Nodes,
		Edges: []frame.Edge{
			{
				Source:         1,
				Target:         2,
				Label:          "Req",
				Latency:        3,
				BandwidthLimit: 2,
				PipelineStages: []frame.PipelineStage{
					{StageIndex: 0, PacketCount: 2},
					{StageIndex: 1, PacketCount: 1},
					{StageIndex: 2, PacketCount: 0},
				},
			},
		},
	}
}

func resetFrame() *frame.Frame {
	return &frame.Frame{
		Cycle:         0,
		Paused:        true,
		InFlightCount: 0,
		ConfigHash:    "cfg-demo-b",
		Nodes: []frame.Node{
			{ID: 10, Label: "Router 10", Type: "RT"},
			{ID: 11, Label: "Master 11", Type: "master"},
			{ID: 12, Label: "Slave 12", Type: "slave"},
		},
		Edges: []frame.Edge{
			{Source: 11, Target: 10, Label: "Req", Latency: 2, BandwidthLimit: 1},
			{Source: 10, Target: 12, Label: "Rsp", Latency: 2, BandwidthLimit: 1},
		},
	}
}

func TestFlowView_BackpressureSignals(t *testing.T) {
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

	// Emit frame with backpressure signals
	if err := mockCtrl.EmitFrame(backpressureFrame()); err != nil {
		t.Fatalf("emit backpressure frame: %v", err)
	}

	browser := launchBrowser(t)
	defer browser.MustClose()

	page := browser.MustPage(srv.BaseURL())
	page.MustWaitLoad()

	waitForText(t, page, "#currentCycle", "0")
	waitForText(t, page, "#simStatus", "Status: Paused")

	// Wait for nodes to be rendered
	waitForCondition(t, 5*time.Second, func() bool {
		return page.MustEval(`() => (window.__flowViewCy ? window.__flowViewCy.nodes().length : 0)`).Int() >= 2
	})

	// Test 1: Verify node with in-queue backpressure has red border
	node1HasRedBorder := page.MustEval(`() => {
		const cy = window.__flowViewCy;
		if (!cy) return false;
		const node = cy.getElementById('1');
		if (!node || node.length === 0) return false;
		const color = node.style('border-color');
		return color === 'rgb(255, 77, 79)' || color === 'rgb(255,77,79)' || color === '#ff4d4f';
	}`).Bool()
	if !node1HasRedBorder {
		t.Error("expected node 1 (with in-queue backpressure) to have red border")
	}

	// Test 2: Verify node with out-queue backpressure has red border
	node2HasRedBorder := page.MustEval(`() => {
		const cy = window.__flowViewCy;
		if (!cy) return false;
		const node = cy.getElementById('2');
		if (!node || node.length === 0) return false;
		const color = node.style('border-color');
		return color === 'rgb(255, 77, 79)' || color === 'rgb(255,77,79)' || color === '#ff4d4f';
	}`).Bool()
	if !node2HasRedBorder {
		t.Error("expected node 2 (with out-queue backpressure) to have red border")
	}

	// Test 3: Verify node with downstream backpressure has red border
	node3HasRedBorder := page.MustEval(`() => {
		const cy = window.__flowViewCy;
		if (!cy) return false;
		const node = cy.getElementById('3');
		if (!node || node.length === 0) return false;
		const color = node.style('border-color');
		return color === 'rgb(255, 77, 79)' || color === 'rgb(255,77,79)' || color === '#ff4d4f';
	}`).Bool()
	if !node3HasRedBorder {
		t.Error("expected node 3 (with downstream backpressure) to have red border")
	}

	// Test 4: Verify node without backpressure has normal border
	node4HasNormalBorder := page.MustEval(`() => {
		const cy = window.__flowViewCy;
		if (!cy) return false;
		const node = cy.getElementById('4');
		if (!node || node.length === 0) return false;
		const color = node.style('border-color');
		return color !== 'rgb(255, 77, 79)' && color !== 'rgb(255,77,79)' && color !== '#ff4d4f';
	}`).Bool()
	if !node4HasNormalBorder {
		t.Error("expected node 4 (without backpressure) to have normal border")
	}

	// Test 5: Verify edge with backpressure has red dashed line
	edgeBackpressured := page.MustEval(`() => {
		const cy = window.__flowViewCy;
		if (!cy) return false;
		const edge = cy.getElementById('e1-2');
		if (!edge || edge.length === 0) return false;
		const lineColor = edge.style('line-color');
		const lineStyle = edge.style('line-style');
		const isRed = lineColor === 'rgb(255, 77, 79)' || lineColor === 'rgb(255,77,79)' || lineColor === '#ff4d4f';
		const isDashed = lineStyle === 'dashed';
		return isRed && isDashed;
	}`).Bool()
	if !edgeBackpressured {
		t.Error("expected edge 1->2 (with backpressure) to have red dashed line")
	}

	// Test 6: Verify edge without backpressure has normal line
	edgeNormal := page.MustEval(`() => {
		const cy = window.__flowViewCy;
		if (!cy) return false;
		const edge = cy.getElementById('e4-5');
		if (!edge || edge.length === 0) return false;
		const lineColor = edge.style('line-color');
		const lineStyle = edge.style('line-style');
		const isNotRed = lineColor !== 'rgb(255, 77, 79)' && lineColor !== 'rgb(255,77,79)' && lineColor !== '#ff4d4f';
		const isSolid = lineStyle === 'solid';
		return isNotRed && isSolid;
	}`).Bool()
	if !edgeNormal {
		t.Error("expected edge 4->5 (without backpressure) to have normal line")
	}

	// Test 7: Verify node label contains backpressure markers
	node1HasMarker := page.MustEval(`() => {
		const cy = window.__flowViewCy;
		if (!cy) return false;
		const node = cy.getElementById('1');
		if (!node || node.length === 0) return false;
		const label = node.data('label') || '';
		return label.indexOf('IN-BP') !== -1;
	}`).Bool()
	if !node1HasMarker {
		t.Error("expected node 1 label to contain 'IN-BP'")
	}

	node2HasMarker := page.MustEval(`() => {
		const cy = window.__flowViewCy;
		if (!cy) return false;
		const node = cy.getElementById('2');
		if (!node || node.length === 0) return false;
		const label = node.data('label') || '';
		return label.indexOf('OUT-BP') !== -1;
	}`).Bool()
	if !node2HasMarker {
		t.Error("expected node 2 label to contain 'OUT-BP'")
	}

	node3HasMarker := page.MustEval(`() => {
		const cy = window.__flowViewCy;
		if (!cy) return false;
		const node = cy.getElementById('3');
		if (!node || node.length === 0) return false;
		const label = node.data('label') || '';
		return label.indexOf('DS-BP') !== -1;
	}`).Bool()
	if !node3HasMarker {
		t.Error("expected node 3 label to contain 'DS-BP'")
	}
}

func backpressureFrame() *frame.Frame {
	return &frame.Frame{
		Cycle:         0,
		Paused:        true,
		InFlightCount: 0,
		ConfigHash:    "cfg-backpressure-test",
		Nodes: []frame.Node{
			{
				ID:                 1,
				Label:              "Node 1",
				Type:               "generic",
				InQueueBackpressure: true, // Has in-queue backpressure
				Queues: []frame.Queue{{
					Name:     "in-queue",
					Length:   8,
					Capacity: 8, // Full
				}},
			},
			{
				ID:                  2,
				Label:               "Node 2",
				Type:                "generic",
				OutQueueBackpressure: true, // Has out-queue backpressure
				Queues: []frame.Queue{{
					Name:     "out-queue",
					Length:   16,
					Capacity: 16, // Full
				}},
			},
			{
				ID:                    3,
				Label:                 "Node 3",
				Type:                  "generic",
				DownstreamBackpressure: true, // Has downstream backpressure
				Queues: []frame.Queue{{
					Name:     "out-queue",
					Length:   5,
					Capacity: 16, // Not full, but downstream is backpressured
				}},
			},
			{
				ID:    4,
				Label: "Node 4",
				Type:  "generic",
				// No backpressure signals
				Queues: []frame.Queue{{
					Name:     "in-queue",
					Length:   2,
					Capacity: 8, // Not full
				}},
			},
			{
				ID:    5,
				Label: "Node 5",
				Type:  "generic",
				// No backpressure signals
				Queues: []frame.Queue{{
					Name:     "in-queue",
					Length:   0,
					Capacity: 8,
				}},
			},
		},
		Edges: []frame.Edge{
			{
				Source:        1,
				Target:        2,
				Label:         "1→2",
				Latency:       3,
				BandwidthLimit: 2,
				Backpressured: true, // Has backpressure
			},
			{
				Source:        4,
				Target:        5,
				Label:         "4→5",
				Latency:       2,
				BandwidthLimit: 1,
				Backpressured: false, // No backpressure
			},
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
