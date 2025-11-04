package main

import (
	"encoding/json"
	"net/http"
	"sync"
)

// WebServer provides HTTP endpoints for visualization and control.
type WebServer struct {
	mu          sync.RWMutex
	latestFrame *SimulationFrame
	latestStats *SimulationStats
	commands    chan ControlCommand
	server      *http.Server
}

// NewWebServer creates a new web server instance.
func NewWebServer(addr string) *WebServer {
	ws := &WebServer{
		commands: make(chan ControlCommand, 10),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/frame", ws.handleFrame)
	mux.HandleFunc("/api/stats", ws.handleStats)
	mux.HandleFunc("/api/control", ws.handleControl)
	mux.Handle("/", http.FileServer(http.Dir("web/static")))

	ws.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return ws
}

// Start starts the HTTP server in a goroutine.
func (ws *WebServer) Start() error {
	go func() {
		if err := ws.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error if needed, but don't block
		}
	}()
	return nil
}

// UpdateFrame updates the latest frame and stats.
func (ws *WebServer) UpdateFrame(frame *SimulationFrame) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.latestFrame = frame
	if frame != nil {
		ws.latestStats = frame.Stats
	}
}

// NextCommand returns the next control command if available, non-blocking.
func (ws *WebServer) NextCommand() (ControlCommand, bool) {
	select {
	case cmd := <-ws.commands:
		return cmd, true
	default:
		return ControlCommand{Type: CommandNone}, false
	}
}

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

type controlRequest struct {
	Type   string  `json:"type"`
	Config *Config `json:"config,omitempty"`
}

func (ws *WebServer) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req controlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var cmd ControlCommand
	switch req.Type {
	case "pause":
		cmd.Type = CommandPause
	case "resume":
		cmd.Type = CommandResume
	case "reset":
		cmd.Type = CommandReset
		if req.Config != nil {
			if err := ws.validateConfig(req.Config); err != nil {
				http.Error(w, "Invalid config: "+err.Error(), http.StatusBadRequest)
				return
			}
			cmd.ConfigOverride = req.Config
		}
	default:
		http.Error(w, "Invalid command type", http.StatusBadRequest)
		return
	}

	select {
	case ws.commands <- cmd:
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("Command accepted"))
	default:
		http.Error(w, "Command queue full", http.StatusServiceUnavailable)
	}
}

func (ws *WebServer) validateConfig(cfg *Config) error {
	if cfg.NumMasters <= 0 || cfg.NumSlaves <= 0 {
		return &validationError{msg: "NumMasters and NumSlaves must be positive"}
	}
	if cfg.TotalCycles <= 0 {
		return &validationError{msg: "TotalCycles must be positive"}
	}
	if cfg.RequestRate < 0 || cfg.RequestRate > 1 {
		return &validationError{msg: "RequestRate must be between 0 and 1"}
	}
	if len(cfg.SlaveWeights) != cfg.NumSlaves {
		return &validationError{msg: "SlaveWeights length must match NumSlaves"}
	}
	return nil
}

type validationError struct {
	msg string
}

func (e *validationError) Error() string {
	return e.msg
}

