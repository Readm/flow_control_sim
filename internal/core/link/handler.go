package link

import (
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// LinkHandler defines the behavior for specific link types.
// It handles packet processing, flow control, and readiness signaling.
// This mirrors the NodeHandler pattern used in nodes.
type LinkHandler interface {
	// Process handles data for the current cycle.
	// It is responsible for:
	// 1. Sending packets to downstream (via l.toDownstream)
	// 2. Buffering packets that cannot be sent (via l.pendingPackets or internal state)
	// 3. Updating the ready state for upstream (via l.fromUpstream.UpdateReady)
	//
	// Parameters:
	//   l: reference to the parent Link
	//   cycle: current simulation cycle
	//   targetCycle: the upper bound cycle for this run (inclusive).
	//   incoming: packets received from upstream in this cycle
	Process(l *Link, cycle int, targetCycle int, incoming []packet.Packet) error

	// Reset resets the handler state.
	Reset()

	// Init initializes the handler after being connected to the network.
	// It handles bootstrapping tasks like initial ready signaling.
	// If initialReady is specified on the link, it should be honored.
	Init(l *Link)

	// GetOccupancy returns the number of pending packets per future cycle offset.
	// Index i corresponds to packets scheduled for (CurrentCycle + i).
	GetOccupancy(currentCycle int) []int
}
