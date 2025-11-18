package link

import (
	"github.com/Readm/flow_sim/internal/dataflow/flow"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Link represents a directed edge in the topology。每条链路拥有独立的 cycle 延迟和 slot。
type Link struct {
	sourceID  int
	targetID  int
	target    flow.Flow
	latency   uint64
	slotCount uint64
	slots     [][]packet.Packet
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

// Transmit schedules a packet for delivery after the configured latency.
func (l *Link) Transmit(cycle uint64, pkt packet.Packet) {
	targetCycle := cycle + l.latency
	index := targetCycle % l.slotCount
	l.slots[index] = append(l.slots[index], pkt)
}

// Advance releases the packets whose delivery cycle matches the provided cycle.
// Packets that cannot be delivered immediately due to backpressure remain in
// the slot and are retried in the next rotation.
func (l *Link) Advance(cycle uint64) {
	index := cycle % l.slotCount
	slot := l.slots[index]
	if len(slot) == 0 {
		return
	}

	mailbox := l.target.Mailbox()
	remaining := slot[:0]
	for _, pkt := range slot {
		env := packet.Envelope{
			Cycle:  cycle,
			Packet: pkt,
		}
		select {
		case mailbox <- env:
		default:
			remaining = append(remaining, pkt)
		}
	}

	l.slots[index] = remaining
}
