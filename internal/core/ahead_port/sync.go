package ahead_port

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Readm/flow_sim/internal/core/debug"
)

const (
	stateUnset    int8 = 0
	stateReady    int8 = 1
	stateNotReady int8 = 2
)

// ComponentSync provides shared synchronization logic for components.
// It manages both "done" and "ready" states with efficient atomic operations
// and channel-based notifications optimized for a single waiter.
type ComponentSync struct {
	// Done state
	done        int64         // Component's done cycle (atomic)
	doneNotify  chan struct{} // 1-waiter optimized notification channel
	doneWaiting int32         // Atomic flag: 1 if WaitDone is waiting, 0 otherwise

	// Formatting padding to prevent false sharing between producer (done) and consumer (ready)
	_ [64]byte

	// Consumer state
	waitingFor int64 // Cycle the consumer is waiting for (Targeted Wakeup)

	// Ready state
	readyUntil      int64         // Ready until cycle (atomic, fast path)
	readyNotify     chan struct{} // 1-waiter optimized notification channel
	readyStates     []int8        // Dense array: index 0 corresponds to cycle readyUntil
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
		readyStates:     make([]int8, 0),
		lastAccessCycle: -1,
	}
	return cs
}

// ===== Done state management =====

// SetDone sets the component's done state.
func (cs *ComponentSync) SetDone(cycle int) {
	atomic.StoreInt64(&cs.done, int64(cycle))

	// Optimization: Only notify if someone is waiting
	if atomic.LoadInt32(&cs.doneWaiting) == 1 {
		// Targeted Wakeup: only notify if we reached the target cycle
		// This prevents waking up the consumer for every intermediate cycle
		target := atomic.LoadInt64(&cs.waitingFor)
		if int64(cycle) >= target {
			// Non-blocking notification
			select {
			case cs.doneNotify <- struct{}{}:
			default:
			}
		}
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

	// Publish what we are waiting for (Targeted Wakeup)
	atomic.StoreInt64(&cs.waitingFor, int64(targetCycle))

	// Signify that we are waiting
	atomic.StoreInt32(&cs.doneWaiting, 1)
	defer atomic.StoreInt32(&cs.doneWaiting, 0)

	// Double check after setting flag to avoid race
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

	// Slow path: check readyStates or wait
	for {
		if int64(cycle) < atomic.LoadInt64(&cs.readyUntil) {
			return true
		}

		// Check readyStates under lock
		cs.waiterMu.Lock()
		found, result := cs.checkStatesAndPrune(cycle)
		cs.waiterMu.Unlock()

		if found {
			return result
		}

		// Wait for notification
		<-cs.readyNotify
	}
}

func (cs *ComponentSync) checkStatesAndPrune(cycle int) (bool, bool) {
	currentReadyUntil := atomic.LoadInt64(&cs.readyUntil)
	offset := int(int64(cycle) - currentReadyUntil)

	if offset < 0 {
		return true, true
	}

	if offset >= len(cs.readyStates) {
		return false, false
	}

	state := cs.readyStates[offset]
	if state == stateUnset {
		return false, false
	}

	// Prune: since access is monotonic, any request for 'cycle' effectively
	// invalidates anything before it in the array (they've been checked or skipped).
	// However, unlike the queue, we can just slice the array.
	cs.readyStates = cs.readyStates[offset+1:]
	atomic.StoreInt64(&cs.readyUntil, int64(cycle+1))

	return true, state == stateReady
}

// IsReadyNonBlocking checks ready state without blocking.
func (cs *ComponentSync) IsReadyNonBlocking(cycle int) (bool, bool) {
	currentReadyUntil := atomic.LoadInt64(&cs.readyUntil)
	if int64(cycle) < currentReadyUntil {
		return true, true
	}

	cs.waiterMu.Lock()
	defer cs.waiterMu.Unlock()

	// Re-check after lock
	currentReadyUntil = atomic.LoadInt64(&cs.readyUntil)
	if int64(cycle) < currentReadyUntil {
		return true, true
	}

	offset := int(int64(cycle) - currentReadyUntil)
	if offset >= len(cs.readyStates) {
		return false, false
	}

	state := cs.readyStates[offset]
	if state == stateUnset {
		return false, false
	}

	return true, state == stateReady
}

// UpdateReady updates the component's ready state.
func (cs *ComponentSync) UpdateReady(cycle int, ready bool) {
	// Optimization: sequential ready updates (Lock-free fast path)
	if ready && atomic.LoadInt64(&cs.readyUntil) == int64(cycle) {
		cs.waiterMu.Lock()
		// Double check under lock if we can advance
		if atomic.LoadInt64(&cs.readyUntil) == int64(cycle) && (len(cs.readyStates) == 0 || cs.readyStates[0] == stateUnset) {
			atomic.StoreInt64(&cs.readyUntil, int64(cycle+1))
			if len(cs.readyStates) > 0 {
				cs.readyStates = cs.readyStates[1:]
			}
			cs.waiterMu.Unlock()
			cs.notifyReady()
			return
		}
		cs.waiterMu.Unlock()
	}

	cs.waiterMu.Lock()
	defer cs.waiterMu.Unlock()

	currentReadyUntil := atomic.LoadInt64(&cs.readyUntil)
	if int64(cycle) < currentReadyUntil {
		if !ready {
			panic(fmt.Sprintf("ComponentSync: cycle %d is already marked as ready (until %d), cannot change to false", cycle, currentReadyUntil))
		}
		return
	}

	offset := int(int64(cycle) - currentReadyUntil)

	// Ensure capacity
	if offset >= len(cs.readyStates) {
		// Grow the slice
		newStates := make([]int8, offset+1)
		copy(newStates, cs.readyStates)
		cs.readyStates = newStates
	}

	// Immutability check
	if cs.readyStates[offset] != stateUnset {
		existingReady := cs.readyStates[offset] == stateReady
		if existingReady != ready {
			panic(fmt.Sprintf("ComponentSync: cycle %d already has ready=%v, cannot change to %v", cycle, existingReady, ready))
		}
		return
	}

	// Set state
	if ready {
		cs.readyStates[offset] = stateReady
	} else {
		cs.readyStates[offset] = stateNotReady
	}

	// Compaction: advance readyUntil if possible
	advanced := false
	for len(cs.readyStates) > 0 {
		if cs.readyStates[0] == stateReady {
			currentReadyUntil++
			cs.readyStates = cs.readyStates[1:]
			advanced = true
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
	cs.waiterMu.Lock()
	defer cs.waiterMu.Unlock()

	current := atomic.LoadInt64(&cs.readyUntil)
	if int64(cycle) <= current {
		return
	}

	// When jumping ahead, we need to clear/skip states in the array
	diff := int(int64(cycle) - current)
	if diff < len(cs.readyStates) {
		cs.readyStates = cs.readyStates[diff:]
	} else {
		cs.readyStates = nil
	}

	atomic.StoreInt64(&cs.readyUntil, int64(cycle))
	cs.notifyReady()
}

// InitReady initializes ready state for the first 'limit' cycles.
func (cs *ComponentSync) InitReady(limit int) {
	cs.SetReadyUntil(limit)
}
