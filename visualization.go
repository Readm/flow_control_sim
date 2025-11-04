package main

// ControlCommandType represents types of control instructions from UI.
type ControlCommandType string

const (
	CommandNone   ControlCommandType = "none"
	CommandPause  ControlCommandType = "pause"
	CommandResume ControlCommandType = "resume"
	CommandReset  ControlCommandType = "reset"
)

// ControlCommand captures a control instruction for the simulator.
type ControlCommand struct {
	Type           ControlCommandType
	ConfigOverride *Config
}

// NodeSnapshot describes a node state in a given cycle for visualization.
type NodeSnapshot struct {
	ID      int            `json:"id"`
	Type    NodeType       `json:"type"`
	Label   string         `json:"label"`
	Queues  []QueueInfo    `json:"queues"`
	Payload map[string]any `json:"payload,omitempty"`
}

// EdgeSnapshot describes a logical connection between nodes.
type EdgeSnapshot struct {
	Source  int    `json:"source"`
	Target  int    `json:"target"`
	Label   string `json:"label"`
	Latency int    `json:"latency"`
}

// SimulationFrame aggregates information required by frontends for a cycle.
type SimulationFrame struct {
	Cycle         int              `json:"cycle"`
	Nodes         []NodeSnapshot   `json:"nodes"`
	Edges         []EdgeSnapshot   `json:"edges"`
	InFlightCount int              `json:"inFlightCount"`
	Stats         *SimulationStats `json:"stats,omitempty"`
}

// Visualizer defines methods for visualization implementations.
type Visualizer interface {
	SetHeadless(headless bool)
	IsHeadless() bool
	PublishFrame(frame *SimulationFrame)
	NextCommand() (ControlCommand, bool)
}

// NullVisualizer is a no-op implementation used for headless mode.
type NullVisualizer struct {
	headless bool
}

// NewNullVisualizer creates a new NullVisualizer.
func NewNullVisualizer() *NullVisualizer {
	return &NullVisualizer{headless: true}
}

func (n *NullVisualizer) SetHeadless(headless bool) {
	n.headless = headless
}

func (n *NullVisualizer) IsHeadless() bool {
	return n.headless
}

func (n *NullVisualizer) PublishFrame(frame *SimulationFrame) {}

func (n *NullVisualizer) NextCommand() (ControlCommand, bool) {
	return ControlCommand{Type: CommandNone}, false
}
