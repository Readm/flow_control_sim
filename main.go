package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/pipeline"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
	"github.com/Readm/flow_sim/pkg/controller"
	"github.com/Readm/flow_sim/pkg/visual/frame"
	"github.com/Readm/flow_sim/pkg/visual/recorder"
)

const (
	defaultPort        = ":8080"
	defaultTotalCycles = 64
	mailboxSize        = 8
	linkLatency        = 5
	linkBandwidth      = 1
)

func main() {
	// Get static directory path
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("cannot determine caller path")
	}
	projectRoot := filepath.Dir(filename)
	staticDir := filepath.Join(projectRoot, "web", "static")

	// Create ManagerBuilder
	builder := createManagerBuilder()

	// Create Controller
	ctrl := controller.New(builder)

	// Create Web Server
	srv, err := newWebServer(staticDir, ctrl)
	if err != nil {
		log.Fatalf("failed to create web server: %v", err)
	}

	// Create and send initial frame (cycle 0, paused)
	initialFrame := createInitialFrame()
	srv.setLatestFrame(initialFrame)
	srv.broadcast(initialFrame)

	// Start consuming frames from controller
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.consumeFrames(ctx, ctrl)

	// Start HTTP server
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	} else if port[0] != ':' {
		port = ":" + port
	}

	log.Printf("Starting web server on http://localhost%s", port)
	if err := http.ListenAndServe(port, srv.mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// flowNode implements node.Node for two-node packet exchange demo.
type flowNode struct {
	id          int
	peerID      int
	flow        pipeline.Pipeline
	totalCycles uint64
}

func newFlowNode(id, peerID int, totalCycles uint64) *flowNode {
	f := pipeline.NewFIFO(id, mailboxSize)
	return &flowNode{
		id:          id,
		peerID:      peerID,
		flow:        f,
		totalCycles: totalCycles,
	}
}

func (n *flowNode) ID() int {
	return n.id
}

func (n *flowNode) Flows() []pipeline.Pipeline {
	return []pipeline.Pipeline{n.flow}
}

func (n *flowNode) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
	// Send packet to peer if not the last cycle (before processing, so it's included in this cycle)
	if cycle+1 < n.totalCycles {
		payload := fmt.Sprintf("node-%d-cycle-%d", n.id, cycle)
		n.flow.Emit(packet.Packet{
			SourceID: n.id,
			TargetID: n.peerID,
			Payload:  payload,
		})
	}

	// Process incoming packets
	if err := n.flow.ProcessCycle(int(cycle)); err != nil {
		return err
	}

	return nil
}

// createManagerBuilder returns a ManagerBuilder that creates two nodes exchanging packets.
func createManagerBuilder() controller.ManagerBuilder {
	return func(cfg config.EntityConfig) (*network.Manager, uint64, error) {
		// Determine total cycles
		totalCycles := uint64(defaultTotalCycles)
		if len(cfg.Nodes) < 2 {
			return nil, 0, errors.New("config requires at least 2 nodes")
		}

		// Create two nodes (ID 0 and 1) that exchange packets
		node0 := newFlowNode(0, 1, totalCycles)
		node1 := newFlowNode(1, 0, totalCycles)

		nodes := []node.Node{node0, node1}

		// Create output ports for flows
		flow0OutPort := ahead_port.NewAheadPort(mailboxSize)
		flow1OutPort := ahead_port.NewAheadPort(mailboxSize)

		// Connect flows to output ports
		node0.Flows()[0].SetOutPort(flow0OutPort)
		node1.Flows()[0].SetOutPort(flow1OutPort)

		// Create bidirectional links with 5 cycle latency and bandwidth 1
		linkAB := link.NewLink(0, 1, flow0OutPort, node1.Flows()[0].InPort(), linkLatency, linkBandwidth)
		linkBA := link.NewLink(1, 0, flow1OutPort, node0.Flows()[0].InPort(), linkLatency, linkBandwidth)

		graph := map[int][]*link.Link{
			0: {linkAB}, // node 0 -> node 1
			1: {linkBA}, // node 1 -> node 0
		}

		mgr, err := network.NewManager(nodes, graph)
		if err != nil {
			return nil, 0, err
		}

		return mgr, totalCycles, nil
	}
}

// webServer hosts HTTP + WS endpoints compatible with web/static resources.
type webServer struct {
	controller controller.SimulationController
	staticDir  string

	defaultConfig config.EntityConfig
	defaultCycles int

	mux *http.ServeMux

	latestMu    sync.RWMutex
	latestFrame *frame.Frame

	hub *wsHub

	// Persistent simulation state
	simMu        sync.Mutex
	manager      *network.Manager
	recorder     *recorder.Recorder
	currentCycle uint64
	isRunning    bool
}

func newWebServer(staticDir string, ctrl controller.SimulationController) (*webServer, error) {
	if ctrl == nil {
		return nil, errors.New("controller is required")
	}
	if staticDir == "" {
		return nil, errors.New("static dir is required")
	}

	srv := &webServer{
		controller:    ctrl,
		staticDir:     staticDir,
		defaultCycles: defaultTotalCycles,
		defaultConfig: defaultEntityConfig(),
		hub:           newHub(),
		currentCycle:  0,
		isRunning:     false,
	}

	srv.mux = http.NewServeMux()
	srv.mux.HandleFunc("/api/frame", srv.handleFrame)
	srv.mux.HandleFunc("/api/control", srv.handleControl)
	srv.mux.HandleFunc("/api/configs", srv.handleConfigs)
	srv.mux.HandleFunc("/ws", srv.handleWebSocket)
	fileHandler := http.FileServer(http.Dir(srv.staticDir))
	srv.mux.Handle("/", fileHandler)

	return srv, nil
}

func (s *webServer) consumeFrames(ctx context.Context, ctrl controller.SimulationController) {
	for {
		select {
		case frame := <-ctrl.Frames():
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

func (s *webServer) setLatestFrame(f *frame.Frame) {
	s.latestMu.Lock()
	defer s.latestMu.Unlock()
	s.latestFrame = f
}

func (s *webServer) broadcast(frame *frame.Frame) {
	data, err := json.Marshal(frame)
	if err != nil {
		return
	}
	s.hub.broadcast(data)
}

func (s *webServer) handleFrame(w http.ResponseWriter, r *http.Request) {
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

func (s *webServer) handleConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		TotalCycles int    `json:"totalCycles"`
	}{
		{Name: "demo", Description: "Two nodes exchanging packets", TotalCycles: s.defaultCycles},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *webServer) handleControl(w http.ResponseWriter, r *http.Request) {
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

func (s *webServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
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

func (s *webServer) readFromWebSocket(conn *websocket.Conn) {
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

func (s *webServer) applyControl(ctx context.Context, req *controlRequest) error {
	if req == nil {
		return errors.New("control request is nil")
	}
	switch req.Type {
	case "advance":
		cycles := req.Cycles
		if cycles <= 0 {
			cycles = 1
		}
		s.advance(uint64(cycles))
	case "reset":
		s.reset()
	default:
		return errors.New("invalid command type")
	}
	return nil
}

// advance runs N cycles from the current cycle
func (s *webServer) advance(cycles uint64) {
	s.simMu.Lock()
	if s.isRunning {
		s.simMu.Unlock()
		return
	}
	if s.manager == nil {
		// Create manager if it doesn't exist
		mgr, err := s.createManager()
		if err != nil {
			s.simMu.Unlock()
			log.Printf("failed to create manager: %v", err)
			return
		}
		s.manager = mgr
		s.currentCycle = 0
	}
	startCycle := s.currentCycle
	endCycle := startCycle + cycles
	s.isRunning = true
	s.simMu.Unlock()

	go func() {
		defer func() {
			s.simMu.Lock()
			s.isRunning = false
			s.simMu.Unlock()
		}()

		s.simMu.Lock()
		mgr := s.manager
		rec := s.recorder
		if rec == nil {
			rec = recorder.New(32)
			rec.SetPaused(false)
			s.recorder = rec
			mgr.SetCycleHook(rec)
		}
		s.simMu.Unlock()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start frame relay
		frameCh := make(chan *frame.Frame, 32)
		relayCtx, relayCancel := context.WithCancel(context.Background())
		defer relayCancel()
		go func() {
			for {
				select {
				case <-relayCtx.Done():
					return
				case fr, ok := <-rec.Frames():
					if !ok {
						return
					}
					select {
					case frameCh <- fr:
					default:
					}
				}
			}
		}()

		// Run cycles from startCycle to endCycle
		err := s.runCycles(ctx, startCycle, endCycle)
		if err != nil {
			log.Printf("advance error: %v", err)
		}

		// Update current cycle
		s.simMu.Lock()
		s.currentCycle = endCycle
		s.simMu.Unlock()

		// Wait a bit for remaining frames
		time.Sleep(50 * time.Millisecond)
		relayCancel()

		// Broadcast remaining frames
		close(frameCh)
		for fr := range frameCh {
			s.setLatestFrame(fr)
			s.broadcast(fr)
		}
	}()
}

// reset creates a new manager and resets to cycle 0
func (s *webServer) reset() {
	s.simMu.Lock()
	defer s.simMu.Unlock()

	if s.isRunning {
		return
	}

	// Create new manager
	mgr, err := s.createManager()
	if err != nil {
		log.Printf("failed to create manager: %v", err)
		return
	}

	s.manager = mgr
	s.currentCycle = 0
	s.recorder = nil

	// Create and send initial frame
	initialFrame := createInitialFrame()
	s.setLatestFrame(initialFrame)
	s.broadcast(initialFrame)
}

// createManager creates a new network manager
func (s *webServer) createManager() (*network.Manager, error) {
	cfg := s.defaultConfig
	builder := createManagerBuilder()
	mgr, _, err := builder(cfg)
	return mgr, err
}

// runCycles runs cycles from startCycle to endCycle (exclusive)
func (s *webServer) runCycles(ctx context.Context, startCycle, endCycle uint64) error {
	if endCycle <= startCycle {
		return nil
	}

	s.simMu.Lock()
	mgr := s.manager
	rec := s.recorder
	if rec == nil {
		rec = recorder.New(32)
		rec.SetPaused(false)
		s.recorder = rec
		mgr.SetCycleHook(rec)
	}
	s.simMu.Unlock()

	// Run cycles
	for cycle := startCycle; cycle < endCycle; cycle++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := s.runSingleCycle(ctx, mgr, rec, cycle); err != nil {
			return err
		}

		// Broadcast frames from recorder
		if rec != nil {
			for {
				select {
				case fr := <-rec.Frames():
					s.setLatestFrame(fr)
					s.broadcast(fr)
				default:
					goto nextCycle
				}
			}
		nextCycle:
		}
	}

	return nil
}

// runSingleCycle runs a single cycle using Manager's RunFrom method
func (s *webServer) runSingleCycle(ctx context.Context, mgr *network.Manager, rec *recorder.Recorder, cycle uint64) error {
	// Use Manager's RunFrom to run a single cycle starting from the specified cycle
	return mgr.RunFrom(ctx, cycle, 1)
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
		},
		Link: config.LinkConfig{
			BaseDelay:  time.Millisecond,
			Multiplier: 1,
		},
	}
}

// createInitialFrame creates an initial frame at cycle 0 with paused state.
func createInitialFrame() *frame.Frame {
	return &frame.Frame{
		Cycle:         0,
		Paused:        true,
		InFlightCount: 0,
		ConfigHash:    "initial",
		Nodes: []frame.Node{
			{
				ID:    0,
				Label: "Node 0",
				Type:  "generic",
				Payload: map[string]any{
					"processed": 0,
				},
				// Explicitly set backpressure fields to false
				InQueueBackpressure:    false,
				OutQueueBackpressure:   false,
				DownstreamBackpressure: false,
			},
			{
				ID:    1,
				Label: "Node 1",
				Type:  "generic",
				Payload: map[string]any{
					"processed": 0,
				},
				// Explicitly set backpressure fields to false
				InQueueBackpressure:    false,
				OutQueueBackpressure:   false,
				DownstreamBackpressure: false,
			},
		},
		Edges: []frame.Edge{
			{
				Source:         0,
				Target:         1,
				Label:          "0→1",
				Latency:        linkLatency,
				BandwidthLimit: int(linkBandwidth),
				PipelineStages: make([]frame.PipelineStage, linkLatency),
				// Explicitly set backpressure to false
				Backpressured: false,
			},
			{
				Source:         1,
				Target:         0,
				Label:          "1→0",
				Latency:        linkLatency,
				BandwidthLimit: int(linkBandwidth),
				PipelineStages: make([]frame.PipelineStage, linkLatency),
				// Explicitly set backpressure to false
				Backpressured: false,
			},
		},
	}
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
