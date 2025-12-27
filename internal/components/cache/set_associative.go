package cache

import (
	"sync"
)

// SetAssociativeCache implements a set-associative cache with LRU replacement.
// Cache is organized as: numSets x numWays
type SetAssociativeCache struct {
	numSets   int
	numWays   int
	blockSize uint64

	// Storage: sets[setIndex][wayIndex]
	sets [][]*CacheLine

	// LRU tracking: lruCounters[setIndex][wayIndex]
	// Higher value = more recently used
	lruCounters [][]uint64
	lruClock    uint64

	// Statistics
	stats CacheStats

	// Callbacks
	evictCB EvictCallback

	mu sync.RWMutex
}

// NewSetAssociativeCache creates a new set-associative cache.
// Parameters:
//   - numSets: Number of sets (e.g., 64, 128, 512)
//   - numWays: Number of ways per set (e.g., 4, 8, 16)
//   - blockSize: Block size in bytes (e.g., 64)
func NewSetAssociativeCache(numSets, numWays int, blockSize uint64) *SetAssociativeCache {
	if numSets <= 0 {
		numSets = 64
	}
	if numWays <= 0 {
		numWays = 4
	}
	if blockSize == 0 {
		blockSize = 64
	}

	// Initialize sets
	sets := make([][]*CacheLine, numSets)
	lruCounters := make([][]uint64, numSets)
	for i := 0; i < numSets; i++ {
		sets[i] = make([]*CacheLine, numWays)
		lruCounters[i] = make([]uint64, numWays)
		// Initialize all lines as nil (invalid)
	}

	return &SetAssociativeCache{
		numSets:     numSets,
		numWays:     numWays,
		blockSize:   blockSize,
		sets:        sets,
		lruCounters: lruCounters,
		lruClock:    0,
	}
}

// getSetIndex calculates the set index for a given address.
func (c *SetAssociativeCache) getSetIndex(addr uint64) int {
	blockAddr := addr / c.blockSize
	return int(blockAddr % uint64(c.numSets))
}

// getBlockAddr aligns address to block boundary.
func (c *SetAssociativeCache) getBlockAddr(addr uint64) uint64 {
	return (addr / c.blockSize) * c.blockSize
}

// findWay finds the way containing the given address in a set.
// Returns: (wayIndex, found)
func (c *SetAssociativeCache) findWay(setIdx int, blockAddr uint64) (int, bool) {
	for way := 0; way < c.numWays; way++ {
		line := c.sets[setIdx][way]
		if line != nil && line.Addr == blockAddr && line.State != StateInvalid {
			return way, true
		}
	}
	return -1, false
}

// findLRUWay finds the LRU (least recently used) way in a set.
// Returns: wayIndex
func (c *SetAssociativeCache) findLRUWay(setIdx int) int {
	lruWay := 0
	lruValue := c.lruCounters[setIdx][0]

	for way := 1; way < c.numWays; way++ {
		if c.lruCounters[setIdx][way] < lruValue {
			lruValue = c.lruCounters[setIdx][way]
			lruWay = way
		}
	}

	return lruWay
}

// updateLRU updates the LRU counter for a way.
func (c *SetAssociativeCache) updateLRU(setIdx, wayIdx int) {
	c.lruClock++
	c.lruCounters[setIdx][wayIdx] = c.lruClock
}

// GetState implements Cache.GetState
func (c *SetAssociativeCache) GetState(addr uint64) State {
	c.mu.RLock()
	defer c.mu.RUnlock()

	blockAddr := c.getBlockAddr(addr)
	setIdx := c.getSetIndex(addr)

	wayIdx, found := c.findWay(setIdx, blockAddr)
	if !found {
		return StateInvalid
	}

	return c.sets[setIdx][wayIdx].State
}

// SetState implements Cache.SetState
func (c *SetAssociativeCache) SetState(addr uint64, state State) {
	c.mu.Lock()
	defer c.mu.Unlock()

	blockAddr := c.getBlockAddr(addr)
	setIdx := c.getSetIndex(addr)

	wayIdx, found := c.findWay(setIdx, blockAddr)
	if !found {
		// Need to allocate a new line
		wayIdx = c.findLRUWay(setIdx)

		// Evict if necessary
		if c.sets[setIdx][wayIdx] != nil {
			c.evictWay(setIdx, wayIdx)
		}

		// Allocate new line
		c.sets[setIdx][wayIdx] = &CacheLine{
			Addr:  blockAddr,
			State: state,
			Data:  nil,
		}
	} else {
		c.sets[setIdx][wayIdx].State = state
	}

	c.updateLRU(setIdx, wayIdx)
}

// GetData implements Cache.GetData
func (c *SetAssociativeCache) GetData(addr uint64) []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()

	blockAddr := c.getBlockAddr(addr)
	setIdx := c.getSetIndex(addr)

	wayIdx, found := c.findWay(setIdx, blockAddr)
	if !found {
		return nil
	}

	line := c.sets[setIdx][wayIdx]
	if line.Data == nil {
		return nil
	}

	// Return a copy
	data := make([]byte, len(line.Data))
	copy(data, line.Data)
	return data
}

// SetData implements Cache.SetData
func (c *SetAssociativeCache) SetData(addr uint64, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	blockAddr := c.getBlockAddr(addr)
	setIdx := c.getSetIndex(addr)

	wayIdx, found := c.findWay(setIdx, blockAddr)
	if !found {
		// Need to allocate a new line
		wayIdx = c.findLRUWay(setIdx)

		// Evict if necessary
		if c.sets[setIdx][wayIdx] != nil {
			c.evictWay(setIdx, wayIdx)
		}

		// Allocate new line
		c.sets[setIdx][wayIdx] = &CacheLine{
			Addr:  blockAddr,
			State: StateModified,
			Data:  nil,
		}
	}

	// Copy data
	line := c.sets[setIdx][wayIdx]
	if data != nil {
		line.Data = make([]byte, len(data))
		copy(line.Data, data)
		if line.State == StateInvalid {
			line.State = StateModified
		}
	} else {
		line.Data = nil
	}

	c.updateLRU(setIdx, wayIdx)
}

// Invalidate implements Cache.Invalidate
func (c *SetAssociativeCache) Invalidate(addr uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	blockAddr := c.getBlockAddr(addr)
	setIdx := c.getSetIndex(addr)

	wayIdx, found := c.findWay(setIdx, blockAddr)
	if !found {
		return
	}

	line := c.sets[setIdx][wayIdx]
	oldState := line.State
	oldData := make([]byte, len(line.Data))
	copy(oldData, line.Data)

	// Call evict callback
	if c.evictCB != nil {
		c.evictCB(blockAddr, oldState, oldData)
	}

	// Mark as invalid
	c.sets[setIdx][wayIdx] = nil
}

// IsPresent implements Cache.IsPresent
func (c *SetAssociativeCache) IsPresent(addr uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	blockAddr := c.getBlockAddr(addr)
	setIdx := c.getSetIndex(addr)

	_, found := c.findWay(setIdx, blockAddr)
	return found
}

// SetEvictCallback implements Cache.SetEvictCallback
func (c *SetAssociativeCache) SetEvictCallback(callback EvictCallback) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictCB = callback
}

// HandleSnoop implements Cache.HandleSnoop
func (c *SetAssociativeCache) HandleSnoop(snoopOpcode int, addr uint64) (*SnoopResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	blockAddr := c.getBlockAddr(addr)
	setIdx := c.getSetIndex(addr)

	wayIdx, found := c.findWay(setIdx, blockAddr)
	if !found {
		// No data to provide
		return &SnoopResponse{
			ResponseOpcode: SnoopResponseNoData,
			Data:           nil,
			HasData:        false,
		}, nil
	}

	line := c.sets[setIdx][wayIdx]

	// Use MESI state transition helpers
	var newState State
	var shouldProvideData bool

	switch snoopOpcode {
	case SnoopRead:
		newState, shouldProvideData = MESIHandleReadSnoop(line.State)
	case SnoopReadX, SnoopInvalidate:
		newState, shouldProvideData = MESIHandleWriteSnoop(line.State)
	default:
		// Unknown opcode
		return &SnoopResponse{
			ResponseOpcode: SnoopResponseNoData,
			Data:           nil,
			HasData:        false,
		}, nil
	}

	// Update state
	line.State = newState

	// Prepare response
	response := &SnoopResponse{
		ResponseOpcode: SnoopResponseNoData,
		Data:           nil,
		HasData:        false,
	}

	if shouldProvideData && line.Data != nil {
		response.ResponseOpcode = SnoopResponseData
		response.Data = line.Data
		response.HasData = true
	}

	// If invalidated, remove the line
	if newState == StateInvalid {
		c.sets[setIdx][wayIdx] = nil
	}

	return response, nil
}

// CanForward implements Cache.CanForward
func (c *SetAssociativeCache) CanForward(addr uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	blockAddr := c.getBlockAddr(addr)
	setIdx := c.getSetIndex(addr)

	wayIdx, found := c.findWay(setIdx, blockAddr)
	if !found {
		return false
	}

	line := c.sets[setIdx][wayIdx]
	return line.State == StateModified ||
		line.State == StateExclusive ||
		line.State == StateOwned
}

// Access implements Cache.Access
func (c *SetAssociativeCache) Access(addr uint64, isWrite bool) *AccessResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.Accesses++

	blockAddr := c.getBlockAddr(addr)
	setIdx := c.getSetIndex(addr)

	wayIdx, found := c.findWay(setIdx, blockAddr)

	// Miss case
	if !found {
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
	line := c.sets[setIdx][wayIdx]
	oldState := line.State
	newState := oldState

	// State transition for write
	if isWrite {
		if oldState != StateModified {
			newState = StateModified
			line.State = StateModified
		}
	}

	// Update LRU
	c.updateLRU(setIdx, wayIdx)

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
func (c *SetAssociativeCache) Fill(addr uint64, data []byte, state State) (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	blockAddr := c.getBlockAddr(addr)
	setIdx := c.getSetIndex(addr)

	// Check if line already exists
	wayIdx, found := c.findWay(setIdx, blockAddr)
	if found {
		// Update existing line
		line := c.sets[setIdx][wayIdx]
		line.Data = make([]byte, len(data))
		copy(line.Data, data)
		line.State = state
		c.updateLRU(setIdx, wayIdx)
		return 0, false
	}

	// Find LRU way
	wayIdx = c.findLRUWay(setIdx)

	// Evict if necessary
	evictedAddr := uint64(0)
	needWriteback := false

	if c.sets[setIdx][wayIdx] != nil {
		evictedAddr, needWriteback = c.evictWay(setIdx, wayIdx)
		c.stats.Evictions++
		if needWriteback {
			c.stats.Writebacks++
		}
	}

	// Allocate new line
	c.sets[setIdx][wayIdx] = &CacheLine{
		Addr:  blockAddr,
		State: state,
		Data:  make([]byte, len(data)),
	}
	copy(c.sets[setIdx][wayIdx].Data, data)

	c.updateLRU(setIdx, wayIdx)

	return evictedAddr, needWriteback
}

// evictWay evicts a specific way in a set.
// Returns: (evicted address, needs writeback)
func (c *SetAssociativeCache) evictWay(setIdx, wayIdx int) (uint64, bool) {
	line := c.sets[setIdx][wayIdx]
	if line == nil {
		return 0, false
	}

	evictedAddr := line.Addr
	needWriteback := line.State == StateModified

	// Call evict callback
	if c.evictCB != nil {
		oldData := make([]byte, len(line.Data))
		copy(oldData, line.Data)
		c.evictCB(evictedAddr, line.State, oldData)
	}

	// Remove line
	c.sets[setIdx][wayIdx] = nil

	return evictedAddr, needWriteback
}

// GetStats implements Cache.GetStats
func (c *SetAssociativeCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// ResetStats implements Cache.ResetStats
func (c *SetAssociativeCache) ResetStats() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = CacheStats{}
}

// GetNumSets returns the number of sets.
func (c *SetAssociativeCache) GetNumSets() int {
	return c.numSets
}

// GetNumWays returns the number of ways per set.
func (c *SetAssociativeCache) GetNumWays() int {
	return c.numWays
}

// GetBlockSize returns the block size.
func (c *SetAssociativeCache) GetBlockSize() uint64 {
	return c.blockSize
}
