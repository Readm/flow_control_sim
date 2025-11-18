package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/Readm/flow_sim/framework/core"
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
	DefaultRequestCacheCapacity  = 64   // Default number of cache lines for RequestNode
	DefaultHomeCacheCapacity     = 128  // Default number of cache lines for HomeNode

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

type PacketInfo = core.PacketInfo
type QueueInfo = core.QueueInfo
type Position = core.Position

// PluginConfig describes optional plugin selections.
type PluginConfig struct {
	Incentives []string `json:"incentives,omitempty"`
}

// Config holds simulation configuration values.
type Config struct {
	TotalCycles int

	// Request generation configuration
	RequestGenerator  RequestGenerator                  // optional override applied to all requester nodes
	RequestRateConfig float64                           // probability for default probability generator (0.0-1.0)
	NodeSchedules     map[string]map[int][]ScheduleItem // graphNodeID -> cycle -> items

	// channel bandwidth limit (default for edges without explicit bandwidth)
	BandwidthLimit int

	// dispatch queue capacity for RequestNode
	DispatchQueueCapacity int

	// Cache configuration
	RequestCacheCapacity int // max cache lines per RequestNode (LRU eviction), <=0 uses default
	HomeCacheCapacity    int // max cache lines for HomeNode (LRU eviction), <=0 uses default

	// visualization settings
	Headless   bool
	VisualMode string // "gui" | "web" | "none"

	Plugins PluginConfig

	// Initial cache state (for test scenarios)
	InitialCacheState map[int]map[uint64]CacheState

	// Packet history tracking configuration
	EnablePacketHistory   bool
	MaxPacketHistorySize  int
	HistoryOverflowMode   string
	MaxTransactionHistory int

	// Graph describes an arbitrary topology graph (required)
	Graph *GraphConfig
}

// GraphConfig captures optional arbitrary directed graph information.
type GraphConfig struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

// GraphNode represents a node in the topology graph.
type GraphNode struct {
	ID           string
	Label        string
	Capabilities []string
	Metadata     map[string]string
	Position     *Position
}

// GraphEdge represents a directed edge with latency/bandwidth attributes.
type GraphEdge struct {
	From      string
	To        string
	Latency   int
	Bandwidth int
	Metadata  map[string]string
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
	graphNodeCount := 0
	graphEdgeCount := 0
	if cfg.Graph != nil {
		graphNodeCount = len(cfg.Graph.Nodes)
		graphEdgeCount = len(cfg.Graph.Edges)
	}

	hashInput := fmt.Sprintf("%d-%f-%d-%d-%t-%d-%d",
		cfg.TotalCycles,
		cfg.RequestRateConfig,
		cfg.DispatchQueueCapacity,
		cfg.BandwidthLimit,
		cfg.Headless,
		graphNodeCount,
		graphEdgeCount)

	// Compute SHA256 hash
	hash := sha256.Sum256([]byte(hashInput))
	// Return first ConfigHashLength characters of hex representation
	return hex.EncodeToString(hash[:])[:ConfigHashLength]
}
