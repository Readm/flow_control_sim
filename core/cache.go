package core

// CacheState represents cache line state.
type CacheState string

const (
	CacheStateInvalid CacheState = "Invalid"
	CacheStateShared  CacheState = "Shared"
	CacheStateUnique  CacheState = "Unique"
)

// CacheStateTransition represents a cache state transition.
type CacheStateTransition struct {
	Cycle     int
	FromState CacheState
	ToState   CacheState
	Reason    string
}

// DirectoryState represents directory state.
type DirectoryState string

const (
	DirectoryStateExclusive DirectoryState = "Exclusive"
	DirectoryStateShared    DirectoryState = "Shared"
	DirectoryStateInvalid   DirectoryState = "Invalid"
)

// DirectoryOperation represents a directory operation.
type DirectoryOperation struct {
	Cycle     int
	Operation string
	Address   uint64
}

// OrderingDecision represents an ordering decision point.
type OrderingDecision struct {
	Cycle       int
	Decision    string
	RelatedTxns []int64
}


