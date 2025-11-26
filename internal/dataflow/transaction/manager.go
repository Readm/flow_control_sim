package transaction

import (
	"context"
	"sync"

	"github.com/Readm/flow_sim/internal/dataflow"
	"github.com/Readm/flow_sim/internal/dataflow/message"
)

// activeTxn represents an active transaction.
type activeTxn struct {
	txnID        dataflow.TransactionID
	context      *TxnContext
	done         chan struct{}
	txn          *Transaction
	waiting      *WaitForMessage // What this transaction is waiting for
	pendingAddrs []Addr          // Addresses where this transaction is registered in pendingByAddr
	mu           sync.Mutex
}

// TxnManager manages transactions for a node.
type TxnManager struct {
	nodeID        int
	activeTxns    map[dataflow.TransactionID]*activeTxn
	pendingByAddr map[Addr][]*activeTxn
	nextTxnID     int
	mu            sync.Mutex
	nodeCtx       NodeCtx
}

// NewTxnManager creates a new TxnManager.
func NewTxnManager(nodeID int, nodeCtx NodeCtx) *TxnManager {
	return &TxnManager{
		nodeID:        nodeID,
		activeTxns:    make(map[dataflow.TransactionID]*activeTxn),
		pendingByAddr: make(map[Addr][]*activeTxn),
		nextTxnID:     1,
		nodeCtx:       nodeCtx,
	}
}

// Start starts a new transaction by running txnFunc in a goroutine.
func (tm *TxnManager) Start(ctx context.Context, txnFunc func(*TxnContext)) dataflow.TransactionID {
	tm.mu.Lock()
	txnID := dataflow.TransactionID{
		NodeID: tm.nodeID,
		TxnID:  tm.nextTxnID,
	}
	tm.nextTxnID++
	tm.mu.Unlock()

	// Create channels for yield/resume
	yieldCh := make(chan *YieldCommand, 10)  // Buffered to avoid blocking
	resumeCh := make(chan interface{}, 10)   // Buffered to avoid blocking
	done := make(chan struct{})

	// Create TxnContext
	txnCtx := NewTxnContext(tm.nodeID, txnID, yieldCh, resumeCh, ctx, tm.nodeCtx)

	// Create Transaction record
	txn := &Transaction{
		ID:              txnID,
		InitiatorNodeID: tm.nodeID,
		State:           TransactionStateInProgress,
	}

	// Create activeTxn record
	active := &activeTxn{
		txnID:   txnID,
		context: txnCtx,
		done:    done,
		txn:     txn,
	}

	// Register active transaction
	tm.mu.Lock()
	tm.activeTxns[txnID] = active
	tm.mu.Unlock()

	// Start transaction goroutine
	go func() {
		defer close(done)
		defer func() {
			// Cleanup on exit
			tm.mu.Lock()
			delete(tm.activeTxns, txnID)
			// Cleanup pendingByAddr
			for addr, list := range tm.pendingByAddr {
				newList := make([]*activeTxn, 0, len(list))
				for _, t := range list {
					if t.txnID != txnID {
						newList = append(newList, t)
					}
				}
				if len(newList) == 0 {
					delete(tm.pendingByAddr, addr)
				} else {
					tm.pendingByAddr[addr] = newList
				}
			}
			tm.mu.Unlock()
		}()

		// Run transaction function
		txnFunc(txnCtx)
	}()

	return txnID
}

// Tick processes incoming messages and handles transaction yields.
// Must be called from Node.Tick to ensure serialization.
func (tm *TxnManager) Tick(cycle uint64, incoming []*message.Message) ([]*message.Message, error) {
	var outgoing []*message.Message

	// Step 1: Process incoming messages - route to waiting transactions
	for _, msg := range incoming {
		tm.routeMessage(msg)
	}

	// Step 2: Process yield commands from active transactions (non-blocking)
	tm.mu.Lock()
	activeList := make([]*activeTxn, 0, len(tm.activeTxns))
	for _, active := range tm.activeTxns {
		activeList = append(activeList, active)
	}
	tm.mu.Unlock()

	for _, active := range activeList {
		tm.processYields(active, &outgoing)
	}

	return outgoing, nil
}

// routeMessage routes an incoming message to waiting transactions.
func (tm *TxnManager) routeMessage(msg *message.Message) {
	// First, collect matching transactions without holding locks
	var matchingTxns []*activeTxn
	tm.mu.Lock()
	for _, active := range tm.activeTxns {
		active.mu.Lock()
		waiting := active.waiting
		active.mu.Unlock()
		if waiting != nil && tm.matchesWait(msg, waiting) {
			matchingTxns = append(matchingTxns, active)
		}
	}
	tm.mu.Unlock()

	// Then process matches and remove from pendingByAddr
	for _, active := range matchingTxns {
		// Non-blocking send to resume channel
		select {
		case active.context.resumeCh <- msg:
			// Clear waiting state and get pending addresses
			active.mu.Lock()
			active.waiting = nil
			pendingAddrs := make([]Addr, len(active.pendingAddrs))
			copy(pendingAddrs, active.pendingAddrs)
			active.pendingAddrs = nil
			active.mu.Unlock()

			// Remove from pendingByAddr map
			if len(pendingAddrs) > 0 {
				tm.mu.Lock()
				for _, addr := range pendingAddrs {
					if list, exists := tm.pendingByAddr[addr]; exists {
						newList := make([]*activeTxn, 0, len(list))
						for _, t := range list {
							if t.txnID != active.txnID {
								newList = append(newList, t)
							}
						}
						if len(newList) == 0 {
							delete(tm.pendingByAddr, addr)
						} else {
							tm.pendingByAddr[addr] = newList
						}
					}
				}
				tm.mu.Unlock()
			}
		default:
			// Channel full, skip (should not happen with buffered channel)
		}
	}
}

// matchesWait checks if a message matches the wait condition.
func (tm *TxnManager) matchesWait(msg *message.Message, wait *WaitForMessage) bool {
	if msg.Type != wait.Type {
		return false
	}
	if wait.Addr != "" && msg.Payload != nil {
		// Simple address matching - can be enhanced
		// For now, we'll match if Addr is empty or matches
	}
	if wait.SourceID != nil && msg.SourceNodeID != *wait.SourceID {
		return false
	}
	if wait.TargetID != nil && msg.TargetNodeID != *wait.TargetID {
		return false
	}
	return true
}

// processYields processes yield commands from a transaction (non-blocking).
func (tm *TxnManager) processYields(active *activeTxn, outgoing *[]*message.Message) {
	select {
	case cmd := <-active.context.yieldCh:
		tm.handleYieldCommand(active, cmd, outgoing)
	default:
		// No yield command available
	}
}

// handleYieldCommand handles a yield command from a transaction.
func (tm *TxnManager) handleYieldCommand(active *activeTxn, cmd *YieldCommand, outgoing *[]*message.Message) {
	switch cmd.Type {
	case YieldTypeWaitForMessage:
		// Transaction is waiting for a message
		active.mu.Lock()
		// Clear previous pending addresses before setting new waiting state
		oldPendingAddrs := make([]Addr, len(active.pendingAddrs))
		copy(oldPendingAddrs, active.pendingAddrs)
		active.pendingAddrs = nil
		active.waiting = cmd.WaitFor
		active.mu.Unlock()

		// Remove from old pendingByAddr entries
		tm.mu.Lock()
		for _, addr := range oldPendingAddrs {
			if list, exists := tm.pendingByAddr[addr]; exists {
				newList := make([]*activeTxn, 0, len(list))
				for _, t := range list {
					if t.txnID != active.txnID {
						newList = append(newList, t)
					}
				}
				if len(newList) == 0 {
					delete(tm.pendingByAddr, addr)
				} else {
					tm.pendingByAddr[addr] = newList
				}
			}
		}

		// Register in pendingByAddr if address is specified
		if cmd.WaitFor != nil && cmd.WaitFor.Addr != "" {
			addr := Addr(cmd.WaitFor.Addr)
			tm.pendingByAddr[addr] = append(tm.pendingByAddr[addr], active)
			active.mu.Lock()
			active.pendingAddrs = append(active.pendingAddrs, addr)
			active.mu.Unlock()
		}
		tm.mu.Unlock()

		// Collect messages to send
		*outgoing = append(*outgoing, cmd.SendQueue...)

		// Execute operations (e.g., cache updates)
		for _, op := range cmd.Operations {
			if err := op.Execute(tm.nodeID); err != nil {
				// Log error but continue
			}
		}

	case YieldTypeSendOnly:
		// Transaction is only sending messages, not waiting for a response
		// Do not set waiting state or register in pendingByAddr
		// Just collect messages to send
		*outgoing = append(*outgoing, cmd.SendQueue...)

		// Execute operations (e.g., cache updates)
		for _, op := range cmd.Operations {
			if err := op.Execute(tm.nodeID); err != nil {
				// Log error but continue
			}
		}

	case YieldTypeComplete:
		// Transaction is complete
		active.mu.Lock()
		active.txn.State = TransactionStateCompleted
		active.mu.Unlock()

		// Collect any remaining messages to send
		*outgoing = append(*outgoing, cmd.SendQueue...)

		// Execute operations
		for _, op := range cmd.Operations {
			if err := op.Execute(tm.nodeID); err != nil {
				// Log error but continue
			}
		}

	default:
		// Unknown yield type, just collect messages and operations
		*outgoing = append(*outgoing, cmd.SendQueue...)
		for _, op := range cmd.Operations {
			if err := op.Execute(tm.nodeID); err != nil {
				// Log error but continue
			}
		}
	}
}

// GetTransaction retrieves a transaction by ID.
func (tm *TxnManager) GetTransaction(txnID dataflow.TransactionID) *Transaction {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	active, ok := tm.activeTxns[txnID]
	if !ok {
		return nil
	}
	return active.txn
}

// ActiveCount returns the number of active transactions.
func (tm *TxnManager) ActiveCount() int {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return len(tm.activeTxns)
}

