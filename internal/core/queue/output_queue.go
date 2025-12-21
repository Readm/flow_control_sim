package queue

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// OutputQueue is a simple wrapper for buffering and sending packets to downstream.
// It acts as a bridge between Node's packet generation and the network.
// This is NOT a full Queue component - it only handles output direction.
//
// Design:
// - Packets are injected via InjectPackets() by the Node
// - Packets are stored in a fixed-size ring buffer for zero-alloc operation
// - Tick() sends packets to downstream respecting bandwidth limits using strict FIFO
type OutputQueue struct {
	// ===== Port reference (not owned) =====
	toDownstream ahead_port.InPort // Send to downstream

	// ===== Ring Buffer Storage =====
	buffer   []packet.PacketWithCycle // Fixed size buffer
	head     int                      // Read index
	tail     int                      // Write index
	count    int                      // Current item count
	capacity int                      // Maximum capacity

	// ===== Configuration parameters =====
	outBandwidth int // Max packets per cycle to send

	// ===== Hooks =====
	onPacketSent func(packet.Packet)
}

// NewOutputQueue creates a new OutputQueue with the specified capacity and bandwidth.
// Port must be set separately using SetDownstreamPort, or via Connect().
// Note: inBandwidth parameter has been removed as it was unused.
func NewOutputQueue(capacity int, outBandwidth int) *OutputQueue {
	if capacity <= 0 {
		capacity = 8
	}
	if outBandwidth <= 0 {
		panic("outBandwidth must be positive")
	}

	return &OutputQueue{
		buffer:       make([]packet.PacketWithCycle, capacity),
		capacity:     capacity,
		outBandwidth: outBandwidth,
		head:         0,
		tail:         0,
		count:        0,
	}
}

// SetDownstreamPort sets the port for sending data to downstream.
// OutputQueue acts as upstream for this port.
func (oq *OutputQueue) SetDownstreamPort(port ahead_port.InPort) {
	oq.toDownstream = port
}

// Tick sends up to outBandwidth packets to downstream.
// Implements strict FIFO sending with Head-of-Line blocking.
func (oq *OutputQueue) Tick(cycle int) error {
	// If no downstream connected, nothing to do
	if oq.toDownstream == nil {
		return nil
	}

	// ===== 1. Send up to outBandwidth packets =====
	sent := 0

	// Loop as long as we have packets and haven't reached bandwidth limit
	for sent < oq.outBandwidth && oq.count > 0 {
		// Peek at the packet at head
		pkt := oq.buffer[oq.head]

		pwc := ahead_port.PacketWithCycle{
			Cycle:  cycle,
			Packet: pkt.Packet,
		}

		// Try to send packet
		if oq.toDownstream.TrySend(cycle, pwc) {
			// Success: Advance head, decrement count
			oq.head = (oq.head + 1) % oq.capacity
			oq.count--
			sent++

			if oq.onPacketSent != nil {
				oq.onPacketSent(pkt.Packet)
			}
			debug.Logf("OutputQueue: Sent packet: Src=%d Dst=%d at cycle %d", pkt.Packet.SourceID, pkt.Packet.TargetID, cycle)
		} else {
			// Failed: Downstream not ready.
			// strict FIFO means we MUST STOP here. We cannot skip this packet to send others.
			// Head remains at current position.
			debug.Logf("OutputQueue: Downstream not ready, buffered packet: Src=%d Dst=%d at cycle %d", pkt.Packet.SourceID, pkt.Packet.TargetID, cycle)
			break
		}
	}

	// ===== 2. Mark this cycle as done for downstream =====
	oq.toDownstream.MarkDone(cycle)

	return nil
}

// InjectPackets injects packets into the output queue for transmission.
// Atomic operation: either all packets are injected, or none if capacity is insufficient.
func (oq *OutputQueue) InjectPackets(cycle int, packets []packet.Packet) error {
	if len(packets) == 0 {
		return nil
	}

	// 1. Check capacity for ALL packets first
	if oq.count+len(packets) > oq.capacity {
		return fmt.Errorf("OutputQueue: capacity exceeded (%d + %d > %d), cannot inject %d packets",
			oq.count, len(packets), oq.capacity, len(packets))
	}

	// 2. Inject all packets
	for _, pkt := range packets {
		debug.Logf("OutputQueue: Injected packet: Src=%d Dst=%d at cycle %d (queue: %d/%d)",
			pkt.SourceID, pkt.TargetID, cycle, oq.count+1, oq.capacity)

		oq.buffer[oq.tail] = packet.PacketWithCycle{
			Cycle:  cycle,
			Packet: pkt,
		}
		oq.tail = (oq.tail + 1) % oq.capacity
		oq.count++
	}

	return nil
}

// Length returns the current number of packets in the queue.
func (oq *OutputQueue) Length() int {
	return oq.count
}

// Capacity returns the maximum capacity.
func (oq *OutputQueue) Capacity() int {
	return oq.capacity
}

// IsFull checks if the queue is at capacity.
func (oq *OutputQueue) IsFull() bool {
	return oq.count >= oq.capacity
}

// SetPacketSentHook configures a hook invoked when packets are sent.
func (oq *OutputQueue) SetPacketSentHook(hook func(packet.Packet)) {
	oq.onPacketSent = hook
}

// GetVisualState returns the visual representation of this output queue.
func (oq *OutputQueue) GetVisualState() string {
	if visualization.VisualizationMode == "none" {
		return ""
	}

	if visualization.VisualizationMode == "ascii" {
		return fmt.Sprintf("[%d/%d]", oq.Length(), oq.Capacity())
	}

	return ""
}
