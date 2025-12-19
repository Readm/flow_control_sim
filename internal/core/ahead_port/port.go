package ahead_port

import (
	"sync"

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
}

// NewPort creates a new port instance.
func NewPort() *Port {
	return &Port{
		channel:        make(chan PacketWithCycle, 64), // Increased capacity
		upstreamSync:   NewComponentSync(),
		downstreamSync: NewComponentSync(),
		pendingPackets: make(map[int][]packet.Packet),
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
	return p.downstreamSync.Ready(cycle)
}

// MarkDone marks that the upstream component has completed the specified cycle.
func (p *Port) MarkDone(cycle int) {
	p.upstreamSync.SetDone(cycle)
}

// ===== OutPort interface implementation (downstream view) =====

// Receive retrieves all packets for the specified cycle from the upstream component.
// This is now a blocking call that ensures all packets for the cycle are collected.
func (p *Port) Receive(cycle int) []packet.Packet {
	// 1. Drain channel into cache
	p.drainChannel()

	// 2. Wait for upstream to be done with this cycle
	p.WaitDone(cycle)

	// 3. Final drain to catch anything sent just before MarkDone
	p.drainChannel()

	// 4. Return cached packets
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	pkts := p.pendingPackets[cycle]
	delete(p.pendingPackets, cycle)
	return pkts
}

// drainChannel reads all currently available packets from the channel into the cache.
func (p *Port) drainChannel() {
	for {
		select {
		case pwc := <-p.channel:
			p.pendingMu.Lock()
			p.pendingPackets[pwc.Cycle] = append(p.pendingPackets[pwc.Cycle], pwc.Packet)
			p.pendingMu.Unlock()
		default:
			return
		}
	}
}

// WaitDone blocks until the upstream component has completed the specified cycle.
func (p *Port) WaitDone(cycle int) {
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
