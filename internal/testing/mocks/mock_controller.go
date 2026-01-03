package mocks

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/internal/core/loadbench"
	"github.com/Readm/flow_sim/internal/core/network"
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

	// Real network for simulation mode
	realNetwork *network.Network

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

// LoadPreset loads a pre-defined network topology.
func (m *Controller) LoadPreset(name string, params map[string]int) error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()

	var net *network.Network
	var err error

	switch name {
	case "bi_ring":
		nodes := 16
		if n, ok := params["nodes"]; ok && n > 0 {
			nodes = n
		}
		net, err = loadbench.BuildBidirectionalRing(nodes)
	default:
		return fmt.Errorf("unknown preset: %s", name)
	}

	if err != nil {
		return err
	}

	m.realNetwork = net

	// Export initial state
	// We use DetailLevelSummary for initial load. Run loop might use DetailLevelFull if needed.
	initialState := net.ExportState(state.ExportConfig{DetailLevel: state.DetailLevelSummary})
	m.latest = &initialState

	// Notify subscribers of the reset (new state at cycle 0)
	go func() {
		m.subsMu.Lock()
		defer m.subsMu.Unlock()
		for _, ch := range m.subs {
			select {
			case ch <- initialState:
			default:
			}
		}
	}()

	return nil
}

// Rebuild resets the simulation state based on the provided configuration.
func (m *Controller) Rebuild(cfg config.EntityConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	m.stateMu.Lock()

	// Clear real network if we represent a static config build
	m.realNetwork = nil

	// Construct new state from config
	newState := state.NetworkState{
		CurrentCycle: 0,
		Nodes:        make([]state.NodeState, 0, len(cfg.Nodes)),
		Links:        make([]state.LinkState, 0, len(cfg.Edges)),
	}

	// Map Nodes
	for _, n := range cfg.Nodes {
		newState.Nodes = append(newState.Nodes, state.NodeState{
			ID:   n.ID,
			Type: n.Type,
		})
	}

	// Map Links
	for _, e := range cfg.Edges {
		newState.Links = append(newState.Links, state.LinkState{
			SourceID:  e.Src,
			TargetID:  e.Dst,
			Occupancy: []int{0},
		})
	}

	m.latest = &newState
	m.stateMu.Unlock()

	// Notify subscribers of the reset (new state at cycle 0)
	go func() {
		m.subsMu.Lock()
		defer m.subsMu.Unlock()
		for _, ch := range m.subs {
			select {
			case ch <- newState:
			default:
			}
		}
	}()

	return nil
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

	// Update the cycle count
	m.stateMu.Lock()
	var newState state.NetworkState

	if m.realNetwork != nil {
		// Run real simulation
		// Note: We ignore errors here for simplicity in this mock wrapper,
		// but in production log them.
		_ = m.realNetwork.AdvanceTo(int(cycles))
		newState = m.realNetwork.ExportState(state.ExportConfig{DetailLevel: state.DetailLevelSummary})
		m.latest = &newState
	} else if m.latest != nil {
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
