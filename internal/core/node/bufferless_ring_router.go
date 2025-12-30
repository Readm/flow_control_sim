package node

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// BufferlessRingRouterNode implements a router for bufferless ring topology.
//
// Architecture:
// - 4 ports: ringIn, ringOut, localIn, localOut
// - Internal buffer ONLY for local injection (ring traffic never buffered)
// - Routing logic: check packet.TargetID to decide ejection vs. forwarding
//
// Key Design Principles:
// - Ring traffic NEVER buffered in router (always forwarded immediately)
// - Packets that cannot eject continue on ring (loop around)
// - Only local injection needs buffering (when ring temporarily busy)
// - Backpressure only (no packet dropping)
//
// Queue Assignment:
// - inputs[0]: ringInQueue (from previous router)
// - inputs[1]: localInQueue (from local worker)
// - outputs[0]: ringOutQueue (to next router)
// - outputs[1]: localOutQueue (to local worker)
type BufferlessRingRouterNode struct {
	*BaseNode // Embed BaseNode for base functionality

	workerID       int // ID of the connected worker node
	bufferCapacity int // Internal buffer capacity
	bufferMask     int // Bitmask for fast modulo (capacity - 1)

	// Ring buffer for local injection (zero-allocation, lock-free)
	injectionBuffer []packet.Packet // Pre-allocated buffer
	bufferHead      int             // Read index
	bufferTail      int             // Write index
	bufferSize      int             // Current occupancy

	// Pre-allocated temporary slices for batch operations
	tempForward []packet.Packet // Reused for forwarding packets
	tempEject   []packet.Packet // Reused for ejecting packets
	tempInject  []packet.Packet // Reused for injecting packets

	// Cached output queue references (set on first Process call)
	ringOutQueue  OutputQueue
	localOutQueue OutputQueue
}

// NewBufferlessRingRouter creates a router node for bufferless ring topology.
//
// Parameters:
// - routerID: ID of this router node
// - workerID: ID of the connected worker node
// - bufferCapacity: internal buffer size for injection queue (should be power of 2 for optimal performance)
func NewBufferlessRingRouter(routerID, workerID, bufferCapacity int) *BufferlessRingRouterNode {
	// Ensure capacity is power of 2 for fast modulo via bitmask
	capacity := bufferCapacity
	if capacity&(capacity-1) != 0 {
		// Round up to next power of 2
		capacity = 1
		for capacity < bufferCapacity {
			capacity <<= 1
		}
	}

	router := &BufferlessRingRouterNode{
		workerID:        workerID,
		bufferCapacity:  capacity,
		bufferMask:      capacity - 1,                        // For fast modulo: index & mask
		injectionBuffer: make([]packet.Packet, capacity),     // Pre-allocate full capacity
		bufferHead:      0,
		bufferTail:      0,
		bufferSize:      0,
		tempForward:     make([]packet.Packet, 0, 64), // Pre-allocate with reasonable capacity
		tempEject:       make([]packet.Packet, 0, 64),
		tempInject:      make([]packet.Packet, 0, 64),
	}
	router.BaseNode = NewBaseNode(routerID, router)
	return router
}

// Process implements the NodeHandler interface.
// Highly optimized version with zero-allocation, batch operations, and cached references.
func (r *BufferlessRingRouterNode) Process(cycle uint64, inputs [][]queue.PacketRef) error {
	// Cache output queues on first call (avoid OutputQueues() slice copy every cycle)
	if r.ringOutQueue == nil {
		outputs := r.OutputQueues()
		r.ringOutQueue = outputs[0]
		r.localOutQueue = outputs[1]
	}

	// Direct references (no slice copies)
	ringInRefs := inputs[0]
	localInRefs := inputs[1]
	ringOutQueue := r.ringOutQueue
	localOutQueue := r.localOutQueue

	// Cache IsFull results to avoid repeated checks
	ringOutFull := ringOutQueue.IsFull()
	localOutFull := localOutQueue.IsFull()

	// Reset temp slices (reuse allocated capacity, zero length)
	r.tempForward = r.tempForward[:0]
	r.tempEject = r.tempEject[:0]
	r.tempInject = r.tempInject[:0]

	// === Priority 1: Process ring packets (classify into forward/eject) ===
	for _, ref := range ringInRefs {
		pkt := ref.Packet
		if pkt.TargetID == r.workerID && !localOutFull {
			// Eject to local worker
			r.tempEject = append(r.tempEject, pkt)
			ref.Queue.Free(ref.Slot)
		} else {
			// Forward on ring
			r.tempForward = append(r.tempForward, pkt)
			ref.Queue.Free(ref.Slot)
		}
	}

	// === Priority 2: Process buffered injection packets ===
	if r.bufferSize > 0 && !ringOutFull {
		// Try to inject buffered packets
		for r.bufferSize > 0 {
			pkt := r.injectionBuffer[r.bufferHead]
			r.tempInject = append(r.tempInject, pkt)
			r.bufferHead = (r.bufferHead + 1) & r.bufferMask // Fast modulo via bitmask
			r.bufferSize--
		}
	}

	// === Priority 3: Process new local packets ===
	for _, ref := range localInRefs {
		pkt := ref.Packet
		if !ringOutFull || r.bufferSize < r.bufferCapacity {
			if !ringOutFull {
				// Inject directly
				r.tempInject = append(r.tempInject, pkt)
				ref.Queue.Free(ref.Slot)
			} else {
				// Buffer it (ring is full but buffer has space)
				r.injectionBuffer[r.bufferTail] = pkt
				r.bufferTail = (r.bufferTail + 1) & r.bufferMask // Fast modulo via bitmask
				r.bufferSize++
				ref.Queue.Free(ref.Slot)
			}
		}
		// else: backpressure - DO NOT Free, packet stays in InputQueue
	}

	// === Batch inject all packets ===
	iCycle := int(cycle)

	if len(r.tempEject) > 0 {
		localOutQueue.InjectPackets(iCycle, r.tempEject)
	}

	// Combine forward + inject into ringOut
	if len(r.tempForward) > 0 || len(r.tempInject) > 0 {
		// Merge into tempForward to reuse one batch call
		r.tempForward = append(r.tempForward, r.tempInject...)
		ringOutQueue.InjectPackets(iCycle, r.tempForward)
	}

	return nil
}

// GetWorkerID returns the ID of the connected worker node.
func (r *BufferlessRingRouterNode) GetWorkerID() int {
	return r.workerID
}

// GetBufferOccupancy returns the current injection buffer occupancy.
func (r *BufferlessRingRouterNode) GetBufferOccupancy() int {
	return r.bufferSize
}

// GetInjectionBufferOccupancy returns the injection buffer occupancy.
func (r *BufferlessRingRouterNode) GetInjectionBufferOccupancy() int {
	return r.bufferSize
}

// GetBufferCapacity returns the buffer capacity.
func (r *BufferlessRingRouterNode) GetBufferCapacity() int {
	return r.bufferCapacity
}

// GetVisualState returns the visual representation of this router.
// Format depends on global visualization.VisualizationMode.
func (r *BufferlessRingRouterNode) GetVisualState() string {
	if visualization.VisualizationMode == "none" {
		return ""
	}

	if visualization.VisualizationMode == "ascii" {
		// 格式: R<routerID>[buf:<占用>/<容量>] W<workerID>
		return fmt.Sprintf("R%d[%d/%d]W%d",
			r.id-100, // Router ID (显示为0-3而不是100-103)
			r.bufferSize,
			r.bufferCapacity,
			r.workerID)
	}

	return ""
}
