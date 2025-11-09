package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// PacketHistoryConfig holds configuration for packet history tracking
type PacketHistoryConfig struct {
	EnablePacketHistory   bool
	MaxPacketHistorySize  int
	HistoryOverflowMode   string // "circular" or "initial"
	MaxTransactionHistory int
}

// TransactionManager is the core component responsible for:
// - Creating and managing Transaction objects
// - Recording dependencies between transactions
// - Maintaining Transaction Graph
// - Providing query interfaces
// - Tracking packet history for timeline visualization
type TransactionManager struct {
	mu           sync.RWMutex
	transactions map[int64]*Transaction
	dependencies map[int64][]*TransactionDependency // fromID -> dependencies
	nextTxnID    int64

	// Packet history tracking
	packetHistory    map[int64][]*PacketEvent // transaction ID -> events
	allPacketHistory []*PacketEvent           // all events (for full history mode)
	historyConfig    *PacketHistoryConfig
	nodeLabels       map[int]string // node ID -> label (set by simulator)

	eventCh  chan *PacketEvent
	eventSeq int64
}

// NewTransactionManager creates a new TransactionManager
func NewTransactionManager() *TransactionManager {
	tm := &TransactionManager{
		transactions:     make(map[int64]*Transaction),
		dependencies:     make(map[int64][]*TransactionDependency),
		nextTxnID:        1,
		packetHistory:    make(map[int64][]*PacketEvent),
		allPacketHistory: make([]*PacketEvent, 0),
		historyConfig: &PacketHistoryConfig{
			EnablePacketHistory:   true,
			MaxPacketHistorySize:  0, // unlimited
			HistoryOverflowMode:   "circular",
			MaxTransactionHistory: 1000,
		},
		nodeLabels: make(map[int]string),
		eventCh:    make(chan *PacketEvent, 1024),
	}
	go tm.runEventLoop()
	return tm
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
		TransactionID:         txnID,
		TransactionType:       transactionType,
		Address:               address,
		State:                 TxStateInitiated,
		StateHistory:          make([]StateTransition, 0),
		InitiatedAt:           initiatedAt,
		CompletedAt:           0,
		SameAddrOrdering:      make([]int64, 0),
		GlobalOrdering:        make([]int64, 0),
		GeneratedTransactions: make([]int64, 0),
		Metadata:              make(map[string]string),
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
		Cycle:        cycle,
		FromState:    oldState,
		ToState:      newState,
		Reason:       reason,
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
	tm.packetHistory = make(map[int64][]*PacketEvent)
	tm.allPacketHistory = make([]*PacketEvent, 0)
}

// SetHistoryConfig sets the packet history tracking configuration
func (tm *TransactionManager) SetHistoryConfig(cfg *PacketHistoryConfig) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if cfg != nil {
		tm.historyConfig = cfg
	}
}

// SetNodeLabels sets the node ID to label mapping
func (tm *TransactionManager) SetNodeLabels(labels map[int]string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.nodeLabels = labels
}

func (tm *TransactionManager) runEventLoop() {
	for event := range tm.eventCh {
		tm.handlePacketEvent(event)
	}
}

// RecordPacketEvent records a packet event for timeline visualization
func (tm *TransactionManager) RecordPacketEvent(event *PacketEvent) {
	if event == nil {
		return
	}

	seq := atomic.AddInt64(&tm.eventSeq, 1)
	event.Sequence = seq

	select {
	case tm.eventCh <- event:
	default:
		GetLogger().Warnf("transaction manager event channel full; processing event %d inline", seq)
		tm.handlePacketEvent(event)
	}
}

func (tm *TransactionManager) handlePacketEvent(event *PacketEvent) {
	if event == nil {
		return
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Check if history tracking is enabled
	if !tm.historyConfig.EnablePacketHistory {
		return
	}

	// Set node label if not set
	if event.NodeLabel == "" && tm.nodeLabels != nil {
		if label, ok := tm.nodeLabels[event.NodeID]; ok {
			event.NodeLabel = label
		}
	}

	// Add to transaction-specific history
	if event.TransactionID > 0 {
		tm.packetHistory[event.TransactionID] = append(tm.packetHistory[event.TransactionID], event)
	}

	// Add to all history (for full history mode)
	tm.allPacketHistory = append(tm.allPacketHistory, event)

	// Check if we need to cleanup
	if tm.historyConfig.MaxPacketHistorySize > 0 && len(tm.allPacketHistory) > tm.historyConfig.MaxPacketHistorySize {
		tm.cleanupHistoryLocked()
	}
}

// cleanupHistoryLocked performs history cleanup (must be called with lock held)
func (tm *TransactionManager) cleanupHistoryLocked() {
	if tm.historyConfig.HistoryOverflowMode == "initial" {
		// Keep only the first N transactions
		if len(tm.packetHistory) > tm.historyConfig.MaxTransactionHistory {
			// Find oldest transactions and remove them
			// This is a simplified version - in practice, we'd track transaction creation order
			count := 0
			for txnID := range tm.packetHistory {
				if count >= len(tm.packetHistory)-tm.historyConfig.MaxTransactionHistory {
					break
				}
				delete(tm.packetHistory, txnID)
				count++
			}
			// Rebuild allPacketHistory
			tm.allPacketHistory = make([]*PacketEvent, 0)
			for _, events := range tm.packetHistory {
				tm.allPacketHistory = append(tm.allPacketHistory, events...)
			}
		}
	} else {
		// Circular mode: remove oldest events
		excess := len(tm.allPacketHistory) - tm.historyConfig.MaxPacketHistorySize
		if excess > 0 {
			// Remove oldest events
			tm.allPacketHistory = tm.allPacketHistory[excess:]
			// Rebuild packetHistory map
			tm.packetHistory = make(map[int64][]*PacketEvent)
			for _, event := range tm.allPacketHistory {
				if event.TransactionID > 0 {
					tm.packetHistory[event.TransactionID] = append(tm.packetHistory[event.TransactionID], event)
				}
			}
		}
	}
}

// CleanupHistory performs history cleanup based on configuration
func (tm *TransactionManager) CleanupHistory() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.cleanupHistoryLocked()
}

// TransactionSummary represents a summary of a transaction for listing
type TransactionSummary struct {
	ID          int64              `json:"id"`
	Type        CHITransactionType `json:"type"`
	Address     uint64             `json:"address"`
	InitiatedAt int                `json:"initiatedAt"`
	CompletedAt int                `json:"completedAt"`
	State       TransactionState   `json:"state"`
}

// GetAllTransactionSummaries returns summaries of all transactions
func (tm *TransactionManager) GetAllTransactionSummaries() []*TransactionSummary {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	summaries := make([]*TransactionSummary, 0, len(tm.transactions))
	for _, txn := range tm.transactions {
		if txn != nil && txn.Context != nil {
			summaries = append(summaries, &TransactionSummary{
				ID:          txn.Context.TransactionID,
				Type:        txn.Context.TransactionType,
				Address:     txn.Context.Address,
				InitiatedAt: txn.Context.InitiatedAt,
				CompletedAt: txn.Context.CompletedAt,
				State:       txn.Context.State,
			})
		}
	}
	return summaries
}

// NodeInfo represents node information for timeline
type NodeInfo struct {
	ID    int      `json:"id"`
	Label string   `json:"label"`
	Type  NodeType `json:"type"`
}

// TimeRange represents a time range
type TimeRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// TransactionTimeline represents the timeline data for a transaction
type TransactionTimeline struct {
	TransactionID int64          `json:"transactionID"`
	Events        []*PacketEvent `json:"events"`
	Nodes         []NodeInfo     `json:"nodes"`
	TimeRange     TimeRange      `json:"timeRange"`
	Packets       []PacketInfo   `json:"packets"`
}

// GetTransactionTimeline retrieves the timeline for a specific transaction
func (tm *TransactionManager) GetTransactionTimeline(txnID int64) *TransactionTimeline {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	txn := tm.transactions[txnID]
	if txn == nil {
		return nil
	}

	events := tm.packetHistory[txnID]
	if events == nil {
		events = make([]*PacketEvent, 0)
	}

	// Collect unique nodes
	nodeMap := make(map[int]NodeInfo)
	minCycle := int(^uint(0) >> 1) // max int
	maxCycle := 0

	for _, event := range events {
		if _, exists := nodeMap[event.NodeID]; !exists {
			nodeType := NodeTypeRN // default
			label := event.NodeLabel
			if label == "" {
				if tm.nodeLabels != nil {
					label = tm.nodeLabels[event.NodeID]
				}
				if label == "" {
					label = fmt.Sprintf("Node %d", event.NodeID)
				}
			}
			// Try to infer node type from label
			if len(label) >= 2 {
				prefix := label[:2]
				switch prefix {
				case "RN":
					nodeType = NodeTypeRN
				case "HN":
					nodeType = NodeTypeHN
				case "SN":
					nodeType = NodeTypeSN
				}
			}
			nodeMap[event.NodeID] = NodeInfo{
				ID:    event.NodeID,
				Label: label,
				Type:  nodeType,
			}
		}
		if event.Cycle < minCycle {
			minCycle = event.Cycle
		}
		if event.Cycle > maxCycle {
			maxCycle = event.Cycle
		}
	}

	// Convert node map to slice and sort
	nodes := make([]NodeInfo, 0, len(nodeMap))
	for _, node := range nodeMap {
		nodes = append(nodes, node)
	}

	// Sort nodes by type (RN -> HN -> SN) and then by ID
	for i := 0; i < len(nodes)-1; i++ {
		for j := i + 1; j < len(nodes); j++ {
			typeOrder := map[NodeType]int{
				NodeTypeRN: 0,
				NodeTypeHN: 1,
				NodeTypeSN: 2,
			}
			if typeOrder[nodes[i].Type] > typeOrder[nodes[j].Type] ||
				(typeOrder[nodes[i].Type] == typeOrder[nodes[j].Type] && nodes[i].ID > nodes[j].ID) {
				nodes[i], nodes[j] = nodes[j], nodes[i]
			}
		}
	}

	// Collect unique packet IDs
	packetMap := make(map[int64]bool)
	for _, event := range events {
		if event.PacketID > 0 {
			packetMap[event.PacketID] = true
		}
	}

	// Create packet info list (simplified - in practice, we'd need to query packet details)
	packets := make([]PacketInfo, 0, len(packetMap))
	for packetID := range packetMap {
		packets = append(packets, PacketInfo{
			ID: packetID,
		})
	}

	if minCycle > maxCycle {
		minCycle = 0
		maxCycle = 0
	}

	return &TransactionTimeline{
		TransactionID: txnID,
		Events:        events,
		Nodes:         nodes,
		TimeRange: TimeRange{
			Start: minCycle,
			End:   maxCycle,
		},
		Packets: packets,
	}
}
