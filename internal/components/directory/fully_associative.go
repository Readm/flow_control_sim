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

// MustWaitForWriteback implements Directory.MustWaitForWriteback
func (d *FullyAssociativeDirectory) MustWaitForWriteback(addr uint64) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	entry, exists := d.entries[addr]
	if !exists {
		return false
	}

	// Need to wait for writeback if line is Modified
	return entry.State == StateModified
}

// HasPendingRequest implements Directory.HasPendingRequest
func (d *FullyAssociativeDirectory) HasPendingRequest(addr uint64) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// TODO: This requires tracking pending requests
	// For now, return false (no conflict detection)
	// Can be enhanced later with a pending request map
	return false
}

// HandleMESIRequest implements Directory.HandleMESIRequest
func (d *FullyAssociativeDirectory) HandleMESIRequest(addr uint64, requesterID int, isWrite bool) *MESIAction {
	d.mu.Lock()
	defer d.mu.Unlock()

	entry, exists := d.entries[addr]

	action := &MESIAction{
		InvalidateList: []int{},
		ForwarderID:    -1,
		NeedMemory:     false,
		NewState:       StateNotPresent,
		GrantExclusive: false,
	}

	// Case 1: No directory entry exists (first access)
	if !exists || entry.State == StateNotPresent {
		if isWrite {
			// Write request, no sharers -> grant Exclusive
			action.NeedMemory = true
			action.NewState = StateExclusive
			action.GrantExclusive = true

			// Update directory
			d.ensureCapacity()
			d.entries[addr] = &DirectoryEntry{
				Addr:    addr,
				State:   StateExclusive,
				Sharers: []int{},
				Owner:   requesterID,
			}
		} else {
			// Read request, no sharers -> grant Exclusive (will be Shared if others request)
			action.NeedMemory = true
			action.NewState = StateExclusive
			action.GrantExclusive = true

			// Update directory
			d.ensureCapacity()
			d.entries[addr] = &DirectoryEntry{
				Addr:    addr,
				State:   StateExclusive,
				Sharers: []int{requesterID},
				Owner:   requesterID,
			}
		}
		return action
	}

	// Case 2: Entry exists in Exclusive state
	if entry.State == StateExclusive {
		owner := entry.Owner

		if isWrite {
			// Write request
			if owner == requesterID {
				// Same owner, upgrade to Modified (no action needed)
				entry.State = StateModified
				action.NewState = StateModified
				action.GrantExclusive = true
			} else {
				// Different owner, invalidate current owner
				action.InvalidateList = []int{owner}
				action.ForwarderID = owner // May forward data
				action.NewState = StateModified
				action.GrantExclusive = true

				// Update directory
				entry.State = StateModified
				entry.Owner = requesterID
				entry.Sharers = []int{}
			}
		} else {
			// Read request
			if owner == requesterID {
				// Same owner, already have data (no action)
				action.NewState = StateExclusive
			} else {
				// Different owner, downgrade to Shared
				action.ForwarderID = owner // Current owner provides data
				action.NewState = StateShared

				// Update directory
				entry.State = StateShared
				entry.Sharers = []int{owner, requesterID}
				entry.Owner = -1
			}
		}
		return action
	}

	// Case 3: Entry exists in Modified state
	if entry.State == StateModified {
		owner := entry.Owner

		if isWrite {
			// Write request
			if owner == requesterID {
				// Same owner, already Modified (no action)
				action.NewState = StateModified
				action.GrantExclusive = true
			} else {
				// Different owner, invalidate and transfer ownership
				action.InvalidateList = []int{owner}
				action.ForwarderID = owner // Current owner must provide dirty data
				action.NewState = StateModified
				action.GrantExclusive = true

				// Update directory
				entry.Owner = requesterID
			}
		} else {
			// Read request
			if owner == requesterID {
				// Same owner, already have data
				action.NewState = StateModified
			} else {
				// Different owner, owner must writeback and downgrade to Shared
				action.ForwarderID = owner // Owner provides data
				action.NewState = StateShared

				// Update directory
				entry.State = StateShared
				entry.Sharers = []int{owner, requesterID}
				entry.Owner = -1
			}
		}
		return action
	}

	// Case 4: Entry exists in Shared state
	if entry.State == StateShared {
		if isWrite {
			// Write request, invalidate all sharers except requester
			for _, sharerID := range entry.Sharers {
				if sharerID != requesterID {
					action.InvalidateList = append(action.InvalidateList, sharerID)
				}
			}

			// Check if requester is already a sharer
			isSharer := false
			for _, sharerID := range entry.Sharers {
				if sharerID == requesterID {
					isSharer = true
					break
				}
			}

			if !isSharer {
				// Not a sharer, need data from memory or another sharer
				if len(entry.Sharers) > 0 {
					action.ForwarderID = entry.Sharers[0]
				} else {
					action.NeedMemory = true
				}
			}

			action.NewState = StateModified
			action.GrantExclusive = true

			// Update directory
			entry.State = StateModified
			entry.Sharers = []int{}
			entry.Owner = requesterID
		} else {
			// Read request, add to sharers if not already
			isSharer := false
			for _, sharerID := range entry.Sharers {
				if sharerID == requesterID {
					isSharer = true
					break
				}
			}

			if !isSharer {
				// Need data from memory or another sharer
				if len(entry.Sharers) > 0 {
					action.ForwarderID = entry.Sharers[0]
				} else {
					action.NeedMemory = true
				}
				entry.Sharers = append(entry.Sharers, requesterID)
			}

			action.NewState = StateShared
		}
		return action
	}

	// Default: should not reach here
	return action
}

