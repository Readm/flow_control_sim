package queue

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// InputQueue handles packet reception from upstream components.
// It wraps an internal Queue and provides InPort interface for upstream connections.
// Exposes received packets through Pick/GetReceivedPackets without sending downstream.
type InputQueue struct {
	queue            *Queue
	queueInPort      ahead_port.InPort   // Internal Queue's InPort (for receiving from upstream)
	queueOutPort     ahead_port.OutPort  // Internal Queue's OutPort (for Network connections)
	processor        *InputQueueCycleProcessor
	packetProc       *InputQueuePacketProcessor
	lastCyclePackets []packet.Packet
	onPacketReceived func(packet.Packet)
	readyOnce        sync.Once
}

// InputQueuePacketProcessor handles packet processing for InputQueue.
type InputQueuePacketProcessor struct {
	inputQueue *InputQueue
}

// NewInputQueue creates a new InputQueue with the specified buffer size and bandwidth parameters.
// inBandwidth and outBandwidth must be positive, otherwise it panics.
func NewInputQueue(bufferSize int, inBandwidth int, outBandwidth int) *InputQueue {
	if bufferSize <= 0 {
		bufferSize = 8
	}
	if inBandwidth <= 0 {
		panic("inBandwidth must be positive")
	}
	if outBandwidth <= 0 {
		panic("outBandwidth must be positive")
	}

	queue, queueIn, queueOut := NewQueue(bufferSize, inBandwidth, outBandwidth, 1)

	iq := &InputQueue{
		queue:            queue,
		queueInPort:      queueIn,
		queueOutPort:     queueOut,
		lastCyclePackets: make([]packet.Packet, 0),
	}
	iq.packetProc = &InputQueuePacketProcessor{
		inputQueue: iq,
	}
	iq.processor = &InputQueueCycleProcessor{
		inputQueue: iq,
		processor:  iq.packetProc,
	}

	// Initialize Queue's ready state for initial cycles
	primeQueueReady(queue, bufferSize)

	return iq
}

// QueueInPort returns the internal Queue's InPort for Network connections.
// This is used by Network to connect Link's output to InputQueue's input.
func (iq *InputQueue) QueueInPort() ahead_port.InPort {
	return iq.queueInPort
}

// QueueOutPort returns the internal Queue's OutPort for Network connections.
// This is used by Network to connect InputQueue's output to other components.
func (iq *InputQueue) QueueOutPort() ahead_port.OutPort {
	return iq.queueOutPort
}

// AsInPort is a convenience method that returns QueueInPort.
// Kept for backward compatibility with Network code.
func (iq *InputQueue) AsInPort() ahead_port.InPort {
	return iq.queueInPort
}

// Tick processes a cycle by receiving packets from upstream and storing them internally.
func (iq *InputQueue) Tick(cycle int) error {
	if iq.processor == nil {
		return nil
	}

	return iq.processor.Tick(cycle)
}

// Pick returns packets stored in the queue in FIFO order.
func (iq *InputQueue) Pick() []packet.Packet {
	if iq.queue == nil {
		return nil
	}

	picked := iq.queue.Pick()
	if len(picked) == 0 {
		return nil
	}

	result := make([]packet.Packet, len(picked))
	for i, pkt := range picked {
		result[i] = pkt.Packet
		debug.Logf("InputQueue: Picked packet: Src=%d Dst=%d", pkt.Packet.SourceID, pkt.Packet.TargetID)
	}
	return result
}

// GetReceivedPackets returns packets received during the last Tick call.
func (iq *InputQueue) GetReceivedPackets() []packet.Packet {
	result := make([]packet.Packet, len(iq.lastCyclePackets))
	copy(result, iq.lastCyclePackets)
	return result
}

// SetPacketReceivedHook configures a hook that fires whenever
// a packet is successfully stored in the InputQueue.
func (iq *InputQueue) SetPacketReceivedHook(hook func(packet.Packet)) {
	iq.onPacketReceived = hook
}

// EnableAlwaysReady configures the queue to stay ready for all future cycles.
// This is useful for scenarios that do not model backpressure (e.g., simplified network tests).
func (iq *InputQueue) EnableAlwaysReady() {
	iq.readyOnce.Do(func() {
		if iq.queue == nil {
			return
		}

		capacity := iq.queue.Capacity()

		go func(start int) {
			cycle := start
			for {
				iq.queue.updateReady(cycle, true)
				cycle++
				if cycle%128 == 0 {
					runtime.Gosched()
					time.Sleep(0)
				}
			}
		}(capacity)
	})
}

// Length returns the number of packets currently stored in the queue.
func (iq *InputQueue) Length() int {
	if iq.queue == nil {
		return 0
	}
	return iq.queue.Length()
}

// Capacity returns the queue capacity.
func (iq *InputQueue) Capacity() int {
	if iq.queue == nil {
		return 0
	}
	return iq.queue.Capacity()
}

// IsFull reports whether the queue is at capacity.
func (iq *InputQueue) IsFull() bool {
	if iq.queue == nil {
		return false
	}
	return iq.queue.IsFull()
}

// ProcessPackets processes packets for InputQueue.
func (iqpp *InputQueuePacketProcessor) ProcessPackets(
	receiveChan <-chan ahead_port.PacketWithCycle,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(ahead_port.PacketWithCycle),
	setDone func(int),
	updateUpstreamReady func(cycle int, ready bool),
) {
	iq := iqpp.inputQueue
	if iq == nil || iq.queue == nil {
		updateUpstreamReady(cycle+1, true)
		if setDone != nil {
			setDone(cycle)
		}
		return
	}

	var received []packet.Packet

	for {
		select {
		case pkt := <-receiveChan:
			slot := iq.queue.findFreeSlot()
			if slot >= 0 {
				iq.queue.arrayMu.Lock()
				iq.queue.slots[slot] = packet.PacketWithCycle(pkt)
				iq.queue.freeBitmap[slot] = false
				iq.queue.blockReasons[slot] = 0
				iq.queue.arrayMu.Unlock()
				received = append(received, pkt.Packet)
				if iq.onPacketReceived != nil {
					iq.onPacketReceived(pkt.Packet)
				}
			}
		default:
			goto done
		}
	}

done:
	iq.lastCyclePackets = received

	if setDone != nil {
		setDone(cycle)
	}

	hasCapacity := true
	if iq.queue != nil {
		hasCapacity = iq.queue.Length() < iq.queue.Capacity()
	}

	updateUpstreamReady(cycle+1, hasCapacity)
}

// primeQueueReady initializes the queue's ready state for initial cycles.
func primeQueueReady(queue *Queue, limit int) {
	if queue == nil {
		return
	}
	if limit <= 0 {
		limit = 8
	}
	for cycle := 0; cycle <= limit; cycle++ {
		queue.updateReady(cycle, true)
	}
}

// InputQueueCycleProcessor is a custom cycle processor for InputQueue.
type InputQueueCycleProcessor struct {
	inputQueue *InputQueue
	processor  *InputQueuePacketProcessor
}

// Tick implements the cycle processing workflow for InputQueue.
func (iqcp *InputQueueCycleProcessor) Tick(cycle int) error {
	if iqcp.processor == nil {
		panic("InputQueueCycleProcessor.processor is nil")
	}

	iq := iqcp.inputQueue
	queue := iq.queue

	// Wait for upstream Done >= cycle-1
	if iq.queueInPort != nil {
		if baseIn, ok := iq.queueInPort.(*queueInPort); ok && baseIn.UpstreamOut != nil {
			baseIn.UpstreamOut.WaitDone(cycle - 1)
		}
	}

	// Prepare updateUpstreamReady function
	updateUpstreamReady := func(c int, ready bool) {
		if queue != nil {
			queue.updateReady(c, ready)
		}
	}

	// Get receive channel
	var receiveChan <-chan ahead_port.PacketWithCycle
	if iq.queueInPort != nil {
		if baseIn, ok := iq.queueInPort.(*queueInPort); ok && baseIn.InputChan != nil {
			receiveChan = baseIn.InputChan
		}
	}
	if receiveChan == nil {
		receiveChan = make(chan ahead_port.PacketWithCycle)
	}

	// checkReady - InputQueue doesn't send downstream, always ready
	checkReady := func(int) bool {
		return true
	}

	// sendPacket - InputQueue doesn't send packets downstream
	sendPacket := func(ahead_port.PacketWithCycle) {}

	// setDone function
	setDone := func(c int) {
		if queue != nil {
			queue.setDone(c)
		}
	}

	// Call PacketProcessor
	iqcp.processor.ProcessPackets(
		receiveChan,
		cycle,
		checkReady,
		sendPacket,
		setDone,
		updateUpstreamReady,
	)

	// Ensure Done state is correct
	if queue != nil {
		currentDone := queue.getDone()
		if currentDone < cycle {
			queue.setDone(cycle)
		}
	}

	return nil
}

// GetVisualState returns the visual representation of this input queue.
func (iq *InputQueue) GetVisualState() string {
	if visualization.VisualizationMode == "none" {
		return ""
	}

	if visualization.VisualizationMode == "ascii" {
		// 格式: [len/cap]
		return fmt.Sprintf("[%d/%d]", iq.Length(), iq.Capacity())
	}

	return ""
}
