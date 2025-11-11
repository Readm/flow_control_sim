package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"flow_sim/core"
)

// Simulation constants
const (
	// DefaultVisualizationDelay is the delay between visualization updates in web mode
	DefaultVisualizationDelay = 50 * time.Millisecond

	// Queue capacity constants
	DefaultSlaveQueueCapacity    = 20   // Limited capacity for high load visualization
	UnlimitedQueueCapacity       = -1   // Unlimited queue capacity
	DefaultRequestQueueCapacity  = 1024 // Default capacity for dispatch queue (changed from -1)
	DefaultForwardQueueCapacity  = -1   // Unlimited for forward queue
	DefaultDispatchQueueCapacity = 1024 // Default capacity for dispatch queue

	// Link and bandwidth constants
	DefaultBandwidthLimit = 1 // Default maximum packets per slot in pipeline

	// Address and data size constants
	DefaultAddressBase   = uint64(0x1000) // Base address for CHI transactions
	DefaultCacheLineSize = 64             // Standard cache line size in bytes

	// Config hash constants
	ConfigHashLength = 16 // Length of config hash in hex characters
)

// CHITransactionType represents CHI protocol transaction types
type CHITransactionType = core.CHITransactionType

const (
	CHITxnReadNoSnp   CHITransactionType = core.CHITxnReadNoSnp
	CHITxnWriteNoSnp  CHITransactionType = core.CHITxnWriteNoSnp
	CHITxnReadOnce    CHITransactionType = core.CHITxnReadOnce
	CHITxnWriteUnique CHITransactionType = core.CHITxnWriteUnique
)

// CHIMessageType represents CHI protocol message types
type CHIMessageType = core.CHIMessageType

const (
	CHIMsgReq     CHIMessageType = core.CHIMsgReq     // Request message
	CHIMsgResp    CHIMessageType = core.CHIMsgResp    // Response message
	CHIMsgData    CHIMessageType = core.CHIMsgData    // Data message
	CHIMsgComp    CHIMessageType = core.CHIMsgComp    // Completion message
	CHIMsgSnp     CHIMessageType = core.CHIMsgSnp     // Snoop request message
	CHIMsgSnpResp CHIMessageType = core.CHIMsgSnpResp // Snoop response message
)

// CHIResponseType represents CHI response types
type CHIResponseType = core.CHIResponseType

const (
	CHIRespCompData   CHIResponseType = core.CHIRespCompData   // Completion with data
	CHIRespCompAck    CHIResponseType = core.CHIRespCompAck    // Completion acknowledgment
	CHIRespSnpData    CHIResponseType = core.CHIRespSnpData    // Snoop response with data
	CHIRespSnpInvalid CHIResponseType = core.CHIRespSnpInvalid // Snoop response invalidating cache
	CHIRespSnpNoData  CHIResponseType = core.CHIRespSnpNoData  // Snoop response with no data
)

// Packet represents a CHI protocol message flowing through the simulator.
// It supports both legacy "request"/"response" types and new CHI protocol fields.
//
// Migration status:
// - Primary fields: Use CHI protocol fields (TransactionType, MessageType, ResponseType)
// - Legacy fields: Kept for backward compatibility, will be deprecated in future versions
// - When to use: New code should prefer CHI fields; legacy fields are checked as fallback
type Packet = core.Packet

// EdgeKey represents a unique edge in the network (fromID -> toID).
type EdgeKey = core.EdgeKey

type NodeType = core.NodeType
type PacketInfo = core.PacketInfo
type QueueInfo = core.QueueInfo
type Position = core.Position

// NodeType constants re-exported for compatibility.
const (
	NodeTypeRN core.NodeType = core.NodeTypeRN
	NodeTypeHN core.NodeType = core.NodeTypeHN
	NodeTypeSN core.NodeType = core.NodeTypeSN
)

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
	SlaveProcessRate int // requests processed per cycle per slave

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

	// dispatch queue capacity for RequestNode
	DispatchQueueCapacity int // dispatch_queue capacity (default: 1024, -1 is invalid)

	// weighting for choosing destination slave (length == NumSlaves)
	SlaveWeights []int

	// visualization settings
	Headless   bool   // true to run without visualization
	VisualMode string // "gui" | "web" | "none" (default: "gui" if Headless is false)

	// Initial cache state (for test scenarios)
	// Format: map[nodeID]map[address]CacheState
	InitialCacheState map[int]map[uint64]CacheState

	// Packet history tracking configuration
	EnablePacketHistory   bool   // Enable packet history tracking (default: true)
	MaxPacketHistorySize  int    // Maximum packet history size (0 = unlimited, default: 0)
	HistoryOverflowMode   string // "circular" or "initial" (default: "circular")
	MaxTransactionHistory int    // Maximum number of transactions to keep history for (default: 1000)
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
	mu   sync.Mutex
	next int64
}

func NewPacketIDAllocator() *PacketIDAllocator {
	return &PacketIDAllocator{next: 1}
}

func (a *PacketIDAllocator) Allocate() int64 {
	a.mu.Lock()
	id := a.next
	a.next++
	a.mu.Unlock()
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
