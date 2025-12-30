//go:build e2e

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
	"github.com/Readm/flow_sim/tests/e2e/mocks"
)

// Options configures the bridge server.
type Options struct {
	Controller         *mocks.Controller
	StaticDir          string
	DefaultConfig      config.EntityConfig
	DefaultTotalCycles int
}

// Server hosts HTTP endpoints compatible with CyEditor.
type Server struct {
	controller *mocks.Controller
	staticDir  string

	defaultConfig config.EntityConfig
	defaultCycles int

	mux        *http.ServeMux
	httpServer *httptest.Server

	clientsMu sync.Mutex
	clients   map[*websocket.Conn]bool
	upgrader  websocket.Upgrader

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
		clients:       make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // Allow all CORS for e2e
		},
	}

	srv.mux = http.NewServeMux()

	// API Endpoints for CyEditor integration
	srv.mux.HandleFunc("/load_networks", srv.cors(srv.handleLoadNetworks))
	srv.mux.HandleFunc("/reset_network", srv.cors(srv.handleResetNetwork))
	srv.mux.HandleFunc("/advance_to", srv.cors(srv.handleAdvanceTo))
	srv.mux.HandleFunc("/build_network", srv.cors(srv.handleBuildNetwork))
	srv.mux.HandleFunc("/load_preset", srv.cors(srv.handleLoadPreset))
	srv.mux.HandleFunc("/ws", srv.handleWS)

	// Serve Static Files
	fileHandler := http.FileServer(http.Dir(srv.staticDir))
	srv.mux.Handle("/", fileHandler)

	ctx, cancel := context.WithCancel(context.Background())
	srv.cancel = cancel

	// Start broadcast loop
	go srv.broadcastLoop(ctx)

	srv.httpServer = httptest.NewServer(loggingMiddleware(srv.mux))
	return srv, nil
}

func (s *Server) broadcastLoop(ctx context.Context) {
	sub := s.controller.Subscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case ns := <-sub:
			// Convert to CyNetwork
			cyNet := visualization.StateToCyNetwork(ns)

			// Broadcast to all clients
			s.clientsMu.Lock()
			for client := range s.clients {
				err := client.WriteJSON(cyNet)
				if err != nil {
					log.Printf("WS write error: %v", err)
					client.Close()
					delete(s.clients, client)
				}
			}
			s.clientsMu.Unlock()
		}
	}
}

// Close shuts down HTTP listeners and goroutines.
func (s *Server) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.httpServer != nil {
		s.httpServer.Close()
	}
}

// BaseURL returns the root URL of the server.
func (s *Server) BaseURL() string {
	if s.httpServer == nil {
		return ""
	}
	return s.httpServer.URL
}

// Handler returns the HTTP handler for custom serving.
func (s *Server) Handler() http.Handler {
	return loggingMiddleware(s.mux)
}

func (s *Server) StaticDir() string {
	return filepath.Clean(s.staticDir)
}

// cors adds CORS headers to the response
func (s *Server) cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

// GET /ws
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS upgrade failed: %v", err)
		return
	}

	s.clientsMu.Lock()
	s.clients[conn] = true
	s.clientsMu.Unlock()

	// Send initial state
	ns := s.controller.GetState()
	if ns != nil {
		cyNet := visualization.StateToCyNetwork(*ns)
		if err := conn.WriteJSON(cyNet); err != nil {
			log.Println("WS initial write failed:", err)
			return
		}
	}

	// Keep connection open until closed
	go func() {
		defer func() {
			s.clientsMu.Lock()
			delete(s.clients, conn)
			s.clientsMu.Unlock()
			conn.Close()
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}

// GET /load_networks
func (s *Server) handleLoadNetworks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ns := s.controller.GetState()

	var networks []protocol.CyNetwork
	if ns != nil {
		cyNet := visualization.StateToCyNetwork(*ns)
		networks = append(networks, cyNet)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(networks)
}

// POST /reset_network
func (s *Server) handleResetNetwork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ns := s.controller.GetState()
	if ns != nil {
		cyNet := visualization.StateToCyNetwork(*ns)
		json.NewEncoder(w).Encode(cyNet)
	} else {
		w.Write([]byte("{}"))
	}
}

// POST /advance_to
func (s *Server) handleAdvanceTo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Cycle int `json:"cycle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	go func() {
		_ = s.controller.Run(context.Background(), s.defaultConfig, uint64(req.Cycle))
	}()

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]string{"status": fmt.Sprintf("Advanced to %d", req.Cycle)}
	json.NewEncoder(w).Encode(resp)
}

// POST /build_network
func (s *Server) handleBuildNetwork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cyNet protocol.CyNetwork
	if err := json.NewDecoder(r.Body).Decode(&cyNet); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := visualization.CyNetworkToConfig(cyNet)
	if err := s.controller.Rebuild(cfg); err != nil {
		http.Error(w, "rebuild failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// Retrieve new state to confirm
	ns := s.controller.GetState()
	if ns != nil {
		newCyNet := visualization.StateToCyNetwork(*ns)
		json.NewEncoder(w).Encode(newCyNet)
	} else {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// POST /load_preset
func (s *Server) handleLoadPreset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name   string         `json:"name"`
		Params map[string]int `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.controller.LoadPreset(req.Name, req.Params); err != nil {
		http.Error(w, "load preset failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// Retrieve new state to return
	ns := s.controller.GetState()
	if ns != nil {
		cyNet := visualization.StateToCyNetwork(*ns)
		json.NewEncoder(w).Encode(cyNet)
	} else {
		// Should not happen if LoadPreset succeeds
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
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
		fmt.Printf("[%s] %s %s\n", time.Now().Format(time.RFC3339), r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
