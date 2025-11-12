package main

import (
	"context"
	"net/http"
	"sync"

	"github.com/Readm/flow_sim/visual"
)

// WebServer provides HTTP endpoints for visualization and control.
type WebServer struct {
	mu          sync.RWMutex
	latestFrame *SimulationFrame
	latestStats *SimulationStats
	commands    CommandQueue
	server      *http.Server
	hub         *wsHub
	txnMgr      *TransactionManager // for transaction timeline API
}

// NewWebServer creates a new web server instance.
func NewWebServer(addr string, txnMgr *TransactionManager) *WebServer {
	ws := &WebServer{
		commands: newChannelCommandQueue(10),
		txnMgr:   txnMgr,
	}

	ws.hub = newHub()
	router := NewRouter(ws)
	ws.server = &http.Server{
		Addr:    addr,
		Handler: router,
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

func (ws *WebServer) registerHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/frame", ws.handleFrame)
	mux.HandleFunc("/api/stats", ws.handleStats)
	mux.HandleFunc("/api/control", ws.handleControl)
	mux.HandleFunc("/api/configs", ws.handleConfigs)
	mux.HandleFunc("/api/transactions", ws.handleTransactions)
	mux.HandleFunc("/api/transaction/", ws.handleTransactionTimeline)
	mux.HandleFunc("/api/transactions/timelines", ws.handleTransactionTimelines)
	mux.HandleFunc("/ws", ws.handleWebSocket)
	mux.Handle("/", http.FileServer(http.Dir("web/static")))
}

// UpdateFrame updates the latest frame and stats, and broadcasts to WebSocket clients.
func (ws *WebServer) UpdateFrame(frame *SimulationFrame) {
	ws.mu.Lock()
	ws.latestFrame = frame
	if frame != nil {
		ws.latestStats = frame.Stats
	}
	ws.mu.Unlock()

	ws.hub.broadcastFrame(frame)
}

// handleWebSocket handles WebSocket connections.
func (ws *WebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ws.hub.handle(ws, w, r)
}

// queueCommand queues a control command. Returns true if queued successfully.
func (ws *WebServer) queueCommand(cmd visual.ControlCommand) bool {
	if ws.commands == nil {
		return false
	}
	return ws.commands.Enqueue(cmd)
}

// NextCommand returns the next control command if available, non-blocking.
func (ws *WebServer) NextCommand() (visual.ControlCommand, bool) {
	if ws.commands == nil {
		return visual.ControlCommand{Type: visual.CommandNone}, false
	}
	return ws.commands.TryDequeue()
}

// WaitCommand blocks until a control command is available or the context is cancelled.
func (ws *WebServer) WaitCommand(ctx context.Context) (visual.ControlCommand, bool) {
	if ws.commands == nil {
		return visual.ControlCommand{Type: visual.CommandNone}, false
	}
	return ws.commands.Next(ctx)
}

type controlRequest struct {
	Type        string  `json:"type"`
	Config      *Config `json:"config,omitempty"`
	ConfigName  string  `json:"configName,omitempty"`
	TotalCycles *int    `json:"totalCycles,omitempty"`
}

// processControlRequest processes a control request and returns a ControlCommand.
// Returns (command, error). If error is not nil, the command is invalid.
func (ws *WebServer) processControlRequest(req *controlRequest) (*visual.ControlCommand, error) {
	var cmd visual.ControlCommand

	switch req.Type {
	case "pause":
		cmd.Type = visual.CommandPause
	case "resume":
		cmd.Type = visual.CommandResume
	case "reset":
		cmd.Type = visual.CommandReset
		if req.ConfigName != "" {
			predefinedCfg := GetConfigByName(req.ConfigName)
			if predefinedCfg == nil {
				return nil, &validationError{msg: "Invalid config name: " + req.ConfigName}
			}
			if req.TotalCycles != nil && *req.TotalCycles > 0 {
				predefinedCfg.TotalCycles = *req.TotalCycles
			}
			cmd.ConfigOverride = predefinedCfg
		} else if req.Config != nil {
			if err := ws.validateConfig(req.Config); err != nil {
				return nil, err
			}
			cmd.ConfigOverride = req.Config
		}
	case "step":
		cmd.Type = visual.CommandStep
	default:
		return nil, &validationError{msg: "Invalid command type: " + req.Type}
	}

	return &cmd, nil
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

// SetTransactionManager updates the transaction manager reference used by APIs.
func (ws *WebServer) SetTransactionManager(txnMgr *TransactionManager) {
	ws.mu.Lock()
	ws.txnMgr = txnMgr
	ws.mu.Unlock()
}
