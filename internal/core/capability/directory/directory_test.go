package directory

import (
	"testing"
)

func TestFullyAssociativeDirectory_BasicOperations(t *testing.T) {
	dir := NewFullyAssociativeDirectory(4)

	addr := uint64(0x1000)

	// Test initial state
	if dir.GetState(addr) != StateNotPresent {
		t.Errorf("expected StateNotPresent, got %v", dir.GetState(addr))
	}
	if len(dir.GetSharers(addr)) != 0 {
		t.Error("sharers list should be empty initially")
	}
	if dir.GetOwner(addr) != -1 {
		t.Errorf("expected owner -1, got %d", dir.GetOwner(addr))
	}

	// Test AddSharer
	dir.AddSharer(addr, 1)
	sharers := dir.GetSharers(addr)
	if len(sharers) != 1 || sharers[0] != 1 {
		t.Errorf("expected sharer [1], got %v", sharers)
	}
	if dir.GetState(addr) != StateExclusive {
		t.Errorf("expected StateExclusive, got %v", dir.GetState(addr))
	}

	// Test AddSharer again (should not duplicate)
	dir.AddSharer(addr, 1)
	sharers = dir.GetSharers(addr)
	if len(sharers) != 1 {
		t.Errorf("expected 1 sharer, got %d", len(sharers))
	}

	// Test AddSharer with another node
	dir.AddSharer(addr, 2)
	sharers = dir.GetSharers(addr)
	if len(sharers) != 2 {
		t.Errorf("expected 2 sharers, got %d", len(sharers))
	}
	if dir.GetState(addr) != StateShared {
		t.Errorf("expected StateShared, got %v", dir.GetState(addr))
	}

	// Test RemoveSharer
	dir.RemoveSharer(addr, 1)
	sharers = dir.GetSharers(addr)
	if len(sharers) != 1 || sharers[0] != 2 {
		t.Errorf("expected sharer [2], got %v", sharers)
	}
	if dir.GetState(addr) != StateExclusive {
		t.Errorf("expected StateExclusive, got %v", dir.GetState(addr))
	}

	// Test ClearSharers
	dir.ClearSharers(addr)
	if len(dir.GetSharers(addr)) != 0 {
		t.Error("sharers list should be empty after ClearSharers")
	}
	if dir.GetState(addr) != StateNotPresent {
		t.Errorf("expected StateNotPresent, got %v", dir.GetState(addr))
	}
}

func TestFullyAssociativeDirectory_Owner(t *testing.T) {
	dir := NewFullyAssociativeDirectory(4)

	addr := uint64(0x1000)

	// Test SetOwner
	dir.SetOwner(addr, 5)
	if dir.GetOwner(addr) != 5 {
		t.Errorf("expected owner 5, got %d", dir.GetOwner(addr))
	}
	if dir.GetState(addr) != StateExclusive {
		t.Errorf("expected StateExclusive, got %v", dir.GetState(addr))
	}

	// Test SetOwner to -1
	dir.SetOwner(addr, -1)
	if dir.GetOwner(addr) != -1 {
		t.Errorf("expected owner -1, got %d", dir.GetOwner(addr))
	}
	if dir.GetState(addr) != StateNotPresent {
		t.Errorf("expected StateNotPresent, got %v", dir.GetState(addr))
	}
}

func TestFullyAssociativeDirectory_Capacity(t *testing.T) {
	dir := NewFullyAssociativeDirectory(2)

	// Fill directory to capacity
	dir.AddSharer(0x1000, 1)
	dir.AddSharer(0x2000, 2)

	if dir.GetSize() != 2 {
		t.Errorf("expected size 2, got %d", dir.GetSize())
	}

	// Add one more, should evict one randomly
	dir.AddSharer(0x3000, 3)

	if dir.GetSize() != 2 {
		t.Errorf("expected size 2 after eviction, got %d", dir.GetSize())
	}

	// Verify new entry exists
	if len(dir.GetSharers(0x3000)) == 0 {
		t.Error("new entry should have sharers")
	}
}

func TestFullyAssociativeDirectory_EvictCallback(t *testing.T) {
	dir := NewFullyAssociativeDirectory(2)
	evicted := false
	var evictedAddr uint64
	var evictedState State
	var evictedSharers []int
	var evictedOwner int

	dir.SetEvictCallback(func(addr uint64, state State, sharers []int, owner int) {
		evicted = true
		evictedAddr = addr
		evictedState = state
		evictedSharers = make([]int, len(sharers))
		copy(evictedSharers, sharers)
		evictedOwner = owner
	})

	// Fill directory
	dir.AddSharer(0x1000, 1)
	dir.AddSharer(0x2000, 2)

	// Add one more to trigger eviction
	dir.AddSharer(0x3000, 3)

	if !evicted {
		t.Error("evict callback should have been called")
	}
	if evictedAddr == 0 {
		t.Error("evicted address should be set")
	}
	if evictedState == StateNotPresent {
		t.Error("evicted state should not be NotPresent")
	}
	_ = evictedOwner // Owner may be -1, which is valid
}

func TestFullyAssociativeDirectory_SetState(t *testing.T) {
	dir := NewFullyAssociativeDirectory(4)

	addr := uint64(0x1000)

	// Test SetState
	dir.SetState(addr, StateModified)
	if dir.GetState(addr) != StateModified {
		t.Errorf("expected StateModified, got %v", dir.GetState(addr))
	}

	dir.SetState(addr, StateShared)
	if dir.GetState(addr) != StateShared {
		t.Errorf("expected StateShared, got %v", dir.GetState(addr))
	}
}

