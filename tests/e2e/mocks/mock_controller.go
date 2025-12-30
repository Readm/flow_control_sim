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

	subsMu sync.Mutex
	subs   []chan state.NetworkState
}

// NewController creates a mock controller ready for injection.
func NewController() *Controller {
	return &Controller{
		subs: make([]chan state.NetworkState, 0),
	}
}

// Subscribe returns a channel that receives state updates.
func (m *Controller) Subscribe() <-chan state.NetworkState {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	ch := make(chan state.NetworkState, 10)
	m.subs = append(m.subs, ch)
	return ch
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

	// Update the cycle count in the mock state
	m.stateMu.Lock()
	var newState state.NetworkState
	if m.latest != nil {
		m.latest.CurrentCycle = int(cycles)
		// Also simulate some time passing or traffic changes if needed
		// For now, just toggling link occupancy to show liveliness
		for i := range m.latest.Links {
			if len(m.latest.Links[i].Occupancy) > 0 {
				m.latest.Links[i].Occupancy[0] = (m.latest.Links[i].Occupancy[0] + 1) % 10
			}
		}
		newState = *m.latest
	}
	m.stateMu.Unlock()

	// Notify subscribers
	m.subsMu.Lock()
	for _, ch := range m.subs {
		select {
		case ch <- newState:
		default:
			// Drop if blocked
		}
	}
	m.subsMu.Unlock()

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
