package transaction

import (
	"fmt"

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
	ID             int64                // Unique identifier
	InitiatorNodeID int                  // Initiator node
	State          TransactionState      // Current state
	CreatedCycle   uint64                // Creation time (cycle)
	CompletedCycle uint64                // Completion time (cycle, 0 means not completed)
	Messages       []*message.Message    // Associated messages
	Events         []Event               // Tracking events
}

// AddMessage adds a message to the transaction.
func (t *Transaction) AddMessage(msg *message.Message) {
	if msg == nil {
		return
	}
	t.Messages = append(t.Messages, msg)
}

// AddEvent adds a tracking event to the transaction.
func (t *Transaction) AddEvent(event Event) {
	t.Events = append(t.Events, event)
}

// UpdateState updates the transaction state.
func (t *Transaction) UpdateState(state TransactionState, cycle uint64) {
	oldState := t.State
	t.State = state

	if state == TransactionStateCompleted || state == TransactionStateFailed {
		t.CompletedCycle = cycle
	}

	// Add state change event
	t.AddEvent(Event{
		Cycle:     cycle,
		NodeID:    t.InitiatorNodeID,
		EventType: "StateChanged",
		Details:   fmt.Sprintf("State changed from %s to %s", oldState, state),
	})
}

// GetMessagesByType returns messages of the specified type.
func (t *Transaction) GetMessagesByType(msgType message.MessageType) []*message.Message {
	result := []*message.Message{}
	for _, msg := range t.Messages {
		if msg.Type == msgType {
			result = append(result, msg)
		}
	}
	return result
}

// IsComplete checks if the transaction is complete.
func (t *Transaction) IsComplete() bool {
	return t.State == TransactionStateCompleted || t.State == TransactionStateFailed
}

// GetMessageByID returns the message with the specified ID.
func (t *Transaction) GetMessageByID(msgID int64) *message.Message {
	for _, msg := range t.Messages {
		if msg.ID == msgID {
			return msg
		}
	}
	return nil
}

