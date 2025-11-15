package slicc

import (
	"fmt"
	"strings"
)

// StateSpec describes a single cache state.
type StateSpec struct {
	Name        string
	Description string
}

// EventSpec describes an input event that may trigger transitions.
type EventSpec struct {
	Name        string
	Description string
}

// TransitionSpec connects states and events with optional actions.
type TransitionSpec struct {
	FromStates []string
	Events     []string
	ToState    string
	Actions    []string
}

// StateMachineSpec contains the declarative description of a protocol.
type StateMachineSpec struct {
	Name         string
	Description  string
	DefaultState string
	States       []StateSpec
	Events       []EventSpec
	Transitions  []TransitionSpec
}

// Validate ensures the specification is self-consistent.
func (s *StateMachineSpec) Validate() error {
	if s == nil {
		return fmt.Errorf("spec is nil")
	}
	if s.Name == "" {
		return fmt.Errorf("spec name is empty")
	}
	stateSet := make(map[string]struct{})
	for _, st := range s.States {
		if st.Name == "" {
			return fmt.Errorf("state name cannot be empty")
		}
		stateSet[st.Name] = struct{}{}
	}
	if len(stateSet) == 0 {
		return fmt.Errorf("no states defined")
	}

	eventSet := make(map[string]struct{})
	for _, ev := range s.Events {
		if ev.Name == "" {
			return fmt.Errorf("event name cannot be empty")
		}
		eventSet[ev.Name] = struct{}{}
	}
	if len(eventSet) == 0 {
		return fmt.Errorf("no events defined")
	}

	defaultState := s.DefaultState
	if defaultState == "" {
		// fallback to first state
		defaultState = s.States[0].Name
	}
	if _, ok := stateSet[defaultState]; !ok {
		return fmt.Errorf("default state %q not declared", defaultState)
	}

	if len(s.Transitions) == 0 {
		return fmt.Errorf("no transitions defined")
	}
	for i, tr := range s.Transitions {
		if len(tr.FromStates) == 0 {
			return fmt.Errorf("transition #%d missing fromStates", i)
		}
		if len(tr.Events) == 0 {
			return fmt.Errorf("transition #%d missing events", i)
		}
		for _, st := range tr.FromStates {
			if _, ok := stateSet[st]; !ok {
				return fmt.Errorf("transition #%d references undefined state %q", i, st)
			}
		}
		for _, ev := range tr.Events {
			if _, ok := eventSet[ev]; !ok {
				return fmt.Errorf("transition #%d references undefined event %q", i, ev)
			}
		}
		if tr.ToState != "" {
			if _, ok := stateSet[tr.ToState]; !ok {
				return fmt.Errorf("transition #%d has undefined target state %q", i, tr.ToState)
			}
		}
	}
	return nil
}

// NormalizedKey returns a deterministic key for caching/registry purposes.
func (s *StateMachineSpec) NormalizedKey() string {
	var b strings.Builder
	b.WriteString(s.Name)
	b.WriteByte('|')
	b.WriteString(s.DefaultState)
	return b.String()
}
