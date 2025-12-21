package node

import (
	"sync" // Added missing import

	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// WorkerNode is a concrete node type for general purpose use (e.g. in simulator).
// It allows injecting custom behavior via hooks.
type WorkerNode struct {
	*BaseNode
	processHook func(cycle uint64, inputs [][]queue.PacketRef) error

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
func (w *WorkerNode) SetProcessHook(hook func(cycle uint64, inputs [][]queue.PacketRef) error) {
	w.processHook = hook
}

// Process implements the NodeHandler interface.
func (w *WorkerNode) Process(cycle uint64, inputs [][]queue.PacketRef) error {
	w.bufferMu.Lock()
	defer w.bufferMu.Unlock()

	// Capture inputs for inspection and consume them (Sink behavior)
	w.processBuffer = nil

	// If hook is set, delegate to it
	if w.processHook != nil {
		// Hook is responsible for Free()ing if it consumes!
		// But wait, if we clear processBuffer here, we might want to fill it AFTER hook?
		// Or hook fills it?
		// Original logic: fill buffer, then call hook.
		// Let's preserve that.

		// Fill buffer with peeks (Read Only)
		for _, q := range inputs {
			for _, ref := range q {
				w.processBuffer = append(w.processBuffer, ref.Packet)
			}
		}

		return w.processHook(cycle, inputs)
	}

	// Default behavior: Sink (Consume all)
	for _, q := range inputs {
		for _, ref := range q {
			w.processBuffer = append(w.processBuffer, ref.Packet)
			// Explicitly Free the packet to remove it from InputQueue
			ref.Queue.Free(ref.Slot)
		}
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
