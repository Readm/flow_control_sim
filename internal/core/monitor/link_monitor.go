package monitor

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/core/trace"
)

// LinkMonitor handles tracing for a Link.
type LinkMonitor struct {
	linkID int

	// Tracing state
	tracer              *trace.TraceRecorder
	localTraceBuffer    []trace.TraceEvent
	currentProcessStart float64
}

// NewLinkMonitor creates a new monitor for a link.
func NewLinkMonitor(linkID int) *LinkMonitor {
	return &LinkMonitor{
		linkID:           linkID,
		localTraceBuffer: make([]trace.TraceEvent, 0, 1024),
	}
}

// SetTracer registers the tracer.
func (m *LinkMonitor) SetTracer(t *trace.TraceRecorder) {
	if t == nil {
		m.tracer = nil
		return
	}
	m.tracer = t
	// Register source is handled by caller (Link) usually
}

// GetTraceEvents returns the local buffer.
func (m *LinkMonitor) GetTraceEvents() []trace.TraceEvent {
	return m.localTraceBuffer
}

// ===== Hooks =====

func (m *LinkMonitor) OnReceiveStart() uint64 {
	if m.tracer == nil {
		return 0
	}
	return GetCPUCycles()
}

func (m *LinkMonitor) OnReceiveEnd(start uint64, cycle uint64, packetCount int) {
	if m.tracer == nil || packetCount == 0 || !m.tracer.IsCycleTraced(int64(cycle)) {
		return
	}
	end := GetCPUCycles()

	ev := trace.NewCompleteEvent(
		"Receive", "link", "yellow",
		m.linkID, trace.TidReceive,
		float64(start), float64(end),
		map[string]interface{}{
			"cycle":   cycle,
			"packets": packetCount,
		},
	)
	m.localTraceBuffer = append(m.localTraceBuffer, ev)
}

func (m *LinkMonitor) OnProcessStart() uint64 {
	if m.tracer == nil {
		return 0
	}
	return GetCPUCycles()
}

func (m *LinkMonitor) OnProcessEnd(start uint64, cycle uint64) {
	if m.tracer == nil || !m.tracer.IsCycleTraced(int64(cycle)) {
		return
	}
	end := GetCPUCycles()

	ev := trace.NewCompleteEvent(
		fmt.Sprintf("Process %d", cycle), "link", "good",
		m.linkID, trace.TidProcess,
		float64(start), float64(end),
		map[string]interface{}{
			"cycle": cycle,
		},
	)
	m.localTraceBuffer = append(m.localTraceBuffer, ev)
}

func (m *LinkMonitor) OnSendStart() uint64 {
	if m.tracer == nil {
		return 0
	}
	return GetCPUCycles()
}

func (m *LinkMonitor) OnSendEnd(start uint64, cycle uint64) {
	if m.tracer == nil || !m.tracer.IsCycleTraced(int64(cycle)) {
		return
	}
	end := GetCPUCycles()

	ev := trace.NewCompleteEvent(
		"Send", "link", "olive",
		m.linkID, trace.TidSend,
		float64(start), float64(end),
		map[string]interface{}{
			"cycle": cycle,
		},
	)
	m.localTraceBuffer = append(m.localTraceBuffer, ev)
}

// PauseProcess temporarily stops the process event.
func (m *LinkMonitor) PauseProcess(currentStart float64, currentCycle int) {
	if m.tracer == nil || currentStart == 0 {
		return
	}
	// We use OnProcessEnd logic manually here using float64 start api for compatibility
	// Or we can convert types.
	// Since OnProcessEnd takes uint64, let's keep internal consistency or cast.
	// But wait, OnProcessEnd uses GetCPUCycles() for end time.
	// The inputs here are float64.
	// trace.NewCompleteEvent takes float64.

	end := float64(GetCPUCycles())
	if !m.tracer.IsCycleTraced(int64(currentCycle)) {
		return
	}

	ev := trace.NewCompleteEvent(
		fmt.Sprintf("Process %d", currentCycle), "link", "good",
		m.linkID, trace.TidProcess,
		currentStart, end,
		map[string]interface{}{
			"cycle": currentCycle,
		},
	)
	m.localTraceBuffer = append(m.localTraceBuffer, ev)
}

// ResumeProcess restarts a process event.
func (m *LinkMonitor) ResumeProcess() float64 {
	if m.tracer == nil {
		return 0
	}
	// Use trace.GetCPUCycles() logic (runtime.nanotime / 1000.0) if we want float64 precision matching old trace?
	// But GetCPUCycles() in monitor returns uint64.
	// We cast to float64.
	return float64(GetCPUCycles())
}

// OnWait records a wait event.
func (m *LinkMonitor) OnWait(start, end float64, cycle int) {
	if m.tracer == nil || !m.tracer.IsCycleTraced(int64(cycle)) {
		return
	}

	ev := trace.NewCompleteEvent(
		"WaitReady", "sync", "bad",
		m.linkID, trace.TidProcess, // Use TidProcess or TidReceive? Original used 1 (Unified/Receive?).
		// Actually TracedInPort used 1. TidReceive=1.
		// But WaitReady happens during Process (TidProcess=2)?
		// The original code used 1. I will stick to 1 (TidReceive) if that's what it was,
		// OR better: use TidProcess if it pauses process?
		// TracedInPort said: "Same TID/PID to appear on same track (Unified)".
		// If TidProcess is 2, and we want it on same track, maybe we should use the same TID.
		// But trace_disabled.go defines TidReceive=1, TidProcess=2.
		// Let's use TidProcess (2) if it breaks the process bar.
		start, end,
		map[string]interface{}{
			"cycle": cycle,
		},
	)
	m.localTraceBuffer = append(m.localTraceBuffer, ev)
}
