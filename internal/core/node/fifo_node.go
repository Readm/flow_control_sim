package node

import (
	"context"
	"sync"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// FIFONode is a convenience wrapper around Node that behaves like a simple
// FIFO pipeline stage with exactly one InputQueue and one OutputQueue. Every
// cycle it dequeues at most one packet from the input and enqueues it on the
// output.
type FIFONode struct {
	*BaseNode
	input         InputQueue
	output        OutputQueue
	processBuffer []packet.Packet
	bufferMu      sync.Mutex
}

// NewFIFONode constructs a FIFONode with the given ID, input queue, and output queue.
func NewFIFONode(id int, input InputQueue, output OutputQueue) *FIFONode {
	f := &FIFONode{
		input:         input,
		output:        output,
		processBuffer: make([]packet.Packet, 0),
	}
	f.BaseNode = NewBaseNode(id, f)
	_ = f.AddInputQueue(input)
	_ = f.AddOutputQueue(output)
	return f
}

// Process implements the NodeHandler interface.
func (f *FIFONode) Process(ctx context.Context, cycle uint64, inputs [][]packet.Packet) error {
	f.bufferMu.Lock()
	defer f.bufferMu.Unlock()
	f.processBuffer = nil // Clear previous

	// FIFO node expects exactly 1 input queue (inputs[0])
	if len(inputs) == 0 || len(inputs[0]) == 0 {
		return nil
	}

	// Only forward the first packet per cycle from the first input queue.
	pkt := inputs[0][0]
	if err := f.output.InjectPackets(int(cycle), []packet.Packet{pkt}); err != nil {
		return err
	}

	// Store for inspection
	f.processBuffer = []packet.Packet{pkt}
	return nil
}

// Tick advances the FIFO node by one cycle.
func (f *FIFONode) Tick(ctx context.Context, cycle uint64) error {
	return f.BaseNode.Tick(ctx, cycle, time.Duration(0))
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
	f.bufferMu.Lock()
	defer f.bufferMu.Unlock()
	buf := make([]packet.Packet, len(f.processBuffer))
	copy(buf, f.processBuffer)
	return buf
}
