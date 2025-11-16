package slicc

import "testing"

func TestStateMachineSpecValidateSuccess(t *testing.T) {
	spec := &StateMachineSpec{
		Name:         "MESI",
		DefaultState: "I",
		States: []StateSpec{
			{Name: "I"},
			{Name: "S"},
			{Name: "M"},
		},
		Events: []EventSpec{
			{Name: "Load"},
			{Name: "Store"},
		},
		Transitions: []TransitionSpec{
			{FromStates: []string{"I"}, Events: []string{"Load"}, ToState: "S"},
			{FromStates: []string{"S"}, Events: []string{"Store"}, ToState: "M"},
		},
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestStateMachineSpecValidateMissingState(t *testing.T) {
	spec := &StateMachineSpec{
		Name: "broken",
		States: []StateSpec{
			{Name: "I"},
		},
		Events: []EventSpec{
			{Name: "Load"},
		},
		Transitions: []TransitionSpec{
			{FromStates: []string{"X"}, Events: []string{"Load"}, ToState: "I"},
		},
	}
	if err := spec.Validate(); err == nil {
		t.Fatalf("expected error for undefined state")
	}
}

func TestNormalizedKey(t *testing.T) {
	spec := &StateMachineSpec{
		Name:         "MESI",
		DefaultState: "I",
		States:       []StateSpec{{Name: "I"}},
		Events:       []EventSpec{{Name: "Load"}},
		Transitions:  []TransitionSpec{{FromStates: []string{"I"}, Events: []string{"Load"}}},
	}
	key := spec.NormalizedKey()
	if key != "MESI|I" {
		t.Fatalf("unexpected key %q", key)
	}
}
