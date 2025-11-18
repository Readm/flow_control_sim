package controller

import (
	"context"
	"errors"
	"sync"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/pkg/visual/frame"
	"github.com/Readm/flow_sim/pkg/visual/recorder"
)

// Errors exposed for callers and tests.
var (
	ErrNoBuilder  = errors.New("controller requires a manager builder")
	ErrNoCycles   = errors.New("cycles must be greater than zero")
	ErrNilContext = errors.New("context cannot be nil")
)

// SimulationController exposes the minimal API used by CLI/Web layers to run a
// simulation to completion.
type SimulationController interface {
	Run(ctx context.Context, cfg config.EntityConfig, cycles uint64) error
	Frames() <-chan *frame.Frame
	LatestFrame() *frame.Frame
}

// ManagerBuilder constructs a network.Manager and returns the default cycles
// count if the caller does not specify one.
type ManagerBuilder func(cfg config.EntityConfig) (*network.Manager, uint64, error)

// New creates a SimulationController backed by the provided builder.
func New(builder ManagerBuilder) SimulationController {
	return &managerController{
		builder: builder,
		frameCh: make(chan *frame.Frame, 32),
	}
}

type managerController struct {
	builder ManagerBuilder

	mu sync.Mutex

	frameCh          chan *frame.Frame
	frameRecorder    *recorder.Recorder
	frameRelayCancel context.CancelFunc
	frameMu          sync.RWMutex
	latestFrame      *frame.Frame
}

// Run executes the simulation for the requested cycles. The call blocks until
// the manager finishes or the context is canceled.
func (c *managerController) Run(ctx context.Context, cfg config.EntityConfig, cycles uint64) error {
	if ctx == nil {
		return ErrNilContext
	}
	if c.builder == nil {
		return ErrNoBuilder
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	mgr, defaultCycles, err := c.builder(cfg)
	if err != nil {
		return err
	}
	if mgr == nil {
		return errors.New("manager builder returned nil manager")
	}

	runCycles := cycles
	if runCycles == 0 {
		runCycles = defaultCycles
	}
	if runCycles == 0 {
		return ErrNoCycles
	}

	rec := recorder.New(32)
	rec.SetPaused(false)
	mgr.SetCycleHook(rec)

	c.mu.Lock()
	c.stopFrameStreamingLocked()
	c.frameRecorder = rec
	c.startFrameRelayLocked(rec)
	c.mu.Unlock()

	err = mgr.Run(ctx, runCycles)

	c.mu.Lock()
	c.stopFrameStreamingLocked()
	c.mu.Unlock()

	return err
}

// Frames returns a non-blocking stream of visualization frames.
func (c *managerController) Frames() <-chan *frame.Frame {
	return c.frameCh
}

// LatestFrame returns a copy of the last recorded frame if available.
func (c *managerController) LatestFrame() *frame.Frame {
	c.frameMu.RLock()
	defer c.frameMu.RUnlock()
	if c.latestFrame == nil {
		return nil
	}
	clone := *c.latestFrame
	clone.Nodes = append([]frame.Node(nil), c.latestFrame.Nodes...)
	clone.Edges = append([]frame.Edge(nil), c.latestFrame.Edges...)
	return &clone
}

func (c *managerController) startFrameRelayLocked(rec *recorder.Recorder) {
	if rec == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.frameRelayCancel = cancel
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case fr, ok := <-rec.Frames():
				if !ok {
					return
				}
				c.publishFrame(fr)
			}
		}
	}()
}

func (c *managerController) stopFrameStreamingLocked() {
	if c.frameRelayCancel != nil {
		c.frameRelayCancel()
		c.frameRelayCancel = nil
	}
	if c.frameRecorder != nil {
		c.frameRecorder.SetPaused(true)
		c.frameRecorder.Close()
		c.frameRecorder = nil
	}
}

func (c *managerController) publishFrame(f *frame.Frame) {
	if f == nil {
		return
	}
	c.frameMu.Lock()
	c.latestFrame = f
	c.frameMu.Unlock()
	select {
	case c.frameCh <- f:
	default:
	}
}
