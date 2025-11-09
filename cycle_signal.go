package main

import "sync"

// CycleSignal wraps a condition variable that tracks the largest completed logic cycle.
// Components use it to publish SendFinishedCycle / ReceiveFinishedCycle values and to wait
// until counterparts reach a specific logical cycle.
type CycleSignal struct {
	mu    sync.Mutex
	cond  *sync.Cond
	value int
}

// NewCycleSignal creates a CycleSignal with the provided initial value.
func NewCycleSignal(initial int) *CycleSignal {
	cs := &CycleSignal{
		value: initial,
	}
	cs.cond = sync.NewCond(&cs.mu)
	return cs
}

// Update raises the completed logic cycle and notifies all waiters when the value grows.
func (cs *CycleSignal) Update(cycle int) {
	cs.mu.Lock()
	if cycle > cs.value {
		cs.value = cycle
		cs.cond.Broadcast()
	}
	cs.mu.Unlock()
}

// WaitUntil blocks until the signal value is >= targetCycle.
func (cs *CycleSignal) WaitUntil(targetCycle int) {
	cs.mu.Lock()
	for cs.value < targetCycle {
		cs.cond.Wait()
	}
	cs.mu.Unlock()
}

// Value returns the current completed cycle.
func (cs *CycleSignal) Value() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.value
}
