package ahead_port

import (
	"sync"
	"sync/atomic"
)

// SinglePort implements AheadPort interface.
// It provides bidirectional synchronization between upstream and downstream components.
// A single SinglePort instance can be used by both upstream (sender) and downstream (receiver).
type SinglePort struct {
	// done is the cycle until which upstream has completed processing.
	// Updated by upstream using atomic operations.
	// Done N means upstream has completed cycle N and all previous cycles, and all packets for cycle N have been sent.
	// Downstream uses WaitForDone to block until this value reaches a target cycle.
	// Using int64 for atomic operations, but represents int values.
	done int64

	// readyUntil is the cycle until which downstream can execute ahead (fast path).
	// If cycle < readyUntil, Ready(cycle) returns true immediately without checking readyMap.
	// Updated atomically when UpdateReady sets a cycle to ready and that cycle >= readyUntil.
	// This provides an optimization to avoid map lookups for cycles that are guaranteed to be ready.
	// Using int64 for atomic operations, but represents int values.
	readyUntil int64

	// readyMap stores ready status for specific cycles that are not covered by readyUntil.
	// Key: cycle, Value: ready status (true = ready, false = not ready).
	// Protected by waiterMu for concurrent access.
	// Used when cycle >= readyUntil to check if a specific cycle is ready.
	readyMap map[int]bool

	// packetChan is the internal channel for packet transmission.
	// Upstream pushes packets through Chan() (write-only view).
	// Downstream receives packets from ReceiveChan() (read-only view).
	// Both views refer to the same underlying channel.
	packetChan chan PacketWithCycle

	// waiterMu protects readyMap and cond for concurrent access.
	// Used when checking/updating readyMap and when waiting for ready status changes.
	waiterMu sync.Mutex
	// cond is a condition variable for waiting on readyMap changes.
	// Used by waitForReady to block goroutines until UpdateReady is called.
	cond *sync.Cond

	// doneMu protects doneCond and allows waiting for Done changes.
	doneMu sync.Mutex
	// doneCond is a condition variable for waiting on Done changes.
	// Used by WaitForDone to block goroutines until SetDone is called.
	doneCond *sync.Cond
}

// NewAheadPort creates a new AheadPort with the specified channel buffer size.
func NewAheadPort(bufferSize int) *SinglePort {
	if bufferSize <= 0 {
		bufferSize = 8
	}
	return &SinglePort{
		done:       -1,
		readyUntil: -1,
		readyMap:   make(map[int]bool),
		packetChan: make(chan PacketWithCycle, bufferSize),
	}
}

// SetDone updates Done using atomic store.
// Called by upstream to notify downstream that it has completed processing up to cycle N.
// Done N means:
//   - Upstream has completed cycle N and all previous cycles
//   - All packets for cycle N have been sent
//
// This wakes up all goroutines waiting in WaitForDone for Done to reach a certain value.
func (p *SinglePort) SetDone(cycle int) {
	atomic.StoreInt64(&p.done, int64(cycle))

	// Wake up all goroutines waiting for Done changes
	p.doneMu.Lock()
	if p.doneCond != nil {
		p.doneCond.Broadcast()
	}
	p.doneMu.Unlock()
}

// GetDone returns the current Done value set by upstream.
// Can be called by both upstream and downstream to check progress.
// This is useful for upstream to verify its own progress, or for downstream
// to check upstream completion status without blocking.
func (p *SinglePort) GetDone() int {
	return int(atomic.LoadInt64(&p.done))
}

// Chan returns a write-only channel for upstream to push packets to downstream.
// Upstream sends (Packet, Cycle) pairs through this channel.
// The same underlying channel is accessible to downstream via ReceiveChan().
func (p *SinglePort) Chan() chan<- PacketWithCycle {
	return p.packetChan
}

// ReceiveChan returns a read-only channel for downstream to receive packets from upstream.
// This is the same underlying channel as Chan(), but from downstream's perspective.
// Downstream reads (Packet, Cycle) pairs from this channel.
func (p *SinglePort) ReceiveChan() <-chan PacketWithCycle {
	return p.packetChan
}

// Ready checks if downstream is ready to process the given cycle.
// Called by upstream before sending a packet for a specific cycle.
// Returns true if downstream is ready, false otherwise.
// This method may block waiting for downstream to become ready.
//
// Fast path: if cycle < readyUntil, returns true immediately (downstream can execute ahead).
// Otherwise, queries readyMap or blocks until downstream signals readiness via UpdateReady.
func (p *SinglePort) Ready(cycle int) bool {
	// Fast path: if cycle < readyUntil, downstream can execute ahead
	readyUntil := atomic.LoadInt64(&p.readyUntil)
	if int64(cycle) < readyUntil {
		return true
	}

	// Check readyMap for specific cycle status
	p.waiterMu.Lock()
	ready, exists := p.readyMap[cycle]
	p.waiterMu.Unlock()

	if exists {
		return ready
	}

	// Cycle not configured in readyMap, need to wait for downstream to call UpdateReady
	return p.waitForReady(cycle)
}

// ReadyNonBlocking checks if downstream is ready to process the given cycle without blocking.
// This method never blocks and is useful for assertions and checking configuration status.
// Returns (ready, configured):
//   - ready: true if downstream is ready, false otherwise
//   - configured: true if the cycle is configured (readyMap contains it or readyUntil covers it),
//     false if the cycle is not configured and Ready() would block
func (p *SinglePort) ReadyNonBlocking(cycle int) (ready bool, configured bool) {
	// Fast path: if cycle < readyUntil, downstream can execute ahead
	readyUntil := atomic.LoadInt64(&p.readyUntil)
	if int64(cycle) < readyUntil {
		return true, true // ready and configured
	}

	// Check readyMap for specific cycle status
	p.waiterMu.Lock()
	ready, exists := p.readyMap[cycle]
	p.waiterMu.Unlock()

	if exists {
		return ready, true // configured (ready or not ready)
	}

	// Cycle not configured in readyMap, Ready() would block
	return false, false // not ready and not configured (would block)
}

// waitForReady blocks until the given cycle becomes ready.
// Uses sync.Cond to efficiently wait for readyMap changes, avoiding busy waiting.
// The goroutine will block until UpdateReady is called for this cycle.
func (p *SinglePort) waitForReady(cycle int) bool {
	p.waiterMu.Lock()
	defer p.waiterMu.Unlock()

	// Initialize condition variable if needed
	if p.cond == nil {
		p.cond = sync.NewCond(&p.waiterMu)
	}

	// Wait until the cycle is configured in readyMap
	for {
		if ready, exists := p.readyMap[cycle]; exists {
			return ready
		}
		// Wait() will:
		// 1. Unlock waiterMu
		// 2. Block the goroutine
		// 3. When Broadcast() is called in UpdateReady, re-lock waiterMu and continue
		p.cond.Wait()
	}
}

// UpdateReady updates the ready status for a specific cycle and wakes up waiting goroutines.
// Called by downstream (via CycleProcessor) when it determines readiness for a cycle.
// This is an internal method, not part of the AheadPort interface.
// It updates both readyMap and readyUntil, and wakes up all goroutines waiting in waitForReady.
func (p *SinglePort) UpdateReady(cycle int, ready bool) {
	p.waiterMu.Lock()
	defer p.waiterMu.Unlock()

	// Update readyMap with the cycle's ready status
	p.readyMap[cycle] = ready

	// Update readyUntil if ready and cycle is ahead of current readyUntil
	// This extends the fast path: cycles < readyUntil will return true immediately
	if ready {
		currentReadyUntil := atomic.LoadInt64(&p.readyUntil)
		if int64(cycle) >= currentReadyUntil {
			atomic.StoreInt64(&p.readyUntil, int64(cycle)+1)
		}
	}

	// Wake up all goroutines waiting in waitForReady
	// They will re-check readyMap and return if their cycle is now configured
	if p.cond != nil {
		p.cond.Broadcast()
	}
}

// SetChannel replaces the internal packet channel.
// This is used for optimizing multi-upstream aggregation by sharing a single channel.
// WARNING: This should only be called during initialization before any processing starts.
func (p *SinglePort) SetChannel(ch chan PacketWithCycle) {
	p.packetChan = ch
}

// RemoveReadyBefore removes readyMap entries for cycles less than the given cycle.
// Called by downstream to clean up old entries that are no longer needed.
// This is useful for memory management when processing many cycles.
func (p *SinglePort) RemoveReadyBefore(cycle int) {
	p.waiterMu.Lock()
	defer p.waiterMu.Unlock()

	for c := range p.readyMap {
		if c < cycle {
			delete(p.readyMap, c)
		}
	}
}

// SetReadyUntil sets the readyUntil value directly.
// Called by downstream to indicate it can execute ahead up to a certain cycle.
// This extends the fast path: cycles < readyUntil will return true immediately in Ready().
// Useful for initialization or when downstream knows it can process many cycles ahead.
func (p *SinglePort) SetReadyUntil(cycle int) {
	atomic.StoreInt64(&p.readyUntil, int64(cycle))
}

// GetReadyUntil returns the current readyUntil value.
// Can be used to check the current fast path threshold.
func (p *SinglePort) GetReadyUntil() int {
	return int(atomic.LoadInt64(&p.readyUntil))
}

// WaitForDone blocks the calling goroutine until upstream's Done >= targetCycle.
// Called by downstream at the start of cycle N to ensure upstream has completed cycle N-1.
// This uses condition variable to avoid busy waiting - the goroutine will block until
// upstream calls SetDone with a value >= targetCycle.
// Returns immediately if Done >= targetCycle (no blocking needed).
func (p *SinglePort) WaitForDone(targetCycle int) {
	// Fast path: check if already satisfied
	if p.GetDone() >= targetCycle {
		return
	}

	p.doneMu.Lock()
	defer p.doneMu.Unlock()

	// Initialize condition variable if needed
	if p.doneCond == nil {
		p.doneCond = sync.NewCond(&p.doneMu)
	}

	// Wait until condition is satisfied
	for p.GetDone() < targetCycle {
		// Wait() will:
		// 1. Unlock doneMu
		// 2. Block the goroutine
		// 3. When Broadcast() is called in SetDone, re-lock doneMu and continue
		p.doneCond.Wait()
	}
}
