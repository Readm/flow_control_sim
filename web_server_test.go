package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebServer_FrameEndpoint(t *testing.T) {
	server := NewWebServer("127.0.0.1:0")
	
	// Test empty frame
	req := httptest.NewRequest("GET", "/api/frame", nil)
	w := httptest.NewRecorder()
	server.handleFrame(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for empty frame, got %d", w.Code)
	}

	// Test with frame
	frame := &SimulationFrame{
		Cycle: 10,
		Nodes: []NodeSnapshot{
			{ID: 1, Type: NodeTypeRN, Label: "RN 0"},
		},
		Edges: []EdgeSnapshot{},
	}
	server.UpdateFrame(frame)

	req = httptest.NewRequest("GET", "/api/frame", nil)
	w = httptest.NewRecorder()
	server.handleFrame(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var result SimulationFrame
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if result.Cycle != 10 {
		t.Errorf("Expected cycle 10, got %d", result.Cycle)
	}
	if len(result.Nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(result.Nodes))
	}

	// Test wrong method
	req = httptest.NewRequest("POST", "/api/frame", nil)
	w = httptest.NewRecorder()
	server.handleFrame(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", w.Code)
	}
}

func TestWebServer_StatsEndpoint(t *testing.T) {
	server := NewWebServer("127.0.0.1:0")

	// Test empty stats
	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	server.handleStats(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for empty stats, got %d", w.Code)
	}

	// Test with stats
	stats := &SimulationStats{
		Global: &GlobalStats{
			TotalRequests: 100,
			Completed:      50,
		},
	}
	frame := &SimulationFrame{
		Cycle: 10,
		Stats: stats,
	}
	server.UpdateFrame(frame)

	req = httptest.NewRequest("GET", "/api/stats", nil)
	w = httptest.NewRecorder()
	server.handleStats(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var result SimulationStats
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if result.Global.TotalRequests != 100 {
		t.Errorf("Expected 100 requests, got %d", result.Global.TotalRequests)
	}
}

func TestWebServer_ControlEndpoint(t *testing.T) {
	server := NewWebServer("127.0.0.1:0")

	// Test pause command
	cmdJSON := `{"type":"pause"}`
	req := httptest.NewRequest("POST", "/api/control", bytes.NewBufferString(cmdJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.handleControl(w, req)
	if w.Code != http.StatusAccepted {
		t.Errorf("Expected 202, got %d", w.Code)
	}

	cmd, ok := server.NextCommand()
	if !ok {
		t.Fatal("Expected command, got none")
	}
	if cmd.Type != CommandPause {
		t.Errorf("Expected pause command, got %s", cmd.Type)
	}

	// Test resume command
	cmdJSON = `{"type":"resume"}`
	req = httptest.NewRequest("POST", "/api/control", bytes.NewBufferString(cmdJSON))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.handleControl(w, req)
	if w.Code != http.StatusAccepted {
		t.Errorf("Expected 202, got %d", w.Code)
	}

	cmd, ok = server.NextCommand()
	if !ok {
		t.Fatal("Expected command, got none")
	}
	if cmd.Type != CommandResume {
		t.Errorf("Expected resume command, got %s", cmd.Type)
	}

	// Test reset command with config
	cfg := &Config{
		NumMasters: 2,
		NumSlaves:  2,
		TotalCycles: 100,
		RequestRateConfig: 0.5,
		SlaveWeights: []int{1, 1},
	}
	cfgJSON, _ := json.Marshal(map[string]interface{}{
		"type":   "reset",
		"config": cfg,
	})
	req = httptest.NewRequest("POST", "/api/control", bytes.NewBuffer(cfgJSON))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.handleControl(w, req)
	if w.Code != http.StatusAccepted {
		t.Errorf("Expected 202, got %d", w.Code)
	}

	cmd, ok = server.NextCommand()
	if !ok {
		t.Fatal("Expected command, got none")
	}
	if cmd.Type != CommandReset {
		t.Errorf("Expected reset command, got %s", cmd.Type)
	}
	if cmd.ConfigOverride == nil {
		t.Fatal("Expected config override, got nil")
	}
	if cmd.ConfigOverride.NumMasters != 2 {
		t.Errorf("Expected 2 masters, got %d", cmd.ConfigOverride.NumMasters)
	}

	// Test invalid command type
	cmdJSON = `{"type":"invalid"}`
	req = httptest.NewRequest("POST", "/api/control", bytes.NewBufferString(cmdJSON))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.handleControl(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	// Test invalid JSON
	req = httptest.NewRequest("POST", "/api/control", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.handleControl(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	// Test invalid config
	invalidCfgJSON, _ := json.Marshal(map[string]interface{}{
		"type": "reset",
		"config": map[string]interface{}{
			"numMasters": -1,
			"numSlaves":  2,
		},
	})
	req = httptest.NewRequest("POST", "/api/control", bytes.NewBuffer(invalidCfgJSON))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.handleControl(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid config, got %d", w.Code)
	}

	// Test wrong method
	req = httptest.NewRequest("GET", "/api/control", nil)
	w = httptest.NewRecorder()
	server.handleControl(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", w.Code)
	}
}

func TestWebServer_NextCommand_NonBlocking(t *testing.T) {
	server := NewWebServer("127.0.0.1:0")

	// Test empty queue
	cmd, ok := server.NextCommand()
	if ok {
		t.Errorf("Expected no command, got %v", cmd)
	}
	if cmd.Type != CommandNone {
		t.Errorf("Expected CommandNone, got %s", cmd.Type)
	}

	// Send command
	cmdJSON := `{"type":"pause"}`
	req := httptest.NewRequest("POST", "/api/control", bytes.NewBufferString(cmdJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.handleControl(w, req)

	// Should get command now
	cmd, ok = server.NextCommand()
	if !ok {
		t.Fatal("Expected command, got none")
	}
	if cmd.Type != CommandPause {
		t.Errorf("Expected pause command, got %s", cmd.Type)
	}
}

