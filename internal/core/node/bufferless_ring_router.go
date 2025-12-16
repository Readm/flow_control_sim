package node

import (
	"context"
	"fmt"
	"time"

	"github.com/Readm/flow_sim/internal/core/debug"
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
	*Node // Embed Node for base functionality

	workerID       int                // ID of the connected worker node
	bufferCapacity int                // Internal buffer capacity

	// Only one buffer for local injection (ring traffic never buffered in router)
	injectionBuffer []packet.Packet   // Buffer for local packets waiting to inject onto ring
}

// NewBufferlessRingRouter creates a router node for bufferless ring topology.
//
// Parameters:
// - routerID: ID of this router node
// - workerID: ID of the connected worker node
// - bufferCapacity: internal buffer size for injection queue
func NewBufferlessRingRouter(routerID, workerID, bufferCapacity int) *BufferlessRingRouterNode {
	baseNode := New(routerID)

	router := &BufferlessRingRouterNode{
		Node:            baseNode,
		workerID:        workerID,
		bufferCapacity:  bufferCapacity,
		injectionBuffer: make([]packet.Packet, 0, bufferCapacity),
	}

	return router
}

// Tick overrides Node's Tick to implement custom packet collection and routing.
// This is necessary because the Router needs to distinguish between ring and local traffic,
// which requires picking from input queues separately rather than using Node's collectPackets().
func (r *BufferlessRingRouterNode) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
	inputs := r.Node.InputQueues()
	outputs := r.Node.OutputQueues()

	if len(inputs) < 2 {
		return fmt.Errorf("router node %d: expected 2 input queues, got %d", r.id, len(inputs))
	}
	if len(outputs) < 2 {
		return fmt.Errorf("router node %d: expected 2 output queues, got %d", r.id, len(outputs))
	}

	ringInQueue := inputs[0]
	localInQueue := inputs[1]
	ringOutQueue := outputs[0]
	localOutQueue := outputs[1]

	// Collect packets from each input separately to distinguish ring vs local traffic
	ringInPackets := ringInQueue.Pick()
	localInPackets := localInQueue.Pick()

	debug.Logf("Router[%d]: cycle=%d, ringIn=%d, localIn=%d, buffer=%d",
		r.id, cycle, len(ringInPackets), len(localInPackets), len(r.injectionBuffer))

	// === Priority 1: Process ring packets (ALWAYS forwarded, never buffered) ===
	for _, pkt := range ringInPackets {
		// Check if this packet should be ejected to local worker
		if pkt.TargetID == r.workerID && !localOutQueue.IsFull() {
			// Eject to local worker
			debug.Logf("Router[%d]: Ejecting packet Src=%d Dst=%d to worker %d",
				r.id, pkt.SourceID, pkt.TargetID, r.workerID)
			if err := localOutQueue.InjectPackets(int(cycle), []packet.Packet{pkt}); err != nil {
				return fmt.Errorf("router %d: failed to eject packet to worker: %w", r.id, err)
			}
		} else {
			// Forward to next router on ring (either wrong destination OR local busy)
			debug.Logf("Router[%d]: Forwarding packet Src=%d Dst=%d on ring (target=%d, localFull=%v)",
				r.id, pkt.SourceID, pkt.TargetID, r.workerID, localOutQueue.IsFull())
			if err := ringOutQueue.InjectPackets(int(cycle), []packet.Packet{pkt}); err != nil {
				return fmt.Errorf("router %d: failed to forward packet on ring: %w", r.id, err)
			}
		}
	}

	// === Priority 2: Process buffered injection packets ===
	newInjectionBuffer := make([]packet.Packet, 0)
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
	r.injectionBuffer = newInjectionBuffer

	// === Priority 3: Process new local packets ===
	for _, pkt := range localInPackets {
		if !ringOutQueue.IsFull() {
			// Inject directly onto ring
			debug.Logf("Router[%d]: Injecting local packet Src=%d Dst=%d onto ring",
				r.id, pkt.SourceID, pkt.TargetID)
			if err := ringOutQueue.InjectPackets(int(cycle), []packet.Packet{pkt}); err != nil {
				return fmt.Errorf("router %d: failed to inject local packet: %w", r.id, err)
			}
		} else {
			// Ring is full, buffer the packet
			if len(r.injectionBuffer) < r.bufferCapacity {
				debug.Logf("Router[%d]: Buffering local packet Src=%d Dst=%d (ring full)",
					r.id, pkt.SourceID, pkt.TargetID)
				r.injectionBuffer = append(r.injectionBuffer, pkt)
			} else {
				// Injection buffer full - this is a backpressure condition
				return fmt.Errorf("router %d: injection buffer full, cannot accept more local packets", r.id)
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
