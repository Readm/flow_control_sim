package capabilities

import (
	"testing"

	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/slicc"
)

type stubRequestCache struct {
	states map[uint64]core.MESIState
}

func newStubRequestCache() *stubRequestCache {
	return &stubRequestCache{states: make(map[uint64]core.MESIState)}
}

func (s *stubRequestCache) GetState(addr uint64) core.MESIState {
	return s.states[addr]
}

func (s *stubRequestCache) SetState(addr uint64, state core.MESIState) {
	s.states[addr] = state
}

func (s *stubRequestCache) Invalidate(addr uint64) {
	delete(s.states, addr)
}

func (s *stubRequestCache) Clear() {
	s.states = make(map[uint64]core.MESIState)
}

func TestStateMachineCapabilityApplyEvent(t *testing.T) {
	spec := &slicc.StateMachineSpec{
		Name:         "MESI",
		DefaultState: "I",
		States: []slicc.StateSpec{
			{Name: "I"},
			{Name: "S"},
			{Name: "M"},
		},
		Events: []slicc.EventSpec{
			{Name: "Load"},
			{Name: "Store"},
		},
		Transitions: []slicc.TransitionSpec{
			{FromStates: []string{"I"}, Events: []string{"Load"}, ToState: "S"},
			{FromStates: []string{"S"}, Events: []string{"Store"}, ToState: "M"},
		},
	}
	cache := newStubRequestCache()
	sm, err := NewStateMachineCapability("test-sm", spec, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state, changed := sm.ApplyEvent(0x1000, "Load")
	if !changed || state != "S" {
		t.Fatalf("expected transition to S, got %q changed=%v", state, changed)
	}
	if cache.GetState(0x1000) != core.MESIShared {
		t.Fatalf("cache state not updated")
	}

	state, changed = sm.ApplyEvent(0x1000, "Store")
	if !changed || state != "M" {
		t.Fatalf("expected transition to M, got %q", state)
	}
	if cache.GetState(0x1000) != core.MESIModified {
		t.Fatalf("cache state not updated to Modified")
	}

	state, changed = sm.ApplyEvent(0x1000, "Load")
	if changed {
		t.Fatalf("unexpected change when transition missing, state=%q", state)
	}
}
