package pipeline

import "github.com/Readm/flow_sim/internal/dataflow/packet"

// Queue represents a unified queue for packets. It can be instantiated as:
// - in_queue: for receiving packets from upstream (link should be nil)
// - out_queue: for staging processed packets (link should be nil)
// - dispatch_queue: for routing packets to specific links (link should be set)
type Queue struct {
	mailbox           chan packet.PacketWithCycle
	sendFinishedCycle uint64      // Only used by dispatch_queue
	link              interface{} // Associated Link (only used by dispatch_queue, use interface{} to avoid circular dependency)
}

// NewQueue creates a new queue with the specified capacity.
// link can be nil for in_queue and out_queue, and should be set for dispatch_queue.
func NewQueue(capacity int, link interface{}) *Queue {
	if capacity <= 0 {
		capacity = 16 // Default capacity
	}
	return &Queue{
		mailbox: make(chan packet.PacketWithCycle, capacity),
		link:    link,
	}
}

// IsFull checks if the queue is at capacity.
func (q *Queue) IsFull() bool {
	return len(q.mailbox) == cap(q.mailbox)
}

// Length returns the current number of packets in the queue.
func (q *Queue) Length() int {
	return len(q.mailbox)
}

// Capacity returns the maximum capacity of the queue.
func (q *Queue) Capacity() int {
	return cap(q.mailbox)
}

// SendFinishedCycle returns the SFC of this queue.
func (q *Queue) SendFinishedCycle() uint64 {
	return q.sendFinishedCycle
}

// SetSendFinishedCycle updates the SFC of this queue.
func (q *Queue) SetSendFinishedCycle(cycle uint64) {
	q.sendFinishedCycle = cycle
}

// Link returns the associated link.
func (q *Queue) Link() interface{} {
	return q.link
}

// Mailbox returns the send-only channel for this queue.
// This provides a unified interface for sending packets via channel.
func (q *Queue) Mailbox() chan<- packet.PacketWithCycle {
	return q.mailbox
}

// ReceiveMailbox returns the receive-only channel for this queue.
// This allows receivers to read packets from the queue via channel.
func (q *Queue) ReceiveMailbox() <-chan packet.PacketWithCycle {
	return q.mailbox
}

// Drain drains all packets from the queue non-blockingly.
// Returns all packets currently in the queue.
func (q *Queue) Drain() []packet.Packet {
	var packets []packet.Packet
	for {
		select {
		case env := <-q.mailbox:
			packets = append(packets, env.Packet)
		default:
			return packets
		}
	}
}
