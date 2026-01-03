//go:build trace

package link

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/core/trace"
)

// traceReceive records the start of the receive phase.
// Returns the start time (cycles) to be passed to end.
func (l *Link) traceReceiveStart() float64 {
	if l.tracer == nil {
		return 0
	}
	return trace.GetCPUCycles()
}

// traceReceiveEnd records the completion of receive phase.
func (l *Link) traceReceiveEnd(start float64, cycle int, packetCount int) {
	if l.tracer == nil || packetCount == 0 || !l.tracer.IsCycleTraced(int64(cycle)) {
		return
	}
	end := trace.GetCPUCycles()

	// User Request: Light Green (good), Light Red (bad), Yellow, Olive
	// Receive -> yellow
	ev := trace.NewCompleteEvent(
		"Receive", "link", "yellow",
		l.ID(), 1, // pid, tid=1 (Unified)
		start, end, // start, end
		map[string]interface{}{
			"cycle":   cycle,
			"packets": packetCount,
		},
	)
	l.localTraceBuffer = append(l.localTraceBuffer, ev)
}

// traceProcessStart records start of processing.
func (l *Link) traceProcessStart() float64 {
	if l.tracer == nil {
		return 0
	}
	return trace.GetCPUCycles()
}

// traceProcessEnd records end of processing.
func (l *Link) traceProcessEnd(start float64, cycle int) {
	if l.tracer == nil || !l.tracer.IsCycleTraced(int64(cycle)) {
		return
	}
	end := trace.GetCPUCycles()
	// Process -> good (Green)
	name := fmt.Sprintf("Process %d", cycle)
	ev := trace.NewCompleteEvent(
		name, "link", "good",
		l.ID(), 1, // pid, tid=1 (Unified)
		start, end, // start, end
		map[string]interface{}{
			"cycle": cycle,
		},
	)
	l.localTraceBuffer = append(l.localTraceBuffer, ev)
}

// traceSendStart records start of sending/completion.
func (l *Link) traceSendStart() float64 {
	if l.tracer == nil {
		return 0
	}
	return trace.GetCPUCycles()
}

// traceSendEnd records end of sending.
func (l *Link) traceSendEnd(start float64, cycle int) {
	if l.tracer == nil || !l.tracer.IsCycleTraced(int64(cycle)) {
		return
	}
	end := trace.GetCPUCycles()
	// Send -> olive
	ev := trace.NewCompleteEvent(
		"Send", "link", "olive",
		l.ID(), 1, // pid, tid=1 (Unified)
		start, end, // start, end
		map[string]interface{}{
			"cycle": cycle,
		},
	)
	l.localTraceBuffer = append(l.localTraceBuffer, ev)
}

// PauseProcess ends the current process event temporarily.
// Used when a sub-action (like waiting) needs to be recorded exclusively.
func (l *Link) PauseProcess() {
	if l.tracer == nil || l.currentProcessStart == 0 {
		return
	}
	l.traceProcessEnd(l.currentProcessStart, l.currentCycle)
	l.currentProcessStart = 0
}

// ResumeProcess starts a new process event after a pause.
func (l *Link) ResumeProcess() {
	if l.tracer == nil {
		return
	}
	l.currentProcessStart = l.traceProcessStart()
}
