package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

// WebServer provides HTTP endpoints for visualization and control.
type WebServer struct {
	mu          sync.RWMutex
	latestFrame *SimulationFrame
	latestStats *SimulationStats
	commands    chan ControlCommand
	server      *http.Server
	clients     map[*websocket.Conn]bool
	clientsMu   sync.Mutex
}

// NewWebServer creates a new web server instance.
func NewWebServer(addr string) *WebServer {
	ws := &WebServer{
		commands: make(chan ControlCommand, 10),
		clients:  make(map[*websocket.Conn]bool),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/frame", ws.handleFrame)
	mux.HandleFunc("/api/stats", ws.handleStats)
	mux.HandleFunc("/api/control", ws.handleControl)
	mux.HandleFunc("/api/configs", ws.handleConfigs)
	mux.HandleFunc("/ws", ws.handleWebSocket)
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

// UpdateFrame updates the latest frame and stats, and broadcasts to WebSocket clients.
func (ws *WebServer) UpdateFrame(frame *SimulationFrame) {
	ws.mu.Lock()
	ws.latestFrame = frame
	if frame != nil {
		ws.latestStats = frame.Stats
	}
	ws.mu.Unlock()

	// Broadcast to all WebSocket clients
	ws.broadcastFrame(frame)
}

// broadcastFrame sends frame data to all connected WebSocket clients.
func (ws *WebServer) broadcastFrame(frame *SimulationFrame) {
	if frame == nil {
		return
	}

	ws.clientsMu.Lock()
	defer ws.clientsMu.Unlock()

	data, err := json.Marshal(frame)
	if err != nil {
		log.Printf("[ERROR] Failed to marshal frame for WebSocket: %v", err)
		return
	}

	for conn := range ws.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("[WARN] Failed to send frame to WebSocket client: %v", err)
			delete(ws.clients, conn)
			conn.Close()
		}
	}
}

// handleWebSocket handles WebSocket connections.
func (ws *WebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ERROR] WebSocket upgrade failed: %v", err)
		return
	}

	ws.clientsMu.Lock()
	ws.clients[conn] = true
	ws.clientsMu.Unlock()

	// Send latest frame immediately upon connection
	ws.mu.RLock()
	if ws.latestFrame != nil {
		data, err := json.Marshal(ws.latestFrame)
		ws.mu.RUnlock()
		if err == nil {
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("[WARN] Failed to send initial frame to WebSocket client: %v", err)
			}
		}
	} else {
		ws.mu.RUnlock()
	}

	// Handle incoming messages (for control commands via WebSocket)
	go func() {
		defer func() {
			ws.clientsMu.Lock()
			delete(ws.clients, conn)
			ws.clientsMu.Unlock()
			conn.Close()
		}()

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("[WARN] WebSocket error: %v", err)
				}
				break
			}

			// Handle control commands from WebSocket
			var req controlRequest
			if err := json.Unmarshal(message, &req); err == nil {
				// Process control request using shared logic
				cmd, err := ws.processControlRequest(&req)
				if err == nil {
					// Queue command (silently ignore if queue is full)
					ws.queueCommand(*cmd)
				}
				// Silently ignore errors for WebSocket (no response needed)
			}
		}
	}()
}

// queueCommand queues a control command. Returns true if queued successfully, false if queue is full.
func (ws *WebServer) queueCommand(cmd ControlCommand) bool {
	select {
	case ws.commands <- cmd:
		return true
	default:
		return false
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
	Type        string  `json:"type"`
	Config      *Config `json:"config,omitempty"`
	ConfigName  string  `json:"configName,omitempty"`
	TotalCycles *int    `json:"totalCycles,omitempty"`
}

// processControlRequest processes a control request and returns a ControlCommand.
// Returns (command, error). If error is not nil, the command is invalid.
func (ws *WebServer) processControlRequest(req *controlRequest) (*ControlCommand, error) {
	var cmd ControlCommand

	switch req.Type {
	case "pause":
		cmd.Type = CommandPause
	case "resume":
		cmd.Type = CommandResume
	case "reset":
		cmd.Type = CommandReset
		if req.ConfigName != "" {
			// Use predefined configuration by name
			predefinedCfg := GetConfigByName(req.ConfigName)
			if predefinedCfg == nil {
				return nil, &validationError{msg: "Invalid config name: " + req.ConfigName}
			}
			// Override TotalCycles if provided
			if req.TotalCycles != nil && *req.TotalCycles > 0 {
				predefinedCfg.TotalCycles = *req.TotalCycles
			}
			cmd.ConfigOverride = predefinedCfg
		} else if req.Config != nil {
			// Direct config provided (for backward compatibility and testing)
			if err := ws.validateConfig(req.Config); err != nil {
				return nil, err
			}
			cmd.ConfigOverride = req.Config
		}
	case "step":
		cmd.Type = CommandStep
	default:
		return nil, &validationError{msg: "Invalid command type: " + req.Type}
	}

	return &cmd, nil
}

func (ws *WebServer) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body for debugging
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[DEBUG] Error reading request body: %v", err)
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	log.Printf("[DEBUG] Received /api/control request: Method=%s, Body=%s", r.Method, string(bodyBytes))

	// Create a new reader for JSON decoder since we already read the body
	var req controlRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		log.Printf("[DEBUG] Error decoding JSON: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	log.Printf("[DEBUG] Parsed request: Type=%s, ConfigName=%s, TotalCycles=%v", req.Type, req.ConfigName, req.TotalCycles)

	// Process control request using shared logic
	cmd, err := ws.processControlRequest(&req)
	if err != nil {
		log.Printf("[DEBUG] Error processing control request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if cmd.Type == CommandReset {
		log.Printf("[DEBUG] Processing reset command, ConfigName=%s", req.ConfigName)
		if cmd.ConfigOverride != nil {
			log.Printf("[DEBUG] Found config '%s': NumMasters=%d, NumSlaves=%d, TotalCycles=%d",
				req.ConfigName, cmd.ConfigOverride.NumMasters, cmd.ConfigOverride.NumSlaves, cmd.ConfigOverride.TotalCycles)
		}
	}

	// Queue command
	if !ws.queueCommand(*cmd) {
		log.Printf("[DEBUG] Command queue full, cannot accept command")
		http.Error(w, "Command queue full", http.StatusServiceUnavailable)
		return
	}

	log.Printf("[DEBUG] Command queued successfully: Type=%s, HasConfig=%v", cmd.Type, cmd.ConfigOverride != nil)
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("Command accepted"))
}

func (ws *WebServer) validateConfig(cfg *Config) error {
	if cfg.NumMasters <= 0 || cfg.NumSlaves <= 0 {
		return &validationError{msg: "NumMasters and NumSlaves must be positive"}
	}
	if cfg.TotalCycles <= 0 {
		return &validationError{msg: "TotalCycles must be positive"}
	}
	if cfg.RequestRateConfig < 0 || cfg.RequestRateConfig > 1 {
		return &validationError{msg: "RequestRateConfig must be between 0 and 1"}
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

func (ws *WebServer) handleConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configs := GetPredefinedConfigs()
	// Return only name and description, not the full config
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
