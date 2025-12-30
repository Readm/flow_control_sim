//go:build e2e

package mocks

import (
	"context"
	"errors"
	"sync"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/internal/core/state"
)

var (
	ErrNilContext = errors.New("nil context")
	ErrNoCycles   = errors.New("cycles must be > 0")
)

// Controller implements a simple simulation controller for testing purposes.
type Controller struct {
	stateMu sync.RWMutex
	latest  *state.NetworkState
}

// NewController creates a mock controller ready for injection.
func NewController() *Controller {
	return &Controller{}
}

// Run satisfies the simulation controller contract.
func (m *Controller) Run(ctx context.Context, cfg config.EntityConfig, cycles uint64) error {
	if ctx == nil {
		return ErrNilContext
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cycles == 0 {
		return ErrNoCycles
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// SetState updates the current network state.
func (m *Controller) SetState(ns state.NetworkState) {
	m.stateMu.Lock()
	m.latest = &ns
	m.stateMu.Unlock()
}

// GetState returns the last recorded state.
func (m *Controller) GetState() *state.NetworkState {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	return m.latest
}
