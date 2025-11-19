package link

import (
	"github.com/Readm/flow_sim/internal/dataflow/flow"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Link represents a directed edge in the topology。每条链路拥有独立的 cycle 延迟和 slot。
type Link struct {
	sourceID            int
	targetID            int
	target              flow.Flow
	latency             uint64
	slotCount           uint64
	slots               [][]packet.Packet
	backpressured       bool
	currentCycle        uint64
	sendFinishedCycle   uint64
	noBackpressureUntil uint64
}

// NewLink creates a link between source and target. slotCount defaults to
// latency+1, ensuring packets are not overwritten before they are delivered.
func NewLink(sourceID int, target flow.Flow, latency uint64, slotCount uint64) *Link {
	if latency == 0 {
		latency = 1
	}
	if slotCount == 0 {
		slotCount = latency + 1
	}
	slots := make([][]packet.Packet, slotCount)
	return &Link{
		sourceID:  sourceID,
		targetID:  target.ID(),
		target:    target,
		latency:   latency,
		slotCount: slotCount,
		slots:     slots,
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
			l.transmitViaRingBuffer(cycle, pkt)
		}
		return
	}

	// Ring buffer path: backpressure risk, need buffering
	l.transmitViaRingBuffer(cycle, pkt)
}

// transmitViaRingBuffer puts packet into ring buffer for delayed delivery.
func (l *Link) transmitViaRingBuffer(cycle uint64, pkt packet.Packet) {
	targetCycle := cycle + l.latency
	index := targetCycle % l.slotCount
	l.slots[index] = append(l.slots[index], pkt)
}

// Advance releases the packets whose delivery cycle matches the provided cycle.
// Only processes ring buffer path packets.
// Packets that cannot be delivered immediately due to backpressure remain in
// the slot and are retried in the next rotation.
func (l *Link) Advance(cycle uint64) {
	// Check if receiver is backpressured
	if cycle > l.noBackpressureUntil {
		// Backpressured: don't update currentCycle, ring buffer pointer doesn't move
		// All packets stay in original slot, don't send to channel
		return
	}

	// Normal advance: update currentCycle and process corresponding slot
	l.currentCycle = cycle
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
	mailbox := l.target.Mailbox()
	remaining := slot[:0]
	for _, pkt := range slot {
		env := packet.Envelope{
			Cycle:  cycle,
			Packet: pkt,
		}
		select {
		case mailbox <- env:
			// Successfully sent
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

// ReadFromFlow reads packets from Flow's out_queue based on Flow's SFC.
func (l *Link) ReadFromFlow(f flow.Flow) {
	flowSFC := f.OutQueueSendFinishedCycle()
	// If currentCycle <= flowSFC, can read all data up to flowSFC
	if l.currentCycle <= flowSFC {
		packets := f.DrainOutgoing()
		for _, pkt := range packets {
			l.Transmit(l.currentCycle, pkt)
		}
	}
}
