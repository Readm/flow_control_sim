package node

import (
	"context"
	"fmt"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// SimpleRouterNode is a generic router that uses a pluggable RoutingStrategy.
type SimpleRouterNode struct {
	*BaseNode
	strategy RoutingStrategy
}

// NewSimpleRouterNode creates a new SimpleRouterNode.
func NewSimpleRouterNode(id int, strategy RoutingStrategy) *SimpleRouterNode {
	r := &SimpleRouterNode{
		strategy: strategy,
	}
	r.BaseNode = NewBaseNode(id, r)
	return r
}

// Process implements NodeHandler.
func (r *SimpleRouterNode) Process(ctx context.Context, cycle uint64, inputs [][]packet.Packet) error {
	outputs := r.OutputQueues()
	outputCount := len(outputs)

	// Flatten all inputs to route them individually
	for _, inputQueuePackets := range inputs {
		for _, pkt := range inputQueuePackets {
			// Decide where to send
			targetIndices, err := r.strategy.Route(pkt, outputCount)
			if err != nil {
				return fmt.Errorf("router %d route error: %w", r.id, err)
			}

			// Multicast support
			for _, idx := range targetIndices {
				outQ := r.GetOutputQueue(idx)
				if outQ == nil {
					continue
				}
				// Inject
				if err := outQ.InjectPackets(int(cycle), []packet.Packet{pkt}); err != nil {
					return fmt.Errorf("router %d inject to output %d failed: %w", r.id, idx, err)
				}
			}
		}
	}
	return nil
}
