package queue

import "testing"

func TestBlockRegistryRegister(t *testing.T) {
	reg := NewBlockRegistry(0)
	idxA, err := reg.Register("a")
	if err != nil {
		t.Fatalf("register a: %v", err)
	}
	idxB, err := reg.Register("b")
	if err != nil {
		t.Fatalf("register b: %v", err)
	}
	if idxA == idxB {
		t.Fatalf("expected unique indices")
	}
	idxA2, err := reg.Register("a")
	if err != nil {
		t.Fatalf("register a again: %v", err)
	}
	if idxA != idxA2 {
		t.Fatalf("expected idempotent registration")
	}
	if reg.Count() != 2 {
		t.Fatalf("expected count 2, got %d", reg.Count())
	}
	desc, ok := reg.Descriptor(idxB)
	if !ok || desc.Name != "b" {
		t.Fatalf("descriptor mismatch: %v", desc)
	}
}

func TestBlockRegistryLimit(t *testing.T) {
	reg := NewBlockRegistry(1)
	if _, err := reg.Register("first"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := reg.Register("second"); err == nil {
		t.Fatalf("expected limit error")
	}
}


