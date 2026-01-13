package packet

import (
	"github.com/Readm/flow_sim/internal/dataflow"
)

// Packet represents the minimal unit exchanged between nodes through links.
// Higher level metadata (transaction IDs, QoS, etc.) can be layered on top of
// this structure without affecting the Core/Entity contracts.
type Packet struct {
	SourceID int `json:"src"`
	TargetID int `json:"dst"`
	// Payload has been flattened into native fields for performance

	TransactionID dataflow.TransactionID `json:"txn_id"` // Associated Transaction ID
	MessageID     dataflow.MessageID     `json:"msg_id"` // Associated Message ID
	Sequence      int                    `json:"seq"`    // Sequence number
	Type          int                    `json:"type"`   // logical type

	// === Common Native Fields (Zero-Copy) ===
	Addr    uint64 `json:"addr,omitempty"`     // Physical Address
	VAddr   uint64 `json:"v_addr,omitempty"`   // Virtual Address
	Data    uint64 `json:"data,omitempty"`     // Data payload
	InstrID uint64 `json:"instr_id,omitempty"` // Instruction ID
	Op      int    `json:"op,omitempty"`       // Operation Subtype
	Cycle   uint64 `json:"cycle,omitempty"`    // Timestamp

	// === Metadata Extension ===
	Metadata map[string]interface{} `json:"meta,omitempty"`
}

// PacketWithCycle associates a packet with the cycle in which it becomes visible to
// the destination flow. This is the element transmitted through link channels.
type PacketWithCycle struct {
	Cycle  int
	Packet Packet
}
