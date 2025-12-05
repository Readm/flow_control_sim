package transaction

import (
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/message"
)

// YieldType defines the type of yield command.
type YieldType string

const (
	YieldTypeWaitForMessage YieldType = "WaitForMessage"
	YieldTypeWaitForTimeout YieldType = "WaitForTimeout"
	YieldTypeSendOnly       YieldType = "SendOnly"       // Send messages without waiting (non-blocking)
	YieldTypeSendAndWait    YieldType = "SendAndWait"    // Send messages and wait for processing (blocking)
	YieldTypeComplete       YieldType = "Complete"

	// YieldTypeMigrateTo requests migration to another node.
	// This enables continuous-style transactions that span multiple nodes.
	YieldTypeMigrateTo      YieldType = "MigrateTo"
)

// WaitForMessage describes what message a transaction is waiting for.
type WaitForMessage struct {
	Type      int    // Message type to wait for
	Addr      string // Optional: address filter (empty means any message of this type)
	SourceID  *int   // Optional: source node ID filter (nil means any source)
	TargetID  *int   // Optional: target node ID filter (nil means any target)
}

// YieldCommand represents a command sent from Transaction to TxnManager.
type YieldCommand struct {
	Type      YieldType
	WaitFor   *WaitForMessage
	Timeout   time.Duration
	SendQueue []*message.Message
	// Operations to be executed in Node.Tick (e.g., cache updates)
	Operations []Operation

	// MigrateToNodeID specifies the target node for migration (used with YieldTypeMigrateTo).
	MigrateToNodeID int
}

// MigrationResult is returned when a Transaction is resumed after migration.
// It contains the NodeAccessor for the new node and any triggering message.
type MigrationResult struct {
	NodeAccessor NodeAccessor
	Message      *message.Message
}

// Operation represents an operation to be executed in Node.Tick.
type Operation interface {
	Execute(nodeID int) error
}

// CacheUpdateOperation represents a cache line update operation.
type CacheUpdateOperation struct {
	Addr      string
	NewState  string
	Data      []byte
}

// Execute implements Operation interface.
func (op *CacheUpdateOperation) Execute(nodeID int) error {
	// This will be implemented by the Node that uses TxnManager
	// For now, it's a placeholder
	return nil
}

// Addr represents a memory address (simplified as string for now).
type Addr string

// Migration message types
const (
	// MsgTypeMigrationRequest is a special message type for transaction migration.
	// This is a framework-level message type, not a protocol-specific opcode.
	MsgTypeMigrationRequest = -1
)

// MigrationPayload is the payload for migration request messages.
type MigrationPayload struct {
	TxnID    string // TransactionID string representation
	YieldCh  chan *YieldCommand
	ResumeCh chan interface{}
}

