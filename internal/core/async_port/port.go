package async_port

import (
	"sync"
	"sync/atomic"
)

// Port implements ASyncPort interface.
// It provides synchronization between upstream and downstream components.
type Port struct {
	// doneUntil is the cycle until which upstream has completed.
	// Updated by upstream using atomic operations.
	doneUntil int64

	// readyUntil is the cycle until which downstream can execute ahead.
	// If cycle < readyUntil, Ready() returns true immediately.
	readyUntil int64

	// readyMap stores ready status for specific cycles.
	// Key: cycle, Value: ready status (true = ready, false = not ready).
	// No lock protection needed as per requirements.
	readyMap map[int]bool

	// packetChan is the internal channel for packet transmission.
	// Upstream pushes through Chan() (write-only), downstream receives from ReceiveChan().
	packetChan chan PacketWithCycle

	// readyWaiters stores channels waiting for specific cycles to become ready.
	// Key: cycle, Value: list of channels to notify when ready.
	readyWaiters map[int][]chan bool
	waiterMu     sync.Mutex
	cond         *sync.Cond // Condition variable for waiting on readyMap changes

	// doneUntilMu protects doneUntilCond and allows waiting for DoneUntil changes
	doneUntilMu   sync.Mutex
	doneUntilCond *sync.Cond // Condition variable for waiting on DoneUntil changes
}

// NewPort creates a new ASyncPort with the specified channel buffer size.
func NewPort(bufferSize int) *Port {
	if bufferSize <= 0 {
		bufferSize = 8
	}
	return &Port{
		doneUntil:    -1,
		readyUntil:   -1,
		readyMap:     make(map[int]bool),
		packetChan:   make(chan PacketWithCycle, bufferSize),
		readyWaiters: make(map[int][]chan bool),
	}
}

// SetDoneUntil updates DoneUntil using atomic store.
// Called by upstream to indicate completion up to cycle N.
// Wakes up goroutines waiting for DoneUntil to reach a certain value.
func (p *Port) SetDoneUntil(cycle int) {
	atomic.StoreInt64(&p.doneUntil, int64(cycle))

	// Wake up goroutines waiting for DoneUntil changes
	p.doneUntilMu.Lock()
	if p.doneUntilCond != nil {
		p.doneUntilCond.Broadcast() // Wake all waiters
	}
	p.doneUntilMu.Unlock()
}

// GetDoneUntil returns the current DoneUntil value.
// Called by downstream to check upstream progress.
func (p *Port) GetDoneUntil() int {
	return int(atomic.LoadInt64(&p.doneUntil))
}

// Chan returns a write-only channel for upstream to push packets.
func (p *Port) Chan() chan<- PacketWithCycle {
	return p.packetChan
}

// ReceiveChan returns a read-only channel for downstream to receive packets.
// This is an internal method for downstream use.
func (p *Port) ReceiveChan() <-chan PacketWithCycle {
	return p.packetChan
}

// Ready checks if downstream is ready to process the given cycle.
// Returns true if ready, false otherwise. May block waiting for downstream to become ready.
func (p *Port) Ready(cycle int) bool {
	// Fast path: if cycle < readyUntil, downstream can execute ahead
	readyUntil := atomic.LoadInt64(&p.readyUntil)
	if int64(cycle) < readyUntil {
		return true
	}

	// Check readyMap
	p.waiterMu.Lock()
	ready, exists := p.readyMap[cycle]
	p.waiterMu.Unlock()

	if exists {
		return ready
	}

	// Not exist, need to wait
	return p.waitForReady(cycle)
}

// ReadyNonBlocking checks if downstream is ready to process the given cycle without blocking.
// Returns (ready, configured):
//   - ready: true if downstream is ready, false otherwise
//   - configured: true if the cycle is configured (readyMap contains it or readyUntil covers it),
//     false if the cycle is not configured and Ready() would block
func (p *Port) ReadyNonBlocking(cycle int) (ready bool, configured bool) {
	// Fast path: if cycle < readyUntil, downstream can execute ahead
	readyUntil := atomic.LoadInt64(&p.readyUntil)
	if int64(cycle) < readyUntil {
		return true, true // ready and configured
	}

	// Check readyMap
	p.waiterMu.Lock()
	ready, exists := p.readyMap[cycle]
	p.waiterMu.Unlock()

	if exists {
		return ready, true // configured (ready or not ready)
	}

	// Not exist, not configured
	return false, false // not ready and not configured (would block)
}

// waitForReady blocks until the given cycle becomes ready.
// Go 语言没有像 C++/Java 一样的条件变量(condvar)，但 sync.Cond 可以实现类似效果。
// 下面用 sync.Cond 优化等待 readyMap，避免 goroutine/channel 的单独唤醒，实现直接监听 readyMap 的变化。

func (p *Port) waitForReady(cycle int) bool {
	p.waiterMu.Lock()
	defer p.waiterMu.Unlock()

	// 初始化条件变量
	if p.cond == nil {
		p.cond = sync.NewCond(&p.waiterMu)
	}

	for {
		if ready, exists := p.readyMap[cycle]; exists {
			return ready
		}
		// 等待条件变化（会自动解锁并阻塞，收到 Signal/Broadcast 时再加锁苏醒）
		p.cond.Wait()
	}
}

// UpdateReady updates the ready status for a specific cycle and wakes up waiting goroutines.
// Called by downstream when it determines readiness for a cycle.
func (p *Port) UpdateReady(cycle int, ready bool) {
	p.waiterMu.Lock()
	defer p.waiterMu.Unlock()

	// Update readyMap
	p.readyMap[cycle] = ready

	// Update readyUntil if ready and cycle is ahead
	if ready {
		currentReadyUntil := atomic.LoadInt64(&p.readyUntil)
		if int64(cycle) >= currentReadyUntil {
			atomic.StoreInt64(&p.readyUntil, int64(cycle)+1)
		}
	}

	// Wake up waiters for this cycle
	if waiters, exists := p.readyWaiters[cycle]; exists {
		for _, waiter := range waiters {
			select {
			case waiter <- ready:
			default:
			}
		}
		delete(p.readyWaiters, cycle)
	}

	// Wake up goroutines waiting on cond
	if p.cond != nil {
		p.cond.Broadcast()
	}
}

// RemoveReadyBefore removes readyMap entries for cycles less than the given cycle.
// Called by downstream to clean up old entries.
func (p *Port) RemoveReadyBefore(cycle int) {
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
func (p *Port) SetReadyUntil(cycle int) {
	atomic.StoreInt64(&p.readyUntil, int64(cycle))
}

// GetReadyUntil returns the current readyUntil value.
func (p *Port) GetReadyUntil() int {
	return int(atomic.LoadInt64(&p.readyUntil))
}

// WaitForDoneUntil blocks until DoneUntil >= targetCycle.
// This uses condition variable to avoid busy waiting.
// Returns immediately if DoneUntil >= targetCycle.
func (p *Port) WaitForDoneUntil(targetCycle int) {
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
		// 1. Unlock the mutex
		// 2. Block the goroutine
		// 3. When Broadcast() is called, re-lock the mutex and continue
		p.doneUntilCond.Wait()
	}
}
