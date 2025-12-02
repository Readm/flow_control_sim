package chi

import (
	"sync"

	"github.com/Readm/flow_sim/internal/dataflow"
	"github.com/Readm/flow_sim/internal/dataflow/message"
)

// MessageBuilder creates protocol messages.
type MessageBuilder struct {
	nodeID       int
	msgIDCounter int
	mu           sync.Mutex
}

// NewMessageBuilder creates a new MessageBuilder for a node.
func NewMessageBuilder(nodeID int) *MessageBuilder {
	return &MessageBuilder{
		nodeID: nodeID,
	}
}

// NewMessage creates a new message with the given parameters.
func (b *MessageBuilder) NewMessage(
	txnID dataflow.TransactionID,
	msgType int,
	sourceID, targetID int,
	payload interface{},
) *message.Message {
	b.mu.Lock()
	b.msgIDCounter++
	msgID := b.msgIDCounter
	b.mu.Unlock()

	return &message.Message{
		ID: dataflow.MessageID{
			NodeID:    sourceID,
			MessageID: msgID,
		},
		TransactionID: txnID,
		Type:          msgType,
		SourceNodeID:  sourceID,
		TargetNodeID:  targetID,
		Payload:       payload,
		CreatedCycle:  0, // Will be set by TxnManager
	}
}
