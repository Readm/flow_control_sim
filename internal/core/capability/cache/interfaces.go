package cache

// State represents the state of a cache line.
// States are generic and protocol-agnostic.
type State string

const (
	StateInvalid  State = "Invalid"  // Cache line is invalid
	StateShared   State = "Shared"   // Cache line is shared (read-only)
	StateExclusive State = "Exclusive" // Cache line is exclusive (read-write, not modified)
	StateModified State = "Modified" // Cache line is modified (dirty)
)

// EvictCallback is called when a cache line is evicted.
// Parameters: addr (address of evicted line), state (state before eviction), data (data before eviction)
type EvictCallback func(addr uint64, state State, data []byte)

// Cache defines the interface for cache storage and state management.
// This interface is protocol-agnostic and focuses purely on state/data storage.
type Cache interface {
	// GetState returns the current state of the cache line at the given address.
	// Returns StateInvalid if the line is not present.
	GetState(addr uint64) State

	// SetState updates the state of the cache line at the given address.
	SetState(addr uint64, state State)

	// GetData retrieves the data stored in the cache line at the given address.
	// Returns nil if the line is invalid or not present.
	GetData(addr uint64) []byte

	// SetData updates the data stored in the cache line at the given address.
	// This may also implicitly set the state to Modified if the line exists.
	SetData(addr uint64, data []byte)

	// Invalidate marks the cache line at the given address as invalid.
	Invalidate(addr uint64)

	// IsPresent checks if a cache line exists for the given address (regardless of state).
	IsPresent(addr uint64) bool

	// SetEvictCallback sets the callback function to be called when a cache line is evicted.
	SetEvictCallback(callback EvictCallback)
}

