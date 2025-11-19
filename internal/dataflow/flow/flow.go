package flow

import (
	"context"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Flow defines the contract for moving packets through a node. Concrete
// implementations can apply arbitrary policies while conforming to this API.
type Flow interface {
	ID() int
	Mailbox() chan<- packet.Envelope
	Tick(ctx context.Context, cycle uint64) error
	Emit(pkts ...packet.Packet)
	DrainOutgoing() []packet.Packet
	ProcessedCount() int
	// Backpressure methods
	IsInQueueFull() bool
	IsOutQueueFull() bool
	SetDownstreamBackpressure(bool)
	GetDownstreamBackpressure() bool
	SetUpstreamBackpressureCallback(func())
}

// FIFO implements Flow by draining packets in the order they arrive. It uses a
// dedicated mailbox channel so links can push envelopes without locking.
type FIFO struct {
	id                          int
	mailbox                     chan packet.Envelope
	incoming                    []packet.Packet
	outgoing                    []packet.Packet
	processed                   []packet.Packet
	inQueueCapacity             int
	outQueueCapacity            int
	downstreamBackpressure      bool
	upstreamBackpressureCallback func()
}

// NewFIFO constructs a FIFO flow with the provided identifier. mailboxSize
// controls how many envelopes can queue up before backpressure is applied.
// inQueueCapacity and outQueueCapacity control the capacity limits for backpressure.
func NewFIFO(id int, mailboxSize int, inQueueCapacity int, outQueueCapacity int) *FIFO {
	if mailboxSize <= 0 {
		mailboxSize = 8
	}
	if inQueueCapacity <= 0 {
		inQueueCapacity = mailboxSize
	}
	if outQueueCapacity <= 0 {
		outQueueCapacity = 16
	}
	return &FIFO{
		id:               id,
		mailbox:          make(chan packet.Envelope, mailboxSize),
		inQueueCapacity:  inQueueCapacity,
		outQueueCapacity: outQueueCapacity,
	}
}

// ID returns the node identifier that owns the flow.
func (f *FIFO) ID() int {
	return f.id
}

// Mailbox exposes the send-only channel so links can inject envelopes.
func (f *FIFO) Mailbox() chan<- packet.Envelope {
	return f.mailbox
}

// Tick drains the mailbox and processes packets that arrived prior to or during
// the cycle. It respects context cancellation to avoid blocking the scheduler.
// When out_queue is full, processing is blocked. When in_queue is full, upstream
// backpressure callback is triggered.
func (f *FIFO) Tick(ctx context.Context, cycle uint64) error {
	// Check if in_queue is full and notify upstream
	if f.IsInQueueFull() && f.upstreamBackpressureCallback != nil {
		f.upstreamBackpressureCallback()
	}

	for {
		select {
		case env := <-f.mailbox:
			f.incoming = append(f.incoming, env.Packet)
		default:
			goto PROCESS
		case <-ctx.Done():
			return ctx.Err()
		}
	}

PROCESS:
	// Only process if out_queue is not full
	if !f.IsOutQueueFull() {
		for len(f.incoming) > 0 {
			pkt := f.incoming[0]
			f.incoming = f.incoming[1:]
			f.processed = append(f.processed, pkt)
		}
	}

	return nil
}

// Emit stages packets for delivery to downstream links.
// If downstream backpressure is active, packets are not added to out_queue.
func (f *FIFO) Emit(pkts ...packet.Packet) {
	if len(pkts) == 0 {
		return
	}
	// Block emission if downstream backpressure is active
	if f.downstreamBackpressure {
		return
	}
	f.outgoing = append(f.outgoing, pkts...)
}

// DrainOutgoing returns all packets that were emitted since the last drain call.
func (f *FIFO) DrainOutgoing() []packet.Packet {
	if len(f.outgoing) == 0 {
		return nil
	}

	drained := append([]packet.Packet(nil), f.outgoing...)
	f.outgoing = nil
	return drained
}

// ProcessedCount exposes how many packets have been processed lifecycle-wide.
func (f *FIFO) ProcessedCount() int {
	return len(f.processed)
}

// IsInQueueFull checks if the in_queue (mailbox) is full.
func (f *FIFO) IsInQueueFull() bool {
	return len(f.mailbox) == cap(f.mailbox)
}

// IsOutQueueFull checks if the out_queue is full.
func (f *FIFO) IsOutQueueFull() bool {
	return len(f.outgoing) >= f.outQueueCapacity
}

// SetDownstreamBackpressure sets the downstream backpressure state.
func (f *FIFO) SetDownstreamBackpressure(bp bool) {
	f.downstreamBackpressure = bp
}

// GetDownstreamBackpressure returns the downstream backpressure state.
func (f *FIFO) GetDownstreamBackpressure() bool {
	return f.downstreamBackpressure
}

// SetUpstreamBackpressureCallback sets the callback function to notify upstream
// when in_queue is full.
func (f *FIFO) SetUpstreamBackpressureCallback(callback func()) {
	f.upstreamBackpressureCallback = callback
}
