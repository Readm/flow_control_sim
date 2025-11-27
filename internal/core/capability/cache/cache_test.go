package cache

import (
	"testing"
)

func TestFullyAssociativeCache_BasicOperations(t *testing.T) {
	cache := NewFullyAssociativeCache(4)

	addr := uint64(0x1000)
	data := []byte{1, 2, 3, 4}

	// Test initial state
	if cache.IsPresent(addr) {
		t.Error("cache line should not be present initially")
	}
	if cache.GetState(addr) != StateInvalid {
		t.Errorf("expected StateInvalid, got %v", cache.GetState(addr))
	}

	// Test SetData
	cache.SetData(addr, data)
	if !cache.IsPresent(addr) {
		t.Error("cache line should be present after SetData")
	}
	if cache.GetState(addr) != StateModified {
		t.Errorf("expected StateModified, got %v", cache.GetState(addr))
	}

	// Test GetData
	retrievedData := cache.GetData(addr)
	if len(retrievedData) != len(data) {
		t.Errorf("data length mismatch: expected %d, got %d", len(data), len(retrievedData))
	}
	for i := range data {
		if retrievedData[i] != data[i] {
			t.Errorf("data mismatch at index %d: expected %d, got %d", i, data[i], retrievedData[i])
		}
	}

	// Test SetState
	cache.SetState(addr, StateShared)
	if cache.GetState(addr) != StateShared {
		t.Errorf("expected StateShared, got %v", cache.GetState(addr))
	}

	// Test Invalidate
	cache.Invalidate(addr)
	if cache.IsPresent(addr) {
		t.Error("cache line should not be present after Invalidate")
	}
	if cache.GetState(addr) != StateInvalid {
		t.Errorf("expected StateInvalid, got %v", cache.GetState(addr))
	}
}

func TestFullyAssociativeCache_Capacity(t *testing.T) {
	cache := NewFullyAssociativeCache(2)

	// Fill cache to capacity
	cache.SetData(0x1000, []byte{1})
	cache.SetData(0x2000, []byte{2})

	if cache.GetSize() != 2 {
		t.Errorf("expected size 2, got %d", cache.GetSize())
	}

	// Add one more, should evict one randomly
	cache.SetData(0x3000, []byte{3})

	if cache.GetSize() != 2 {
		t.Errorf("expected size 2 after eviction, got %d", cache.GetSize())
	}

	// Verify new entry exists
	if !cache.IsPresent(0x3000) {
		t.Error("new entry should be present")
	}
}

func TestFullyAssociativeCache_EvictCallback(t *testing.T) {
	cache := NewFullyAssociativeCache(2)
	evicted := false
	var evictedAddr uint64
	var evictedState State
	var evictedData []byte

	cache.SetEvictCallback(func(addr uint64, state State, data []byte) {
		evicted = true
		evictedAddr = addr
		evictedState = state
		evictedData = make([]byte, len(data))
		copy(evictedData, data)
	})

	// Fill cache
	cache.SetData(0x1000, []byte{1, 2})
	cache.SetData(0x2000, []byte{3, 4})

	// Add one more to trigger eviction
	cache.SetData(0x3000, []byte{5, 6})

	if !evicted {
		t.Error("evict callback should have been called")
	}
	if evictedAddr == 0 {
		t.Error("evicted address should be set")
	}
	if evictedState == StateInvalid {
		t.Error("evicted state should not be Invalid")
	}
	if len(evictedData) == 0 {
		t.Error("evicted data should not be empty")
	}
}

func TestFullyAssociativeCache_InvalidateCallback(t *testing.T) {
	cache := NewFullyAssociativeCache(4)
	evicted := false
	var evictedAddr uint64

	cache.SetEvictCallback(func(addr uint64, state State, data []byte) {
		evicted = true
		evictedAddr = addr
	})

	// Set data and invalidate
	cache.SetData(0x1000, []byte{1, 2})
	cache.Invalidate(0x1000)

	if !evicted {
		t.Error("evict callback should have been called on Invalidate")
	}
	if evictedAddr != 0x1000 {
		t.Errorf("expected evicted address 0x1000, got 0x%x", evictedAddr)
	}
}

