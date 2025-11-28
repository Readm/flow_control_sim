package transaction

import (
	"time"

	capcache "github.com/Readm/flow_sim/internal/core/capability/cache"
	capdir "github.com/Readm/flow_sim/internal/core/capability/directory"
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
	Type     int  // Message type to wait for
	Addr     Addr // Optional: address filter (0 means any address)
	SourceID *int // Optional: source node ID filter (nil means any source)
	TargetID *int // Optional: target node ID filter (nil means any target)
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

// CapabilityProvider exposes read-only access to cache and directory.
type CapabilityProvider interface {
	Cache() capcache.Cache
	Directory() capdir.Directory
}

// CapabilityExecutor exposes read/write access for applying operations.
type CapabilityExecutor interface {
	CapabilityProvider
}

// Operation represents an operation to be executed in Node.Tick.
type Operation interface {
	Apply(exec CapabilityExecutor) error
}

// CacheOperation represents a cache operation (state/data update or invalidate).
type CacheOperation struct {
	Addr       Addr
	NewState   capcache.State
	Data       []byte
	Invalidate bool
}

// Apply implements Operation interface.
func (op *CacheOperation) Apply(exec CapabilityExecutor) error {
	cache := exec.Cache()
	if cache == nil {
		return nil
	}

	addr := uint64(op.Addr)

	if op.Invalidate {
		cache.Invalidate(addr)
		return nil
	}

	if op.NewState != "" {
		cache.SetState(addr, op.NewState)
	}
	if op.Data != nil {
		cache.SetData(addr, op.Data)
	}
	return nil
}

// DirectoryOpType represents directory operation type.
type DirectoryOpType int

const (
	DirectoryOpSetState DirectoryOpType = iota
	DirectoryOpAddSharer
	DirectoryOpRemoveSharer
	DirectoryOpClearSharers
	DirectoryOpSetOwner
)

// DirectoryOperation represents updates to the directory.
type DirectoryOperation struct {
	Addr   Addr
	Type   DirectoryOpType
	State  capdir.State
	Sharer int
	Owner  int
}

// Apply implements Operation interface.
func (op *DirectoryOperation) Apply(exec CapabilityExecutor) error {
	dir := exec.Directory()
	if dir == nil {
		return nil
	}

	addr := uint64(op.Addr)
	switch op.Type {
	case DirectoryOpSetState:
		dir.SetState(addr, op.State)
	case DirectoryOpAddSharer:
		dir.AddSharer(addr, op.Sharer)
	case DirectoryOpRemoveSharer:
		dir.RemoveSharer(addr, op.Sharer)
	case DirectoryOpClearSharers:
		dir.ClearSharers(addr)
	case DirectoryOpSetOwner:
		dir.SetOwner(addr, op.Owner)
	}
	return nil
}

// Addr represents a memory address (simplified as string for now).
type Addr uint64

// NodeCtx provides safe access to Node state from Transaction.
// All methods must be called from Transaction goroutine and will communicate
// with TxnManager via YieldCommand.
type NodeCtx interface {
	CapabilityProvider
}
