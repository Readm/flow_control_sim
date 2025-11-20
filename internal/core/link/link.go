package link

import (
	"github.com/Readm/flow_sim/internal/dataflow/flow"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Link represents a directed edge in the topology。每条链路拥有独立的 cycle 延迟和 slot。
// Bandwidth limits how many packets each slot can hold and how many packets can be received per cycle.
type Link struct {
	sourceID            int
	targetID            int
	target              flow.Flow
	sourceFlow          flow.Flow          // Source flow (for reading from dispatch queue)
	dispatchQueueIndex  int                // Index of the dispatch queue in source flow
	latency             uint64
	bandwidth           uint64
	slotCount           uint64
	slots               [][]packet.Packet
	backpressured       bool
	currentCycle        uint64
	sendFinishedCycle   uint64
	noBackpressureUntil uint64
}

// NewLink creates a link between source and target.
// - sourceID: ID of the source node
// - target: target flow (receiver)
// - sourceFlow: source flow (sender, for reading from dispatch queue)
// - dispatchQueueIndex: index of the dispatch queue in source flow
// - latency: number of cycles for packet delivery (also determines slot count)
// - bandwidth: maximum packets per slot and per cycle (defaults to 1 if 0)
// - slotCount: number of slots (defaults to latency if 0)
// Design: slotCount = latency, each slot can hold up to bandwidth packets.
func NewLink(sourceID int, target flow.Flow, sourceFlow flow.Flow, dispatchQueueIndex int, latency uint64, bandwidth uint64, slotCount uint64) *Link {
	if latency == 0 {
		latency = 1
	}
	if bandwidth == 0 {
		bandwidth = 1
	}
	if slotCount == 0 {
		slotCount = latency
	}
	slots := make([][]packet.Packet, slotCount)
	return &Link{
		sourceID:            sourceID,
		targetID:            target.ID(),
		target:              target,
		sourceFlow:          sourceFlow,
		dispatchQueueIndex:  dispatchQueueIndex,
		latency:             latency,
		bandwidth:           bandwidth,
		slotCount:           slotCount,
		slots:               slots,
		noBackpressureUntil: 1000000, // Default: no backpressure for a long time
	}
}

// SourceID returns the ID of the upstream node.
func (l *Link) SourceID() int {
	return l.sourceID
}

// TargetID returns the ID of the downstream node.
func (l *Link) TargetID() int {
	return l.targetID
}

// Latency returns the configured delay in cycles.
func (l *Link) Latency() uint64 {
	return l.latency
}

// SlotCount returns the number of stages used to buffer packets.
func (l *Link) SlotCount() uint64 {
	return l.slotCount
}

// Bandwidth returns the maximum packets per slot and per cycle.
func (l *Link) Bandwidth() uint64 {
	return l.bandwidth
}

// SnapshotOccupancy reports the pending packet count per slot.
func (l *Link) SnapshotOccupancy() []int {
	occupancy := make([]int, len(l.slots))
	for i, bucket := range l.slots {
		occupancy[i] = len(bucket)
	}
	return occupancy
}

// Transmit schedules a packet for delivery after the configured latency.
// If the link is backpressured, the packet is not accepted.
// If noBackpressureUntil >= targetCycle, directly sends to channel without ring buffer.
func (l *Link) Transmit(cycle uint64, pkt packet.Packet) {
	if l.backpressured {
		return
	}
	targetCycle := cycle + l.latency

	// Optimization: if receiver guarantees no backpressure until targetCycle, send directly
	if l.noBackpressureUntil >= targetCycle {
		env := packet.Envelope{
			Cycle:  targetCycle,
			Packet: pkt,
		}
		select {
		case l.target.Mailbox() <- env:
			// Successfully sent, update SFC
			if targetCycle > l.sendFinishedCycle {
				l.sendFinishedCycle = targetCycle
			}
		default:
			// Channel full, fallback to ring buffer
			// If slot is full (bandwidth limit), packet is rejected
			_ = l.transmitViaRingBuffer(cycle, pkt)
		}
		return
	}

	// Ring buffer path: backpressure risk, need buffering
	// If slot is full (bandwidth limit), packet is rejected
	_ = l.transmitViaRingBuffer(cycle, pkt)
}

// transmitViaRingBuffer puts packet into ring buffer for delayed delivery.
// Returns true if packet was accepted, false if slot is full (bandwidth limit).
func (l *Link) transmitViaRingBuffer(cycle uint64, pkt packet.Packet) bool {
	targetCycle := cycle + l.latency
	index := targetCycle % l.slotCount

	// Check bandwidth limit: each slot can hold at most bandwidth packets
	if uint64(len(l.slots[index])) >= l.bandwidth {
		return false // Slot is full, packet rejected
	}

	l.slots[index] = append(l.slots[index], pkt)
	return true
}

// Advance releases the packets whose delivery cycle matches the provided cycle.
// Only processes ring buffer path packets.
// Packets that cannot be delivered immediately due to backpressure remain in
// the slot and are retried in the next rotation.
// Also reads packets from source flow's dispatch queue if configured.
func (l *Link) Advance(cycle uint64) {
	// Check if receiver is backpressured
	if cycle > l.noBackpressureUntil {
		// Backpressured: don't update currentCycle, ring buffer pointer doesn't move
		// All packets stay in original slot, don't send to channel
		return
	}

	// Update currentCycle first
	l.currentCycle = cycle

	// Read from source flow's dispatch queue (uses updated currentCycle)
	l.ReadFromFlow()
	index := l.currentCycle % l.slotCount
	slot := l.slots[index]
	if len(slot) == 0 {
		// Update SFC even if slot is empty
		if cycle > l.sendFinishedCycle {
			l.sendFinishedCycle = cycle
		}
		return
	}

	// Send packets to channel (cycle, Packet)
	// Bandwidth limit: each cycle can send at most bandwidth packets
	mailbox := l.target.Mailbox()
	remaining := slot[:0]
	sentCount := uint64(0)
	for _, pkt := range slot {
		// Check bandwidth limit: each cycle can send at most bandwidth packets
		if sentCount >= l.bandwidth {
			remaining = append(remaining, pkt)
			continue
		}

		env := packet.Envelope{
			Cycle:  cycle,
			Packet: pkt,
		}
		select {
		case mailbox <- env:
			// Successfully sent
			sentCount++
		default:
			// Channel full, keep in slot
			remaining = append(remaining, pkt)
		}
	}
	l.slots[index] = remaining

	// Update SFC
	if cycle > l.sendFinishedCycle {
		l.sendFinishedCycle = cycle
	}
}

// SetBackpressure sets the backpressure state of the link.
func (l *Link) SetBackpressure(bp bool) {
	l.backpressured = bp
}

// IsBackpressured returns whether the link is currently backpressured.
func (l *Link) IsBackpressured() bool {
	return l.backpressured
}

// CurrentCycle returns the internal cycle counter.
func (l *Link) CurrentCycle() uint64 {
	return l.currentCycle
}

// SendFinishedCycle returns the SFC (Send Finished Cycle).
func (l *Link) SendFinishedCycle() uint64 {
	return l.sendFinishedCycle
}

// SetSendFinishedCycle sets the SFC.
func (l *Link) SetSendFinishedCycle(cycle uint64) {
	l.sendFinishedCycle = cycle
}

// SetNoBackpressureUntil sets the cycle until which receiver guarantees no backpressure.
func (l *Link) SetNoBackpressureUntil(cycle uint64) {
	l.noBackpressureUntil = cycle
}

// NoBackpressureUntil returns the cycle until which receiver guarantees no backpressure.
func (l *Link) NoBackpressureUntil() uint64 {
	return l.noBackpressureUntil
}

// ReadFromFlow reads packets from Flow's dispatch queue.
// Uses the sourceFlow and dispatchQueueIndex stored in the Link.
// Always attempts to read; if dispatch queue is empty, DrainDispatchQueue returns nil.
func (l *Link) ReadFromFlow() {
	if l.sourceFlow == nil {
		return
	}

	// Read all available packets from dispatch queue
	packets := l.sourceFlow.DrainDispatchQueue(l.dispatchQueueIndex)
	for _, pkt := range packets {
		l.Transmit(l.currentCycle, pkt)
	}
}

// DispatchQueueIndex returns the dispatch queue index this link reads from.
func (l *Link) DispatchQueueIndex() int {
	return l.dispatchQueueIndex
}
