package node

import (
	"context"
	"sync" // Added missing import

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// WorkerNode is a concrete node type for general purpose use (e.g. in simulator).
// It allows injecting custom behavior via hooks.
type WorkerNode struct {
	*BaseNode
	processHook func(ctx context.Context, cycle uint64, inputs [][]packet.Packet) error

	// Buffer for inspection/debugging
	processBuffer []packet.Packet
	bufferMu      sync.Mutex
}

// NewWorkerNode creates a new WorkerNode.
func NewWorkerNode(id int) *WorkerNode {
	w := &WorkerNode{
		processBuffer: make([]packet.Packet, 0),
	}
	w.BaseNode = NewBaseNode(id, w)
	return w
}

// SetProcessHook sets the hook for processing packets.
func (w *WorkerNode) SetProcessHook(hook func(ctx context.Context, cycle uint64, inputs [][]packet.Packet) error) {
	w.processHook = hook
}

// Process implements the NodeHandler interface.
func (w *WorkerNode) Process(ctx context.Context, cycle uint64, inputs [][]packet.Packet) error {
	w.bufferMu.Lock()
	defer w.bufferMu.Unlock()

	// Capture inputs for inspection
	w.processBuffer = nil
	for _, q := range inputs {
		w.processBuffer = append(w.processBuffer, q...)
	}

	if w.processHook != nil {
		return w.processHook(ctx, cycle, inputs)
	}
	return nil
}

// GetProcessBuffer returns the last processed packets.
func (w *WorkerNode) GetProcessBuffer() []packet.Packet {
	w.bufferMu.Lock()
	defer w.bufferMu.Unlock()
	buf := make([]packet.Packet, len(w.processBuffer))
	copy(buf, w.processBuffer)
	return buf
}
