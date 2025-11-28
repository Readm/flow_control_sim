package hook

import (
	"context"

	"github.com/Readm/flow_sim/internal/dataflow/transaction"
)

// IncentiveHook defines the interface for creating transactions.
// This hook will be used by the Incentive plugin in the future.
type IncentiveHook interface {
	// ShouldCreateTransaction determines whether a transaction should be created
	// for the specified node at the given cycle.
	ShouldCreateTransaction(nodeID int, cycle uint64) bool

	// CreateTransaction creates a new transaction for the specified node at the given cycle.
	// Returns an error if creation fails.
	CreateTransaction(ctx context.Context, nodeID int, cycle uint64) error
}

// MockIncentiveHook is a simple implementation of IncentiveHook for testing and examples.
type MockIncentiveHook struct {
	// CreateEveryNCycles creates a transaction every N cycles (0 means never)
	CreateEveryNCycles uint64
	// CreateProbability creates a transaction with this probability (0.0 to 1.0)
	CreateProbability float64
	// MaxTransactionsPerNode limits the number of transactions per node (0 means unlimited)
	MaxTransactionsPerNode int
	// TransactionManager is used to create transactions
	TransactionManager *transaction.TxnManager
	// TransactionFunc defines the transaction logic to run when created
	TransactionFunc func(*transaction.TxnContext)
	// Node transaction counters
	nodeCounters map[int]int
}

// NewMockIncentiveHook creates a new MockIncentiveHook.
func NewMockIncentiveHook(mgr *transaction.TxnManager) *MockIncentiveHook {
	return &MockIncentiveHook{
		TransactionManager:     mgr,
		nodeCounters:           make(map[int]int),
		CreateEveryNCycles:     0, // Default: never
		CreateProbability:      0.0,
		MaxTransactionsPerNode: 0, // Default: unlimited
	}
}

// ShouldCreateTransaction determines whether a transaction should be created.
func (h *MockIncentiveHook) ShouldCreateTransaction(nodeID int, cycle uint64) bool {
	// Check max transactions per node
	if h.MaxTransactionsPerNode > 0 {
		count := h.nodeCounters[nodeID]
		if count >= h.MaxTransactionsPerNode {
			return false
		}
	}

	// Check cycle-based creation
	if h.CreateEveryNCycles > 0 {
		if cycle%h.CreateEveryNCycles == 0 {
			return true
		}
	}

	// Check probability-based creation
	// For simplicity, use cycle as seed for deterministic behavior
	if h.CreateProbability > 0.0 {
		// Simple hash-based probability check
		hash := uint64(nodeID) + cycle
		prob := float64(hash%100) / 100.0
		if prob < h.CreateProbability {
			return true
		}
	}

	return false
}

// CreateTransaction creates a new transaction if conditions are met.
func (h *MockIncentiveHook) CreateTransaction(ctx context.Context, nodeID int, cycle uint64) error {
	if !h.ShouldCreateTransaction(nodeID, cycle) {
		return nil
	}

	if h.TransactionManager == nil || h.TransactionFunc == nil {
		return nil
	}

	h.TransactionManager.Start(ctx, h.TransactionFunc)

	// Update counter
	if h.MaxTransactionsPerNode > 0 {
		h.nodeCounters[nodeID]++
	}

	return nil
}

// SetCreateEveryNCycles sets the cycle interval for transaction creation.
func (h *MockIncentiveHook) SetCreateEveryNCycles(n uint64) {
	h.CreateEveryNCycles = n
}

// SetCreateProbability sets the probability for transaction creation.
func (h *MockIncentiveHook) SetCreateProbability(prob float64) {
	if prob < 0.0 {
		prob = 0.0
	}
	if prob > 1.0 {
		prob = 1.0
	}
	h.CreateProbability = prob
}

// SetMaxTransactionsPerNode sets the maximum number of transactions per node.
func (h *MockIncentiveHook) SetMaxTransactionsPerNode(max int) {
	h.MaxTransactionsPerNode = max
}

// ResetNodeCounters resets the transaction counters for all nodes.
func (h *MockIncentiveHook) ResetNodeCounters() {
	h.nodeCounters = make(map[int]int)
}

// SetTransactionFunc sets the transaction function to execute when creating a transaction.
func (h *MockIncentiveHook) SetTransactionFunc(fn func(*transaction.TxnContext)) {
	h.TransactionFunc = fn
}
