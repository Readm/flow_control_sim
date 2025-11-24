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

// RouterHook is a function type that determines which output port a packet should be routed to.
// Parameters:
//   - pkt: the packet to route
//   - outPorts: all available output AheadPorts
//   - topology: network topology information (optional, can be nil)
//
// Returns:
//   - index of the selected output port, or -1 to discard the packet
type RouterHook func(pkt packet.Packet, outPorts []interface{}, topology interface{}) int

// DefaultRouterHook is the default routing strategy that sends all packets to the first output port.
// This is suitable when there's only one outgoing link or when no routing decision is needed.
func DefaultRouterHook(pkt packet.Packet, outPorts []interface{}, topology interface{}) int {
	if len(outPorts) == 0 {
		return -1 // No output ports available, discard
	}
	// Send to the first output port
	return 0
}

// TODO: Implement shortest path routing strategy
// ShortestPathRouterHook would use topology information to determine the shortest path
// to the packet's target and select the appropriate dispatch queue.
