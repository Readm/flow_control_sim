package directory

import (
	"math/rand"
	"sync"
)

// DirectoryEntry represents a single directory entry.
type DirectoryEntry struct {
	Addr    uint64
	State   State
	Sharers []int // List of node IDs that have a copy
	Owner   int   // Owner node ID (-1 if no owner)
}

// FullyAssociativeDirectory implements a simple fully-associative directory with random replacement.
type FullyAssociativeDirectory struct {
	capacity int
	entries  map[uint64]*DirectoryEntry // Direct lookup by address
	mu       sync.RWMutex
	evictCB  EvictCallback
	rng      *rand.Rand
}

// NewFullyAssociativeDirectory creates a new fully-associative directory with the specified capacity.
func NewFullyAssociativeDirectory(capacity int) *FullyAssociativeDirectory {
	if capacity <= 0 {
		capacity = 64 // Default capacity
	}
	return &FullyAssociativeDirectory{
		capacity: capacity,
		entries:  make(map[uint64]*DirectoryEntry),
		rng:      rand.New(rand.NewSource(0)), // Deterministic seed for testing
	}
}

// GetState returns the current state of the directory entry at the given address.
func (d *FullyAssociativeDirectory) GetState(addr uint64) State {
	d.mu.RLock()
	defer d.mu.RUnlock()

	entry, exists := d.entries[addr]
	if !exists {
		return StateNotPresent
	}
	return entry.State
}

// SetState updates the state of the directory entry at the given address.
func (d *FullyAssociativeDirectory) SetState(addr uint64, state State) {
	d.mu.Lock()
	defer d.mu.Unlock()

	entry, exists := d.entries[addr]
	if !exists {
		// Need to allocate a new entry
		d.ensureCapacity()
		entry = &DirectoryEntry{
			Addr:    addr,
			State:   state,
			Sharers: make([]int, 0),
			Owner:   -1,
		}
		d.entries[addr] = entry
	} else {
		entry.State = state
	}
}

// GetSharers returns the list of node IDs that have a copy of the cache line.
func (d *FullyAssociativeDirectory) GetSharers(addr uint64) []int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	entry, exists := d.entries[addr]
	if !exists {
		return []int{}
	}
	// Return a copy to prevent external modification
	sharers := make([]int, len(entry.Sharers))
	copy(sharers, entry.Sharers)
	return sharers
}

// AddSharer adds a node ID to the sharer list for the given address.
func (d *FullyAssociativeDirectory) AddSharer(addr uint64, nodeID int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	entry, exists := d.entries[addr]
	if !exists {
		// Need to allocate a new entry
		d.ensureCapacity()
		entry = &DirectoryEntry{
			Addr:    addr,
			State:   StateShared,
			Sharers: make([]int, 0),
			Owner:   -1,
		}
		d.entries[addr] = entry
	}

	// Check if nodeID is already in sharers list
	for _, id := range entry.Sharers {
		if id == nodeID {
			return // Already a sharer
		}
	}

	// Add to sharers list
	entry.Sharers = append(entry.Sharers, nodeID)

	// Update state based on number of sharers
	if len(entry.Sharers) == 1 {
		entry.State = StateExclusive
	} else {
		entry.State = StateShared
	}
}

// RemoveSharer removes a node ID from the sharer list for the given address.
func (d *FullyAssociativeDirectory) RemoveSharer(addr uint64, nodeID int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	entry, exists := d.entries[addr]
	if !exists {
		return
	}

	// Remove nodeID from sharers list
	newSharers := make([]int, 0, len(entry.Sharers))
	for _, id := range entry.Sharers {
		if id != nodeID {
			newSharers = append(newSharers, id)
		}
	}
	entry.Sharers = newSharers

	// Update state based on number of sharers
	if len(entry.Sharers) == 0 {
		entry.State = StateNotPresent
	} else if len(entry.Sharers) == 1 {
		entry.State = StateExclusive
	} else {
		entry.State = StateShared
	}
}

// ClearSharers clears all sharers for the given address.
func (d *FullyAssociativeDirectory) ClearSharers(addr uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	entry, exists := d.entries[addr]
	if exists {
		entry.Sharers = []int{}
		entry.State = StateNotPresent
		entry.Owner = -1
	}
}

// GetOwner returns the owner node ID for the given address.
func (d *FullyAssociativeDirectory) GetOwner(addr uint64) int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	entry, exists := d.entries[addr]
	if !exists {
		return -1
	}
	return entry.Owner
}

// SetOwner sets the owner node ID for the given address.
func (d *FullyAssociativeDirectory) SetOwner(addr uint64, nodeID int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	entry, exists := d.entries[addr]
	if !exists {
		// Need to allocate a new entry
		d.ensureCapacity()
		entry = &DirectoryEntry{
			Addr:    addr,
			State:   StateExclusive,
			Sharers: make([]int, 0),
			Owner:   nodeID,
		}
		d.entries[addr] = entry
	} else {
		entry.Owner = nodeID
		if nodeID == -1 {
			entry.State = StateNotPresent
		} else if len(entry.Sharers) == 0 {
			entry.State = StateExclusive
		}
	}
}

// SetEvictCallback sets the callback function to be called when a directory entry is evicted.
func (d *FullyAssociativeDirectory) SetEvictCallback(callback EvictCallback) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.evictCB = callback
}

// ensureCapacity ensures there is capacity for a new directory entry.
// If the directory is full, it randomly evicts one entry.
func (d *FullyAssociativeDirectory) ensureCapacity() {
	if len(d.entries) < d.capacity {
		return
	}

	// Directory is full, need to evict a random entry
	// Collect all addresses
	addrs := make([]uint64, 0, len(d.entries))
	for addr := range d.entries {
		addrs = append(addrs, addr)
	}

	// Randomly select one to evict
	if len(addrs) > 0 {
		evictAddr := addrs[d.rng.Intn(len(addrs))]
		entry := d.entries[evictAddr]

		// Call evict callback before removing
		if d.evictCB != nil {
			oldSharers := make([]int, len(entry.Sharers))
			copy(oldSharers, entry.Sharers)
			d.evictCB(evictAddr, entry.State, oldSharers, entry.Owner)
		}

		// Remove from map
		delete(d.entries, evictAddr)
	}
}

// GetCapacity returns the capacity of the directory.
func (d *FullyAssociativeDirectory) GetCapacity() int {
	return d.capacity
}

// GetSize returns the current number of directory entries.
func (d *FullyAssociativeDirectory) GetSize() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.entries)
}

