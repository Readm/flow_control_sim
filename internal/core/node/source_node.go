package node

import (
	"context"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// PacketGenerator is a function that generates packets for a given cycle.
type PacketGenerator func(ctx context.Context, cycle uint64) ([]packet.Packet, error)

// SourceNode generates packets based on a provided generator function.
// It ignores input packets (acts as a standard source).
type SourceNode struct {
	*BaseNode
	generator PacketGenerator
}

// NewSourceNode creates a new SourceNode.
func NewSourceNode(id int, generator PacketGenerator) *SourceNode {
	s := &SourceNode{
		generator: generator,
	}
	s.BaseNode = NewBaseNode(id, s)
	return s
}

// Process implements NodeHandler.
func (s *SourceNode) Process(ctx context.Context, cycle uint64, inputs [][]packet.Packet) error {
	// SourceNode typically ignores inputs, or could be a "Source/Sink" hybrid.
	// For pure source, we just generate and inject.

	if s.generator == nil {
		return nil
	}

	pkts, err := s.generator(ctx, cycle)
	if err != nil {
		return err
	}

	if len(pkts) == 0 {
		return nil
	}

	// Default behavior: broadcast to all outputs or inject to 0?
	// Tests usually expect injection.
	// We'll inject to Output 0 by default, or maybe we need a RoutingStrategy here too?
	// For simplicity, inject all generated packets to ALL outputs (multicast)
	// or just the first one?
	// Existing tests usually use 1 output.

	outputs := s.OutputQueues()
	if len(outputs) == 0 {
		return nil
	}

	// Strategy: Round Robin or Broadcast?
	// Let's Broadcast for now as it's safest for "generating load".
	// Or maybe the generator returns map[int][]Packet?
	// Keep it simple: Inject to Output 0.

	// Better: Inject to Output 0 if available.
	if len(outputs) > 0 {
		return outputs[0].InjectPackets(int(cycle), pkts)
	}

	return nil
}
