package transaction

import (
	"github.com/Readm/flow_sim/internal/dataflow/message"
)

// TransactionState defines the state of a transaction.
type TransactionState string

const (
	TransactionStatePending    TransactionState = "Pending"
	TransactionStateInProgress TransactionState = "InProgress"
	TransactionStateCompleted  TransactionState = "Completed"
	TransactionStateFailed     TransactionState = "Failed"
)

// Event records an event in the transaction lifecycle.
type Event struct {
	Cycle     uint64 // Occurrence time (cycle)
	NodeID    int    // Occurrence location (node)
	EventType string // Event type (Created, MessageSent, MessageReceived, Processed, Completed)
	MessageID int64  // Associated Message ID (if any)
	Details   string // Detailed information
}

// Transaction represents a complete transaction.
type Transaction struct {
	ID             int64             // Unique identifier
	InitiatorNodeID int               // Initiator node
	State          TransactionState   // Current state
	CreatedCycle   uint64             // Creation time (cycle)
	CompletedCycle uint64             // Completion time (cycle, 0 means not completed)
	Messages       []*message.Message // Associated messages
	Events         []Event            // Tracking events
}

