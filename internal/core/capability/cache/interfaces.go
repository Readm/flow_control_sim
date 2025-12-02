package cache

// State represents the state of a cache line.
// States are generic and protocol-agnostic.
type State string

const (
	StateInvalid   State = "Invalid"   // Cache line is invalid
	StateShared    State = "Shared"    // Cache line is shared (read-only)
	StateExclusive State = "Exclusive" // Cache line is exclusive (read-write, not modified)
	StateModified  State = "Modified"  // Cache line is modified (dirty)
	StateOwned     State = "Owned"     // Cache line is owned (MOESI protocol)
)

// EvictCallback is called when a cache line is evicted.
// Parameters: addr (address of evicted line), state (state before eviction), data (data before eviction)
type EvictCallback func(addr uint64, state State, data []byte)

// SnoopResponse represents the response to a snoop request.
// Used by coherence protocols (e.g., CHI, MESI, MOESI).
type SnoopResponse struct {
	ResponseOpcode int    // Protocol-specific response opcode
	Data           []byte // Data if forwarding
	HasData        bool   // Whether data is included
}

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

	// HandleSnoop handles a snoop request from another cache/directory.
	// Protocol-agnostic: opcode interpretation depends on implementation.
	// Parameters:
	//   - snoopOpcode: Protocol-specific snoop opcode
	//   - addr: Target address
	// Returns:
	//   - *SnoopResponse: Response including data if needed
	//   - error: Any error encountered
	HandleSnoop(snoopOpcode int, addr uint64) (*SnoopResponse, error)

	// CanForward checks if this cache can forward data for the given address.
	// Used in protocols that support direct cache-to-cache transfer (DMT).
	// Returns true if the cache has the data in a state that allows forwarding.
	CanForward(addr uint64) bool
}

