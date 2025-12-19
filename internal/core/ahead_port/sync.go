package ahead_port

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Readm/flow_sim/internal/core/debug"
)

// readyItem represents a ready state for a specific cycle
type readyItem struct {
	cycle int
	ready bool
}

// ComponentSync provides shared synchronization logic for components.
// It manages both "done" and "ready" states with efficient atomic operations
// and channel-based notifications optimized for a single waiter.
type ComponentSync struct {
	// Done state
	done       int64         // Component's done cycle (atomic)
	doneNotify chan struct{} // 1-waiter optimized notification channel

	// Ready state
	readyUntil      int64         // Ready until cycle (atomic, fast path)
	readyNotify     chan struct{} // 1-waiter optimized notification channel
	readyQueue      []readyItem   // Sorted queue of future ready states
	lastAccessCycle int           // For debug: tracking monotonic access

	// Synchronization for ready operations
	waiterMu sync.Mutex
}

// NewComponentSync creates a new ComponentSync instance.
func NewComponentSync() *ComponentSync {
	cs := &ComponentSync{
		done:            -1,
		doneNotify:      make(chan struct{}, 1),
		readyUntil:      0, // Start at 0: no cycles are ready yet
		readyNotify:     make(chan struct{}, 1),
		readyQueue:      make([]readyItem, 0),
		lastAccessCycle: -1,
	}
	return cs
}

// ===== Done state management =====

// SetDone sets the component's done state.
func (cs *ComponentSync) SetDone(cycle int) {
	atomic.StoreInt64(&cs.done, int64(cycle))

	// Non-blocking notification
	select {
	case cs.doneNotify <- struct{}{}:
	default:
	}
}

// GetDone gets the component's done state.
func (cs *ComponentSync) GetDone() int {
	return int(atomic.LoadInt64(&cs.done))
}

// WaitDone waits for the component to complete targetCycle.
func (cs *ComponentSync) WaitDone(targetCycle int) {
	// Fast path
	if int(atomic.LoadInt64(&cs.done)) >= targetCycle {
		return
	}

	// Slow path: wait for notifications
	for int(atomic.LoadInt64(&cs.done)) < targetCycle {
		<-cs.doneNotify
	}
}

// ===== Ready state management =====

// Ready checks if the component is ready to receive data for the given cycle.
// This method blocks if the ready state hasn't been decided yet.
func (cs *ComponentSync) Ready(cycle int) bool {
	// Debug check for monotonic access
	if debug.Enabled() {
		if cycle < cs.lastAccessCycle {
			panic(fmt.Sprintf("Ready access violation: cycle %d < last %d (must be monotonic)", cycle, cs.lastAccessCycle))
		}
		cs.lastAccessCycle = cycle
	}

	// Fast path: if cycle < readyUntil, return true immediately (Lock-free)
	if int64(cycle) < atomic.LoadInt64(&cs.readyUntil) {
		return true
	}

	// Slow path: check readyQueue or wait
	for {
		if int64(cycle) < atomic.LoadInt64(&cs.readyUntil) {
			return true
		}

		// Check readyQueue under lock
		cs.waiterMu.Lock()
		found, result := cs.checkQueueAndPrune(cycle)
		cs.waiterMu.Unlock()

		if found {
			return result
		}

		// Wait for notification
		<-cs.readyNotify
	}
}

func (cs *ComponentSync) checkQueueAndPrune(cycle int) (bool, bool) {
	found := false
	var result bool
	pruneIdx := 0

	for i, item := range cs.readyQueue {
		if item.cycle < cycle {
			continue
		}
		if item.cycle == cycle {
			result = item.ready
			found = true
			pruneIdx = i
			break
		}
		pruneIdx = i
		break
	}

	if !found && len(cs.readyQueue) > 0 {
		if cs.readyQueue[len(cs.readyQueue)-1].cycle < cycle {
			pruneIdx = len(cs.readyQueue)
		}
	}

	if pruneIdx > 0 {
		if pruneIdx >= len(cs.readyQueue) {
			cs.readyQueue = nil
		} else {
			cs.readyQueue = cs.readyQueue[pruneIdx:]
		}
	}

	return found, result
}

// IsReadyNonBlocking checks ready state without blocking.
func (cs *ComponentSync) IsReadyNonBlocking(cycle int) (bool, bool) {
	if int64(cycle) < atomic.LoadInt64(&cs.readyUntil) {
		return true, true
	}

	cs.waiterMu.Lock()
	defer cs.waiterMu.Unlock()

	// Re-check after lock
	if int64(cycle) < atomic.LoadInt64(&cs.readyUntil) {
		return true, true
	}

	for _, item := range cs.readyQueue {
		if item.cycle == cycle {
			return item.ready, true
		}
		if item.cycle > cycle {
			break
		}
	}

	return false, false
}

// UpdateReady updates the component's ready state.
func (cs *ComponentSync) UpdateReady(cycle int, ready bool) {
	// Optimization: sequential ready updates (Lock-free fast path)
	if ready && atomic.LoadInt64(&cs.readyUntil) == int64(cycle) {
		if atomic.CompareAndSwapInt64(&cs.readyUntil, int64(cycle), int64(cycle+1)) {
			cs.notifyReady()
			return
		}
	}

	cs.waiterMu.Lock()
	defer cs.waiterMu.Unlock()

	currentReadyUntil := atomic.LoadInt64(&cs.readyUntil)
	if int64(cycle) < currentReadyUntil {
		return
	}

	// Insert into sorted queue
	idx := len(cs.readyQueue)
	exists := false
	for i, item := range cs.readyQueue {
		if item.cycle == cycle {
			cs.readyQueue[i].ready = ready
			exists = true
			break
		}
		if item.cycle > cycle {
			idx = i
			break
		}
	}

	if !exists {
		if idx == len(cs.readyQueue) {
			cs.readyQueue = append(cs.readyQueue, readyItem{cycle, ready})
		} else {
			cs.readyQueue = append(cs.readyQueue[:idx+1], cs.readyQueue[idx:]...)
			cs.readyQueue[idx] = readyItem{cycle, ready}
		}
	}

	// Compaction: advance readyUntil if possible
	advanced := false
	for len(cs.readyQueue) > 0 {
		head := cs.readyQueue[0]
		if int64(head.cycle) == currentReadyUntil {
			if head.ready {
				currentReadyUntil++
				cs.readyQueue = cs.readyQueue[1:]
				advanced = true
			} else {
				break
			}
		} else if int64(head.cycle) < currentReadyUntil {
			cs.readyQueue = cs.readyQueue[1:]
		} else {
			break
		}
	}

	if advanced {
		atomic.StoreInt64(&cs.readyUntil, currentReadyUntil)
	}
	cs.notifyReady()
}

func (cs *ComponentSync) notifyReady() {
	select {
	case cs.readyNotify <- struct{}{}:
	default:
	}
}

// SetReadyUntil sets readyUntil atomically.
func (cs *ComponentSync) SetReadyUntil(cycle int) {
	for {
		current := atomic.LoadInt64(&cs.readyUntil)
		if int64(cycle) <= current {
			break
		}
		if atomic.CompareAndSwapInt64(&cs.readyUntil, current, int64(cycle)) {
			cs.notifyReady()
			break
		}
	}
}

// InitReady initializes ready state for the first 'limit' cycles.
func (cs *ComponentSync) InitReady(limit int) {
	cs.SetReadyUntil(limit)
}
