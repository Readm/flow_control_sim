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
	capacity  int
	lines     map[uint64]*CacheLine // Direct lookup by address
	mu        sync.RWMutex
	evictCB   EvictCallback
	rng       *rand.Rand
	stats     CacheStats // Statistics counters
	blockSize uint64     // Block size in bytes (default 64)
}

// NewFullyAssociativeCache creates a new fully-associative cache with the specified capacity.
func NewFullyAssociativeCache(capacity int) *FullyAssociativeCache {
	if capacity <= 0 {
		capacity = 64 // Default capacity
	}
	return &FullyAssociativeCache{
		capacity:  capacity,
		lines:     make(map[uint64]*CacheLine),
		rng:       rand.New(rand.NewSource(0)), // Deterministic seed for testing
		blockSize: 64,                          // Default 64-byte blocks
	}
}

// NewFullyAssociativeCacheWithBlockSize creates a cache with custom block size.
func NewFullyAssociativeCacheWithBlockSize(capacity int, blockSize uint64) *FullyAssociativeCache {
	cache := NewFullyAssociativeCache(capacity)
	cache.blockSize = blockSize
	return cache
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

// Access implements Cache.Access
func (c *FullyAssociativeCache) Access(addr uint64, isWrite bool) *AccessResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update access counter
	c.stats.Accesses++

	// Align address to block boundary
	blockAddr := (addr / c.blockSize) * c.blockSize

	line, exists := c.lines[blockAddr]

	// Miss case
	if !exists || line.State == StateInvalid {
		c.stats.Misses++
		return &AccessResult{
			Hit:       false,
			Data:      nil,
			NeedFill:  true,
			OldState:  StateInvalid,
			NewState:  StateInvalid,
			Writeback: false,
		}
	}

	// Hit case
	c.stats.Hits++
	oldState := line.State
	newState := oldState

	// State transition for write
	if isWrite {
		// Read -> Modified transition
		if oldState != StateModified {
			newState = StateModified
			line.State = StateModified
		}
	}

	// Return data copy
	var dataCopy []byte
	if line.Data != nil {
		dataCopy = make([]byte, len(line.Data))
		copy(dataCopy, line.Data)
	}

	return &AccessResult{
		Hit:       true,
		Data:      dataCopy,
		NeedFill:  false,
		OldState:  oldState,
		NewState:  newState,
		Writeback: false,
	}
}

// Fill implements Cache.Fill
func (c *FullyAssociativeCache) Fill(addr uint64, data []byte, state State) (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Align address to block boundary
	blockAddr := (addr / c.blockSize) * c.blockSize

	// Check if line already exists (might happen in race conditions)
	if _, exists := c.lines[blockAddr]; exists {
		// Update existing line
		c.lines[blockAddr].Data = data
		c.lines[blockAddr].State = state
		return 0, false
	}

	// Ensure capacity (might evict)
	evictedAddr := uint64(0)
	needWriteback := false

	if len(c.lines) >= c.capacity {
		// Need to evict
		evictedAddr, needWriteback = c.evictOne()
		c.stats.Evictions++
		if needWriteback {
			c.stats.Writebacks++
		}
	}

	// Allocate new line
	newLine := &CacheLine{
		Addr:  blockAddr,
		State: state,
		Data:  make([]byte, len(data)),
	}
	copy(newLine.Data, data)
	c.lines[blockAddr] = newLine

	return evictedAddr, needWriteback
}

// evictOne evicts one cache line (internal helper)
// Returns: (evicted address, needs writeback)
func (c *FullyAssociativeCache) evictOne() (uint64, bool) {
	if len(c.lines) == 0 {
		return 0, false
	}

	// Collect all addresses
	addrs := make([]uint64, 0, len(c.lines))
	for addr := range c.lines {
		addrs = append(addrs, addr)
	}

	// Randomly select one to evict
	evictAddr := addrs[c.rng.Intn(len(addrs))]
	line := c.lines[evictAddr]

	needWriteback := line.State == StateModified

	// Call evict callback before removing
	if c.evictCB != nil {
		oldData := make([]byte, len(line.Data))
		copy(oldData, line.Data)
		c.evictCB(evictAddr, line.State, oldData)
	}

	// Remove from map
	delete(c.lines, evictAddr)

	return evictAddr, needWriteback
}

// GetStats implements Cache.GetStats
func (c *FullyAssociativeCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// ResetStats implements Cache.ResetStats
func (c *FullyAssociativeCache) ResetStats() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = CacheStats{}
}

