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
	YieldTypeSendOnly       YieldType = "SendOnly" // Send messages without waiting
	YieldTypeComplete       YieldType = "Complete"
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

// NodeCtx provides safe access to Node state from Transaction.
// All methods must be called from Transaction goroutine and will communicate
// with TxnManager via YieldCommand.
type NodeCtx interface {
	GetCacheState(addr Addr) string
	ReadCache(addr Addr) []byte
	UpdateCache(addr Addr, state string, data []byte)
}

