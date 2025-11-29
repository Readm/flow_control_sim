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
		queue:         queue,
		outPort:       nil,
		emptyUpstream: emptyUpstream,
	}
}

// SetOutPort sets the output port for downstream transmission.
func (oq *OutputQueue) SetOutPort(port ahead_port.AheadPort) {
	oq.outPort = port
	oq.updateDownstreamPort()
}

// OutPort returns the output port.
func (oq *OutputQueue) OutPort() ahead_port.AheadPort {
	return oq.outPort
}

// SetPacketSentHook configures a hook invoked whenever a packet leaves OutputQueue.
func (oq *OutputQueue) SetPacketSentHook(hook func(packet.Packet)) {
	oq.onPacketSent = hook
	oq.updateDownstreamPort()
}

func (oq *OutputQueue) updateDownstreamPort() {
	if oq.queue == nil {
		return
	}
	if oq.outPort == nil {
		oq.queue.SetDownstreamPort(nil)
		return
	}
	if oq.onPacketSent == nil {
		oq.queue.SetDownstreamPort(oq.outPort)
		return
	}
	oq.queue.SetDownstreamPort(newHookedDownstreamPort(oq.outPort, oq.onPacketSent))
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

// hookedDownstreamPort wraps an AheadPort to intercept packets leaving OutputQueue.
type hookedDownstreamPort struct {
	target   ahead_port.AheadPort
	hook     func(packet.Packet)
	sendChan chan ahead_port.PacketWithCycle
}

func newHookedDownstreamPort(target ahead_port.AheadPort, hook func(packet.Packet)) ahead_port.AheadPort {
	if hook == nil || target == nil {
		return target
	}

	hp := &hookedDownstreamPort{
		target:   target,
		hook:     hook,
		sendChan: make(chan ahead_port.PacketWithCycle),
	}

	go hp.forward()
	return hp
}

func (hp *hookedDownstreamPort) forward() {
	for pkt := range hp.sendChan {
		if hp.hook != nil {
			hp.hook(pkt.Packet)
		}
		hp.target.SendChan() <- pkt
	}
}

func (hp *hookedDownstreamPort) SetDone(cycle int) {
	hp.target.SetDone(cycle)
}

func (hp *hookedDownstreamPort) GetDone() int {
	return hp.target.GetDone()
}

func (hp *hookedDownstreamPort) SendChan() chan<- ahead_port.PacketWithCycle {
	return hp.sendChan
}

func (hp *hookedDownstreamPort) ReceiveChan() <-chan ahead_port.PacketWithCycle {
	return hp.target.ReceiveChan()
}

func (hp *hookedDownstreamPort) Ready(cycle int) bool {
	return hp.target.Ready(cycle)
}

func (hp *hookedDownstreamPort) ReadyNonBlocking(cycle int) (bool, bool) {
	return hp.target.ReadyNonBlocking(cycle)
}

func (hp *hookedDownstreamPort) WaitForDone(targetCycle int) {
	hp.target.WaitForDone(targetCycle)
}

func (hp *hookedDownstreamPort) SetPacketTypes(types []int) {
	hp.target.SetPacketTypes(types)
}

func (hp *hookedDownstreamPort) PacketTypes() []int {
	return hp.target.PacketTypes()
}
