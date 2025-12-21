package node

import (
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/internal/core/debug"
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

	// Only one buffer for local injection (ring traffic never buffered in router)
	injectionBuffer []packet.Packet // Buffer for local packets waiting to inject onto ring
	bufferMu        sync.Mutex      // Protects injectionBuffer
}

// NewBufferlessRingRouter creates a router node for bufferless ring topology.
//
// Parameters:
// - routerID: ID of this router node
// - workerID: ID of the connected worker node
// - bufferCapacity: internal buffer size for injection queue
func NewBufferlessRingRouter(routerID, workerID, bufferCapacity int) *BufferlessRingRouterNode {
	router := &BufferlessRingRouterNode{
		workerID:        workerID,
		bufferCapacity:  bufferCapacity,
		injectionBuffer: make([]packet.Packet, 0, bufferCapacity),
	}
	router.BaseNode = NewBaseNode(routerID, router)
	return router
}

// Process implements the NodeHandler interface.
// It replaces the old Tick logic for custom packet collection and routing.
func (r *BufferlessRingRouterNode) Process(cycle uint64, inputs [][]queue.PacketRef) error {
	if len(inputs) < 2 {
		return fmt.Errorf("router node %d: expected 2 input queues, got %d", r.id, len(inputs))
	}

	outputs := r.OutputQueues()
	if len(outputs) < 2 {
		return fmt.Errorf("router node %d: expected 2 output queues, got %d", r.id, len(outputs))
	}

	// Map inputs/outputs
	// inputs[0] is ringIn, inputs[1] is localIn
	ringInRefs := inputs[0]
	localInRefs := inputs[1]

	ringOutQueue := outputs[0]
	localOutQueue := outputs[1]

	r.bufferMu.Lock()
	defer r.bufferMu.Unlock()

	debug.Logf("Router[%d]: cycle=%d, ringIn=%d, localIn=%d, buffer=%d",
		r.id, cycle, len(ringInRefs), len(localInRefs), len(r.injectionBuffer))

	// === Priority 1: Process ring packets (ALWAYS forwarded, never buffered) ===
	for _, ref := range ringInRefs {
		pkt := ref.Packet
		// Check if this packet should be ejected to local worker
		if pkt.TargetID == r.workerID && !localOutQueue.IsFull() {
			// Eject to local worker
			debug.Logf("Router[%d]: Ejecting packet Src=%d Dst=%d to worker %d",
				r.id, pkt.SourceID, pkt.TargetID, r.workerID)
			if err := localOutQueue.InjectPackets(int(cycle), []packet.Packet{pkt}); err != nil {
				return fmt.Errorf("router %d: failed to eject packet to worker: %w", r.id, err)
			}
			ref.Queue.Free(ref.Slot)
		} else {
			// Forward to next router on ring (either wrong destination OR local busy)
			debug.Logf("Router[%d]: Forwarding packet Src=%d Dst=%d on ring (target=%d, localFull=%v)",
				r.id, pkt.SourceID, pkt.TargetID, r.workerID, localOutQueue.IsFull())
			if err := ringOutQueue.InjectPackets(int(cycle), []packet.Packet{pkt}); err != nil {
				return fmt.Errorf("router %d: failed to forward packet on ring: %w", r.id, err)
			}
			ref.Queue.Free(ref.Slot)
		}
	}

	// === Priority 2: Process buffered injection packets ===
	newInjectionBuffer := make([]packet.Packet, 0, len(r.injectionBuffer)) // Reuse capacity hint or implementation optimization
	for _, pkt := range r.injectionBuffer {
		if !ringOutQueue.IsFull() {
			debug.Logf("Router[%d]: Injecting buffered packet Src=%d Dst=%d onto ring",
				r.id, pkt.SourceID, pkt.TargetID)
			if err := ringOutQueue.InjectPackets(int(cycle), []packet.Packet{pkt}); err != nil {
				return fmt.Errorf("router %d: failed to inject buffered local packet: %w", r.id, err)
			}
		} else {
			// Still blocked, keep in buffer
			newInjectionBuffer = append(newInjectionBuffer, pkt)
		}
	}
	r.injectionBuffer = newInjectionBuffer // Optimization: This does alloc. Better to use a ring buffer or similar internally if this is hot.
	// But optimizing router internal buffer is out of scope for "BaseNode optimization", sticking to requirements.

	// === Priority 3: Process new local packets ===
	for _, ref := range localInRefs {
		pkt := ref.Packet
		if !ringOutQueue.IsFull() {
			// Inject directly onto ring
			debug.Logf("Router[%d]: Injecting local packet Src=%d Dst=%d onto ring",
				r.id, pkt.SourceID, pkt.TargetID)
			if err := ringOutQueue.InjectPackets(int(cycle), []packet.Packet{pkt}); err != nil {
				return fmt.Errorf("router %d: failed to inject local packet: %w", r.id, err)
			}
			ref.Queue.Free(ref.Slot)
		} else {
			// Ring is full, try to buffer the packet
			if len(r.injectionBuffer) < r.bufferCapacity {
				debug.Logf("Router[%d]: Buffering local packet Src=%d Dst=%d (ring full)",
					r.id, pkt.SourceID, pkt.TargetID)
				r.injectionBuffer = append(r.injectionBuffer, pkt)
				ref.Queue.Free(ref.Slot)
			} else {
				// Injection buffer full - this is a backpressure condition
				// DO NOT Free -> Packet stays in InputQueue -> Upstream sees backpressure
				debug.Logf("Router[%d]: Backpressure to local: injection buffer full", r.id)
			}
		}
	}

	return nil
}

// GetWorkerID returns the ID of the connected worker node.
func (r *BufferlessRingRouterNode) GetWorkerID() int {
	return r.workerID
}

// GetBufferOccupancy returns the current injection buffer occupancy.
func (r *BufferlessRingRouterNode) GetBufferOccupancy() int {
	return len(r.injectionBuffer)
}

// GetInjectionBufferOccupancy returns the injection buffer occupancy.
func (r *BufferlessRingRouterNode) GetInjectionBufferOccupancy() int {
	return len(r.injectionBuffer)
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
			len(r.injectionBuffer),
			r.bufferCapacity,
			r.workerID)
	}

	return ""
}
