package queue

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// OutputQueue is a simple wrapper for buffering and sending packets to downstream.
// It acts as a bridge between Node's packet generation and the network's OutPort interface.
// This is NOT a full Queue component - it only handles output direction.
//
// Design:
// - Packets are injected via InjectPackets() by the Node
// - Packets are stored in a simple array with capacity limit
// - Tick() sends packets to downstream respecting bandwidth limits
// - Exposes an OutPort interface for network connections
type OutputQueue struct {
	// Storage
	slots    []packet.PacketWithCycle // Array storage for packets
	capacity int                      // Maximum capacity

	// Bandwidth control
	outBandwidth int // Max packets per cycle to send

	// Port for downstream connection
	outPort *outputQueueOutPort

	// Hooks
	onPacketSent func(packet.Packet)

	// Synchronization
	done     int64      // Done cycle (atomic)
	doneMu   sync.Mutex // Protects done updates
	doneCond *sync.Cond // Condition variable for WaitDone
}

// outputQueueOutPort implements OutPort interface for OutputQueue.
type outputQueueOutPort struct {
	ahead_port.BaseOutPort
	outputQueue    *OutputQueue
	pendingPackets map[int][]packet.Packet // Cached packets for future cycles
}

// GetPackets retrieves all packets for the specified cycle.
func (p *outputQueueOutPort) GetPackets(cycle int) []packet.Packet {
	// OutputQueue doesn't have upstream, no waiting needed

	// Check if we have cached packets for this cycle
	if p.pendingPackets == nil {
		p.pendingPackets = make(map[int][]packet.Packet)
	}

	if cached, ok := p.pendingPackets[cycle]; ok {
		delete(p.pendingPackets, cycle)
		return cached
	}

	// Read from channel and filter by cycle
	var result []packet.Packet
	if p.OutputChan == nil {
		return result
	}

	for {
		select {
		case pwc := <-p.OutputChan:
			if pwc.Cycle == cycle {
				result = append(result, pwc.Packet)
			} else if pwc.Cycle > cycle {
				// Future packet, cache it
				p.pendingPackets[pwc.Cycle] = append(p.pendingPackets[pwc.Cycle], pwc.Packet)
			} else {
				// Past packet - skip it (already processed or stale)
				continue
			}
		default:
			return result
		}
	}
}

// WaitDone waits for OutputQueue to complete the target cycle.
func (p *outputQueueOutPort) WaitDone(targetCycle int) {
	p.outputQueue.waitDone(targetCycle)
}

// GetDone returns OutputQueue's current done cycle.
func (p *outputQueueOutPort) GetDone() int {
	return p.outputQueue.getDone()
}

// Plug overrides BaseOutPort.Plug to pass self.
func (p *outputQueueOutPort) Plug(in ahead_port.InPort) chan ahead_port.PacketWithCycle {
	return p.BaseOutPort.PlugWithSelf(p, in)
}

// NewOutputQueue creates a new OutputQueue with the specified buffer size and bandwidth.
// inBandwidth is not used internally but validated for API consistency.
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

	oq := &OutputQueue{
		slots:        make([]packet.PacketWithCycle, 0, bufferSize),
		capacity:     bufferSize,
		outBandwidth: outBandwidth,
		done:         -1,
	}
	oq.doneCond = sync.NewCond(&oq.doneMu)

	oq.outPort = &outputQueueOutPort{
		outputQueue: oq,
	}

	return oq
}

// QueueInPort returns nil - OutputQueue doesn't have an input port.
// This method exists for API compatibility but should not be used.
func (oq *OutputQueue) QueueInPort() ahead_port.InPort {
	return nil
}

// QueueOutPort returns the OutPort for downstream connections.
func (oq *OutputQueue) QueueOutPort() ahead_port.OutPort {
	return oq.outPort
}

// Tick sends up to outBandwidth packets to downstream.
func (oq *OutputQueue) Tick(cycle int) error {
	// If no downstream connected, just mark done
	if oq.outPort.BaseOutPort.DownstreamIn == nil {
		oq.setDone(cycle)
		return nil
	}

	// Send up to outBandwidth packets
	sent := 0
	newSlots := make([]packet.PacketWithCycle, 0, len(oq.slots))

	for _, pkt := range oq.slots {
		if sent >= oq.outBandwidth {
			// Reached bandwidth limit, keep remaining packets
			newSlots = append(newSlots, pkt)
			continue
		}

		// Try to send packet (blocks on ready check, returns false if not ready)
		if oq.outPort.BaseOutPort.DownstreamIn.TrySendPacket(pkt.Cycle, pkt) {
			sent++
			if oq.onPacketSent != nil {
				oq.onPacketSent(pkt.Packet)
			}
		} else {
			// Not ready, keep packet for next cycle
			newSlots = append(newSlots, pkt)
		}
	}

	oq.slots = newSlots
	oq.setDone(cycle)
	return nil
}

// InjectPackets injects packets into the output queue for transmission.
func (oq *OutputQueue) InjectPackets(cycle int, packets []packet.Packet) error {
	for _, pkt := range packets {
		if len(oq.slots) >= oq.capacity {
			// Queue full, drop packet
			debug.Logf("OutputQueue: DROPPED packet (queue full %d/%d): Src=%d Dst=%d at cycle %d",
				len(oq.slots), oq.capacity, pkt.SourceID, pkt.TargetID, cycle)
			continue
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

// setDone sets the done cycle and broadcasts to waiters.
func (oq *OutputQueue) setDone(cycle int) {
	oq.doneMu.Lock()
	atomic.StoreInt64(&oq.done, int64(cycle))
	oq.doneCond.Broadcast()
	oq.doneMu.Unlock()
}

// getDone returns the current done cycle.
func (oq *OutputQueue) getDone() int {
	return int(atomic.LoadInt64(&oq.done))
}

// waitDone blocks until done >= targetCycle using condition variable.
func (oq *OutputQueue) waitDone(targetCycle int) {
	oq.doneMu.Lock()
	defer oq.doneMu.Unlock()

	for oq.getDone() < targetCycle {
		oq.doneCond.Wait()
	}
}

// GetVisualState returns the visual representation of this output queue.
func (oq *OutputQueue) GetVisualState() string {
	if visualization.VisualizationMode == "none" {
		return ""
	}

	if visualization.VisualizationMode == "ascii" {
		// 格式: [len/cap]
		return fmt.Sprintf("[%d/%d]", oq.Length(), oq.Capacity())
	}

	return ""
}
