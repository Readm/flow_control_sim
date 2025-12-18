package node

import (
	"context"
	"sync/atomic"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// SinkNode consumes all packets and counts them.
type SinkNode struct {
	*BaseNode
	receivedCount uint64
}

// NewSinkNode creates a new SinkNode.
func NewSinkNode(id int) *SinkNode {
	s := &SinkNode{
		receivedCount: 0,
	}
	s.BaseNode = NewBaseNode(id, s)
	return s
}

// Process implements NodeHandler.
func (s *SinkNode) Process(ctx context.Context, cycle uint64, inputs [][]packet.Packet) error {
	count := 0
	for _, inputQ := range inputs {
		count += len(inputQ)
	}
	atomic.AddUint64(&s.receivedCount, uint64(count))
	return nil
}

// GetReceivedCount returns the total number of packets received.
func (s *SinkNode) GetReceivedCount() uint64 {
	return atomic.LoadUint64(&s.receivedCount)
}
