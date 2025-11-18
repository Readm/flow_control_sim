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
}

// FIFO implements Flow by draining packets in the order they arrive. It uses a
// dedicated mailbox channel so links can push envelopes without locking.
type FIFO struct {
	id        int
	mailbox   chan packet.Envelope
	incoming  []packet.Packet
	outgoing  []packet.Packet
	processed []packet.Packet
}

// NewFIFO constructs a FIFO flow with the provided identifier. mailboxSize
// controls how many envelopes can queue up before backpressure is applied.
func NewFIFO(id int, mailboxSize int) *FIFO {
	if mailboxSize <= 0 {
		mailboxSize = 8
	}
	return &FIFO{
		id:      id,
		mailbox: make(chan packet.Envelope, mailboxSize),
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
func (f *FIFO) Tick(ctx context.Context, cycle uint64) error {
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
	for len(f.incoming) > 0 {
		pkt := f.incoming[0]
		f.incoming = f.incoming[1:]
		f.processed = append(f.processed, pkt)
	}

	return nil
}

// Emit stages packets for delivery to downstream links.
func (f *FIFO) Emit(pkts ...packet.Packet) {
	if len(pkts) == 0 {
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
