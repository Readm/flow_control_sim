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
type InPort interface {
	// TrySend attempts to send a packet to the downstream component.
	// Returns true if the packet was sent successfully, false if downstream is not ready.
	TrySend(cycle int, pkt PacketWithCycle) bool

	// PeekReady checks if the downstream component is ready to receive data for the given cycle.
	// Returns (ready, decided):
	//   - ready: true if downstream is ready, false otherwise
	//   - decided: true if the ready state has been determined, false if undecided
	PeekReady(cycle int) (ready bool, decided bool)

	// MarkDone marks that the upstream component has completed the specified cycle.
	// This allows the downstream component to safely read data for this cycle.
	MarkDone(cycle int)
}

// OutPort represents the downstream view of a port (receiver's perspective).
// Downstream components use this interface to receive data from upstream components.
type OutPort interface {
	// Receive retrieves all packets for the specified cycle from the upstream component.
	// Returns all packets belonging to this cycle (may be empty).
	Receive(cycle int) []packet.Packet

	// WaitUpstreamDone blocks until the upstream component has completed the specified cycle.
	WaitUpstreamDone(cycle int)

	// PeekDone returns the highest cycle that the upstream component has completed.
	// This is a non-blocking query method.
	PeekDone() int

	// UpdateReady updates the downstream component's ready state for the given cycle.
	// This is called by the downstream component to inform the upstream whether it's ready to receive data.
	UpdateReady(cycle int, ready bool)
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

	// ===== Upstream Done state =====
	upstreamDone   int
	upstreamDoneCh chan int
	upstreamDoneMu sync.Mutex

	// ===== Downstream Ready state =====
	downstreamReady   map[int]bool
	downstreamReadyCh map[int]chan bool
	downstreamReadyMu sync.Mutex

	// ===== Packet cache for out-of-order packets =====
	pendingPackets map[int][]packet.Packet
	pendingMu      sync.Mutex
}

// NewPort creates a new port instance.
func NewPort() *Port {
	return &Port{
		channel:           make(chan PacketWithCycle, 8),
		upstreamDone:      -1,
		upstreamDoneCh:    make(chan int, 1),
		downstreamReady:   make(map[int]bool),
		downstreamReadyCh: make(map[int]chan bool),
		pendingPackets:    make(map[int][]packet.Packet),
	}
}

// ===== InPort interface implementation (upstream view) =====

// TrySend attempts to send a packet to the downstream component.
// Returns true if the packet was sent successfully, false if downstream is not ready.
func (p *Port) TrySend(cycle int, pkt PacketWithCycle) bool {
	// Check if downstream is ready
	ready, decided := p.PeekReady(cycle)
	if !decided || !ready {
		return false
	}

	// Send packet to channel
	p.channel <- pkt
	return true
}

// PeekReady checks if the downstream component is ready to receive data for the given cycle.
// Returns (ready, decided):
//   - ready: true if downstream is ready, false otherwise
//   - decided: true if the ready state has been determined, false if undecided
func (p *Port) PeekReady(cycle int) (bool, bool) {
	p.downstreamReadyMu.Lock()
	ready, decided := p.downstreamReady[cycle]
	p.downstreamReadyMu.Unlock()
	return ready, decided
}

// MarkDone marks that the upstream component has completed the specified cycle.
// This allows the downstream component to safely read data for this cycle.
func (p *Port) MarkDone(cycle int) {
	p.upstreamDoneMu.Lock()
	defer p.upstreamDoneMu.Unlock()

	if cycle > p.upstreamDone {
		p.upstreamDone = cycle
		// Notify waiting goroutines
		select {
		case p.upstreamDoneCh <- cycle:
		default:
		}
	}
}

// ===== OutPort interface implementation (downstream view) =====

// Receive retrieves all packets for the specified cycle from the upstream component.
// Returns all packets belonging to this cycle (may be empty).
func (p *Port) Receive(cycle int) []packet.Packet {
	// 1. Check cache first
	p.pendingMu.Lock()
	if cached, ok := p.pendingPackets[cycle]; ok {
		delete(p.pendingPackets, cycle)
		p.pendingMu.Unlock()
		return cached
	}
	p.pendingMu.Unlock()

	// 2. Read from channel
	var result []packet.Packet
	for {
		select {
		case pwc := <-p.channel:
			if pwc.Cycle == cycle {
				// Packet for current cycle
				result = append(result, pwc.Packet)
			} else if pwc.Cycle > cycle {
				// Future packet, cache it
				p.pendingMu.Lock()
				p.pendingPackets[pwc.Cycle] = append(p.pendingPackets[pwc.Cycle], pwc.Packet)
				p.pendingMu.Unlock()
			}
			// Past packets (pwc.Cycle < cycle) are silently dropped
		default:
			// No more packets available
			return result
		}
	}
}

// WaitUpstreamDone blocks until the upstream component has completed the specified cycle.
func (p *Port) WaitUpstreamDone(cycle int) {
	for {
		p.upstreamDoneMu.Lock()
		currentDone := p.upstreamDone
		p.upstreamDoneMu.Unlock()

		if currentDone >= cycle {
			return
		}

		// Wait for notification
		<-p.upstreamDoneCh
	}
}

// PeekDone returns the highest cycle that the upstream component has completed.
// This is a non-blocking query method.
func (p *Port) PeekDone() int {
	p.upstreamDoneMu.Lock()
	defer p.upstreamDoneMu.Unlock()
	return p.upstreamDone
}

// UpdateReady updates the downstream component's ready state for the given cycle.
// This is called by the downstream component to inform the upstream whether it's ready to receive data.
func (p *Port) UpdateReady(cycle int, ready bool) {
	p.downstreamReadyMu.Lock()
	defer p.downstreamReadyMu.Unlock()

	p.downstreamReady[cycle] = ready

	// Notify any waiting goroutines
	if ch, ok := p.downstreamReadyCh[cycle]; ok {
		select {
		case ch <- ready:
		default:
		}
		delete(p.downstreamReadyCh, cycle)
	}
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
