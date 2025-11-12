package main

import (
	"encoding/json"
	"io"
	"net/http"

	"flow_sim/visual"
)

func (ws *WebServer) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		GetLogger().Debugf("Error reading request body: %v", err)
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	GetLogger().Debugf("Received /api/control request: Method=%s, Body=%s", r.Method, string(bodyBytes))

	var req controlRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		GetLogger().Debugf("Error decoding JSON: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	GetLogger().Debugf("Parsed request: Type=%s, ConfigName=%s, TotalCycles=%v", req.Type, req.ConfigName, req.TotalCycles)

	cmd, err := ws.processControlRequest(&req)
	if err != nil {
		GetLogger().Debugf("Error processing control request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if cmd.Type == visual.CommandReset {
		GetLogger().Debugf("Processing reset command, ConfigName=%s", req.ConfigName)
		if cfg, ok := cmd.ConfigOverride.(*Config); ok {
			GetLogger().Debugf("Found config '%s': NumMasters=%d, NumSlaves=%d, TotalCycles=%d",
				req.ConfigName, cfg.NumMasters, cfg.NumSlaves, cfg.TotalCycles)
		}
	}

	if !ws.queueCommand(*cmd) {
		GetLogger().Debugf("Command queue full, cannot accept command")
		http.Error(w, "Command queue full", http.StatusServiceUnavailable)
		return
	}

	GetLogger().Debugf("Command queued successfully: Type=%s, HasConfig=%v", cmd.Type, cmd.ConfigOverride != nil)
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("Command accepted"))
}
