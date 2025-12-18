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
// - Packets are stored in a simple array with capacity limit
// - Tick() sends packets to downstream respecting bandwidth limits
type OutputQueue struct {
	// ===== Port reference (not owned) =====
	toDownstream ahead_port.InPort // Send to downstream

	// ===== Storage =====
	slots    []packet.PacketWithCycle // Array storage for packets
	capacity int                      // Maximum capacity

	// ===== Configuration parameters =====
	outBandwidth int // Max packets per cycle to send

	// ===== Hooks =====
	onPacketSent func(packet.Packet)
}

// NewOutputQueue creates a new OutputQueue with the specified capacity and bandwidth.
// Port must be set separately using SetDownstreamPort, or via Connect().
func NewOutputQueue(capacity int, inBandwidth int, outBandwidth int) *OutputQueue {
	if capacity <= 0 {
		capacity = 8
	}
	if inBandwidth <= 0 {
		panic("inBandwidth must be positive")
	}
	if outBandwidth <= 0 {
		panic("outBandwidth must be positive")
	}

	return &OutputQueue{
		slots:        make([]packet.PacketWithCycle, 0, capacity),
		capacity:     capacity,
		outBandwidth: outBandwidth,
	}
}

// SetDownstreamPort sets the port for sending data to downstream.
// OutputQueue acts as upstream for this port.
func (oq *OutputQueue) SetDownstreamPort(port ahead_port.InPort) {
	oq.toDownstream = port
}

// Tick sends up to outBandwidth packets to downstream.
// This is dramatically simpler than the old implementation because Port handles all synchronization.
func (oq *OutputQueue) Tick(cycle int) error {
	// If no downstream connected, nothing to do
	if oq.toDownstream == nil {
		return nil
	}

	// ===== 1. Send up to outBandwidth packets =====
	sent := 0
	newSlots := make([]packet.PacketWithCycle, 0, len(oq.slots))

	for _, pkt := range oq.slots {
		if sent >= oq.outBandwidth {
			// Reached bandwidth limit, keep remaining packets
			newSlots = append(newSlots, pkt)
			continue
		}

		// Try to send packet
		pwc := ahead_port.PacketWithCycle{
			Cycle:  cycle,
			Packet: pkt.Packet,
		}
		if oq.toDownstream.TrySend(cycle, pwc) {
			sent++
			if oq.onPacketSent != nil {
				oq.onPacketSent(pkt.Packet)
			}
			debug.Logf("OutputQueue: Sent packet: Src=%d Dst=%d at cycle %d", pkt.Packet.SourceID, pkt.Packet.TargetID, cycle)
		} else {
			// Not ready, keep packet for next cycle
			newSlots = append(newSlots, pkt)
			debug.Logf("OutputQueue: Downstream not ready, buffered packet: Src=%d Dst=%d at cycle %d", pkt.Packet.SourceID, pkt.Packet.TargetID, cycle)
		}
	}

	oq.slots = newSlots

	// ===== 2. Mark this cycle as done for downstream =====
	oq.toDownstream.MarkDone(cycle)

	return nil
}

// InjectPackets injects packets into the output queue for transmission.
func (oq *OutputQueue) InjectPackets(cycle int, packets []packet.Packet) error {
	for _, pkt := range packets {
		if len(oq.slots) >= oq.capacity {
			return fmt.Errorf("OutputQueue: capacity exceeded (%d/%d), cannot inject packet Src=%d Dst=%d",
				len(oq.slots), oq.capacity, pkt.SourceID, pkt.TargetID)
		}

		debug.Logf("OutputQueue: Injected packet: Src=%d Dst=%d at cycle %d (queue: %d/%d)",
			pkt.SourceID, pkt.TargetID, cycle, len(oq.slots)+1, oq.capacity)

		oq.slots = append(oq.slots, packet.PacketWithCycle{
			Cycle:  cycle,
			Packet: pkt,
		})
	}
	return nil
}

// Length returns the current number of packets in the queue.
func (oq *OutputQueue) Length() int {
	return len(oq.slots)
}

// Capacity returns the maximum capacity.
func (oq *OutputQueue) Capacity() int {
	return oq.capacity
}

// IsFull checks if the queue is at capacity.
func (oq *OutputQueue) IsFull() bool {
	return len(oq.slots) >= oq.capacity
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
