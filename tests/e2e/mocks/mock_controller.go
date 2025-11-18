//go:build e2e

package mocks

import (
	"context"
	"errors"
	"sync"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/pkg/controller"
	"github.com/Readm/flow_sim/pkg/visual/frame"
)

// Controller implements controller.SimulationController for testing purposes.
type Controller struct {
	frames chan *frame.Frame

	frameMu sync.RWMutex
	latest  *frame.Frame
}

// NewController creates a mock controller ready for injection.
func NewController() *Controller {
	return &Controller{
		frames: make(chan *frame.Frame, 32),
	}
}

// Run satisfies SimulationController. It validates the config and records the
// requested run for assertions.
func (m *Controller) Run(ctx context.Context, cfg config.EntityConfig, cycles uint64) error {
	if ctx == nil {
		return controller.ErrNilContext
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cycles == 0 {
		return controller.ErrNoCycles
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// EmitFrame pushes a frame into the update stream consumed by the bridge.
func (m *Controller) EmitFrame(frame *frame.Frame) error {
	if frame == nil {
		return errors.New("frame cannot be nil")
	}
	m.frameMu.Lock()
	m.latest = frame
	m.frameMu.Unlock()
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
func (m *Controller) Frames() <-chan *frame.Frame {
	return m.frames
}

// LatestFrame returns the last published frame if available.
func (m *Controller) LatestFrame() *frame.Frame {
	m.frameMu.RLock()
	defer m.frameMu.RUnlock()
	if m.latest == nil {
		return nil
	}
	clone := *m.latest
	clone.Nodes = append([]frame.Node(nil), m.latest.Nodes...)
	clone.Edges = append([]frame.Edge(nil), m.latest.Edges...)
	return &clone
}
