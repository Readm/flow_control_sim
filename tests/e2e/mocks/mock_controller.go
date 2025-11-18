//go:build e2e

package mocks

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/pkg/controller"
	"github.com/Readm/flow_sim/tests/e2e/model"
)

// Command captures control intents routed through the mock controller.
type Command struct {
	Type      string
	Cycles    uint64
	Timestamp time.Time
}

// Controller implements controller.SimulationController for testing purposes.
type Controller struct {
	mu        sync.Mutex
	status    controller.Status
	frames    chan *model.Frame
	commands  chan Command
	lastCfg   config.EntityConfig
	lastCycle uint64
}

// NewController creates a mock controller ready for injection.
func NewController() *Controller {
	return &Controller{
		status:   controller.StatusIdle,
		frames:   make(chan *model.Frame, 32),
		commands: make(chan Command, 32),
	}
}

// Start satisfies SimulationController. It validates the config and records
// the start event for later assertions.
func (m *Controller) Start(ctx context.Context, cfg config.EntityConfig, cycles uint64) error {
	if ctx == nil {
		return controller.ErrNilContext
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cycles == 0 {
		return controller.ErrNoCycles
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	switch m.status {
	case controller.StatusRunning, controller.StatusStarting:
		return controller.ErrAlreadyRunning
	}
	m.status = controller.StatusRunning
	m.lastCfg = cfg
	m.lastCycle = cycles
	m.enqueueCommand(Command{Type: "start", Cycles: cycles})
	return nil
}

// Stop transitions the controller into the stopped state.
func (m *Controller) Stop(ctx context.Context) error {
	if ctx == nil {
		return controller.ErrNilContext
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status != controller.StatusRunning && m.status != controller.StatusStarting {
		return controller.ErrNotRunning
	}
	m.status = controller.StatusStopped
	m.enqueueCommand(Command{Type: "stop"})
	return nil
}

// State exposes current lifecycle status.
func (m *Controller) State() controller.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// EmitFrame pushes a frame into the update stream consumed by the bridge.
func (m *Controller) EmitFrame(frame *model.Frame) error {
	if frame == nil {
		return errors.New("frame cannot be nil")
	}
	select {
	case m.frames <- frame:
		return nil
	default:
		// remove oldest to keep channel responsive
		<-m.frames
		m.frames <- frame
		return nil
	}
}

// Frames exposes the frame stream for subscribers.
func (m *Controller) Frames() <-chan *model.Frame {
	return m.frames
}

// Commands exposes recorded control commands.
func (m *Controller) Commands() <-chan Command {
	return m.commands
}

// NotifyControl records high-level commands such as run/reset/pause triggered
// by the test HTTP layer.
func (m *Controller) NotifyControl(cmd string, cycles uint64) {
	m.enqueueCommand(Command{Type: cmd, Cycles: cycles})
}

func (m *Controller) enqueueCommand(cmd Command) {
	cmd.Timestamp = time.Now()
	select {
	case m.commands <- cmd:
	default:
		<-m.commands
		m.commands <- cmd
	}
}
