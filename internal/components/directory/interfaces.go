package directory

// State represents the state of a directory entry.
type State string

const (
	StateNotPresent State = "NotPresent" // No sharers
	StateShared     State = "Shared"     // Multiple sharers
	StateExclusive  State = "Exclusive"  // Single sharer (exclusive)
	StateModified   State = "Modified"  // Single sharer (modified/dirty)
)

// EvictCallback is called when a directory entry is evicted.
// Parameters: addr (address of evicted entry), state (state before eviction), sharers (sharers before eviction), owner (owner before eviction)
type EvictCallback func(addr uint64, state State, sharers []int, owner int)

// Directory defines the interface for directory storage and sharer tracking.
// This interface is protocol-agnostic and focuses purely on tracking who has copies of cache lines.
type Directory interface {
	// GetState returns the current state of the directory entry at the given address.
	GetState(addr uint64) State

	// SetState updates the state of the directory entry at the given address.
	SetState(addr uint64, state State)

	// GetSharers returns the list of node IDs that have a copy of the cache line.
	// Returns an empty slice if no sharers exist.
	GetSharers(addr uint64) []int

	// AddSharer adds a node ID to the sharer list for the given address.
	AddSharer(addr uint64, nodeID int)

	// RemoveSharer removes a node ID from the sharer list for the given address.
	RemoveSharer(addr uint64, nodeID int)

	// ClearSharers clears all sharers for the given address.
	ClearSharers(addr uint64)

	// GetOwner returns the owner node ID for the given address (if in Exclusive or Modified state).
	// Returns -1 if no owner exists.
	GetOwner(addr uint64) int

	// SetOwner sets the owner node ID for the given address.
	SetOwner(addr uint64, nodeID int)

	// SetEvictCallback sets the callback function to be called when a directory entry is evicted.
	SetEvictCallback(callback EvictCallback)

	// MustWaitForWriteback checks if a writeback must be awaited before processing
	// a request for the given address. Returns true if the line is in Modified state
	// and needs writeback.
	MustWaitForWriteback(addr uint64) bool

	// HasPendingRequest checks if there are pending requests for the given address.
	// Used for conflict detection and request serialization.
	// Returns true if another transaction is already processing this address.
	HasPendingRequest(addr uint64) bool
}

