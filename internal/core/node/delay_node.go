package node

import (
	"context"
	"fmt"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// DelayNode simulates processing delay.
// It holds packets for N cycles before forwarding them.
type DelayNode struct {
	*BaseNode
	delayCycles int
	// internal storage for delayed packets: map[dispatchCycle][]Packet
	delayedPackets map[uint64][]packet.Packet
}

// NewDelayNode creates a new DelayNode.
func NewDelayNode(id int, delayCycles int) *DelayNode {
	d := &DelayNode{
		delayCycles:    delayCycles,
		delayedPackets: make(map[uint64][]packet.Packet),
	}
	d.BaseNode = NewBaseNode(id, d)
	return d
}

// Process implements NodeHandler.
func (d *DelayNode) Process(ctx context.Context, cycle uint64, inputs [][]packet.Packet) error {
	// 1. Buffer incoming packets for future dispatch
	dispatchCycle := cycle + uint64(d.delayCycles)
	var incoming []packet.Packet
	for _, q := range inputs {
		incoming = append(incoming, q...)
	}

	if len(incoming) > 0 {
		d.delayedPackets[dispatchCycle] = append(d.delayedPackets[dispatchCycle], incoming...)
	}

	// 2. Dispatch packets scheduled for current cycle
	toSend, ok := d.delayedPackets[cycle]
	if ok {
		delete(d.delayedPackets, cycle)
		// Forward to output 0
		outputs := d.OutputQueues()
		if len(outputs) > 0 {
			if err := outputs[0].InjectPackets(int(cycle), toSend); err != nil {
				return fmt.Errorf("delay node %d inject failed: %w", d.id, err)
			}
		}
	}

	return nil
}
