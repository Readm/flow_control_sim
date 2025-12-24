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

func TestFullyAssociativeCache_HandleSnoop(t *testing.T) {
	cache := NewFullyAssociativeCache(4)
	addr := uint64(0x1000)
	testData := []byte{10, 20, 30, 40}

	// Test snoop on non-present line
	resp, err := cache.HandleSnoop(0x01, addr)
	if err != nil {
		t.Errorf("HandleSnoop on non-present line should not error: %v", err)
	}
	if resp.HasData {
		t.Error("HandleSnoop on non-present line should not have data")
	}

	// Set data in Modified state
	cache.SetData(addr, testData)
	cache.SetState(addr, StateModified)

	// Test snoop on Modified line
	resp, err = cache.HandleSnoop(0x01, addr)
	if err != nil {
		t.Errorf("HandleSnoop failed: %v", err)
	}
	if !resp.HasData {
		t.Error("expected HasData to be true for Modified state")
	}
	if len(resp.Data) != len(testData) {
		t.Errorf("data length mismatch: expected %d, got %d", len(testData), len(resp.Data))
	}
	for i := range testData {
		if resp.Data[i] != testData[i] {
			t.Errorf("data mismatch at index %d: expected %d, got %d", i, testData[i], resp.Data[i])
		}
	}

	// After snoop, state should be downgraded to Shared
	if cache.GetState(addr) != StateShared {
		t.Errorf("expected state to be Shared after snoop, got %v", cache.GetState(addr))
	}

	// Test snoop on Exclusive state
	cache.SetState(addr, StateExclusive)
	resp, err = cache.HandleSnoop(0x01, addr)
	if err != nil {
		t.Errorf("HandleSnoop on Exclusive failed: %v", err)
	}
	if !resp.HasData {
		t.Error("expected HasData to be true for Exclusive state")
	}
	if cache.GetState(addr) != StateShared {
		t.Errorf("expected state to be Shared after snoop on Exclusive, got %v", cache.GetState(addr))
	}

	// Test snoop on Owned state
	cache.SetState(addr, StateOwned)
	resp, err = cache.HandleSnoop(0x01, addr)
	if err != nil {
		t.Errorf("HandleSnoop on Owned failed: %v", err)
	}
	if !resp.HasData {
		t.Error("expected HasData to be true for Owned state")
	}
	if cache.GetState(addr) != StateShared {
		t.Errorf("expected state to be Shared after snoop on Owned, got %v", cache.GetState(addr))
	}

	// Test snoop on Shared state (should not provide data)
	cache.SetState(addr, StateShared)
	resp, err = cache.HandleSnoop(0x01, addr)
	if err != nil {
		t.Errorf("HandleSnoop on Shared failed: %v", err)
	}
	if resp.HasData {
		t.Error("Shared state should not provide data on snoop")
	}

	// Test snoop on Invalid state
	cache.SetState(addr, StateInvalid)
	resp, err = cache.HandleSnoop(0x01, addr)
	if err != nil {
		t.Errorf("HandleSnoop on Invalid failed: %v", err)
	}
	if resp.HasData {
		t.Error("Invalid state should not provide data on snoop")
	}
}

func TestFullyAssociativeCache_CanForward(t *testing.T) {
	cache := NewFullyAssociativeCache(4)
	addr := uint64(0x2000)

	// Non-present line cannot forward
	if cache.CanForward(addr) {
		t.Error("non-present line should not be able to forward")
	}

	// Invalid state cannot forward
	cache.SetData(addr, []byte{1})
	cache.SetState(addr, StateInvalid)
	if cache.CanForward(addr) {
		t.Error("Invalid state should not be able to forward")
	}

	// Shared state cannot forward
	cache.SetState(addr, StateShared)
	if cache.CanForward(addr) {
		t.Error("Shared state should not be able to forward")
	}

	// Modified state can forward
	cache.SetState(addr, StateModified)
	if !cache.CanForward(addr) {
		t.Error("Modified state should be able to forward")
	}

	// Exclusive state can forward
	cache.SetState(addr, StateExclusive)
	if !cache.CanForward(addr) {
		t.Error("Exclusive state should be able to forward")
	}

	// Owned state can forward
	cache.SetState(addr, StateOwned)
	if !cache.CanForward(addr) {
		t.Error("Owned state should be able to forward")
	}
}

func TestFullyAssociativeCache_OwnedState(t *testing.T) {
	cache := NewFullyAssociativeCache(4)
	addr := uint64(0x3000)

	// Test setting Owned state
	cache.SetData(addr, []byte{1, 2, 3})
	cache.SetState(addr, StateOwned)

	if cache.GetState(addr) != StateOwned {
		t.Errorf("expected StateOwned, got %v", cache.GetState(addr))
	}

	// Owned state should be present
	if !cache.IsPresent(addr) {
		t.Error("Owned state should be present")
	}

	// Owned state should be able to forward
	if !cache.CanForward(addr) {
		t.Error("Owned state should be able to forward")
	}

	// Owned state should provide data on snoop
	resp, err := cache.HandleSnoop(0x01, addr)
	if err != nil {
		t.Errorf("HandleSnoop on Owned failed: %v", err)
	}
	if !resp.HasData {
		t.Error("Owned state should provide data on snoop")
	}
}

