package main

import (
	"flag"
	"log"
	"net/http"
	"path/filepath"

	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/testing/mocks"
	"github.com/Readm/flow_sim/internal/visualization/mockserver"
)

func main() {
	port := flag.String("port", "8080", "Port to listen on")
	staticDir := flag.String("static", "./web/examples", "Path to static frontend files")
	flag.Parse()

	// 1. Create Mock Controller
	log.Printf(" Step 1: Creating Mock Controller...")
	ctrl := mocks.NewController()
	log.Printf(" Mock Controller created")

	// 2. Initialize with some dummy state so the UI has something to show
	log.Printf(" Step 2: Setting initial state...")
	initialState := state.NetworkState{
		CurrentCycle: 0,
		Nodes: []state.NodeState{
			{ID: 1, Type: "WorkerNode"},
			{ID: 2, Type: "CentralSwitch"},
			{ID: 3, Type: "WorkerNode"},
		},
		Links: []state.LinkState{
			{SourceID: 1, TargetID: 2, Occupancy: []int{0}},
			{SourceID: 2, TargetID: 3, Occupancy: []int{0}},
		},
	}
	ctrl.SetState(initialState)
	log.Printf(" Initial state set")

	// 3. Create Server
	log.Printf(" Step 3: Creating mockserver...")
	absStatic, _ := filepath.Abs(*staticDir)
	log.Printf("Serving static files from: %s", absStatic)

	srv, err := mockserver.New(mockserver.Options{
		Controller:         ctrl,
		StaticDir:          *staticDir,
		DefaultTotalCycles: 1000,
	})
	if err != nil {
		log.Fatalf(" Failed to create server: %v", err)
	}
	defer srv.Close()
	log.Printf(" Mockserver created")

	// 4. Start HTTP Server
	log.Printf(" Step 4: Starting HTTP server...")
	addr := ":" + *port
	log.Printf("Visualization Server listening on http://localhost%s", addr)

	// Use the exposed Handler to serve on our custom port
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatalf(" Server failed: %v", err)
	}
}
