package async_port

import (
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// PacketWithCycle represents a packet with its associated cycle.
// It is an alias for packet.Envelope.
type PacketWithCycle = packet.Envelope

// ASyncPort is the interface exposed by downstream to upstream.
// Upstream uses this interface to push packets and check readiness.
type ASyncPort interface {
	// SetDoneUntil is called by upstream to update DoneUntil using atomic store.
	// DoneUntil N means upstream has completed cycle N-1 and all packets for cycle N-1 have been sent.
	SetDoneUntil(cycle int)

	// Chan returns a write-only channel for upstream to push packets.
	// Upstream pushes (Packet, Cycle) pairs through this channel.
	Chan() chan<- PacketWithCycle

	// Ready checks if downstream is ready to process the given cycle.
	// Returns true if ready, false otherwise. May block waiting for downstream to become ready.
	// If cycle < ReadyUntil, returns true immediately.
	// Otherwise, queries readyMap or blocks until ready.
	Ready(cycle int) bool
}
