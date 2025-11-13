package main

import (
	"context"
	"fmt"

	"github.com/Readm/flow_sim/hooks"
	"github.com/Readm/flow_sim/visual"
)

// WebVisualizer bridges the simulator with the web server.
type WebVisualizer struct {
	headless bool
	server   *WebServer
	txnMgr   *TransactionManager
}

// NewWebVisualizer creates a new web visualizer instance and starts the server.
func NewWebVisualizer(txnMgr *TransactionManager) *WebVisualizer {
	addr := "127.0.0.1:8080"
	server := NewWebServer(addr, txnMgr)
	server.Start()

	fmt.Printf("[DEBUG] Web server started at http://%s\n", addr)

	return &WebVisualizer{
		headless: false,
		server:   server,
		txnMgr:   txnMgr,
	}
}

// SetTransactionManager updates the transaction manager used by the visualizer APIs.
func (w *WebVisualizer) SetTransactionManager(txnMgr *TransactionManager) {
	w.txnMgr = txnMgr
	if w.server != nil {
		w.server.SetTransactionManager(txnMgr)
	}
}

// SetHeadless switches headless state.
func (w *WebVisualizer) SetHeadless(headless bool) {
	w.headless = headless
}

// IsHeadless returns whether visualizer runs without UI.
func (w *WebVisualizer) IsHeadless() bool {
	return w.headless
}

// PublishFrame updates the server with the latest frame.
func (w *WebVisualizer) PublishFrame(frame any) {
	sf, _ := frame.(*SimulationFrame)
	if w.server != nil && sf != nil {
		w.server.UpdateFrame(sf)
	}
}

// NextCommand returns the next control command if available, non-blocking.
func (w *WebVisualizer) NextCommand() (visual.ControlCommand, bool) {
	if w.server == nil {
		return visual.ControlCommand{Type: visual.CommandNone}, false
	}
	cmd, ok := w.server.NextCommand()
	if !ok {
		return visual.ControlCommand{Type: visual.CommandNone}, false
	}
	return cmd, true
}

// WaitCommand blocks until a command is available or the context is cancelled.
func (w *WebVisualizer) WaitCommand(ctx context.Context) (visual.ControlCommand, bool) {
	if w.server == nil {
		return visual.ControlCommand{Type: visual.CommandNone}, false
	}
	return w.server.WaitCommand(ctx)
}

func (w *WebVisualizer) SetPluginRegistry(reg *hooks.Registry) {
	if w == nil {
		return
	}
	if w.server != nil {
		w.server.SetPluginRegistry(reg)
	}
}
