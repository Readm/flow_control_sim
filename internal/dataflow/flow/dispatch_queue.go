package flow

import "github.com/Readm/flow_sim/internal/dataflow/packet"

// DispatchQueue represents a queue for packets destined for a specific Link.
// Each dispatch queue has independent capacity and SFC tracking.
type DispatchQueue struct {
	packets           []packet.Packet
	capacity          int
	sendFinishedCycle uint64
	link              interface{} // Associated Link (use interface{} to avoid circular dependency)
}

// NewDispatchQueue creates a new dispatch queue with the specified capacity.
func NewDispatchQueue(link interface{}, capacity int) *DispatchQueue {
	if capacity <= 0 {
		capacity = 16 // Default capacity
	}
	return &DispatchQueue{
		packets:  make([]packet.Packet, 0, capacity),
		capacity: capacity,
		link:     link,
	}
}

// Enqueue adds a packet to the dispatch queue if capacity allows.
// Returns true if the packet was added, false if the queue is full.
func (dq *DispatchQueue) Enqueue(pkt packet.Packet) bool {
	if len(dq.packets) >= dq.capacity {
		return false // Queue is full
	}
	dq.packets = append(dq.packets, pkt)
	return true
}

// Drain removes and returns all packets from the dispatch queue.
func (dq *DispatchQueue) Drain() []packet.Packet {
	if len(dq.packets) == 0 {
		return nil
	}
	drained := append([]packet.Packet(nil), dq.packets...)
	dq.packets = dq.packets[:0]
	return drained
}

// IsFull checks if the dispatch queue is at capacity.
func (dq *DispatchQueue) IsFull() bool {
	return len(dq.packets) >= dq.capacity
}

// Length returns the current number of packets in the queue.
func (dq *DispatchQueue) Length() int {
	return len(dq.packets)
}

// Capacity returns the maximum capacity of the queue.
func (dq *DispatchQueue) Capacity() int {
	return dq.capacity
}

// SendFinishedCycle returns the SFC of this dispatch queue.
func (dq *DispatchQueue) SendFinishedCycle() uint64 {
	return dq.sendFinishedCycle
}

// SetSendFinishedCycle updates the SFC of this dispatch queue.
func (dq *DispatchQueue) SetSendFinishedCycle(cycle uint64) {
	dq.sendFinishedCycle = cycle
}

// Link returns the associated link.
func (dq *DispatchQueue) Link() interface{} {
	return dq.link
}

// RouterHook is a function type that determines which dispatch queue a packet should be routed to.
// Parameters:
//   - pkt: the packet to route
//   - dispatchQueues: all available dispatch queues
//   - topology: network topology information (optional, can be nil)
//
// Returns:
//   - index of the selected dispatch queue, or -1 to discard the packet
type RouterHook func(pkt packet.Packet, dispatchQueues []*DispatchQueue, topology interface{}) int

// DefaultRouterHook is the default routing strategy that sends all packets to the first dispatch queue.
// This is suitable when there's only one outgoing link or when no routing decision is needed.
func DefaultRouterHook(pkt packet.Packet, dispatchQueues []*DispatchQueue, topology interface{}) int {
	if len(dispatchQueues) == 0 {
		return -1 // No dispatch queues available, discard
	}
	// Send to the first dispatch queue
	return 0
}

// TODO: Implement shortest path routing strategy
// ShortestPathRouterHook would use topology information to determine the shortest path
// to the packet's target and select the appropriate dispatch queue.

