package transaction

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Readm/flow_sim/internal/dataflow/message"
)

// Manager manages the lifecycle of transactions.
type Manager struct {
	transactions map[int64]*Transaction
	nextID       int64
	mu           sync.RWMutex
}

// NewManager creates a new Transaction Manager.
func NewManager() *Manager {
	return &Manager{
		transactions: make(map[int64]*Transaction),
		nextID:       1,
	}
}

// NewTransaction creates a new transaction.
func (m *Manager) NewTransaction(initiatorNodeID int, cycle uint64) *Transaction {
	id := atomic.AddInt64(&m.nextID, 1) - 1

	txn := &Transaction{
		ID:              id,
		InitiatorNodeID: initiatorNodeID,
		State:           TransactionStatePending,
		CreatedCycle:    cycle,
		Messages:        []*message.Message{},
		Events:          []Event{},
	}

	// Add creation event
	txn.AddEvent(Event{
		Cycle:     cycle,
		NodeID:    initiatorNodeID,
		EventType: "Created",
		Details:   fmt.Sprintf("Transaction %d created", id),
	})

	m.mu.Lock()
	m.transactions[id] = txn
	m.mu.Unlock()

	return txn
}

// GetTransaction retrieves a transaction by ID.
func (m *Manager) GetTransaction(id int64) (*Transaction, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	txn, ok := m.transactions[id]
	return txn, ok
}

// AddMessageToTransaction adds a message to a transaction.
func (m *Manager) AddMessageToTransaction(txnID int64, msg *message.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	txn, ok := m.transactions[txnID]
	if !ok {
		return fmt.Errorf("transaction %d not found", txnID)
	}

	txn.AddMessage(msg)

	// Update state to InProgress if still Pending
	if txn.State == TransactionStatePending {
		txn.UpdateState(TransactionStateInProgress, msg.CreatedCycle)
	}

	return nil
}

// CompleteTransaction marks a transaction as completed.
func (m *Manager) CompleteTransaction(id int64, cycle uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	txn, ok := m.transactions[id]
	if !ok {
		return fmt.Errorf("transaction %d not found", id)
	}

	if txn.IsComplete() {
		return fmt.Errorf("transaction %d already completed", id)
	}

	txn.UpdateState(TransactionStateCompleted, cycle)
	return nil
}

// FailTransaction marks a transaction as failed.
func (m *Manager) FailTransaction(id int64, cycle uint64, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	txn, ok := m.transactions[id]
	if !ok {
		return fmt.Errorf("transaction %d not found", id)
	}

	if txn.IsComplete() {
		return fmt.Errorf("transaction %d already completed", id)
	}

	txn.UpdateState(TransactionStateFailed, cycle)
	txn.AddEvent(Event{
		Cycle:     cycle,
		NodeID:    txn.InitiatorNodeID,
		EventType: "Failed",
		Details:   reason,
	})

	return nil
}

// GetTransactionsByNode returns all transactions related to a node.
func (m *Manager) GetTransactionsByNode(nodeID int) []*Transaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := []*Transaction{}
	for _, txn := range m.transactions {
		if txn.InitiatorNodeID == nodeID {
			result = append(result, txn)
			continue
		}
		// Check if any message involves this node
		for _, msg := range txn.Messages {
			if msg.SourceNodeID == nodeID || msg.TargetNodeID == nodeID {
				result = append(result, txn)
				break
			}
		}
	}

	return result
}

// GetAllTransactions returns all transactions.
func (m *Manager) GetAllTransactions() []*Transaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Transaction, 0, len(m.transactions))
	for _, txn := range m.transactions {
		result = append(result, txn)
	}
	return result
}

