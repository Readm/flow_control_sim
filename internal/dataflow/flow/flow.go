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
	ProcessedCount() int
	// Backpressure methods
	IsInQueueFull() bool
	IsOutQueueFull() bool
	SetDownstreamBackpressure(bool)
	GetDownstreamBackpressure() bool
	SetUpstreamBackpressureCallback(func())
	// Cross-cycle parallel methods
	CurrentCycle() uint64
	SetNoBackpressureUntil(uint64)
	NoBackpressureUntil() uint64
	AdvanceTo(cycle uint64, linkSFC uint64) error
	SetUpstreamLink(link interface{}) // Use interface{} to avoid circular dependency
	// Dispatch queue methods
	SetRouterHook(hook RouterHook)
	AddDispatchQueue(link interface{}, capacity int)
	DispatchQueueCount() int
	DrainDispatchQueue(index int) []packet.Packet
	DispatchQueueSendFinishedCycle(index int) uint64
	IsDispatchQueueFull(index int) bool
	DispatchQueueMailbox(index int) <-chan packet.Envelope
}

// FIFO implements Flow by draining packets in the order they arrive. It uses a
// dedicated mailbox channel so links can push envelopes without locking.
type FIFO struct {
	id                           int
	inQueue                      *Queue // in_queue instance
	outQueue                     *Queue // out_queue instance
	incoming                     []packet.Packet
	processed                    []packet.Packet
	downstreamBackpressure       bool
	upstreamBackpressureCallback func()
	currentCycle                 uint64
	noBackpressureUntil          uint64
	upstreamLink                 interface{} // Use interface{} to avoid circular dependency
	// Dispatch queue fields
	dispatchQueues []*Queue // dispatch_queue instances
	routerHook     RouterHook
	topology       interface{} // Network topology information for Router Hook
}

// NewFIFO constructs a FIFO flow with the provided identifier. mailboxSize
// controls how many envelopes can queue up before backpressure is applied.
// inQueueCapacity and outQueueCapacity control the capacity limits for backpressure.
// links: list of links for which to create dispatch queues (optional)
// dispatchQueueCapacity: capacity of each dispatch queue (defaults to outQueueCapacity).
// All queues (in_queue, out_queue, dispatch_queue) use the unified Queue implementation.
func NewFIFO(id int, mailboxSize int, inQueueCapacity int, outQueueCapacity int, links []interface{}, dispatchQueueCapacity int) *FIFO {
	if mailboxSize <= 0 {
		mailboxSize = 8
	}
	if inQueueCapacity <= 0 {
		inQueueCapacity = mailboxSize
	}
	if outQueueCapacity <= 0 {
		outQueueCapacity = 16
	}
	if dispatchQueueCapacity <= 0 {
		dispatchQueueCapacity = outQueueCapacity
	}

	// Create dispatch queues for each link
	dispatchQueues := make([]*Queue, len(links))
	for i, link := range links {
		dispatchQueues[i] = NewDispatchQueue(link, dispatchQueueCapacity)
	}

	return &FIFO{
		id:             id,
		inQueue:        NewQueue(mailboxSize, nil),      // in_queue instance
		outQueue:       NewQueue(outQueueCapacity, nil), // out_queue instance
		dispatchQueues: dispatchQueues,
		routerHook:     DefaultRouterHook, // Use default router hook
	}
}

// ID returns the node identifier that owns the flow.
func (f *FIFO) ID() int {
	return f.id
}

// Mailbox exposes the send-only channel so links can inject envelopes.
func (f *FIFO) Mailbox() chan<- packet.Envelope {
	return f.inQueue.Mailbox()
}

// Tick drains the mailbox and processes packets that arrived prior to or during
// the cycle. It respects context cancellation to avoid blocking the scheduler.
// When out_queue is full, processing is blocked. When in_queue is full, upstream
// backpressure callback is triggered.
func (f *FIFO) Tick(ctx context.Context, cycle uint64) error {
	// Update currentCycle
	f.currentCycle = cycle

	// Check if in_queue is full and notify upstream
	if f.IsInQueueFull() && f.upstreamBackpressureCallback != nil {
		f.upstreamBackpressureCallback()
	}

	for {
		select {
		case env := <-f.inQueue.ReceiveMailbox():
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

	// Route packets from out_queue to dispatch_queues
	f.Route()

	return nil
}

// Route takes packets from the out_queue channel and distributes them to dispatch_queues
// based on the router hook function. Packets are received from outQueue channel
// and sent to dispatch queue channels.
func (f *FIFO) Route() {
	if len(f.dispatchQueues) == 0 {
		// No dispatch queues, drain outQueue to prevent blocking
		for {
			select {
			case <-f.outQueue.ReceiveMailbox():
				// Discard packets
			default:
				return
			}
		}
	}

	// Receive packets from outQueue channel and route to dispatch queues
	for {
		select {
		case env := <-f.outQueue.ReceiveMailbox():
			// Call router hook to determine which dispatch queue to use
			index := f.routerHook(env.Packet, f.dispatchQueues, f.topology)

			// Discard packet if index is -1 or invalid
			if index < 0 || index >= len(f.dispatchQueues) {
				continue
			}

			// Send to selected dispatch queue via channel
			dq := f.dispatchQueues[index]
			select {
			case dq.Mailbox() <- env:
				// Successfully sent to dispatch queue
				// Update dispatch queue SFC
				if f.currentCycle >= dq.SendFinishedCycle() {
					dq.SetSendFinishedCycle(f.currentCycle)
				}
			default:
				// Dispatch queue channel is full, packet is dropped (backpressure)
				// Could optionally buffer back to outgoing slice, but for simplicity we drop it
			}
		default:
			// No more packets in outQueue
			return
		}
	}
}

// Emit stages packets for delivery to downstream links.
// If downstream backpressure is active, packets are not added to out_queue.
// Packets are sent via outQueue channel.
func (f *FIFO) Emit(pkts ...packet.Packet) {
	if len(pkts) == 0 {
		return
	}
	// Block emission if downstream backpressure is active
	if f.downstreamBackpressure {
		return
	}
	// Send packets via channel
	for _, pkt := range pkts {
		env := packet.Envelope{
			Cycle:  f.currentCycle,
			Packet: pkt,
		}
		select {
		case f.outQueue.Mailbox() <- env:
			// Successfully sent
		default:
			// Channel full, packet is dropped (backpressure)
		}
	}
}

// ProcessedCount exposes how many packets have been processed lifecycle-wide.
func (f *FIFO) ProcessedCount() int {
	return len(f.processed)
}

// IsInQueueFull checks if the in_queue is full.
func (f *FIFO) IsInQueueFull() bool {
	return f.inQueue.IsFull()
}

// IsOutQueueFull checks if the out_queue channel is full or if all dispatch queues are full.
func (f *FIFO) IsOutQueueFull() bool {
	// Check if out_queue channel is full
	if f.outQueue.IsFull() {
		return true
	}
	// If there are dispatch queues, check if all of them are full
	if len(f.dispatchQueues) > 0 {
		allFull := true
		for _, dq := range f.dispatchQueues {
			if !dq.IsFull() {
				allFull = false
				break
			}
		}
		return allFull
	}
	return false
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

// CurrentCycle returns the current cycle the flow has advanced to.
func (f *FIFO) CurrentCycle() uint64 {
	return f.currentCycle
}

// SetNoBackpressureUntil sets the cycle until which receiver guarantees no backpressure.
func (f *FIFO) SetNoBackpressureUntil(cycle uint64) {
	f.noBackpressureUntil = cycle
	// Notify upstream link
	if f.upstreamLink != nil {
		if link, ok := f.upstreamLink.(interface {
			SetNoBackpressureUntil(uint64)
		}); ok {
			link.SetNoBackpressureUntil(cycle)
		}
	}
}

// NoBackpressureUntil returns the cycle until which receiver guarantees no backpressure.
func (f *FIFO) NoBackpressureUntil() uint64 {
	return f.noBackpressureUntil
}

// AdvanceTo advances the flow to the specified cycle, using Link SFC to determine execution condition.
func (f *FIFO) AdvanceTo(cycle uint64, linkSFC uint64) error {
	// Check execution condition: currentCycle <= linkSFC
	if f.currentCycle > linkSFC {
		return nil // Cannot execute, wait for Link SFC to update
	}

	// Receive data from channel (if no data, means no data until linkSFC)
	for f.currentCycle <= linkSFC && f.currentCycle < cycle {
		select {
		case env := <-f.inQueue.ReceiveMailbox():
			f.incoming = append(f.incoming, env.Packet)
			f.currentCycle = env.Cycle
		default:
			// No data, means no data until linkSFC
			f.currentCycle = linkSFC
			goto process
		}
	}
process:

	// Process data (only if out_queue is not full)
	if !f.IsOutQueueFull() {
		for len(f.incoming) > 0 {
			pkt := f.incoming[0]
			f.incoming = f.incoming[1:]
			f.processed = append(f.processed, pkt)
		}
	}

	// Calculate noBackpressureUntil (based on in_queue capacity)
	f.noBackpressureUntil = f.calculateNoBackpressureUntil()

	// Notify upstream link
	if f.upstreamLink != nil {
		if link, ok := f.upstreamLink.(interface {
			SetNoBackpressureUntil(uint64)
		}); ok {
			link.SetNoBackpressureUntil(f.noBackpressureUntil)
		}
	}

	return nil
}

// SetUpstreamLink sets the upstream link for notifying backpressure signals.
func (f *FIFO) SetUpstreamLink(link interface{}) {
	f.upstreamLink = link
}

// calculateNoBackpressureUntil calculates the cycle until which no backpressure will occur.
func (f *FIFO) calculateNoBackpressureUntil() uint64 {
	// Based on in_queue current capacity and bandwidth
	remainingCapacity := f.inQueue.Capacity() - f.inQueue.Length()
	// Assume receiving 1 packet per cycle
	return f.currentCycle + uint64(remainingCapacity)
}

// SetRouterHook sets the routing hook function.
func (f *FIFO) SetRouterHook(hook RouterHook) {
	if hook == nil {
		f.routerHook = DefaultRouterHook
	} else {
		f.routerHook = hook
	}
}

// AddDispatchQueue adds a new dispatch queue for the specified link.
func (f *FIFO) AddDispatchQueue(link interface{}, capacity int) {
	dq := NewDispatchQueue(link, capacity)
	f.dispatchQueues = append(f.dispatchQueues, dq)
}

// DispatchQueueCount returns the number of dispatch queues.
func (f *FIFO) DispatchQueueCount() int {
	return len(f.dispatchQueues)
}

// DrainDispatchQueue drains all packets from the specified dispatch queue.
func (f *FIFO) DrainDispatchQueue(index int) []packet.Packet {
	if index < 0 || index >= len(f.dispatchQueues) {
		return nil
	}
	return f.dispatchQueues[index].Drain()
}

// DispatchQueueSendFinishedCycle returns the SFC of the specified dispatch queue.
func (f *FIFO) DispatchQueueSendFinishedCycle(index int) uint64 {
	if index < 0 || index >= len(f.dispatchQueues) {
		return 0
	}
	return f.dispatchQueues[index].SendFinishedCycle()
}

// IsDispatchQueueFull checks if the specified dispatch queue is full.
func (f *FIFO) IsDispatchQueueFull(index int) bool {
	if index < 0 || index >= len(f.dispatchQueues) {
		return false
	}
	return f.dispatchQueues[index].IsFull()
}

// DispatchQueueMailbox returns the receive-only channel for the specified dispatch queue.
// This allows Link to receive packets from the dispatch queue via channel.
func (f *FIFO) DispatchQueueMailbox(index int) <-chan packet.Envelope {
	if index < 0 || index >= len(f.dispatchQueues) {
		return nil
	}
	return f.dispatchQueues[index].ReceiveMailbox()
}
