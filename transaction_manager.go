package main

import (
	"sync"
)

// TransactionManager is the core component responsible for:
// - Creating and managing Transaction objects
// - Recording dependencies between transactions
// - Maintaining Transaction Graph
// - Providing query interfaces
type TransactionManager struct {
	mu           sync.RWMutex
	transactions map[int64]*Transaction
	dependencies map[int64][]*TransactionDependency // fromID -> dependencies
	nextTxnID    int64
}

// NewTransactionManager creates a new TransactionManager
func NewTransactionManager() *TransactionManager {
	return &TransactionManager{
		transactions: make(map[int64]*Transaction),
		dependencies: make(map[int64][]*TransactionDependency),
		nextTxnID:    1,
	}
}

// CreateTransaction creates a new transaction and returns it
func (tm *TransactionManager) CreateTransaction(
	transactionType CHITransactionType,
	address uint64,
	initiatedAt int,
) *Transaction {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	txnID := tm.nextTxnID
	tm.nextTxnID++

	ctx := &TransactionContext{
		TransactionID:    txnID,
		TransactionType:   transactionType,
		Address:           address,
		State:             TxStateInitiated,
		StateHistory:      make([]StateTransition, 0),
		InitiatedAt:       initiatedAt,
		CompletedAt:       0,
		SameAddrOrdering:  make([]int64, 0),
		GlobalOrdering:    make([]int64, 0),
		GeneratedTransactions: make([]int64, 0),
		Metadata:          make(map[string]string),
	}

	// Record initial state transition
	ctx.StateHistory = append(ctx.StateHistory, StateTransition{
		Cycle:     initiatedAt,
		FromState: "",
		ToState:   TxStateInitiated,
		Reason:    "Transaction created",
	})

	txn := &Transaction{
		Context: ctx,
	}

	tm.transactions[txnID] = txn
	return txn
}

// GetTransaction retrieves a transaction by ID
func (tm *TransactionManager) GetTransaction(txnID int64) *Transaction {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.transactions[txnID]
}

// AddDependency records a dependency relationship between two transactions
func (tm *TransactionManager) AddDependency(
	fromTxnID, toTxnID int64,
	depType DependencyType,
	reason string,
	cycle int,
) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Verify both transactions exist
	fromTxn := tm.transactions[fromTxnID]
	toTxn := tm.transactions[toTxnID]
	if fromTxn == nil || toTxn == nil {
		return
	}

	// Create dependency
	dep := &TransactionDependency{
		FromTransactionID: fromTxnID,
		ToTransactionID:   toTxnID,
		DependencyType:    depType,
		Reason:            reason,
		Cycle:             cycle,
	}

	// Add to dependencies map
	tm.dependencies[fromTxnID] = append(tm.dependencies[fromTxnID], dep)

	// Update transaction context based on dependency type
	switch depType {
	case DepSameAddrOrdering:
		toTxn.Context.SameAddrOrdering = append(toTxn.Context.SameAddrOrdering, fromTxnID)
	case DepGlobalOrdering:
		toTxn.Context.GlobalOrdering = append(toTxn.Context.GlobalOrdering, fromTxnID)
	case DepCausal:
		fromTxn.Context.GeneratedTransactions = append(fromTxn.Context.GeneratedTransactions, toTxnID)
	}
}

// GetDependencies retrieves all dependencies for a transaction
func (tm *TransactionManager) GetDependencies(txnID int64) []*TransactionDependency {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.dependencies[txnID]
}

// UpdateTransactionState updates the state of a transaction
func (tm *TransactionManager) UpdateTransactionState(
	txnID int64,
	newState TransactionState,
	reason string,
	cycle int,
	relatedTxnID *int64,
) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	txn := tm.transactions[txnID]
	if txn == nil {
		return
	}

	oldState := txn.Context.State
	txn.Context.State = newState

	// Record state transition
	txn.Context.StateHistory = append(txn.Context.StateHistory, StateTransition{
		Cycle:       cycle,
		FromState:   oldState,
		ToState:     newState,
		Reason:      reason,
		RelatedTxnID: relatedTxnID,
	})

	// Update completion time if completed
	if newState == TxStateCompleted || newState == TxStateAborted {
		txn.Context.CompletedAt = cycle
	}
}

// MarkTransactionInFlight marks a transaction as in-flight
func (tm *TransactionManager) MarkTransactionInFlight(txnID int64, cycle int) {
	tm.UpdateTransactionState(txnID, TxStateInFlight, "Transaction started processing", cycle, nil)
}

// MarkTransactionCompleted marks a transaction as completed
func (tm *TransactionManager) MarkTransactionCompleted(txnID int64, cycle int) {
	tm.UpdateTransactionState(txnID, TxStateCompleted, "Transaction completed", cycle, nil)
}

// MarkTransactionAborted marks a transaction as aborted
func (tm *TransactionManager) MarkTransactionAborted(txnID int64, cycle int, reason string) {
	tm.UpdateTransactionState(txnID, TxStateAborted, reason, cycle, nil)
}

// AddMetadata adds metadata to a transaction
func (tm *TransactionManager) AddMetadata(txnID int64, key, value string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	txn := tm.transactions[txnID]
	if txn == nil {
		return
	}

	if txn.Context.Metadata == nil {
		txn.Context.Metadata = make(map[string]string)
	}
	txn.Context.Metadata[key] = value
}

// GetAllTransactions returns all transactions (for graph construction)
func (tm *TransactionManager) GetAllTransactions() map[int64]*Transaction {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// Return a copy to avoid external modification
	result := make(map[int64]*Transaction)
	for k, v := range tm.transactions {
		result[k] = v
	}
	return result
}

// GetAllDependencies returns all dependencies (for graph construction)
func (tm *TransactionManager) GetAllDependencies() map[int64][]*TransactionDependency {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// Return a copy to avoid external modification
	result := make(map[int64][]*TransactionDependency)
	for k, v := range tm.dependencies {
		deps := make([]*TransactionDependency, len(v))
		copy(deps, v)
		result[k] = deps
	}
	return result
}

// Reset clears all transactions and dependencies (for simulator reset)
func (tm *TransactionManager) Reset() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.transactions = make(map[int64]*Transaction)
	tm.dependencies = make(map[int64][]*TransactionDependency)
	tm.nextTxnID = 1
}

