package flow

import "github.com/Readm/flow_sim/internal/dataflow/packet"

// DispatchQueue is a type alias for Queue, used for dispatch queues.
// It maintains backward compatibility while using the unified Queue implementation.
type DispatchQueue = Queue

// NewDispatchQueue creates a new dispatch queue with the specified capacity.
// This is a convenience function that creates a Queue instance for use as a dispatch queue.
func NewDispatchQueue(link interface{}, capacity int) *DispatchQueue {
	return NewQueue(capacity, link)
}

// RouterHook is a function type that determines which dispatch queue a packet should be routed to.
// Parameters:
//   - pkt: the packet to route
//   - queues: all available dispatch queues
//   - topology: network topology information (optional, can be nil)
//
// Returns:
//   - index of the selected dispatch queue, or -1 to discard the packet
type RouterHook func(pkt packet.Packet, queues []*Queue, topology interface{}) int

// DefaultRouterHook is the default routing strategy that sends all packets to the first dispatch queue.
// This is suitable when there's only one outgoing link or when no routing decision is needed.
func DefaultRouterHook(pkt packet.Packet, queues []*Queue, topology interface{}) int {
	if len(queues) == 0 {
		return -1 // No dispatch queues available, discard
	}
	// Send to the first dispatch queue
	return 0
}

// TODO: Implement shortest path routing strategy
// ShortestPathRouterHook would use topology information to determine the shortest path
// to the packet's target and select the appropriate dispatch queue.
