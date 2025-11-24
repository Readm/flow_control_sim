package ahead_port

import (
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// PacketWithCycle represents a packet with its associated cycle.
// It is an alias for packet.PacketWithCycle.
type PacketWithCycle = packet.PacketWithCycle

// AheadPort is a bidirectional interface for cycle-based synchronous communication between Flow and Link components.
// A single AheadPort instance provides both upstream and downstream operations:
// - Upstream component (e.g., Flow0) uses upstream operations to send packets and check downstream readiness.
// - Downstream component (e.g., Flow1) uses downstream operations to receive packets and wait for upstream completion.
// This bidirectional design allows the same port to be used from both perspectives, enabling flexible
// composition of Flow and Link components in a dataflow graph.
type AheadPort interface {
	// ===== Upstream Operations =====
	// These methods are called by the upstream component (the sender).

	// SetDone is called by upstream to notify downstream that it has completed processing up to cycle N.
	// Done N means:
	//   - Upstream has completed cycle N and all previous cycles
	//   - All packets for cycle N have been sent
	// This uses atomic store for thread-safe updates.
	// Downstream can use WaitForDone to block until this value reaches a target cycle.
	SetDone(cycle int)

	// Chan returns a write-only channel for upstream to push packets to downstream.
	// Upstream sends (Packet, Cycle) pairs through this channel.
	// The same channel is accessible to downstream via ReceiveChan().
	Chan() chan<- PacketWithCycle

	// Ready checks if downstream is ready to process the given cycle.
	// Called by upstream before sending a packet for a specific cycle.
	// Returns true if downstream is ready, false otherwise.
	// This method may block waiting for downstream to become ready.
	// Fast path: if cycle < ReadyUntil, returns true immediately.
	// Otherwise, queries readyMap or blocks until downstream signals readiness.
	Ready(cycle int) bool

	// ReadyNonBlocking checks if downstream is ready to process the given cycle without blocking.
	// Returns (ready, configured):
	//   - ready: true if downstream is ready, false otherwise
	//   - configured: true if the cycle is configured (readyMap contains it or readyUntil covers it),
	//                 false if the cycle is not configured and Ready() would block
	// This method never blocks and is useful for assertions and checking configuration status.
	ReadyNonBlocking(cycle int) (ready bool, configured bool)

	// GetDone returns the current Done value set by upstream.
	// Can be called by both upstream and downstream to check progress.
	// This is useful for upstream to verify its own progress, or for downstream
	// to check upstream completion status without blocking.
	GetDone() int

	// ===== Downstream Operations =====
	// These methods are called by the downstream component (the receiver).

	// ReceiveChan returns a read-only channel for downstream to receive packets from upstream.
	// This is the same underlying channel as Chan(), but from downstream's perspective.
	// Downstream reads (Packet, Cycle) pairs from this channel.
	ReceiveChan() <-chan PacketWithCycle

	// WaitForDone blocks the calling goroutine until upstream's Done >= targetCycle.
	// Called by downstream at the start of cycle N to ensure upstream has completed cycle N-1.
	// This uses condition variable to avoid busy waiting - the goroutine will block until
	// upstream calls SetDone with a value >= targetCycle.
	// Returns immediately if Done >= targetCycle (no blocking needed).
	WaitForDone(targetCycle int)
}
