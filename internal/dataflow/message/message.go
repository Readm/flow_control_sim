package message

import (
	"encoding/json"
	"fmt"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// MessageType defines the type of message.
type MessageType string

const (
	MessageTypeRequest MessageType = "Request"
	MessageTypeData    MessageType = "Data"
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

// ToPackets encodes Message to Packets.
// If the message is large, it will be split into multiple packets.
func (m *Message) ToPackets(maxPacketSize int) []packet.Packet {
	if maxPacketSize <= 0 {
		maxPacketSize = 1024 // Default packet size
	}

	// Create envelope with type information
	envelope := map[string]interface{}{
		"type":    string(m.Type),
		"payload": m.Payload,
	}

	// Serialize envelope to JSON
	payloadBytes, err := json.Marshal(envelope)
	if err != nil {
		// Fallback: include type in string format
		payloadBytes = []byte(fmt.Sprintf(`{"type":"%s","payload":%v}`, string(m.Type), m.Payload))
	}

	payloadStr := string(payloadBytes)
	packets := []packet.Packet{}

	// Split into multiple packets if needed
	if len(payloadStr) <= maxPacketSize {
		// Single packet
		packets = append(packets, packet.Packet{
			SourceID:      m.SourceNodeID,
			TargetID:      m.TargetNodeID,
			Payload:       payloadStr,
			TransactionID: m.TransactionID,
			MessageID:     m.ID,
			Sequence:      0,
		})
	} else {
		// Multiple packets
		seq := 0
		for i := 0; i < len(payloadStr); i += maxPacketSize {
			end := i + maxPacketSize
			if end > len(payloadStr) {
				end = len(payloadStr)
			}
			packets = append(packets, packet.Packet{
				SourceID:      m.SourceNodeID,
				TargetID:      m.TargetNodeID,
				Payload:       payloadStr[i:end],
				TransactionID: m.TransactionID,
				MessageID:     m.ID,
				Sequence:      seq,
			})
			seq++
		}
	}

	m.Packets = packets
	return packets
}

// FromPackets decodes Packets to Message.
// Packets must be sorted by Sequence.
func (m *Message) FromPackets(packets []packet.Packet) error {
	if len(packets) == 0 {
		return fmt.Errorf("no packets to decode")
	}

	// Validate all packets belong to the same message
	for i, pkt := range packets {
		if pkt.MessageID != packets[0].MessageID {
			return fmt.Errorf("packet %d has different MessageID", i)
		}
		if pkt.TransactionID != packets[0].TransactionID {
			return fmt.Errorf("packet %d has different TransactionID", i)
		}
	}

	// Sort packets by sequence (if not already sorted)
	sortedPackets := make([]packet.Packet, len(packets))
	copy(sortedPackets, packets)
	for i := 0; i < len(sortedPackets)-1; i++ {
		for j := i + 1; j < len(sortedPackets); j++ {
			if sortedPackets[i].Sequence > sortedPackets[j].Sequence {
				sortedPackets[i], sortedPackets[j] = sortedPackets[j], sortedPackets[i]
			}
		}
	}

	// Reconstruct payload
	payloadStr := ""
	for _, pkt := range sortedPackets {
		payloadStr += pkt.Payload
	}

	// Deserialize envelope from JSON
	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(payloadStr), &envelope); err != nil {
		// If unmarshal fails, try to extract type from string
		// Fallback: use raw string as payload
		m.ID = sortedPackets[0].MessageID
		m.TransactionID = sortedPackets[0].TransactionID
		m.SourceNodeID = sortedPackets[0].SourceID
		m.TargetNodeID = sortedPackets[0].TargetID
		m.Type = MessageTypeRequest // Default type
		m.Payload = payloadStr
		m.Packets = sortedPackets
		return nil
	}

	// Extract type and payload from envelope
	if typeStr, ok := envelope["type"].(string); ok {
		m.Type = MessageType(typeStr)
	} else {
		m.Type = MessageTypeRequest // Default type
	}

	if payload, ok := envelope["payload"]; ok {
		m.Payload = payload
	} else {
		m.Payload = envelope
	}

	m.ID = sortedPackets[0].MessageID
	m.TransactionID = sortedPackets[0].TransactionID
	m.SourceNodeID = sortedPackets[0].SourceID
	m.TargetNodeID = sortedPackets[0].TargetID
	m.Packets = sortedPackets

	return nil
}

// IsComplete checks if all packets for this message have been received.
func (m *Message) IsComplete() bool {
	if len(m.Packets) == 0 {
		return false
	}

	// Check if we have packets for all expected sequences
	maxSeq := -1
	for _, pkt := range m.Packets {
		if pkt.Sequence > maxSeq {
			maxSeq = pkt.Sequence
		}
	}

	// Check if we have all sequences from 0 to maxSeq
	seqSet := make(map[int]bool)
	for _, pkt := range m.Packets {
		seqSet[pkt.Sequence] = true
	}

	for i := 0; i <= maxSeq; i++ {
		if !seqSet[i] {
			return false
		}
	}

	return true
}

// AddProcessedInfo adds a processing record to the message.
func (m *Message) AddProcessedInfo(cycle uint64, nodeID int, info string) {
	m.ProcessedInfo = append(m.ProcessedInfo, ProcessedInfo{
		Cycle:  cycle,
		NodeID: nodeID,
		Info:   info,
	})
}

// GetLastProcessedInfo returns the last processing record, or nil if not processed.
func (m *Message) GetLastProcessedInfo() *ProcessedInfo {
	if len(m.ProcessedInfo) == 0 {
		return nil
	}
	return &m.ProcessedInfo[len(m.ProcessedInfo)-1]
}

// IsProcessed checks if the message has been processed at least once.
func (m *Message) IsProcessed() bool {
	return len(m.ProcessedInfo) > 0
}

