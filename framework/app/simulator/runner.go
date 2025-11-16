package simulator

import "context"

// Runner glues command handling and visualization helpers for the main simulator loop.
type Runner[TCommand any, Frame any] struct {
	commandLoop *CommandLoop[TCommand]
	visual      *VisualBridge[Frame]
}

// NewRunner creates a new Runner instance.
func NewRunner[TCommand any, Frame any](loop *CommandLoop[TCommand], visual *VisualBridge[Frame]) *Runner[TCommand, Frame] {
	return &Runner[TCommand, Frame]{
		commandLoop: loop,
		visual:      visual,
	}
}

// DrainPendingCommands pulls all queued commands through the underlying command loop.
func (r *Runner[TCommand, Frame]) DrainPendingCommands() bool {
	if r == nil || r.commandLoop == nil {
		return true
	}
	return r.commandLoop.DrainPending()
}

// WaitForCommand blocks on the command loop until a command arrives or context is cancelled.
func (r *Runner[TCommand, Frame]) WaitForCommand(ctx context.Context) bool {
	if r == nil || r.commandLoop == nil {
		return true
	}
	return r.commandLoop.WaitAndHandle(ctx)
}

// PublishFrame emits a frame through the visual bridge if visualization is enabled.
func (r *Runner[TCommand, Frame]) PublishFrame(frame Frame) {
	if r == nil || r.visual == nil {
		return
	}
	r.visual.Publish(frame)
}

// VisualEnabled reports whether visualization bridge is active.
func (r *Runner[TCommand, Frame]) VisualEnabled() bool {
	if r == nil || r.visual == nil {
		return false
	}
	return !r.visual.IsHeadless()
}

// UpdateVisualBridge swaps the visual bridge reference (e.g., after reset).
func (r *Runner[TCommand, Frame]) UpdateVisualBridge(visual *VisualBridge[Frame]) {
	if r == nil {
		return
	}
	r.visual = visual
}

// UpdateCommandLoop swaps the command loop reference.
func (r *Runner[TCommand, Frame]) UpdateCommandLoop(loop *CommandLoop[TCommand]) {
	if r == nil {
		return
	}
	r.commandLoop = loop
}
