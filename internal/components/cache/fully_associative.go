package cache

import (
	"math/rand"
	"sync"
)

// CacheLine represents a single cache line entry.
type CacheLine struct {
	Addr  uint64
	State State
	Data  []byte
}

// FullyAssociativeCache implements a simple fully-associative cache with random replacement.
type FullyAssociativeCache struct {
	capacity int
	lines    map[uint64]*CacheLine // Direct lookup by address
	mu       sync.RWMutex
	evictCB  EvictCallback
	rng      *rand.Rand
}

// NewFullyAssociativeCache creates a new fully-associative cache with the specified capacity.
func NewFullyAssociativeCache(capacity int) *FullyAssociativeCache {
	if capacity <= 0 {
		capacity = 64 // Default capacity
	}
	return &FullyAssociativeCache{
		capacity: capacity,
		lines:    make(map[uint64]*CacheLine),
		rng:      rand.New(rand.NewSource(0)), // Deterministic seed for testing
	}
}

// GetState returns the current state of the cache line at the given address.
func (c *FullyAssociativeCache) GetState(addr uint64) State {
	c.mu.RLock()
	defer c.mu.RUnlock()

	line, exists := c.lines[addr]
	if !exists {
		return StateInvalid
	}
	return line.State
}

// SetState updates the state of the cache line at the given address.
func (c *FullyAssociativeCache) SetState(addr uint64, state State) {
	c.mu.Lock()
	defer c.mu.Unlock()

	line, exists := c.lines[addr]
	if !exists {
		// Need to allocate a new line
		c.ensureCapacity()
		line = &CacheLine{
			Addr:  addr,
			State: state,
			Data:  nil,
		}
		c.lines[addr] = line
	} else {
		line.State = state
	}
}

// GetData retrieves the data stored in the cache line at the given address.
func (c *FullyAssociativeCache) GetData(addr uint64) []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()

	line, exists := c.lines[addr]
	if !exists || line.State == StateInvalid {
		return nil
	}
	// Return a copy to prevent external modification
	if line.Data == nil {
		return nil
	}
	data := make([]byte, len(line.Data))
	copy(data, line.Data)
	return data
}

// SetData updates the data stored in the cache line at the given address.
func (c *FullyAssociativeCache) SetData(addr uint64, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	line, exists := c.lines[addr]
	if !exists {
		// Need to allocate a new line
		c.ensureCapacity()
		line = &CacheLine{
			Addr:  addr,
			State: StateModified, // Implicitly set to Modified when data is written
			Data:  nil,
		}
		c.lines[addr] = line
	}

	// Copy data
	if data != nil {
		line.Data = make([]byte, len(data))
		copy(line.Data, data)
		if line.State == StateInvalid {
			line.State = StateModified
		}
	} else {
		line.Data = nil
	}
}

// Invalidate marks the cache line at the given address as invalid.
func (c *FullyAssociativeCache) Invalidate(addr uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	line, exists := c.lines[addr]
	if exists {
		oldState := line.State
		oldData := make([]byte, len(line.Data))
		copy(oldData, line.Data)

		line.State = StateInvalid
		line.Data = nil

		// Call evict callback
		if c.evictCB != nil {
			c.evictCB(addr, oldState, oldData)
		}

		// Remove from map
		delete(c.lines, addr)
	}
}

// IsPresent checks if a cache line exists for the given address (regardless of state).
func (c *FullyAssociativeCache) IsPresent(addr uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	_, exists := c.lines[addr]
	return exists
}

// SetEvictCallback sets the callback function to be called when a cache line is evicted.
func (c *FullyAssociativeCache) SetEvictCallback(callback EvictCallback) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictCB = callback
}

// ensureCapacity ensures there is capacity for a new cache line.
// If the cache is full, it randomly evicts one line.
func (c *FullyAssociativeCache) ensureCapacity() {
	if len(c.lines) < c.capacity {
		return
	}

	// Cache is full, need to evict a random line
	// Collect all addresses
	addrs := make([]uint64, 0, len(c.lines))
	for addr := range c.lines {
		addrs = append(addrs, addr)
	}

	// Randomly select one to evict
	if len(addrs) > 0 {
		evictAddr := addrs[c.rng.Intn(len(addrs))]
		line := c.lines[evictAddr]

		// Call evict callback before removing
		if c.evictCB != nil {
			oldData := make([]byte, len(line.Data))
			copy(oldData, line.Data)
			c.evictCB(evictAddr, line.State, oldData)
		}

		// Remove from map
		delete(c.lines, evictAddr)
	}
}

// GetCapacity returns the capacity of the cache.
func (c *FullyAssociativeCache) GetCapacity() int {
	return c.capacity
}

// GetSize returns the current number of cache lines.
func (c *FullyAssociativeCache) GetSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.lines)
}

// HandleSnoop implements Cache.HandleSnoop
func (c *FullyAssociativeCache) HandleSnoop(snoopOpcode int, addr uint64) (*SnoopResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	line, exists := c.lines[addr]
	if !exists || line.State == StateInvalid {
		// No data to provide
		return &SnoopResponse{
			ResponseOpcode: 0, // Protocol-specific: no data response
			Data:           nil,
			HasData:        false,
		}, nil
	}

	// Determine if we should provide data based on state
	// Modified, Exclusive, and Owned states provide data
	// Shared and Invalid do not provide data
	shouldProvideData := line.State == StateModified ||
		line.State == StateExclusive ||
		line.State == StateOwned

	response := &SnoopResponse{
		ResponseOpcode: 1,    // Protocol-specific: data response
		Data:           nil,
		HasData:        false,
	}

	if shouldProvideData {
		response.Data = line.Data
		response.HasData = true
	}

	// Downgrade state if needed (simplified logic)
	// Modified, Exclusive, and Owned all downgrade to Shared
	if line.State == StateModified || line.State == StateExclusive || line.State == StateOwned {
		line.State = StateShared
	}

	return response, nil
}

// CanForward implements Cache.CanForward
func (c *FullyAssociativeCache) CanForward(addr uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	line, exists := c.lines[addr]
	if !exists {
		return false
	}

	// Can forward if in Modified, Owned, or Exclusive state
	return line.State == StateModified ||
		line.State == StateExclusive ||
		line.State == StateOwned
}

