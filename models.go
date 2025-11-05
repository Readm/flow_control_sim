package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
type Packet struct {
	ID    int64  // unique packet id
	Type  string // legacy: "request" or "response" (kept for compatibility during transition)
	SrcID int    // source node id
	DstID int    // destination node id

	GeneratedAt int // cycle when generated (for requests)
	SentAt      int // cycle when sent to channel
	ReceivedAt  int // cycle when received by next hop
	CompletedAt int // cycle when processed (for requests)

	// Legacy fields (kept for compatibility)
	MasterID  int   // original master/request node id
	RequestID int64 // request id (same as ID for request packets)

	// CHI protocol fields
	TransactionType CHITransactionType // CHI transaction type (ReadNoSnp, WriteNoSnp, etc.)
	MessageType     CHIMessageType     // CHI message type (Req, Resp, Data, Comp)
	ResponseType    CHIResponseType    // CHI response type (CompData, CompAck, etc.)
	Address         uint64             // memory address for the transaction
	DataSize        int                // data size in bytes (default: 64 for cache line)
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
	RequestRate      float64 // per-master per-cycle probability to generate one request

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
	// Return first 16 characters of hex representation (64 bits of entropy)
	return hex.EncodeToString(hash[:])[:16]
}
