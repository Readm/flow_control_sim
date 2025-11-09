package main

import (
	"math"
	"sync"
	"time"
)

// CycleCoordinator orchestrates target global cycle progression across all components.
// Components repeatedly call WaitForCycle to obtain the current target cycle, execute their
// work for that cycle, and then call MarkDone when they finish. Once all components report
// completion for the target cycle, the coordinator advances to the next cycle (until maxTarget).
type CycleCoordinator struct {
	mu             sync.Mutex
	cond           *sync.Cond
	targetCycle    int
	maxTargetCycle int

	componentDone map[string]int

	// Stall tracking for observability/debug
	stallThreshold time.Duration
	stallBitmap    map[string]bool
	lastProgress   map[string]time.Time

	stopped bool
}

// NewCycleCoordinator creates a coordinator for the provided component identifiers.
func NewCycleCoordinator(componentIDs []string) *CycleCoordinator {
	cc := &CycleCoordinator{
		targetCycle:    0,
		maxTargetCycle: math.MaxInt32,
		componentDone:  make(map[string]int, len(componentIDs)),
		stallBitmap:    make(map[string]bool, len(componentIDs)),
		lastProgress:   make(map[string]time.Time, len(componentIDs)),
	}
	for _, id := range componentIDs {
		cc.componentDone[id] = -1 // no cycle completed yet
		cc.stallBitmap[id] = false
		cc.lastProgress[id] = time.Now()
	}
	cc.cond = sync.NewCond(&cc.mu)
	cc.stallThreshold = 5 * time.Second
	return cc
}

// SetMaxTarget sets the maximum global cycle the coordinator should advance to.
func (cc *CycleCoordinator) SetMaxTarget(maxCycle int) {
	cc.mu.Lock()
	cc.maxTargetCycle = maxCycle
	cc.cond.Broadcast()
	cc.mu.Unlock()
}

// Stop notifies all waiters that the coordinator is shutting down.
func (cc *CycleCoordinator) Stop() {
	cc.mu.Lock()
	cc.stopped = true
	cc.cond.Broadcast()
	cc.mu.Unlock()
}

// WaitForCycle blocks until the coordinator assigns a cycle for the component to execute.
// Returns -1 when the coordinator has been stopped.
func (cc *CycleCoordinator) WaitForCycle(componentID string) int {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	for {
		if cc.stopped {
			return -1
		}
		if cc.targetCycle <= cc.maxTargetCycle {
			if done := cc.componentDone[componentID]; done < cc.targetCycle {
				return cc.targetCycle
			}
		}
		cc.cond.Wait()
	}
}

// MarkDone updates the component's completed cycle and advances the global cycle if all
// components have reached the current target.
func (cc *CycleCoordinator) MarkDone(componentID string, cycle int) {
	cc.mu.Lock()

	prev := cc.componentDone[componentID]
	if cycle > prev {
		cc.componentDone[componentID] = cycle
		cc.lastProgress[componentID] = time.Now()
		cc.stallBitmap[componentID] = false
		if !cc.stopped && cc.allDoneLocked() && cc.targetCycle <= cc.maxTargetCycle {
			cc.targetCycle++
			cc.cond.Broadcast()
		} else {
			cc.cond.Broadcast()
		}
	}

	cc.mu.Unlock()
}

// ReportStall marks a component as currently stalled (no progress). The component should
// call ClearStall once progress resumes.
func (cc *CycleCoordinator) ReportStall(componentID string) {
	cc.mu.Lock()
	cc.stallBitmap[componentID] = true
	cc.mu.Unlock()
}

// ClearStall clears the stall bit for a component (called when progress resumes).
func (cc *CycleCoordinator) ClearStall(componentID string) {
	cc.mu.Lock()
	cc.stallBitmap[componentID] = false
	cc.lastProgress[componentID] = time.Now()
	cc.mu.Unlock()
}

// SnapshotStallBitmap returns a copy of the stall bitmap for diagnostics.
func (cc *CycleCoordinator) SnapshotStallBitmap() map[string]bool {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	result := make(map[string]bool, len(cc.stallBitmap))
	for k, v := range cc.stallBitmap {
		result[k] = v
	}
	return result
}

// TargetCycle returns the current target cycle.
func (cc *CycleCoordinator) TargetCycle() int {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.targetCycle
}

// SnapshotProgress returns copies of target, max, and component completion states.
func (cc *CycleCoordinator) SnapshotProgress() (target int, max int, done map[string]int) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	target = cc.targetCycle
	max = cc.maxTargetCycle
	done = make(map[string]int, len(cc.componentDone))
	for k, v := range cc.componentDone {
		done[k] = v
	}
	return
}

func (cc *CycleCoordinator) allDoneLocked() bool {
	for _, done := range cc.componentDone {
		if done < cc.targetCycle {
			return false
		}
	}
	return true
}
