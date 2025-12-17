package ahead_port

import (
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// PacketWithCycle represents a packet with its associated cycle.
// It is an alias for packet.PacketWithCycle.
type PacketWithCycle = packet.PacketWithCycle

// InPort represents an input port interface for receiving data from upstream components.
// It provides methods to send packets and check readiness for receiving data.
// This interface is used by upstream components to send packets to downstream components.
type InPort interface {
	// TrySendPacket attempts to send a packet for the given cycle.
	// Returns true if the packet was accepted (ready and sent), false otherwise.
	// This method blocks waiting for the ready decision, then sends the packet if ready.
	// If the channel buffer is full, it blocks until space is available.
	TrySendPacket(cycle int, pkt PacketWithCycle) bool

	// IsReadyNonBlocking checks if the port is ready without blocking.
	// Returns (ready, decided):
	//   - ready: true if the port is ready to receive data, false otherwise
	//   - decided: true if the ready state has been determined (won't block),
	//              false if the state is undecided and Ready() would block
	// This method never blocks and is useful for assertions and checking decision status.
	IsReadyNonBlocking(cycle int) (ready bool, decided bool)

	// Plug connects this InPort to an upstream OutPort.
	// Creates a new channel and configures both ports to use it.
	// Returns the created channel (useful for debugging/monitoring).
	// Panics if already plugged.
	// Only one of InPort.Plug(out) or OutPort.Plug(in) should be called, not both.
	Plug(out OutPort) chan PacketWithCycle
}

// OutPort represents an output port interface for sending data to downstream components.
// It provides methods to retrieve packets for a specific cycle.
// This interface is used by downstream components to receive packets from upstream components.
type OutPort interface {
	// GetPackets retrieves all packets for the specified cycle from upstream.
	// This method must be called sequentially for each cycle (0, 1, 2, ...).
	//
	// Behavior:
	// 1. Blocks until upstream completes the necessary cycle (based on component latency)
	// 2. Returns all packets that belong to the specified cycle
	// 3. Filters out packets from other cycles and caches them for future calls
	//
	// Parameters:
	//   cycle: The cycle number to retrieve packets for
	//
	// Returns:
	//   []packet.Packet - All packets for this cycle (may be empty)
	//
	// Note: Each cycle must be requested exactly once, in sequential order.
	GetPackets(cycle int) []packet.Packet

	// Plug connects this OutPort to a downstream InPort.
	// Creates a new channel and configures both ports to use it.
	// Returns the created channel (useful for debugging/monitoring).
	// Panics if already plugged.
	// Only one of OutPort.Plug(in) or InPort.Plug(out) should be called, not both.
	Plug(in InPort) chan PacketWithCycle
}

// InPortSetter is an internal interface for configuring InPort's channel during Plug.
// This interface must be implemented by all InPort implementations to support the Plug pattern.
// It is exported to allow test mocks in other packages to implement it.
type InPortSetter interface {
	SetInChannel(ch chan PacketWithCycle, upstream OutPort)
}

// OutPortSetter is an internal interface for configuring OutPort's channel during Plug.
// This interface must be implemented by all OutPort implementations to support the Plug pattern.
// It is exported to allow test mocks in other packages to implement it.
type OutPortSetter interface {
	SetOutChannel(ch chan PacketWithCycle, downstream InPort)
}

// BaseInPort provides a default implementation of InPort with Plug support.
// Components can embed this struct to inherit the default behavior.
// The embedding struct must implement TrySendPacket() and IsReadyNonBlocking() methods.
type BaseInPort struct {
	InputChan   chan PacketWithCycle // Channel for receiving packets, set by Plug()
	UpstreamOut OutPort              // Reference to upstream OutPort, set by Plug()
	self        InPort               // Reference to the full InPort implementation
}

// Plug connects this InPort to an upstream OutPort.
// Creates a channel and configures both sides.
// The self parameter must be the complete InPort implementation (the embedding struct).
func (p *BaseInPort) PlugWithSelf(self InPort, out OutPort) chan PacketWithCycle {
	if p.InputChan != nil {
		panic("BaseInPort already plugged")
	}

	// Create channel with buffer
	ch := make(chan PacketWithCycle, 8)

	// Configure this side
	p.InputChan = ch
	p.UpstreamOut = out
	p.self = self

	// Configure the other side via type assertion
	if setter, ok := out.(OutPortSetter); ok {
		setter.SetOutChannel(ch, self)
	} else {
		panic("OutPort does not implement OutPortSetter interface")
	}

	return ch
}

// Plug is a convenience wrapper that uses type assertion to get self.
// This works when the receiver is the embedded BaseInPort in a struct that implements InPort.
func (p *BaseInPort) Plug(out OutPort) chan PacketWithCycle {
	// This will be overridden by the embedding struct
	panic("BaseInPort.Plug() should be overridden by embedding struct")
}

// SetInChannel is called by OutPort.Plug() to configure this InPort.
func (p *BaseInPort) SetInChannel(ch chan PacketWithCycle, upstream OutPort) {
	if p.InputChan != nil {
		panic("BaseInPort already has a channel")
	}
	p.InputChan = ch
	p.UpstreamOut = upstream
}

// BaseOutPort provides a default implementation of OutPort with Plug support.
// Components can embed this struct to inherit the default behavior.
// It provides a complete GetPackets() implementation that handles channel reading,
// cycle filtering, and packet caching.
type BaseOutPort struct {
	OutputChan     chan PacketWithCycle       // Channel for sending packets, set by Plug()
	DownstreamIn   InPort                     // Reference to downstream InPort, set by Plug()
	self           OutPort                    // Reference to the full OutPort implementation
	pendingPackets map[int][]packet.Packet    // Cached packets for future cycles
	beforeGetHook  func(cycle int)            // Optional hook called before reading packets (e.g., for waitDone)
}

// PlugWithSelf connects this OutPort to a downstream InPort.
// Creates a channel and configures both sides.
// The self parameter must be the complete OutPort implementation (the embedding struct).
func (p *BaseOutPort) PlugWithSelf(self OutPort, in InPort) chan PacketWithCycle {
	if p.OutputChan != nil {
		panic("BaseOutPort already plugged")
	}

	// Create channel with buffer
	ch := make(chan PacketWithCycle, 8)

	// Configure this side
	p.OutputChan = ch
	p.DownstreamIn = in
	p.self = self

	// Configure the other side via type assertion
	if setter, ok := in.(InPortSetter); ok {
		setter.SetInChannel(ch, self)
	} else {
		panic("InPort does not implement InPortSetter interface")
	}

	return ch
}

// Plug is a convenience wrapper that uses type assertion to get self.
// This works when the receiver is the embedded BaseOutPort in a struct that implements OutPort.
func (p *BaseOutPort) Plug(in InPort) chan PacketWithCycle {
	// This will be overridden by the embedding struct
	panic("BaseOutPort.Plug() should be overridden by embedding struct")
}

// SetOutChannel is called by InPort.Plug() to configure this OutPort.
func (p *BaseOutPort) SetOutChannel(ch chan PacketWithCycle, downstream InPort) {
	if p.OutputChan != nil {
		panic("BaseOutPort already has a channel")
	}
	p.OutputChan = ch
	p.DownstreamIn = downstream
}

// GetPackets provides a default implementation that retrieves packets for a specific cycle.
// This method handles:
// 1. Calling the optional beforeGetHook (for components that need to wait)
// 2. Checking the packet cache
// 3. Reading from the channel and filtering by cycle
// 4. Caching future packets
//
// Components can use this default implementation or override it if needed.
func (p *BaseOutPort) GetPackets(cycle int) []packet.Packet {
	// 1. Call optional hook (e.g., for waitDone)
	if p.beforeGetHook != nil {
		p.beforeGetHook(cycle)
	}

	// 2. Initialize cache if needed
	if p.pendingPackets == nil {
		p.pendingPackets = make(map[int][]packet.Packet)
	}

	// 3. Check cache
	if cached, ok := p.pendingPackets[cycle]; ok {
		delete(p.pendingPackets, cycle)
		return cached
	}

	// 4. Read from channel and filter by cycle
	var result []packet.Packet
	if p.OutputChan == nil {
		return result
	}

	for {
		select {
		case pwc := <-p.OutputChan:
			if pwc.Cycle == cycle {
				result = append(result, pwc.Packet)
			} else if pwc.Cycle > cycle {
				// Future packet, cache it
				p.pendingPackets[pwc.Cycle] = append(p.pendingPackets[pwc.Cycle], pwc.Packet)
			}
			// Past packets (pwc.Cycle < cycle) are silently dropped
		default:
			return result
		}
	}
}

// SetBeforeGetHook sets an optional hook that will be called before reading packets.
// This is useful for components that need to wait for upstream completion (e.g., Link's waitDone).
func (p *BaseOutPort) SetBeforeGetHook(hook func(cycle int)) {
	p.beforeGetHook = hook
}
