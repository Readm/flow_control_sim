package main

import (
	"encoding/json"
	"net/http"
)

func (ws *WebServer) handleFrame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ws.mu.RLock()
	frame := ws.latestFrame
	ws.mu.RUnlock()

	if frame == nil {
		http.Error(w, "No frame available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(frame); err != nil {
		http.Error(w, "Failed to encode frame", http.StatusInternalServerError)
	}
}

func (ws *WebServer) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ws.mu.RLock()
	stats := ws.latestStats
	ws.mu.RUnlock()

	if stats == nil {
		http.Error(w, "No stats available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		http.Error(w, "Failed to encode stats", http.StatusInternalServerError)
	}
}

func (ws *WebServer) handleConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configs := GetPredefinedConfigs()
	configList := make([]struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}, len(configs))
	for i, cfg := range configs {
		configList[i] = struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}{
			Name:        cfg.Name,
			Description: cfg.Description,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(configList); err != nil {
		http.Error(w, "Failed to encode configs", http.StatusInternalServerError)
	}
}

