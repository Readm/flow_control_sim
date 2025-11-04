package main

// WebVisualizer bridges the simulator with the web server.
type WebVisualizer struct {
	headless bool
	server   *WebServer
}

// NewWebVisualizer creates a new web visualizer instance and starts the server.
func NewWebVisualizer() *WebVisualizer {
	addr := "127.0.0.1:8080"
	server := NewWebServer(addr)
	server.Start()

	return &WebVisualizer{
		headless: false,
		server:   server,
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
func (w *WebVisualizer) PublishFrame(frame *SimulationFrame) {
	if w.server != nil {
		w.server.UpdateFrame(frame)
	}
}

// NextCommand returns the next control command if available, non-blocking.
func (w *WebVisualizer) NextCommand() (ControlCommand, bool) {
	if w.server == nil {
		return ControlCommand{Type: CommandNone}, false
	}
	cmd, ok := w.server.NextCommand()
	if !ok {
		return ControlCommand{Type: CommandNone}, false
	}
	return cmd, true
}
