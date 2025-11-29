package node

import (
	"context"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// FIFONode is a convenience wrapper around Node that behaves like a simple
// FIFO pipeline stage with exactly one InputQueue and one OutputQueue. Every
// cycle it dequeues at most one packet from the input and enqueues it on the
// output.
type FIFONode struct {
	inner  *Node
	input  InputQueue
	output OutputQueue
}

// NewFIFONode constructs a FIFONode with the given ID, input queue, and output queue.
func NewFIFONode(id int, input InputQueue, output OutputQueue) *FIFONode {
	n := New(id)
	_ = n.AddInputQueue(input)
	_ = n.AddOutputQueue(output)

	f := &FIFONode{
		inner:  n,
		input:  input,
		output: output,
	}

	n.SetProcessHook(func(_ context.Context, cycle uint64, buf []packet.Packet) ([]packet.Packet, error) {
		if len(buf) == 0 {
			return buf, nil
		}
		// Only forward the first packet per cycle.
		pkt := buf[0]
		if err := f.output.InjectPackets(int(cycle), []packet.Packet{pkt}); err != nil {
			return nil, err
		}
		// Store only the forwarded packet in the process buffer for inspection.
		return []packet.Packet{pkt}, nil
	})

	return f
}

// Tick advances the FIFO node by one cycle.
func (f *FIFONode) Tick(ctx context.Context, cycle uint64) error {
	return f.inner.Tick(ctx, cycle, time.Duration(0))
}

// Input returns the underlying InputQueue.
func (f *FIFONode) Input() InputQueue {
	return f.input
}

// Output returns the underlying OutputQueue.
func (f *FIFONode) Output() OutputQueue {
	return f.output
}

// ProcessBuffer returns the packets processed in the last cycle.
func (f *FIFONode) ProcessBuffer() []packet.Packet {
	return f.inner.ProcessBuffer()
}
