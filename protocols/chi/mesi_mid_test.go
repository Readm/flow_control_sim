package chi

import (
	"testing"

	"github.com/Readm/flow_sim/core"
)

type stubCache struct {
	states map[uint64]core.MESIState
}

func newStubCache() *stubCache {
	return &stubCache{states: make(map[uint64]core.MESIState)}
}

func (s *stubCache) GetState(addr uint64) core.MESIState {
	return s.states[addr]
}

func (s *stubCache) SetState(addr uint64, state core.MESIState) {
	if state == core.MESIInvalid {
		delete(s.states, addr)
		return
	}
	s.states[addr] = state
}

func (s *stubCache) Invalidate(addr uint64) {
	delete(s.states, addr)
}

func (s *stubCache) Clear() {
	s.states = make(map[uint64]core.MESIState)
}

func TestMESIMidStateMachineFlow(t *testing.T) {
	cache := newStubCache()
	sm, err := NewMESIMidStateMachine(cache)
	if err != nil {
		t.Fatalf("failed to build state machine: %v", err)
	}

	addr := uint64(0x1000)

	// I -> IS (LocalGetS)
	if state, changed := sm.ApplyEvent(addr, "LocalGetS"); !changed || state != "IS" {
		t.Fatalf("expected IS, got %q changed=%v", state, changed)
	}
	if _, exists := cache.states[addr]; exists {
		t.Fatalf("cache should not update before data arrival")
	}

	// IS -> S (DataResp)
	if state, changed := sm.ApplyEvent(addr, "DataResp"); !changed || state != "S" {
		t.Fatalf("expected S after data, got %q", state)
	}
	if cache.GetState(addr) != core.MESIShared {
		t.Fatalf("cache should be shared")
	}

	// S -> IM (upgrade)
	if state, changed := sm.ApplyEvent(addr, "LocalGetX"); !changed || state != "IM" {
		t.Fatalf("expected IM, got %q", state)
	}
	// IM -> M
	if state, changed := sm.ApplyEvent(addr, "DataResp"); !changed || state != "M" {
		t.Fatalf("expected M, got %q", state)
	}
	if cache.GetState(addr) != core.MESIModified {
		t.Fatalf("cache not marked modified")
	}

	// M -> I due to snoop
	if state, changed := sm.ApplyEvent(addr, "SnoopGetX"); !changed || state != "I" {
		t.Fatalf("expected I after snoop, got %q", state)
	}
	if _, exists := cache.states[addr]; exists {
		t.Fatalf("cache should be invalidated")
	}
}
