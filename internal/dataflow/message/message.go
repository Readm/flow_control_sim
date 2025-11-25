package message

import (
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// MessageType defines the type of message.
type MessageType string

const (
	MessageTypeRequest  MessageType = "Request"
	MessageTypeData     MessageType = "Data"
	MessageTypeResponse MessageType = "Response"
)

// ProcessedInfo records when and where a message was processed.
type ProcessedInfo struct {
	Cycle  uint64 // Processing time (cycle)
	NodeID int    // Node that processed the message
	Info   string // Additional information about the processing
}

// Message represents a message unit in a transaction.
type Message struct {
	ID            int64           // Unique identifier
	TransactionID int64           // Belongs to Transaction
	Type          MessageType     // Message type
	SourceNodeID  int             // Source node
	TargetNodeID  int             // Target node
	LinkType      string          // Link type (optional, for routing)
	Payload       interface{}     // Message payload
	Packets       []packet.Packet // Associated packets
	CreatedCycle  uint64          // Creation time (cycle)
	ProcessedInfo []ProcessedInfo // Processing history (multiple nodes may process)
}
