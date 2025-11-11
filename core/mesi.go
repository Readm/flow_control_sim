package core

// MESIState represents the MESI cache coherence protocol state.
type MESIState string

const (
	MESIInvalid   MESIState = "Invalid"   // Cache line is invalid (not present or stale)
	MESIShared    MESIState = "Shared"    // Cache line is shared (multiple caches may have it)
	MESIExclusive MESIState = "Exclusive" // Cache line is exclusive (only this cache has it, clean)
	MESIModified  MESIState = "Modified"  // Cache line is modified (only this cache has it, dirty)
)

// IsValid returns true if the cache line is valid (not Invalid).
func (s MESIState) IsValid() bool {
	return s != MESIInvalid
}

// CanProvideData returns true if the cache line can provide data to other caches.
func (s MESIState) CanProvideData() bool {
	return s == MESIShared || s == MESIExclusive || s == MESIModified
}

