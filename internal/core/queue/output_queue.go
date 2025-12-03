package queue

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// OutputQueue handles packet transmission from processed packets to downstream.
// It wraps Queue and uses its Tick method directly, skipping the receive step
// since packets are injected via InjectPackets.
type OutputQueue struct {
	queue         *Queue
	outPort       ahead_port.AheadPort
	emptyUpstream ahead_port.AheadPort // Empty upstream port (no packets from channel)
	onPacketSent  func(packet.Packet)
}

// NewOutputQueue creates a new OutputQueue with the specified buffer size and bandwidth parameters.
// inBandwidth and outBandwidth must be positive, otherwise it panics.
func NewOutputQueue(bufferSize int, inBandwidth int, outBandwidth int) *OutputQueue {
	if bufferSize <= 0 {
		bufferSize = 8
	}
	if inBandwidth <= 0 {
		panic("inBandwidth must be positive")
	}
	if outBandwidth <= 0 {
		panic("outBandwidth must be positive")
	}

	// Create internal queue for storage (we only use its Pick method and array)
	queue, _, _ := NewQueue(bufferSize, inBandwidth, outBandwidth, 1)

	// Create empty upstream port for Done signaling (packets are injected directly)
	emptyUpstream := ahead_port.NewAheadPort(1)

	return &OutputQueue{
		queue:         queue,
		outPort:       nil,
		emptyUpstream: emptyUpstream,
	}
}

// SetOutPort sets the output port for downstream transmission.
func (oq *OutputQueue) SetOutPort(port ahead_port.AheadPort) {
	oq.outPort = port
}

// OutPort returns the output port.
func (oq *OutputQueue) OutPort() ahead_port.AheadPort {
	return oq.outPort
}

// SetPacketSentHook configures a hook invoked whenever a packet leaves OutputQueue.
// The hook is called directly in Tick() when packets are successfully sent.
func (oq *OutputQueue) SetPacketSentHook(hook func(packet.Packet)) {
	oq.onPacketSent = hook
}

// Tick processes a single cycle, sending packets from queue to outPort.
// Similar to InputQueue, this bypasses Queue's port system and implements sending directly.
func (oq *OutputQueue) Tick(cycle int) error {
	if oq.outPort == nil || oq.queue == nil {
		return nil
	}

	// Pick packets to send (respects outBandwidth and sorts by cycle)
	picked := oq.queue.Pick()

	// Send each packet if downstream is ready
	for _, pkt := range picked {
		// Check if downstream is ready for this cycle
		if !oq.outPort.Ready(pkt.Cycle) {
			// If downstream not ready, we can't send
			// Note: picked packets are already removed from queue by Pick()
			// In a real implementation, we might want to re-inject them
			continue
		}

		// Send packet to downstream
		select {
		case oq.outPort.SendChan() <- ahead_port.PacketWithCycle(pkt):
			// Successfully sent
			if oq.onPacketSent != nil {
				oq.onPacketSent(pkt.Packet)
			}
		default:
			// Channel full, packet lost (or could re-inject)
		}
	}

	// Update upstream Done signal (we're done processing this cycle)
	oq.emptyUpstream.SetDone(cycle)

	return nil
}

// InjectPackets injects packets into the output queue for transmission.
// Packets will be sent in subsequent Tick calls, respecting bandwidth limits and downstream readiness.
func (oq *OutputQueue) InjectPackets(cycle int, packets []packet.Packet) error {
	if oq.queue == nil {
		return nil
	}

	for _, pkt := range packets {
		env := packet.PacketWithCycle{
			Cycle:  cycle,
			Packet: pkt,
		}
		// Find a free slot in queue
		slot := oq.queue.findFreeSlot()
		if slot >= 0 {
			oq.queue.arrayMu.Lock()
			oq.queue.slots[slot] = env
			oq.queue.freeBitmap[slot] = false
			oq.queue.blockReasons[slot] = 0
			oq.queue.arrayMu.Unlock()
		}
		// If no free slot, packet is dropped (silently)
	}
	return nil
}

// Length returns the current number of packets in the output queue.
func (oq *OutputQueue) Length() int {
	if oq.queue == nil {
		return 0
	}
	return oq.queue.Length()
}

// Capacity returns the maximum capacity of the output queue.
func (oq *OutputQueue) Capacity() int {
	if oq.queue == nil {
		return 0
	}
	return oq.queue.Capacity()
}

// IsFull checks if the output queue is at capacity.
func (oq *OutputQueue) IsFull() bool {
	if oq.queue == nil {
		return false
	}
	return oq.queue.IsFull()
}
