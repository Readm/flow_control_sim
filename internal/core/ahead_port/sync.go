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
// and condition variables for blocking/waiting.
//
// This component is designed to be embedded in components like Link and InputQueue
// to eliminate code duplication.
type ComponentSync struct {
	// Done state
	done     int64      // Component's done cycle (atomic)
	doneMu   sync.Mutex // Protects done updates
	doneCond *sync.Cond // Condition variable for WaitDone

	// Ready state
	readyUntil      int64       // Ready until cycle (atomic, fast path)
	readyQueue      []readyItem // Sorted queue of future ready states
	lastAccessCycle int         // For debug: tracking monotonic access

	// Synchronization for ready operations
	waiterMu sync.Mutex
	cond     *sync.Cond
}

// NewComponentSync creates a new ComponentSync instance.
func NewComponentSync() *ComponentSync {
	cs := &ComponentSync{
		done:            -1,
		readyUntil:      -1,
		readyQueue:      make([]readyItem, 0),
		lastAccessCycle: -1,
	}
	cs.doneCond = sync.NewCond(&cs.doneMu)
	return cs
}

// ===== Done state management =====

// SetDone sets the component's done state.
func (cs *ComponentSync) SetDone(cycle int) {
	atomic.StoreInt64(&cs.done, int64(cycle))

	cs.doneMu.Lock()
	if cs.doneCond != nil {
		cs.doneCond.Broadcast()
	}
	cs.doneMu.Unlock()
}

// GetDone gets the component's done state.
func (cs *ComponentSync) GetDone() int {
	return int(atomic.LoadInt64(&cs.done))
}

// WaitDone waits for the component to complete targetCycle.
func (cs *ComponentSync) WaitDone(targetCycle int) {
	currentDone := cs.GetDone()
	if currentDone >= targetCycle {
		return
	}

	cs.doneMu.Lock()
	defer cs.doneMu.Unlock()

	if cs.doneCond == nil {
		cs.doneCond = sync.NewCond(&cs.doneMu)
	}

	for cs.GetDone() < targetCycle {
		cs.doneCond.Wait()
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

	// Fast path: if cycle < readyUntil, return true immediately
	readyUntil := atomic.LoadInt64(&cs.readyUntil)
	if int64(cycle) < readyUntil {
		return true
	}

	// Check readyQueue
	cs.waiterMu.Lock()

	// Re-check readyUntil
	currentReadyUntil := atomic.LoadInt64(&cs.readyUntil)
	if int64(cycle) < currentReadyUntil {
		cs.waiterMu.Unlock()
		return true
	}

	found := false
	var result bool

	// Prune and Search
	pruneIdx := 0
	for i, item := range cs.readyQueue {
		if item.cycle < cycle {
			continue
		}
		if item.cycle == cycle {
			result = item.ready
			found = true
			pruneIdx = i + 1
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

	cs.waiterMu.Unlock()

	if found {
		return result
	}

	// Block and wait
	return cs.waitForReady(cycle)
}

// IsReadyNonBlocking checks ready state without blocking.
// Returns (ready, decided):
//   - ready: true if the component is ready to receive data
//   - decided: true if the ready state has been determined (won't block)
func (cs *ComponentSync) IsReadyNonBlocking(cycle int) (bool, bool) {
	// Debug check for monotonic access
	if debug.Enabled() {
		if cycle < cs.lastAccessCycle {
			panic(fmt.Sprintf("Ready access violation (NB): cycle %d < last %d", cycle, cs.lastAccessCycle))
		}
		cs.lastAccessCycle = cycle
	}

	readyUntil := atomic.LoadInt64(&cs.readyUntil)
	if int64(cycle) < readyUntil {
		return true, true
	}

	cs.waiterMu.Lock()
	defer cs.waiterMu.Unlock()

	currentReadyUntil := atomic.LoadInt64(&cs.readyUntil)
	if int64(cycle) < currentReadyUntil {
		return true, true
	}

	// Prune and Search
	pruneIdx := 0
	found := false
	var result bool

	for i, item := range cs.readyQueue {
		if item.cycle < cycle {
			continue
		}
		if item.cycle == cycle {
			result = item.ready
			found = true
			pruneIdx = i // Peek: Do not consume current item
			break
		}
		pruneIdx = i // Stops at > cycle
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

	if found {
		return result, true
	}

	return false, false
}

// waitForReady blocks until ready state is decided.
func (cs *ComponentSync) waitForReady(cycle int) bool {
	cs.waiterMu.Lock()
	defer cs.waiterMu.Unlock()

	if cs.cond == nil {
		cs.cond = sync.NewCond(&cs.waiterMu)
	}

	for {
		currentReadyUntil := atomic.LoadInt64(&cs.readyUntil)
		if int64(cycle) < currentReadyUntil {
			return true
		}

		// Search queue
		found := false
		var result bool

		for i, item := range cs.readyQueue {
			if item.cycle == cycle {
				result = item.ready
				found = true
				// Consume
				if i+1 >= len(cs.readyQueue) {
					cs.readyQueue = nil
				} else {
					cs.readyQueue = cs.readyQueue[i+1:]
				}
				break
			}
			if item.cycle > cycle {
				break
			}
		}

		if found {
			return result
		}
		cs.cond.Wait()
	}
}

// UpdateReady updates the component's ready state.
func (cs *ComponentSync) UpdateReady(cycle int, ready bool) {
	cs.waiterMu.Lock()
	defer cs.waiterMu.Unlock()

	currentReadyUntil := atomic.LoadInt64(&cs.readyUntil)
	if int64(cycle) < currentReadyUntil {
		return
	}

	// Insert
	inserted := false
	if len(cs.readyQueue) == 0 {
		cs.readyQueue = append(cs.readyQueue, readyItem{cycle, ready})
		inserted = true
	} else {
		if cycle > cs.readyQueue[len(cs.readyQueue)-1].cycle {
			cs.readyQueue = append(cs.readyQueue, readyItem{cycle, ready})
			inserted = true
		} else {
			for i, item := range cs.readyQueue {
				if item.cycle == cycle {
					cs.readyQueue[i].ready = ready
					inserted = true
					break
				}
				if item.cycle > cycle {
					cs.readyQueue = append(cs.readyQueue[:i+1], cs.readyQueue[i:]...)
					cs.readyQueue[i] = readyItem{cycle, ready}
					inserted = true
					break
				}
			}
			if !inserted {
				cs.readyQueue = append(cs.readyQueue, readyItem{cycle, ready})
			}
		}
	}

	// Compaction
	for len(cs.readyQueue) > 0 {
		head := cs.readyQueue[0]
		if int64(head.cycle) == currentReadyUntil {
			if head.ready {
				currentReadyUntil++
				cs.readyQueue = cs.readyQueue[1:]
			} else {
				break
			}
		} else if int64(head.cycle) < currentReadyUntil {
			cs.readyQueue = cs.readyQueue[1:]
		} else {
			break
		}
	}

	atomic.StoreInt64(&cs.readyUntil, currentReadyUntil)

	if cs.cond != nil {
		cs.cond.Broadcast()
	}
}

// SetReadyUntil sets readyUntil atomically.
// This is used for initialization where we know the component is ready
// for the first N cycles.
func (cs *ComponentSync) SetReadyUntil(cycle int) {
	// Atomically update readyUntil
	for {
		current := atomic.LoadInt64(&cs.readyUntil)
		if int64(cycle) <= current {
			return
		}
		if atomic.CompareAndSwapInt64(&cs.readyUntil, current, int64(cycle)) {
			break
		}
	}

	// Wake up all waiting goroutines
	cs.waiterMu.Lock()
	if cs.cond != nil {
		cs.cond.Broadcast()
	}
	cs.waiterMu.Unlock()
}

// InitReady initializes ready state for the first 'limit' cycles.
// This is useful for components that are ready during initialization.
func (cs *ComponentSync) InitReady(limit int) {
	for cycle := 0; cycle <= limit; cycle++ {
		cs.UpdateReady(cycle, true)
	}
}
