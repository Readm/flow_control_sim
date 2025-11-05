package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Simulation constants
const (
	// DefaultVisualizationDelay is the delay between visualization updates in web mode
	DefaultVisualizationDelay = 50 * time.Millisecond

	// Queue capacity constants
	DefaultSlaveQueueCapacity   = 20 // Limited capacity for high load visualization
	UnlimitedQueueCapacity      = -1 // Unlimited queue capacity
	DefaultRequestQueueCapacity = -1 // Unlimited for pending requests
	DefaultForwardQueueCapacity = -1 // Unlimited for forward queue

	// Link and bandwidth constants
	DefaultBandwidthLimit = 1 // Default maximum packets per slot in pipeline

	// Address and data size constants
	DefaultAddressBase   = uint64(0x1000) // Base address for CHI transactions
	DefaultCacheLineSize = 64             // Standard cache line size in bytes

	// Config hash constants
	ConfigHashLength = 16 // Length of config hash in hex characters
)

// CHITransactionType represents CHI protocol transaction types
type CHITransactionType string

const (
	CHITxnReadNoSnp   CHITransactionType = "ReadNoSnp"
	CHITxnWriteNoSnp  CHITransactionType = "WriteNoSnp"
	CHITxnReadOnce    CHITransactionType = "ReadOnce"
	CHITxnWriteUnique CHITransactionType = "WriteUnique"
)

// CHIMessageType represents CHI protocol message types
type CHIMessageType string

const (
	CHIMsgReq  CHIMessageType = "Req"  // Request message
	CHIMsgResp CHIMessageType = "Resp" // Response message
	CHIMsgData CHIMessageType = "Data" // Data message
	CHIMsgComp CHIMessageType = "Comp" // Completion message
)

// CHIResponseType represents CHI response types
type CHIResponseType string

const (
	CHIRespCompData CHIResponseType = "CompData" // Completion with data
	CHIRespCompAck  CHIResponseType = "CompAck"  // Completion acknowledgment
)

// Packet represents a CHI protocol message flowing through the simulator.
// It supports both legacy "request"/"response" types and new CHI protocol fields.
//
// Migration status:
// - Primary fields: Use CHI protocol fields (TransactionType, MessageType, ResponseType)
// - Legacy fields: Kept for backward compatibility, will be deprecated in future versions
// - When to use: New code should prefer CHI fields; legacy fields are checked as fallback
type Packet struct {
	ID    int64  // unique packet id
	Type  string // legacy: "request" or "response" - use MessageType instead (kept for compatibility)
	SrcID int    // source node id
	DstID int    // destination node id

	GeneratedAt int // cycle when generated (for requests)
	SentAt      int // cycle when sent to channel
	ReceivedAt  int // cycle when received by next hop
	CompletedAt int // cycle when processed (for requests)

	// Legacy fields (deprecated, kept for backward compatibility)
	// These fields are maintained for compatibility with older code but should not be used in new code.
	// Migration: Use CHI protocol fields (MessageType, TransactionType) instead of Type.
	// MasterID is equivalent to the original Request Node ID in CHI terminology.
	MasterID  int   // legacy: original master/request node id - use CHI MessageType + DstID instead
	RequestID int64 // legacy: request id - same as ID for request packets, preserved for compatibility

	// CHI protocol fields (preferred)
	// These are the primary fields for packet identification and routing in CHI protocol.
	TransactionType CHITransactionType // CHI transaction type (ReadNoSnp, WriteNoSnp, etc.)
	MessageType     CHIMessageType     // CHI message type (Req, Resp, Data, Comp) - primary field for message type
	ResponseType    CHIResponseType    // CHI response type (CompData, CompAck, etc.)
	Address         uint64             // memory address for the transaction
	DataSize        int                // data size in bytes (default: DefaultCacheLineSize)
}

// Config holds simulation configuration values.
type Config struct {
	NumMasters int
	NumSlaves  int
	NumRelays  int // first phase = 1

	TotalCycles int

	// fixed one-way latencies in cycles
	MasterRelayLatency int // Master -> Relay
	RelayMasterLatency int // Relay  -> Master
	RelaySlaveLatency  int // Relay  -> Slave
	SlaveRelayLatency  int // Slave  -> Relay

	// processing and generation
	SlaveProcessRate int     // requests processed per cycle per slave
	
	// Request generation: uses RequestGenerator interface
	// Generators are created in Simulator initialization (requires rng)
	// If RequestGenerators is nil or empty, RequestGenerator is used for all masters
	// If RequestGenerators is provided, it overrides RequestGenerator per master
	RequestGenerator  RequestGenerator   // default generator for all masters (created in Simulator init)
	RequestGenerators []RequestGenerator // per-master override (optional, len should match NumMasters)
	
	// Generator configuration (used to create generators if not already set)
	// These fields are used when RequestGenerator is nil
	RequestRateConfig float64 // probability for ProbabilityGenerator (0.0-1.0)
	
	// ScheduleGenerator configuration (optional)
	// If ScheduleConfig is non-nil, ScheduleGenerator will be created instead of ProbabilityGenerator
	ScheduleConfig map[int]map[int][]ScheduleItem // cycle -> masterIndex -> []ScheduleItem

	// channel bandwidth limit
	BandwidthLimit int // maximum packets per slot in pipeline (per edge per cycle)

	// weighting for choosing destination slave (length == NumSlaves)
	SlaveWeights []int

	// visualization settings
	Headless   bool   // true to run without visualization
	VisualMode string // "gui" | "web" | "none" (default: "gui" if Headless is false)
}

// NodeIDAllocator provides simple incremental ids for nodes.
type NodeIDAllocator struct {
	nextID int
}

func NewNodeIDAllocator() *NodeIDAllocator {
	return &NodeIDAllocator{nextID: 0}
}

func (a *NodeIDAllocator) Allocate() int {
	id := a.nextID
	a.nextID++
	return id
}

// PacketIDAllocator provides unique ids for packets.
type PacketIDAllocator struct {
	next int64
}

func NewPacketIDAllocator() *PacketIDAllocator {
	return &PacketIDAllocator{next: 1}
}

func (a *PacketIDAllocator) Allocate() int64 {
	id := a.next
	a.next++
	return id
}

// computeConfigHash computes a hash of the configuration to detect config changes.
// The hash is based on key configuration fields that affect network topology.
func computeConfigHash(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	// Create a string representation of key config fields that affect topology
	hashInput := fmt.Sprintf("%d-%d-%d-%d-%d-%d-%d-%d-%d",
		cfg.NumMasters,
		cfg.NumSlaves,
		cfg.NumRelays,
		cfg.MasterRelayLatency,
		cfg.RelayMasterLatency,
		cfg.RelaySlaveLatency,
		cfg.SlaveRelayLatency,
		cfg.BandwidthLimit,
		len(cfg.SlaveWeights))

	// Compute SHA256 hash
	hash := sha256.Sum256([]byte(hashInput))
	// Return first ConfigHashLength characters of hex representation
	return hex.EncodeToString(hash[:])[:ConfigHashLength]
}
