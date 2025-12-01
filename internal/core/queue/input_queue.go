package queue

import (
	"runtime"
	"sync"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// InputQueue handles packet reception from upstream components.
// It mirrors OutputQueue but focuses on receiving packets via AheadPort
// and exposing them through Pick/GetReceivedPackets without sending downstream.
type InputQueue struct {
	queue            *Queue
	inPort           ahead_port.AheadPort
	dummyDownstream  ahead_port.AheadPort
	processor        *ahead_port.CycleProcessor
	packetProc       *InputQueuePacketProcessor
	lastCyclePackets []packet.Packet
	onPacketReceived func(packet.Packet)
	readyOnce        sync.Once
}

// InputQueuePacketProcessor implements AheadPort PacketProcessor for InputQueue.
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

	queue := NewQueue(bufferSize, inBandwidth, outBandwidth, 1)
	inPort := ahead_port.NewAheadPort(bufferSize)
	dummyDownstream := ahead_port.NewAheadPort(1)

	iq := &InputQueue{
		queue:            queue,
		inPort:           inPort,
		dummyDownstream:  dummyDownstream,
		lastCyclePackets: make([]packet.Packet, 0),
	}
	iq.packetProc = &InputQueuePacketProcessor{
		inputQueue: iq,
	}
	iq.processor = ahead_port.NewCycleProcessor(iq.inPort, iq.dummyDownstream, iq.packetProc)
	primePortReady(iq.inPort, bufferSize)

	return iq
}

// SetInPort overrides the default upstream AheadPort.
// Recreates the internal processor to use the new port.
func (iq *InputQueue) SetInPort(port ahead_port.AheadPort) {
	if port == nil {
		return
	}

	iq.inPort = port
	iq.processor = ahead_port.NewCycleProcessor(iq.inPort, iq.dummyDownstream, iq.packetProc)
	capacity := 8
	if iq.queue != nil {
		capacity = iq.queue.Capacity()
	}
	primePortReady(iq.inPort, capacity)
}

// InPort returns the upstream AheadPort for receiving packets.
func (iq *InputQueue) InPort() ahead_port.AheadPort {
	return iq.inPort
}

// Tick processes a cycle by receiving packets from upstream and storing them internally.
func (iq *InputQueue) Tick(cycle int) error {
	if iq.processor == nil {
		iq.processor = ahead_port.NewCycleProcessor(iq.inPort, iq.dummyDownstream, iq.packetProc)
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

// EnableAlwaysReady configures the upstream port to stay ready for all future cycles.
// This is useful for scenarios that do not model backpressure (e.g., simplified network tests).
func (iq *InputQueue) EnableAlwaysReady() {
	iq.readyOnce.Do(func() {
		updater, ok := iq.inPort.(interface {
			UpdateReady(int, bool)
		})
		if !ok {
			return
		}

		capacity := 8
		if iq.queue != nil {
			capacity = iq.queue.Capacity()
		}

		go func(start int) {
			cycle := start
			for {
				updater.UpdateReady(cycle, true)
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

// ProcessPackets implements the AheadPort PacketProcessor for InputQueue.
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

func primePortReady(port ahead_port.AheadPort, limit int) {
	if limit <= 0 {
		limit = 8
	}
	if updater, ok := port.(interface{ UpdateReady(int, bool) }); ok {
		for cycle := 0; cycle <= limit; cycle++ {
			updater.UpdateReady(cycle, true)
		}
	}
}
