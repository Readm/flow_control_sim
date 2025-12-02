package chi

import (
	"context"
	"time"
)

// ============================================================================
// CHI Protocol Interfaces - Complete Decoupling from Framework
// ============================================================================
//
// Design Principle:
// - CHI Transactions ONLY depend on interfaces defined in this file
// - NO direct imports from transaction/cache/directory/message packages
// - Framework provides adapters to implement these interfaces
//
// ============================================================================

// ============================================================================
// Section 1: Message Abstraction
// ============================================================================

// MessageID uniquely identifies a message.
type MessageID struct {
	NodeID    int
	MessageID int
}

// TransactionID uniquely identifies a transaction.
type TransactionID struct {
	NodeID int
	TxnID  int
}

// Message represents a protocol message (abstraction of message.Message).
type Message interface {
	// Identity
	GetID() MessageID
	GetTransactionID() TransactionID

	// Routing
	GetType() int // Opcode
	GetSourceNodeID() int
	GetTargetNodeID() int

	// Payload
	GetPayload() interface{}
	SetPayload(payload interface{})

	// Lifecycle
	GetCreatedCycle() uint64
}

// MessageBuilder creates new messages.
type MessageBuilder interface {
	NewMessage(txnID TransactionID, msgType int, sourceID, targetID int, payload interface{}) Message
}

// ============================================================================
// Section 2: Transaction Context Abstraction
// ============================================================================

// YieldType defines the type of yield command.
type YieldType string

const (
	YieldTypeWaitForMessage YieldType = "WaitForMessage"
	YieldTypeWaitForTimeout YieldType = "WaitForTimeout"
	YieldTypeSendOnly       YieldType = "SendOnly"
	YieldTypeComplete       YieldType = "Complete"
)

// WaitCondition describes what a transaction is waiting for.
type WaitCondition struct {
	MessageType int     // Message type to wait for
	Addr        *uint64 // Optional: address filter
	SourceID    *int    // Optional: source node ID filter
	TargetID    *int    // Optional: target node ID filter
}

// YieldCommand represents a command sent from Transaction to TxnManager.
type YieldCommand struct {
	Type      YieldType
	WaitFor   *WaitCondition
	Timeout   time.Duration
	SendQueue []Message
}

// TxnContext provides the execution context for a CHI Transaction.
// This is the CHI-specific view of transaction.TxnContext.
type TxnContext interface {
	// Identity
	GetNodeID() int
	GetTxnID() TransactionID

	// Communication
	Yield(cmd *YieldCommand) (interface{}, error)
	Send(msg Message) error

	// Lifecycle
	Complete(result interface{}) error
	GetContext() context.Context
}

// ============================================================================
// Section 3: Cache Abstraction
// ============================================================================

// CacheState represents the state of a cache line.
type CacheState string

const (
	CacheStateInvalid   CacheState = "Invalid"
	CacheStateShared    CacheState = "Shared"
	CacheStateExclusive CacheState = "Exclusive"
	CacheStateModified  CacheState = "Modified"
	CacheStateOwned     CacheState = "Owned" // MOESI
)

// SnoopResponse represents the response to a snoop request.
type SnoopResponse struct {
	ResponseOpcode int    // CHI response opcode
	Data           []byte // Data if forwarding
	HasData        bool   // Whether data is included
}

// Cache defines the CHI cache interface.
type Cache interface {
	// State Query
	GetState(addr uint64) CacheState
	IsPresent(addr uint64) bool

	// Data Access
	GetData(addr uint64) []byte
	SetData(addr uint64, data []byte)

	// State Modification
	SetState(addr uint64, state CacheState)
	Invalidate(addr uint64)

	// CHI-Specific: Snoop Handling
	HandleSnoop(snoopOpcode int, addr uint64) (*SnoopResponse, error)

	// CHI-Specific: Forwarding Capability
	CanForward(addr uint64) bool
}

// ============================================================================
// Section 4: Directory Abstraction
// ============================================================================

// DirectoryState represents the state of a directory entry.
type DirectoryState string

const (
	DirStateNotPresent DirectoryState = "NotPresent"
	DirStateShared     DirectoryState = "Shared"
	DirStateExclusive  DirectoryState = "Exclusive"
	DirStateModified   DirectoryState = "Modified"
)

// Directory defines the CHI directory interface.
type Directory interface {
	// State Query
	GetState(addr uint64) DirectoryState

	// Sharer Management
	GetSharers(addr uint64) []int
	AddSharer(addr uint64, nodeID int)
	RemoveSharer(addr uint64, nodeID int)
	ClearSharers(addr uint64)

	// Owner Management
	GetOwner(addr uint64) int
	SetOwner(addr uint64, nodeID int)

	// State Modification
	SetState(addr uint64, state DirectoryState)

	// CHI-Specific: Writeback Detection
	MustWaitForWriteback(addr uint64) bool

	// CHI-Specific: Conflict Detection
	HasPendingRequest(addr uint64) bool
}

// ============================================================================
// Section 5: Address Decoder Abstraction
// ============================================================================

// DecodeResult contains the result of address decoding.
type DecodeResult struct {
	Addr       uint64 // Original address
	HomeNodeID int    // Home Node ID for this address
	IsMemory   bool   // Whether this address maps to memory
	IsCacheable bool  // Whether this address is cacheable
	// Future: Sharers []int for directory-based protocols
}

// Decoder decodes addresses to determine routing targets.
type Decoder interface {
	DecodeAddress(addr uint64) (*DecodeResult, error)
}

// ============================================================================
// Section 6: Node Environment
// ============================================================================

// NodeRole defines the role of a node in CHI protocol.
type NodeRole string

const (
	RoleRN NodeRole = "RN" // Request Node (e.g., CPU cache)
	RoleHN NodeRole = "HN" // Home Node (e.g., directory controller)
	RoleSN NodeRole = "SN" // Slave Node (e.g., memory controller)
)

// NodeEnv provides the execution environment for CHI Transactions.
// This is passed to each Transaction function along with TxnContext.
type NodeEnv struct {
	// Node Identity
	NodeID int
	Role   NodeRole

	// Capabilities
	Cache   Cache     // May be nil for HN/SN nodes
	Dir     Directory // May be nil for RN nodes
	Decoder Decoder

	// Message Builder
	MsgBuilder MessageBuilder
}

// ============================================================================
// Section 7: CHI Transaction Function Signatures
// ============================================================================

// TransactionFunc is the signature for a CHI Transaction function.
// All CHI Transaction implementations must follow this signature and
// ONLY use interfaces defined in this file.
type TransactionFunc func(ctx TxnContext, env *NodeEnv, addr uint64) ([]byte, error)

// TransactionHandler is the signature for a CHI Transaction handler
// that processes incoming messages (e.g., HomeNode handlers).
type TransactionHandler func(ctx TxnContext, env *NodeEnv, msg Message) error

// ============================================================================
// Section 8: CHI-Specific Helper Types
// ============================================================================

// CHIAddr represents a CHI address with optional attributes.
type CHIAddr struct {
	Addr       uint64
	Size       int  // Data size in bytes
	IsNonSecure bool // Security attribute
}

// CHIError represents a CHI protocol error.
type CHIError struct {
	Code    int    // CHI error code
	Message string
}

// Error implements the error interface.
func (e *CHIError) Error() string {
	return e.Message
}

// Common CHI error codes
const (
	ErrCodeOK       = 0
	ErrCodeDataErr  = 1
	ErrCodeNonData  = 2
)

// ============================================================================
// Section 9: Utility Functions for CHI Transactions
// ============================================================================

// NewYieldWaitForMessage creates a YieldCommand to wait for a message.
func NewYieldWaitForMessage(msgType int, addr uint64, timeout time.Duration) *YieldCommand {
	addrCopy := addr
	return &YieldCommand{
		Type: YieldTypeWaitForMessage,
		WaitFor: &WaitCondition{
			MessageType: msgType,
			Addr:        &addrCopy,
		},
		Timeout: timeout,
	}
}

// NewYieldSendOnly creates a YieldCommand to send messages without waiting.
func NewYieldSendOnly(msgs ...Message) *YieldCommand {
	return &YieldCommand{
		Type:      YieldTypeSendOnly,
		SendQueue: msgs,
	}
}

// NewYieldSendAndWait creates a YieldCommand to send and wait for response.
func NewYieldSendAndWait(msgType int, addr uint64, timeout time.Duration, msgs ...Message) *YieldCommand {
	addrCopy := addr
	return &YieldCommand{
		Type: YieldTypeWaitForMessage,
		WaitFor: &WaitCondition{
			MessageType: msgType,
			Addr:        &addrCopy,
		},
		Timeout:   timeout,
		SendQueue: msgs,
	}
}

// ============================================================================
// Notes for Framework Integration
// ============================================================================
//
// To integrate CHI with the framework, provide adapter implementations:
//
// 1. Adapt transaction.TxnContext to chi.TxnContext
// 2. Adapt cache.Cache to chi.Cache (implement HandleSnoop, CanForward)
// 3. Adapt directory.Directory to chi.Directory (implement MustWaitForWriteback)
// 4. Adapt message.Message to chi.Message
// 5. Implement chi.Decoder based on network topology
//
// Example adapter pattern:
//
//   type CHITxnContextAdapter struct {
//       underlying *transaction.TxnContext
//   }
//
//   func (a *CHITxnContextAdapter) GetNodeID() int {
//       return a.underlying.NodeID()
//   }
//
// ============================================================================
