package pipeline

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// OutputQueue handles packet transmission from processed packets to downstream.
// It wraps Queue and uses its Tick method directly, skipping the receive step
// since packets are injected via InjectPackets.
type OutputQueue struct {
	queue        *Queue
	outPort      ahead_port.AheadPort
	emptyUpstream ahead_port.AheadPort // Empty upstream port (no packets from channel)
}

// NewOutputQueue creates a new OutputQueue with the specified buffer size.
func NewOutputQueue(bufferSize int) *OutputQueue {
	if bufferSize <= 0 {
		bufferSize = 8
	}

	// Create internal queue for out_queue logic
	// outBandwidth is set to 1 to match typical Link bandwidth limits
	queue := NewQueue(bufferSize, 1, 1, 1)

	// Create empty upstream port (packets are injected directly, not from channel)
	emptyUpstream := ahead_port.NewAheadPort(1)
	// Set upstream port to empty port so Queue won't receive from channel
	queue.SetUpstreamPort(emptyUpstream)

	return &OutputQueue{
		queue:        queue,
		outPort:      nil,
		emptyUpstream: emptyUpstream,
	}
}

// SetOutPort sets the output port for downstream transmission.
func (oq *OutputQueue) SetOutPort(port ahead_port.AheadPort) {
	oq.outPort = port
	if port != nil {
		oq.queue.SetDownstreamPort(port)
	}
}

// OutPort returns the output port.
func (oq *OutputQueue) OutPort() ahead_port.AheadPort {
	return oq.outPort
}

// Tick processes a single cycle, sending packets from queue to outPort.
// It directly uses Queue's Tick method, which handles all the sending logic.
func (oq *OutputQueue) Tick(cycle int) error {
	if oq.outPort == nil {
		return nil
	}

	// Initialize empty upstream Done to avoid blocking
	oq.emptyUpstream.SetDone(cycle - 1)

	// Use Queue's Tick method directly - it will skip receiving (empty channel)
	// and only process sending packets from array to downstream
	return oq.queue.Tick(cycle)
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

