package capabilities

import (
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
	"github.com/Readm/flow_sim/slicc"
)

// ProtocolStateMachine exposes runtime state transition helpers.
type ProtocolStateMachine interface {
	NodeCapability
	ApplyEvent(address uint64, event string) (string, bool)
	CurrentState(address uint64) (string, bool)
}

// NewStateMachineCapability builds a generic state machine capability.
func NewStateMachineCapability(name string, spec *slicc.StateMachineSpec, cache RequestCache) (ProtocolStateMachine, error) {
	if spec == nil {
		return nil, fmt.Errorf("state machine spec is nil")
	}
	engine := newStateMachineEngine(spec)
	return &stateMachineCapability{
		name:        name,
		description: fmt.Sprintf("state machine capability for %s", spec.Name),
		engine:      engine,
		cache:       cache,
	}, nil
}

type stateMachineCapability struct {
	name        string
	description string
	engine      *stateMachineEngine
	cache       RequestCache
}

func (c *stateMachineCapability) Descriptor() hooks.PluginDescriptor {
	return hooks.PluginDescriptor{
		Name:        c.name,
		Category:    hooks.PluginCategoryCapability,
		Description: c.description,
	}
}

func (c *stateMachineCapability) Register(broker *hooks.PluginBroker) error {
	if broker == nil {
		return nil
	}
	broker.RegisterPluginMetadata(c.Descriptor())
	return nil
}

func (c *stateMachineCapability) ApplyEvent(address uint64, event string) (string, bool) {
	if c.engine == nil {
		return "", false
	}
	state, changed := c.engine.apply(address, event)
	if changed {
		c.updateRequestCache(address, state)
	}
	return state, changed
}

func (c *stateMachineCapability) CurrentState(address uint64) (string, bool) {
	if c.engine == nil {
		return "", false
	}
	return c.engine.current(address)
}

func (c *stateMachineCapability) updateRequestCache(address uint64, state string) {
	if c.cache == nil {
		return
	}
	if mesiState, ok := mapToMESIState(state); ok {
		c.cache.SetState(address, mesiState)
	}
}

func mapToMESIState(state string) (core.MESIState, bool) {
	switch state {
	case "I", "II":
		return core.MESIInvalid, true
	case "S":
		return core.MESIShared, true
	case "E":
		return core.MESIExclusive, true
	case "M", "MI", "MII":
		return core.MESIModified, true
	default:
		return "", false
	}
}

type transition struct {
	toState string
	actions []string
}

type stateMachineEngine struct {
	defaultState string
	table        map[string]map[string]transition
	states       map[uint64]string
	mu           sync.RWMutex
}

func newStateMachineEngine(spec *slicc.StateMachineSpec) *stateMachineEngine {
	engine := &stateMachineEngine{
		defaultState: spec.DefaultState,
		table:        make(map[string]map[string]transition),
		states:       make(map[uint64]string),
	}
	if engine.defaultState == "" && len(spec.States) > 0 {
		engine.defaultState = spec.States[0].Name
	}
	for _, tr := range spec.Transitions {
		to := tr.ToState
		for _, from := range tr.FromStates {
			for _, ev := range tr.Events {
				if engine.table[from] == nil {
					engine.table[from] = make(map[string]transition)
				}
				engine.table[from][ev] = transition{
					toState: to,
					actions: append([]string(nil), tr.Actions...),
				}
			}
		}
	}
	return engine
}

func (s *stateMachineEngine) current(address uint64) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.states[address]
	if !ok || state == "" {
		return s.defaultState, false
	}
	return state, true
}

func (s *stateMachineEngine) apply(address uint64, event string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[address]
	if state == "" {
		state = s.defaultState
	}
	next := s.lookup(state, event)
	if next == "" {
		return state, false
	}
	s.states[address] = next
	return next, true
}

func (s *stateMachineEngine) lookup(state, event string) string {
	if state == "" {
		state = s.defaultState
	}
	if transitions, ok := s.table[state]; ok {
		if tr, ok := transitions[event]; ok {
			if tr.toState != "" {
				return tr.toState
			}
			return state
		}
	}
	return ""
}

func (s *stateMachineEngine) reset(address uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, address)
}
