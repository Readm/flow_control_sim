package controller

import (
	"context"
	"errors"
	"sync"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/internal/core/network"
)

// Status represents the lifecycle state of the controller.
type Status int

const (
	// StatusIdle indicates the controller has not started any simulation yet.
	StatusIdle Status = iota
	// StatusStarting means Start was invoked and the network goroutine is
	// being prepared.
	StatusStarting
	// StatusRunning signals the network goroutine is active.
	StatusRunning
	// StatusStopping means Stop is waiting for the goroutine to exit.
	StatusStopping
	// StatusStopped indicates the last run finished (either naturally or by
	// Stop) and no goroutine is active.
	StatusStopped
)

// Errors exposed for callers and tests.
var (
	ErrNoBuilder      = errors.New("controller requires a manager builder")
	ErrAlreadyRunning = errors.New("simulation already running")
	ErrNotRunning     = errors.New("simulation is not running")
	ErrNoCycles       = errors.New("cycles must be greater than zero")
	ErrNilContext     = errors.New("context cannot be nil")
)

// SimulationController exposes the minimal API used by CLI/Web layers to
// orchestrate a simulation lifecycle.
type SimulationController interface {
	Start(ctx context.Context, cfg config.EntityConfig, cycles uint64) error
	Stop(ctx context.Context) error
	State() Status
}

// ManagerBuilder constructs a network.Manager and returns the default cycles
// count if the caller does not specify one.
type ManagerBuilder func(cfg config.EntityConfig) (*network.Manager, uint64, error)

// New creates a SimulationController backed by the provided builder.
func New(builder ManagerBuilder) SimulationController {
	return &managerController{
		builder: builder,
		status:  StatusIdle,
	}
}

type managerController struct {
	builder ManagerBuilder

	mu     sync.Mutex
	status Status

	manager *network.Manager
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	runErr  error
}

func (c *managerController) Start(ctx context.Context, cfg config.EntityConfig, cycles uint64) error {
	if ctx == nil {
		return ErrNilContext
	}
	if c.builder == nil {
		return ErrNoBuilder
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	c.mu.Lock()
	if c.status == StatusRunning || c.status == StatusStarting || c.status == StatusStopping {
		c.mu.Unlock()
		return ErrAlreadyRunning
	}
	builder := c.builder
	c.mu.Unlock()

	mgr, defaultCycles, err := builder(cfg)
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

	runCtx, cancel := context.WithCancel(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status == StatusRunning || c.status == StatusStarting || c.status == StatusStopping {
		cancel()
		return ErrAlreadyRunning
	}

	c.manager = mgr
	c.cancel = cancel
	c.status = StatusStarting
	c.runErr = nil

	c.wg.Add(1)
	go c.run(runCtx, runCycles)

	c.status = StatusRunning
	return nil
}

func (c *managerController) Stop(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}

	c.mu.Lock()
	if c.status != StatusRunning && c.status != StatusStarting {
		c.mu.Unlock()
		return ErrNotRunning
	}
	cancel := c.cancel
	c.status = StatusStopping
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.normalizeRunErr()
	c.manager = nil
	c.cancel = nil
	c.status = StatusStopped
	return err
}

func (c *managerController) State() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *managerController) run(ctx context.Context, cycles uint64) {
	defer c.wg.Done()
	err := c.manager.Run(ctx, cycles)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.runErr = err
	if c.status != StatusStopping {
		c.status = StatusStopped
	}
}

func (c *managerController) normalizeRunErr() error {
	if c.runErr == nil {
		return nil
	}
	if errors.Is(c.runErr, context.Canceled) {
		return nil
	}
	if errors.Is(c.runErr, context.DeadlineExceeded) {
		return c.runErr
	}
	return c.runErr
}
