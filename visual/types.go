package visual

import "context"

// ControlCommandType represents types of control instructions from UI.
type ControlCommandType string

const (
	CommandNone   ControlCommandType = "none"
	CommandPause  ControlCommandType = "pause"
	CommandResume ControlCommandType = "resume"
	CommandReset  ControlCommandType = "reset"
	CommandStep   ControlCommandType = "step"
)

// ControlCommand captures a control instruction for the simulator.
type ControlCommand struct {
	Type           ControlCommandType
	ConfigOverride any
}

// Visualizer defines methods for visualization implementations.
type Visualizer interface {
	SetHeadless(headless bool)
	IsHeadless() bool
	PublishFrame(frame any)
	NextCommand() (ControlCommand, bool)
	WaitCommand(ctx context.Context) (ControlCommand, bool)
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

func (n *NullVisualizer) PublishFrame(frame any) {}

func (n *NullVisualizer) NextCommand() (ControlCommand, bool) {
	return ControlCommand{Type: CommandNone}, false
}

func (n *NullVisualizer) WaitCommand(ctx context.Context) (ControlCommand, bool) {
	select {
	case <-ctx.Done():
		return ControlCommand{Type: CommandNone}, false
	default:
		return ControlCommand{Type: CommandNone}, false
	}
}
