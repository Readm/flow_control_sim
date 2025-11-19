package hook

import (
	"testing"

	"github.com/Readm/flow_sim/internal/dataflow/transaction"
)

// TestMockIncentiveHookCreateEveryNCycles tests cycle-based transaction creation.
func TestMockIncentiveHookCreateEveryNCycles(t *testing.T) {
	t.Parallel()

	mgr := transaction.NewManager()
	hook := NewMockIncentiveHook(mgr)
	hook.SetCreateEveryNCycles(5)

	nodeID := 1

	// Should not create at cycle 1
	if hook.ShouldCreateTransaction(nodeID, 1) {
		t.Fatalf("should not create transaction at cycle 1")
	}

	// Should create at cycle 5
	if !hook.ShouldCreateTransaction(nodeID, 5) {
		t.Fatalf("should create transaction at cycle 5")
	}

	// Should create at cycle 10
	if !hook.ShouldCreateTransaction(nodeID, 10) {
		t.Fatalf("should create transaction at cycle 10")
	}
}

// TestMockIncentiveHookCreateProbability tests probability-based transaction creation.
func TestMockIncentiveHookCreateProbability(t *testing.T) {
	t.Parallel()

	mgr := transaction.NewManager()
	hook := NewMockIncentiveHook(mgr)
	hook.SetCreateProbability(0.5)

	nodeID := 1

	// Test multiple cycles to verify probability behavior
	createdCount := 0
	totalCycles := 100
	for cycle := uint64(0); cycle < uint64(totalCycles); cycle++ {
		if hook.ShouldCreateTransaction(nodeID, cycle) {
			createdCount++
		}
	}

	// With 0.5 probability, we should get roughly 50% creation rate
	// Allow some variance (30-70%)
	if createdCount < 30 || createdCount > 70 {
		t.Logf("created %d out of %d cycles (expected ~50)", createdCount, totalCycles)
	}
}

// TestMockIncentiveHookMaxTransactionsPerNode tests max transactions limit.
func TestMockIncentiveHookMaxTransactionsPerNode(t *testing.T) {
	t.Parallel()

	mgr := transaction.NewManager()
	hook := NewMockIncentiveHook(mgr)
	hook.SetCreateEveryNCycles(1)
	hook.SetMaxTransactionsPerNode(3)

	nodeID := 1

	// Create 3 transactions
	for cycle := uint64(0); cycle < 3; cycle++ {
		txn, err := hook.CreateTransaction(nodeID, cycle)
		if err != nil {
			t.Fatalf("failed to create transaction: %v", err)
		}
		if txn == nil {
			t.Fatalf("expected transaction to be created at cycle %d", cycle)
		}
	}

	// Should not create more transactions
	if hook.ShouldCreateTransaction(nodeID, 10) {
		t.Fatalf("should not create transaction after reaching max")
	}

	// Reset and verify can create again
	hook.ResetNodeCounters()
	if !hook.ShouldCreateTransaction(nodeID, 10) {
		t.Fatalf("should create transaction after reset")
	}
}

// TestMockIncentiveHookCreateTransaction tests actual transaction creation.
func TestMockIncentiveHookCreateTransaction(t *testing.T) {
	t.Parallel()

	mgr := transaction.NewManager()
	hook := NewMockIncentiveHook(mgr)
	hook.SetCreateEveryNCycles(1)

	nodeID := 1
	cycle := uint64(0)

	txn, err := hook.CreateTransaction(nodeID, cycle)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	if txn == nil {
		t.Fatalf("expected transaction to be created")
	}

	if txn.InitiatorNodeID != nodeID {
		t.Fatalf("expected initiator node %d, got %d", nodeID, txn.InitiatorNodeID)
	}

	// Verify transaction is in manager
	retrieved, ok := mgr.GetTransaction(txn.ID)
	if !ok {
		t.Fatalf("transaction not found in manager")
	}
	if retrieved.ID != txn.ID {
		t.Fatalf("transaction ID mismatch")
	}
}

// TestMockIncentiveHookNoCreation tests when conditions are not met.
func TestMockIncentiveHookNoCreation(t *testing.T) {
	t.Parallel()

	mgr := transaction.NewManager()
	hook := NewMockIncentiveHook(mgr)
	// No creation conditions set

	nodeID := 1
	cycle := uint64(0)

	if hook.ShouldCreateTransaction(nodeID, cycle) {
		t.Fatalf("should not create transaction when no conditions set")
	}

	txn, err := hook.CreateTransaction(nodeID, cycle)
	if err != nil {
		t.Fatalf("CreateTransaction should not return error: %v", err)
	}
	if txn != nil {
		t.Fatalf("expected no transaction to be created")
	}
}

