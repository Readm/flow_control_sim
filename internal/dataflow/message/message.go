package message

import (
	"github.com/Readm/flow_sim/internal/dataflow"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// ProcessedInfo records when and where a message was processed.
type ProcessedInfo struct {
	Cycle     uint64 // Processing time (cycle)
	NodeID    int    // Node that processed the message
	PacketIDs []int  // Packet Sequence list involved (optional, for tracking specific packets)
	Info      string // Additional information about the processing
}

// Message represents a message unit in a transaction.
type Message struct {
	ID            dataflow.MessageID     // Unique identifier
	TransactionID dataflow.TransactionID // Belongs to Transaction
	Channel       dataflow.Channel       // Channel type (REQ, RSP, DAT, SNP) - explicit channel differentiation
	Type          int                    // Message type (protocol-specific opcode)
	SourceNodeID  int                    // Source node
	TargetNodeID  int                    // Target node
	LinkType      string                 // Link type (optional, for routing)
	Payload       interface{}            // Message payload (protocol-specific, e.g., CHIPayload)
	Packets       []packet.Packet        // Associated packets
	CreatedCycle  uint64                 // Creation time (cycle)
	ProcessedInfo []ProcessedInfo        // Processing history (multiple nodes may process)
}
