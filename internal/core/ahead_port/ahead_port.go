package ahead_port

import (
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// PacketWithCycle represents a packet with its associated cycle.
// It is an alias for packet.PacketWithCycle.
type PacketWithCycle = packet.PacketWithCycle

// InPort represents an input port interface for receiving data from upstream components.
// It provides write access to a channel and methods to check readiness for receiving data.
// This interface is used by upstream components to send packets to downstream components.
type InPort interface {
	// SendChan returns a write-only channel for upstream to push packets.
	// Upstream sends (Packet, Cycle) pairs through this channel.
	// Panics if called before Plug().
	SendChan() chan<- PacketWithCycle

	// Ready checks if the port is ready to receive data for the given cycle.
	// Called by upstream before sending a packet for a specific cycle.
	// Returns true if the port is ready, false otherwise.
	// This method may block waiting for the port to become ready.
	Ready(cycle int) bool

	// ReadyNonBlocking checks if the port is ready without blocking.
	// Returns (ready, decided):
	//   - ready: true if the port is ready to receive data, false otherwise
	//   - decided: true if the ready state has been determined (won't block),
	//              false if the state is undecided and Ready() would block
	// This method never blocks and is useful for assertions and checking decision status.
	ReadyNonBlocking(cycle int) (ready bool, decided bool)

	// Plug connects this InPort to an upstream OutPort.
	// Creates a new channel and configures both ports to use it.
	// Returns the created channel (useful for debugging/monitoring).
	// Panics if already plugged.
	// Only one of InPort.Plug(out) or OutPort.Plug(in) should be called, not both.
	Plug(out OutPort) chan PacketWithCycle
}

// OutPort represents an output port interface for sending data to downstream components.
// It provides read access to a channel and methods to wait for upstream completion.
// This interface is used by downstream components to receive packets from upstream components.
type OutPort interface {
	// ReceiveChan returns a read-only channel for downstream to receive packets.
	// Downstream reads (Packet, Cycle) pairs from this channel.
	// Panics if called before Plug().
	ReceiveChan() <-chan PacketWithCycle

	// WaitDone blocks the calling goroutine until upstream's Done >= targetCycle.
	// Called by downstream at the start of cycle N to ensure upstream has completed cycle N-1.
	// This uses condition variable to avoid busy waiting - the goroutine will block until
	// upstream calls SetDone with a value >= targetCycle.
	// Returns immediately if Done >= targetCycle (no blocking needed).
	WaitDone(targetCycle int)

	// GetDone returns the current Done value set by upstream.
	// Can be called by downstream to check upstream progress without blocking.
	GetDone() int

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
// The embedding struct must implement the Ready() and ReadyNonBlocking() methods.
type BaseInPort struct {
	InputChan   chan PacketWithCycle // Channel for receiving packets, set by Plug()
	UpstreamOut OutPort              // Reference to upstream OutPort, set by Plug()
	self        InPort               // Reference to the full InPort implementation
}

// SendChan returns the write-only view of the input channel.
// Panics if called before Plug().
func (p *BaseInPort) SendChan() chan<- PacketWithCycle {
	if p.InputChan == nil {
		panic("BaseInPort.SendChan() called before Plug()")
	}
	return p.InputChan
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
// The embedding struct must implement the WaitDone() and GetDone() methods.
type BaseOutPort struct {
	OutputChan   chan PacketWithCycle // Channel for sending packets, set by Plug()
	DownstreamIn InPort               // Reference to downstream InPort, set by Plug()
	self         OutPort              // Reference to the full OutPort implementation
}

// ReceiveChan returns the read-only view of the output channel.
// Panics if called before Plug().
func (p *BaseOutPort) ReceiveChan() <-chan PacketWithCycle {
	if p.OutputChan == nil {
		panic("BaseOutPort.ReceiveChan() called before Plug()")
	}
	return p.OutputChan
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
