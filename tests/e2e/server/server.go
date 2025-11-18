//go:build e2e

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/tests/e2e/mocks"
	"github.com/Readm/flow_sim/tests/e2e/model"
)

// Options configures the bridge server.
type Options struct {
	Controller         *mocks.Controller
	StaticDir          string
	DefaultConfig      config.EntityConfig
	DefaultTotalCycles int
}

// Server hosts HTTP + WS endpoints compatible with web/static resources.
type Server struct {
	controller *mocks.Controller
	staticDir  string

	defaultConfig config.EntityConfig
	defaultCycles int

	mux        *http.ServeMux
	httpServer *httptest.Server

	latestMu    sync.RWMutex
	latestFrame *model.Frame

	hub    *wsHub
	cancel context.CancelFunc
}

// New spins up a new bridge server. Call Close when done.
func New(opts Options) (*Server, error) {
	if opts.Controller == nil {
		return nil, errors.New("controller is required")
	}
	if opts.StaticDir == "" {
		return nil, errors.New("static dir is required")
	}
	if opts.DefaultTotalCycles <= 0 {
		opts.DefaultTotalCycles = 64
	}
	if len(opts.DefaultConfig.Nodes) == 0 {
		opts.DefaultConfig = defaultEntityConfig()
	}

	srv := &Server{
		controller:    opts.Controller,
		staticDir:     opts.StaticDir,
		defaultConfig: opts.DefaultConfig,
		defaultCycles: opts.DefaultTotalCycles,
		hub:           newHub(),
	}

	srv.mux = http.NewServeMux()
	srv.mux.HandleFunc("/api/frame", srv.handleFrame)
	srv.mux.HandleFunc("/api/control", srv.handleControl)
	srv.mux.HandleFunc("/api/configs", srv.handleConfigs)
	srv.mux.HandleFunc("/ws", srv.handleWebSocket)
	fileHandler := http.FileServer(http.Dir(srv.staticDir))
	srv.mux.Handle("/", fileHandler)

	ctx, cancel := context.WithCancel(context.Background())
	srv.cancel = cancel
	go srv.consumeFrames(ctx)

	srv.httpServer = httptest.NewServer(loggingMiddleware(srv.mux))
	return srv, nil
}

// Close shuts down HTTP listeners and goroutines.
func (s *Server) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.httpServer != nil {
		s.httpServer.Close()
	}
	s.hub.closeAll()
}

// BaseURL returns the root URL of the server.
func (s *Server) BaseURL() string {
	if s.httpServer == nil {
		return ""
	}
	return s.httpServer.URL
}

func (s *Server) consumeFrames(ctx context.Context) {
	for {
		select {
		case frame := <-s.controller.Frames():
			if frame == nil {
				continue
			}
			s.latestMu.Lock()
			s.latestFrame = frame
			s.latestMu.Unlock()
			s.broadcast(frame)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) broadcast(frame *model.Frame) {
	data, err := json.Marshal(frame)
	if err != nil {
		return
	}
	s.hub.broadcast(data)
}

func (s *Server) handleFrame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.latestMu.RLock()
	frame := s.latestFrame
	s.latestMu.RUnlock()
	if frame == nil {
		http.Error(w, "no frame available", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(frame)
}

func (s *Server) handleConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		TotalCycles int    `json:"totalCycles"`
	}{
		{Name: "demo", Description: "Minimal topology for Flow View tests", TotalCycles: s.defaultCycles},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var req controlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.applyControl(r.Context(), &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("Command accepted"))
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := (&websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.hub.add(conn)

	s.latestMu.RLock()
	frame := s.latestFrame
	s.latestMu.RUnlock()
	if frame != nil {
		if data, err := json.Marshal(frame); err == nil {
			_ = conn.WriteMessage(websocket.TextMessage, data)
		}
	}

	go s.readFromWebSocket(conn)
}

func (s *Server) readFromWebSocket(conn *websocket.Conn) {
	defer s.hub.remove(conn)
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var req controlRequest
		if err := json.Unmarshal(data, &req); err != nil {
			continue
		}
		_ = s.applyControl(context.Background(), &req)
	}
}

func (s *Server) applyControl(ctx context.Context, req *controlRequest) error {
	if req == nil {
		return errors.New("control request is nil")
	}
	switch req.Type {
	case "pause":
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := s.controller.Stop(ctx); err != nil {
			return err
		}
		s.controller.NotifyControl("pause", 0)
	case "run":
		cycles := req.Cycles
		if cycles <= 0 {
			cycles = 1
		}
		s.controller.NotifyControl("run", uint64(cycles))
	case "reset":
		total := req.TotalCycles
		if total <= 0 {
			total = s.defaultCycles
		}
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := s.controller.Start(ctx, s.defaultConfig, uint64(total)); err != nil {
			return err
		}
		s.controller.NotifyControl("reset", uint64(total))
	default:
		return errors.New("invalid command type")
	}
	return nil
}

type controlRequest struct {
	Type        string `json:"type"`
	ConfigName  string `json:"configName"`
	TotalCycles int    `json:"totalCycles"`
	Cycles      int    `json:"cycles"`
}

func defaultEntityConfig() config.EntityConfig {
	return config.EntityConfig{
		Nodes: []config.NodeConfig{
			{ID: 0},
			{ID: 1},
			{ID: 2},
		},
		Link: config.LinkConfig{
			BaseDelay:  time.Millisecond,
			Multiplier: 1,
		},
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

// wsHub is a lightweight broadcaster for WebSocket clients.
type wsHub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func newHub() *wsHub {
	return &wsHub{
		clients: make(map[*websocket.Conn]struct{}),
	}
}

func (h *wsHub) add(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = struct{}{}
}

func (h *wsHub) remove(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[conn]; ok {
		delete(h.clients, conn)
		conn.Close()
	}
}

func (h *wsHub) broadcast(payload []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			delete(h.clients, conn)
			conn.Close()
		}
	}
}

func (h *wsHub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		conn.Close()
	}
	h.clients = make(map[*websocket.Conn]struct{})
}

// StaticDir returns the absolute static directory path for debugging.
func (s *Server) StaticDir() string {
	return filepath.Clean(s.staticDir)
}
