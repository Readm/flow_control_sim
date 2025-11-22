package cycle_port

import (
	"sync"
	"sync/atomic"
)

// CyclePortImpl implements CyclePort interface.
// It provides bidirectional synchronization between upstream and downstream components.
// A single CyclePortImpl instance can be used by both upstream (sender) and downstream (receiver).
type CyclePortImpl struct {
	// doneUntil is the cycle until which upstream has completed processing.
	// Updated by upstream using atomic operations.
	// DoneUntil N means upstream has completed cycle N-1 and all packets for cycle N-1 have been sent.
	// Downstream uses WaitForDoneUntil to block until this value reaches a target cycle.
	doneUntil int64

	// readyUntil is the cycle until which downstream can execute ahead (fast path).
	// If cycle < readyUntil, Ready(cycle) returns true immediately without checking readyMap.
	// Updated atomically when UpdateReady sets a cycle to ready and that cycle >= readyUntil.
	// This provides an optimization to avoid map lookups for cycles that are guaranteed to be ready.
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

	// doneUntilMu protects doneUntilCond and allows waiting for DoneUntil changes.
	doneUntilMu sync.Mutex
	// doneUntilCond is a condition variable for waiting on DoneUntil changes.
	// Used by WaitForDoneUntil to block goroutines until SetDoneUntil is called.
	doneUntilCond *sync.Cond
}

// NewCyclePort creates a new CyclePort with the specified channel buffer size.
func NewCyclePort(bufferSize int) *CyclePortImpl {
	if bufferSize <= 0 {
		bufferSize = 8
	}
	return &CyclePortImpl{
		doneUntil:  -1,
		readyUntil: -1,
		readyMap:   make(map[int]bool),
		packetChan: make(chan PacketWithCycle, bufferSize),
	}
}

// SetDoneUntil updates DoneUntil using atomic store.
// Called by upstream to notify downstream that it has completed processing up to cycle N-1.
// DoneUntil N means:
//   - Upstream has completed cycle N-1
//   - All packets for cycle N-1 have been sent
//
// This wakes up all goroutines waiting in WaitForDoneUntil for DoneUntil to reach a certain value.
func (p *CyclePortImpl) SetDoneUntil(cycle int) {
	atomic.StoreInt64(&p.doneUntil, int64(cycle))

	// Wake up all goroutines waiting for DoneUntil changes
	p.doneUntilMu.Lock()
	if p.doneUntilCond != nil {
		p.doneUntilCond.Broadcast()
	}
	p.doneUntilMu.Unlock()
}

// GetDoneUntil returns the current DoneUntil value set by upstream.
// Can be called by both upstream and downstream to check progress.
// This is useful for upstream to verify its own progress, or for downstream
// to check upstream completion status without blocking.
func (p *CyclePortImpl) GetDoneUntil() int {
	return int(atomic.LoadInt64(&p.doneUntil))
}

// Chan returns a write-only channel for upstream to push packets to downstream.
// Upstream sends (Packet, Cycle) pairs through this channel.
// The same underlying channel is accessible to downstream via ReceiveChan().
func (p *CyclePortImpl) Chan() chan<- PacketWithCycle {
	return p.packetChan
}

// ReceiveChan returns a read-only channel for downstream to receive packets from upstream.
// This is the same underlying channel as Chan(), but from downstream's perspective.
// Downstream reads (Packet, Cycle) pairs from this channel.
func (p *CyclePortImpl) ReceiveChan() <-chan PacketWithCycle {
	return p.packetChan
}

// Ready checks if downstream is ready to process the given cycle.
// Called by upstream before sending a packet for a specific cycle.
// Returns true if downstream is ready, false otherwise.
// This method may block waiting for downstream to become ready.
//
// Fast path: if cycle < readyUntil, returns true immediately (downstream can execute ahead).
// Otherwise, queries readyMap or blocks until downstream signals readiness via UpdateReady.
func (p *CyclePortImpl) Ready(cycle int) bool {
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
func (p *CyclePortImpl) ReadyNonBlocking(cycle int) (ready bool, configured bool) {
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
func (p *CyclePortImpl) waitForReady(cycle int) bool {
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
// This is an internal method, not part of the CyclePort interface.
// It updates both readyMap and readyUntil, and wakes up all goroutines waiting in waitForReady.
func (p *CyclePortImpl) UpdateReady(cycle int, ready bool) {
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

// RemoveReadyBefore removes readyMap entries for cycles less than the given cycle.
// Called by downstream to clean up old entries that are no longer needed.
// This is useful for memory management when processing many cycles.
func (p *CyclePortImpl) RemoveReadyBefore(cycle int) {
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
func (p *CyclePortImpl) SetReadyUntil(cycle int) {
	atomic.StoreInt64(&p.readyUntil, int64(cycle))
}

// GetReadyUntil returns the current readyUntil value.
// Can be used to check the current fast path threshold.
func (p *CyclePortImpl) GetReadyUntil() int {
	return int(atomic.LoadInt64(&p.readyUntil))
}

// WaitForDoneUntil blocks the calling goroutine until upstream's DoneUntil >= targetCycle.
// Called by downstream at the start of cycle N to ensure upstream has completed cycle N-1.
// This uses condition variable to avoid busy waiting - the goroutine will block until
// upstream calls SetDoneUntil with a value >= targetCycle.
// Returns immediately if DoneUntil >= targetCycle (no blocking needed).
func (p *CyclePortImpl) WaitForDoneUntil(targetCycle int) {
	// Fast path: check if already satisfied
	if p.GetDoneUntil() >= targetCycle {
		return
	}

	p.doneUntilMu.Lock()
	defer p.doneUntilMu.Unlock()

	// Initialize condition variable if needed
	if p.doneUntilCond == nil {
		p.doneUntilCond = sync.NewCond(&p.doneUntilMu)
	}

	// Wait until condition is satisfied
	for p.GetDoneUntil() < targetCycle {
		// Wait() will:
		// 1. Unlock doneUntilMu
		// 2. Block the goroutine
		// 3. When Broadcast() is called in SetDoneUntil, re-lock doneUntilMu and continue
		p.doneUntilCond.Wait()
	}
}
