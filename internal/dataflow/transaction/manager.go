package transaction

import (
	"context"
	"sync"

	"github.com/Readm/flow_sim/internal/core/node"
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
	node          *node.Node
	activeTxns    map[dataflow.TransactionID]*activeTxn
	pendingByAddr map[Addr][]*activeTxn
	nextTxnID     int
	mu            sync.Mutex

	// migratedTxns tracks transactions that have migrated to this node
	migratedTxns map[dataflow.TransactionID]*migratedTxn
}

// migratedTxn represents a transaction that has migrated to this node.
type migratedTxn struct {
	txnID        dataflow.TransactionID
	yieldCh      chan *YieldCommand
	resumeCh     chan interface{}
	sourceNodeID int
	waiting      *WaitForMessage
}

// NewTxnManager creates a new TxnManager.
func NewTxnManager(n *node.Node) *TxnManager {
	return &TxnManager{
		node:          n,
		activeTxns:    make(map[dataflow.TransactionID]*activeTxn),
		pendingByAddr: make(map[Addr][]*activeTxn),
		migratedTxns:  make(map[dataflow.TransactionID]*migratedTxn),
		nextTxnID:     1,
	}
}

// Start starts a new transaction by running txnFunc in a goroutine.
func (tm *TxnManager) Start(ctx context.Context, txnFunc func(*TxnContext)) dataflow.TransactionID {
	nodeID := tm.node.ID()

	tm.mu.Lock()
	txnID := dataflow.TransactionID{
		NodeID: nodeID,
		TxnID:  tm.nextTxnID,
	}
	tm.nextTxnID++
	tm.mu.Unlock()

	// Create channels for yield/resume
	yieldCh := make(chan *YieldCommand, 10)  // Buffered to avoid blocking
	resumeCh := make(chan interface{}, 10)   // Buffered to avoid blocking
	done := make(chan struct{})

	// Create NodeAccessor for this node
	nodeAccessor := NewLocalNodeAccessor(tm.node)

	// Create TxnContext
	txnCtx := NewTxnContext(nodeID, txnID, yieldCh, resumeCh, ctx, nodeAccessor)

	// Create Transaction record
	txn := &Transaction{
		ID:              txnID,
		InitiatorNodeID: nodeID,
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

	// Step 1: Process incoming messages
	for _, msg := range incoming {
		if msg.Type == MsgTypeMigrationRequest {
			// Handle migration request
			tm.handleMigrationRequest(msg, &outgoing)
		} else {
			// Route to waiting transactions
			tm.routeMessage(msg)
		}
	}

	// Step 2: Process yield commands from local transactions (non-blocking)
	tm.mu.Lock()
	activeList := make([]*activeTxn, 0, len(tm.activeTxns))
	for _, active := range tm.activeTxns {
		activeList = append(activeList, active)
	}
	tm.mu.Unlock()

	for _, active := range activeList {
		tm.processYields(active, &outgoing)
	}

	// Step 3: Process yield commands from migrated transactions (non-blocking)
	tm.processMigratedYields(&outgoing)

	return outgoing, nil
}

// routeMessage routes an incoming message to waiting transactions.
func (tm *TxnManager) routeMessage(msg *message.Message) {
	// First, collect matching local transactions
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

	// Process matches for local transactions
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

	// Also check migrated transactions
	var matchingMigrated []*migratedTxn
	tm.mu.Lock()
	for _, mtxn := range tm.migratedTxns {
		if mtxn.waiting != nil && tm.matchesWait(msg, mtxn.waiting) {
			matchingMigrated = append(matchingMigrated, mtxn)
		}
	}
	tm.mu.Unlock()

	// Process matches for migrated transactions
	for _, mtxn := range matchingMigrated {
		select {
		case mtxn.resumeCh <- msg:
			// Clear waiting state
			mtxn.waiting = nil
		default:
			// Channel full, skip
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
	case YieldTypeMigrateTo:
		// Transaction wants to migrate to another node
		// Build migration request message
		migMsg := &message.Message{
			TransactionID: active.txnID,
			Type:          MsgTypeMigrationRequest,
			SourceNodeID:  tm.node.ID(),
			TargetNodeID:  cmd.MigrateToNodeID,
			Payload: &MigrationPayload{
				YieldCh:  active.context.yieldCh,
				ResumeCh: active.context.resumeCh,
			},
		}

		*outgoing = append(*outgoing, migMsg)

		// Remove from local active transactions (it's migrating out)
		// The transaction will be resumed on the target node
		tm.mu.Lock()
		delete(tm.activeTxns, active.txnID)
		tm.mu.Unlock()

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
		nodeID := tm.node.ID()
		for _, op := range cmd.Operations {
			if err := op.Execute(nodeID); err != nil {
				// Log error but continue
			}
		}

	case YieldTypeSendOnly:
		// Transaction is only sending messages, not waiting for a response
		// Do not set waiting state or register in pendingByAddr
		// Just collect messages to send
		*outgoing = append(*outgoing, cmd.SendQueue...)

		// Execute operations (e.g., cache updates)
		nodeID := tm.node.ID()
		for _, op := range cmd.Operations {
			if err := op.Execute(nodeID); err != nil {
				// Log error but continue
			}
		}

		// NOTE: Do NOT send resume for SendOnly!
		// SendOnly is used by non-blocking Send(), which doesn't wait for resume.

	case YieldTypeSendAndWait:
		// Send messages and resume the transaction
		*outgoing = append(*outgoing, cmd.SendQueue...)

		// Execute operations
		nodeID := tm.node.ID()
		for _, op := range cmd.Operations {
			if err := op.Execute(nodeID); err != nil {
				// Log error but continue
			}
		}

		// Resume transaction
		select {
		case active.context.resumeCh <- nil:
		default:
		}

	case YieldTypeComplete:
		// Transaction is complete
		active.mu.Lock()
		active.txn.State = TransactionStateCompleted
		active.mu.Unlock()

		// Collect any remaining messages to send
		*outgoing = append(*outgoing, cmd.SendQueue...)

		// Execute operations
		nodeID := tm.node.ID()
		for _, op := range cmd.Operations {
			if err := op.Execute(nodeID); err != nil {
				// Log error but continue
			}
		}

	default:
		// Unknown yield type, just collect messages and operations
		*outgoing = append(*outgoing, cmd.SendQueue...)
		nodeID := tm.node.ID()
		for _, op := range cmd.Operations {
			if err := op.Execute(nodeID); err != nil {
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

// ===========================================================================
// Migration Support
// ===========================================================================

// handleMigrationRequest handles a migration request from another node.
func (tm *TxnManager) handleMigrationRequest(msg *message.Message, outgoing *[]*message.Message) {
	payload, ok := msg.Payload.(*MigrationPayload)
	if !ok {
		// Invalid payload, ignore
		return
	}

	// Parse transaction ID
	var txnID dataflow.TransactionID
	// Simple parsing: assume format "NodeID:TxnID"
	// For now, we'll reconstruct from message's TransactionID field
	txnID = msg.TransactionID

	// Register migrated transaction
	tm.mu.Lock()
	tm.migratedTxns[txnID] = &migratedTxn{
		txnID:        txnID,
		yieldCh:      payload.YieldCh,
		resumeCh:     payload.ResumeCh,
		sourceNodeID: msg.SourceNodeID,
	}
	tm.mu.Unlock()

	// Build resume value with NodeAccessor for this node
	resumeVal := &MigrationResult{
		NodeAccessor: NewLocalNodeAccessor(tm.node),
		Message:      msg,
	}

	// Resume the transaction (non-blocking send)
	select {
	case payload.ResumeCh <- resumeVal:
		// Transaction resumed successfully
	default:
		// Resume channel full, log error (should not happen with buffered channel)
	}
}

// processMigratedYields processes yield commands from migrated transactions.
func (tm *TxnManager) processMigratedYields(outgoing *[]*message.Message) {
	tm.mu.Lock()
	migratedList := make([]*migratedTxn, 0, len(tm.migratedTxns))
	for _, mtxn := range tm.migratedTxns {
		migratedList = append(migratedList, mtxn)
	}
	tm.mu.Unlock()

	for _, mtxn := range migratedList {
		select {
		case yieldCmd := <-mtxn.yieldCh:
			tm.handleMigratedYield(mtxn, yieldCmd, outgoing)
		default:
			// No yield command available
		}
	}
}

// handleMigratedYield handles a yield command from a migrated transaction.
func (tm *TxnManager) handleMigratedYield(mtxn *migratedTxn, cmd *YieldCommand, outgoing *[]*message.Message) {
	switch cmd.Type {
	case YieldTypeMigrateTo:
		// Transaction wants to migrate to another node
		tm.handleMigrationOut(mtxn, cmd, outgoing)

	case YieldTypeWaitForMessage:
		// Transaction is waiting for a message on this node
		mtxn.waiting = cmd.WaitFor
		*outgoing = append(*outgoing, cmd.SendQueue...)

	case YieldTypeComplete:
		// Transaction complete, remove from migrated map
		tm.mu.Lock()
		delete(tm.migratedTxns, mtxn.txnID)
		tm.mu.Unlock()

		*outgoing = append(*outgoing, cmd.SendQueue...)

	case YieldTypeSendOnly:
		// Just send messages
		*outgoing = append(*outgoing, cmd.SendQueue...)

		// NOTE: Do NOT send resume for SendOnly (same reason as above)

	case YieldTypeSendAndWait:
		// Send messages and resume
		*outgoing = append(*outgoing, cmd.SendQueue...)

		// Resume transaction
		select {
		case mtxn.resumeCh <- nil:
		default:
		}

	default:
		// Unknown type, just collect messages
		*outgoing = append(*outgoing, cmd.SendQueue...)
	}
}

// handleMigrationOut handles a transaction migrating out to another node.
func (tm *TxnManager) handleMigrationOut(mtxn *migratedTxn, cmd *YieldCommand, outgoing *[]*message.Message) {
	// Build migration request message
	migMsg := &message.Message{
		TransactionID: mtxn.txnID,
		Type:          MsgTypeMigrationRequest,
		SourceNodeID:  tm.node.ID(),
		TargetNodeID:  cmd.MigrateToNodeID,
		Payload: &MigrationPayload{
			YieldCh:  mtxn.yieldCh,
			ResumeCh: mtxn.resumeCh,
		},
	}

	*outgoing = append(*outgoing, migMsg)

	// Remove from this node's migrated map (it's migrating out)
	tm.mu.Lock()
	delete(tm.migratedTxns, mtxn.txnID)
	tm.mu.Unlock()
}

