package ahead_port

import (
	"sync"
	"sync/atomic"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// PacketWithCycle represents a packet with its associated cycle.
type PacketWithCycle = packet.PacketWithCycle
type Packet = packet.Packet

// ===== New Interface Definitions =====

// InPort represents the upstream view of a port (sender's perspective).
// Upstream components use this interface to send data to downstream components.
//
// Recommended Workflow (Standard API):
//  1. Check if ready and send data using TrySend().
//  2. After sending all data for the cycle, call MarkDone().
type InPort interface {
	// ===== [Standard API] - Most components should only use these =====

	// TrySend attempts to send a packet to the downstream component.
	// This blocks until the downstream component decides its ready state.
	// Returns true if the packet was sent successfully, false if downstream is declared not ready.
	TrySend(cycle int, pkt PacketWithCycle) bool

	// MarkDone marks that the upstream component has completed the specified cycle.
	// This allows the downstream component to safely read data for this cycle.
	MarkDone(cycle int)

	// ===== [Advanced/Optimization API] - Only use for custom flow control =====

	// PeekReady checks if the downstream component is ready without blocking.
	// Returns (ready, decided).
	PeekReady(cycle int) (ready bool, decided bool)

	// IsReady blocks until the downstream component has decided its ready state.
	// Most components should prefer using TrySend directly.
	IsReady(cycle int) bool
}

// OutPort represents the downstream view of a port (receiver's perspective).
// Downstream components use this interface to receive data from upstream components.
//
// Recommended Workflow (Standard API):
//  1. Retrieve all packets for the cycle using Receive().
//  2. Determine subsequent readiness and call UpdateReady().
type OutPort interface {
	// ===== [Standard API] - Most components should only use these =====

	// Receive retrieves all packets for the specified cycle from the upstream component.
	// This is a blocking call that ensures all packets for the cycle are collected.
	// Note: It internally handles the wait for upstream to be done (WaitDone).
	Receive(cycle int) []packet.Packet

	// UpdateReady updates the downstream component's ready state for the given cycle.
	// This informs the upstream whether it's ready to receive data for the specific cycle.
	UpdateReady(cycle int, ready bool)

	// ===== [Advanced/Optimization API] - Use with caution =====

	// WaitDone blocks until the upstream component has completed the specified cycle.
	// Note: Receive() already calls this internally.
	WaitDone(cycle int)

	// PeekDone returns the highest cycle that the upstream component has completed.
	// This is a non-blocking query method.
	PeekDone() int
}

// ===== Port Implementation =====

// Port is a unified port implementation that acts as a connection between two components.
// It implements both InPort and OutPort interfaces, providing different views for upstream and downstream components.
//
// Design principles:
// - Port is an independent entity, not owned by any component
// - One port instance per connection (not two)
// - Type safety through interface views (AsInPort/AsOutPort)
// - Synchronization logic centralized in Port
type Port struct {
	// ===== Communication channel =====
	channel chan PacketWithCycle

	// ===== Synchronization using ComponentSync =====
	upstreamSync   *ComponentSync
	downstreamSync *ComponentSync

	// ===== Packet cache for out-of-order packets =====
	pendingPackets map[int][]packet.Packet
	pendingMu      sync.Mutex

	// ===== Profiling: Node IDs =====
	sourceNodeID int // Upstream (sender) Node ID
	targetNodeID int // Downstream (receiver) Node ID

	// ===== Profiling: Atomic counters (lock-free) =====
	doneBlockCount  atomic.Uint64 // WaitDone blocked count
	doneFastCount   atomic.Uint64 // WaitDone fast path count
	readyBlockCount atomic.Uint64 // Ready blocked count
	readyFastCount  atomic.Uint64 // Ready fast path count
}

// NewPort creates a new port instance.
// sourceNodeID: ID of the upstream (sender) node
// targetNodeID: ID of the downstream (receiver) node
func NewPort(sourceNodeID, targetNodeID int) *Port {
	return &Port{
		channel:        make(chan PacketWithCycle, 64), // Increased capacity
		upstreamSync:   NewComponentSync(),
		downstreamSync: NewComponentSync(),
		pendingPackets: make(map[int][]packet.Packet),
		sourceNodeID:   sourceNodeID,
		targetNodeID:   targetNodeID,
	}
}

// ===== InPort interface implementation (upstream view) =====

// TrySend attempts to send a packet to the downstream component.
// This blocks until the downstream component decides its ready state.
// Returns true if the packet was sent successfully, false if downstream is declared not ready.
func (p *Port) TrySend(cycle int, pkt PacketWithCycle) bool {
	if !p.IsReady(cycle) {
		return false
	}

	// Send packet to channel
	p.channel <- pkt
	return true
}

// PeekReady checks if the downstream component is ready to receive data for the given cycle.
func (p *Port) PeekReady(cycle int) (bool, bool) {
	return p.downstreamSync.IsReadyNonBlocking(cycle)
}

// IsReady blocks until the downstream component has decided its ready state for the given cycle.
func (p *Port) IsReady(cycle int) bool {
	// Profiling: Check if we'll hit the fast path
	if int64(cycle) < atomic.LoadInt64(&p.downstreamSync.readyUntil) {
		p.readyFastCount.Add(1) // Fast path - no blocking
	} else {
		p.readyBlockCount.Add(1) // Slow path - may block
	}

	return p.downstreamSync.Ready(cycle)
}

// MarkDone marks that the upstream component has completed the specified cycle.
func (p *Port) MarkDone(cycle int) {
	p.upstreamSync.SetDone(cycle)
}

// ===== OutPort interface implementation (downstream view) =====

// Receive retrieves all packets for the specified cycle from the upstream component.
// This is now a blocking call that ensures all packets for the cycle are collected.
// Receive retrieves all packets for the specified cycle from the upstream component.
// This is now a blocking call that ensures all packets for the cycle are collected.
func (p *Port) Receive(cycle int) []packet.Packet {
	// 1. Drain channel into cache (Optimized: only locks if data exists)
	p.drainChannel()

	// 2. Wait for upstream to be done with this cycle
	p.WaitDone(cycle)

	// 3. Final drain to catch anything sent just before MarkDone AND return cached packets
	// Optimization: Combine final drain and data retrieval under a single lock
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()

	// Drain any remaining packets in channel
	for {
		select {
		case pwc := <-p.channel:
			p.pendingPackets[pwc.Cycle] = append(p.pendingPackets[pwc.Cycle], pwc.Packet)
		default:
			goto Drained
		}
	}

Drained:
	pkts := p.pendingPackets[cycle]
	delete(p.pendingPackets, cycle)
	return pkts
}

// drainChannel reads all currently available packets from the channel into the cache.
// Optimization: Acquires lock once for the entire batch instead of per-packet.
func (p *Port) drainChannel() {
	// Fast path: check if channel has data without locking first
	select {
	case pwc := <-p.channel:
		// Has data, acquire lock and drain everything available
		p.pendingMu.Lock()
		defer p.pendingMu.Unlock()

		// Store the first packet we already retrieved
		p.pendingPackets[pwc.Cycle] = append(p.pendingPackets[pwc.Cycle], pwc.Packet)

		// Drain the rest
		for {
			select {
			case pwc := <-p.channel:
				p.pendingPackets[pwc.Cycle] = append(p.pendingPackets[pwc.Cycle], pwc.Packet)
			default:
				return
			}
		}
	default:
		// Channel is empty, nothing to do
		return
	}
}

// WaitDone blocks until the upstream component has completed the specified cycle.
func (p *Port) WaitDone(cycle int) {
	// Profiling: Check if we'll hit the fast path
	if int(atomic.LoadInt64(&p.upstreamSync.done)) >= cycle {
		p.doneFastCount.Add(1) // Fast path - no blocking
	} else {
		p.doneBlockCount.Add(1) // Slow path - will block
	}

	p.upstreamSync.WaitDone(cycle)
}

// PeekDone returns the highest cycle that the upstream component has completed.
func (p *Port) PeekDone() int {
	return p.upstreamSync.GetDone()
}

// UpdateReady updates the downstream component's ready state for the given cycle.
func (p *Port) UpdateReady(cycle int, ready bool) {
	p.downstreamSync.UpdateReady(cycle, ready)
}

// ===== Interface view conversions (for type safety) =====

// AsInPort returns the InPort view of this port.
// This should be used by upstream components that send data.
func (p *Port) AsInPort() InPort {
	return p
}

// AsOutPort returns the OutPort view of this port.
// This should be used by downstream components that receive data.
func (p *Port) AsOutPort() OutPort {
	return p
}

// ===== Profiling Getters =====

// SourceNodeID returns the upstream (sender) node ID.
func (p *Port) SourceNodeID() int {
	return p.sourceNodeID
}

// TargetNodeID returns the downstream (receiver) node ID.
func (p *Port) TargetNodeID() int {
	return p.targetNodeID
}

// DoneBlockCount returns the number of times WaitDone blocked.
func (p *Port) DoneBlockCount() uint64 {
	return p.doneBlockCount.Load()
}

// DoneFastCount returns the number of times WaitDone used fast path.
func (p *Port) DoneFastCount() uint64 {
	return p.doneFastCount.Load()
}

// ReadyBlockCount returns the number of times Ready blocked.
func (p *Port) ReadyBlockCount() uint64 {
	return p.readyBlockCount.Load()
}

// ReadyFastCount returns the number of times Ready used fast path.
func (p *Port) ReadyFastCount() uint64 {
	return p.readyFastCount.Load()
}
