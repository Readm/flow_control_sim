package main

import "flow_sim/core"

// TransactionState represents the state of a CHI transaction
type TransactionState = core.TransactionState

const (
	TxStateInitiated TransactionState = core.TxStateInitiated // Transaction created
	TxStateInFlight  TransactionState = core.TxStateInFlight  // Transaction in progress
	TxStateCompleted TransactionState = core.TxStateCompleted // Transaction completed successfully
	TxStateAborted   TransactionState = core.TxStateAborted   // Transaction aborted
)

// DependencyType represents the type of dependency between transactions
type DependencyType = core.DependencyType

const (
	// Causal dependency: Transaction B is directly triggered by Transaction A
	DepCausal DependencyType = core.DepCausal

	// Cache dependencies: Transaction B triggered due to Transaction A's cache operations
	DepCacheMiss       DependencyType = core.DepCacheMiss
	DepCacheEvict      DependencyType = core.DepCacheEvict
	DepCacheInvalidate DependencyType = core.DepCacheInvalidate

	// Ordering dependencies: Transaction B must wait for Transaction A to complete
	DepSameAddrOrdering DependencyType = core.DepSameAddrOrdering
	DepGlobalOrdering   DependencyType = core.DepGlobalOrdering

	// Directory dependencies: Transaction B needs to wait for Transaction A's directory operation
	DepDirectoryQuery  DependencyType = core.DepDirectoryQuery
	DepDirectoryUpdate DependencyType = core.DepDirectoryUpdate

	// Resource contention: Transaction B competes with Transaction A for the same resource
	DepResourceContention DependencyType = core.DepResourceContention
)

// StateTransition represents a key state transition in a transaction's lifecycle
type StateTransition = core.StateTransition

// TransactionContext represents the context of a CHI transaction
// Design principle: Only store essential information, detailed state queried via interfaces
type TransactionContext = core.TransactionContext

// Transaction represents a CHI protocol transaction
type Transaction = core.Transaction

// TransactionDependency represents a dependency relationship between two transactions
type TransactionDependency = core.TransactionDependency

// CacheStateProvider interface: implemented by Cache module
// Core principle: Abstract model does not store cache/directory state directly, but queries via interfaces
type CacheStateProvider interface {
	// GetCacheState queries cache state for specified address
	GetCacheState(address uint64, nodeID int) (CacheState, bool)
	// GetCacheHit queries cache hit/miss result for specific transaction
	GetCacheHit(transactionID int64) *bool
	// GetCacheStateHistory queries cache state transition history (simplified)
	GetCacheStateHistory(transactionID int64) []CacheStateTransition
}

// DirectoryStateProvider interface: implemented by Directory module
type DirectoryStateProvider interface {
	// GetDirectoryState queries directory state for specified address
	GetDirectoryState(address uint64) DirectoryState
	// GetDirectoryHistory queries directory operation history (simplified)
	GetDirectoryHistory(transactionID int64) []DirectoryOperation
}

// OrderingConstraintProvider interface: implemented by Ordering module
type OrderingConstraintProvider interface {
	// GetSameAddrOrdering queries same-address ordering constraints
	GetSameAddrOrdering(address uint64) []int64 // Returns list of dependent transaction IDs
	// GetGlobalOrdering queries global ordering constraints
	GetGlobalOrdering(transactionID int64) []int64
	// GetOrderingDecisions queries ordering decision points
	GetOrderingDecisions(transactionID int64) []OrderingDecision
}

// Placeholder types for future implementation
// These will be implemented by other modules

// CacheState represents cache line state
type CacheState = core.CacheState

const (
	CacheStateInvalid CacheState = core.CacheStateInvalid
	CacheStateShared  CacheState = core.CacheStateShared
	CacheStateUnique  CacheState = core.CacheStateUnique
)

// CacheStateTransition represents a cache state transition
type CacheStateTransition = core.CacheStateTransition

// DirectoryState represents directory state
type DirectoryState = core.DirectoryState

const (
	DirectoryStateExclusive DirectoryState = core.DirectoryStateExclusive
	DirectoryStateShared    DirectoryState = core.DirectoryStateShared
	DirectoryStateInvalid   DirectoryState = core.DirectoryStateInvalid
)

// DirectoryOperation represents a directory operation
type DirectoryOperation = core.DirectoryOperation

// OrderingDecision represents an ordering decision point
type OrderingDecision = core.OrderingDecision

// PacketEventType represents the type of packet event
type PacketEventType = core.PacketEventType

const (
	PacketEnqueued        PacketEventType = core.PacketEnqueued
	PacketDequeued        PacketEventType = core.PacketDequeued
	PacketProcessingStart PacketEventType = core.PacketProcessingStart
	PacketProcessingEnd   PacketEventType = core.PacketProcessingEnd
	PacketSent            PacketEventType = core.PacketSent
	PacketInTransitEnd    PacketEventType = core.PacketInTransitEnd
	PacketReceived        PacketEventType = core.PacketReceived
	PacketGenerated       PacketEventType = core.PacketGenerated
)

// PacketEvent represents a packet event in the transaction timeline
type PacketEvent = core.PacketEvent
