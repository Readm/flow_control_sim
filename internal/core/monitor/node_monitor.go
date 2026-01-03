package monitor

import (
	"fmt"
	"sync/atomic"

	"github.com/Readm/flow_sim/internal/core/trace"
)

// NodeMonitor handles profiling and tracing for a Node.
type NodeMonitor struct {
	nodeID int

	// Profiling counters
	processExecTime    atomic.Uint64 // Process phase pure execution time
	processCount       atomic.Uint64
	totalProcessCycles atomic.Uint64 // Cumulative time (compatibility)

	// Detailed Phase Profiling
	receiveCycles atomic.Uint64
	processCycles atomic.Uint64
	sendCycles    atomic.Uint64

	// Tracing
	tracer           *trace.TraceRecorder
	localTraceBuffer []trace.TraceEvent
}

// NewNodeMonitor creates a new monitor for a node.
func NewNodeMonitor(nodeID int) *NodeMonitor {
	return &NodeMonitor{
		nodeID:           nodeID,
		localTraceBuffer: make([]trace.TraceEvent, 0, 1024),
	}
}

// ===== Tracing Configuration =====

// SetTracer registers the tracer.
func (m *NodeMonitor) SetTracer(t *trace.TraceRecorder) {
	if t == nil {
		m.tracer = nil
		return
	}
	if !t.IsNodeTraced(m.nodeID) {
		m.tracer = nil
		return
	}
	m.tracer = t
	// Register as source is handled by the caller (Node) usually,
	// but we can expose an interface here.
	// Ideally Node implements TraceSource and delegates to this.
}

// GetTraceEvents returns the local buffer.
func (m *NodeMonitor) GetTraceEvents() []trace.TraceEvent {
	return m.localTraceBuffer
}

// ===== Hooks for Tick Loop =====

// OnReceiveStart is called at the beginning of the Receive phase.
func (m *NodeMonitor) OnReceiveStart() uint64 {
	// We always capture start time if we want accurate profiling,
	// even if tracing is disabled, because we have atomic counters to update.
	return GetCPUCycles()
}

// OnReceiveEnd is called at the end of the Receive phase.
func (m *NodeMonitor) OnReceiveEnd(start uint64, cycle uint64, packetCount int) {
	end := GetCPUCycles()
	duration := end - start
	m.receiveCycles.Add(duration)

	// Trace
	if m.tracer != nil && m.tracer.IsCycleTraced(int64(cycle)) && packetCount > 0 {
		ev := trace.NewCompleteEvent(
			"Receive", "node", "yellow",
			m.nodeID, trace.TidReceive,
			float64(start), float64(end),
			map[string]interface{}{
				"cycle":   cycle,
				"packets": packetCount,
			},
		)
		m.localTraceBuffer = append(m.localTraceBuffer, ev)
	}
}

// OnProcessStart is called at the beginning of the Process phase.
func (m *NodeMonitor) OnProcessStart() uint64 {
	return GetCPUCycles()
}

// OnProcessEnd is called at the end of the Process phase.
func (m *NodeMonitor) OnProcessEnd(start uint64, cycle uint64) {
	end := GetCPUCycles()
	duration := end - start

	m.processCycles.Add(duration)
	m.processExecTime.Add(duration)
	m.totalProcessCycles.Add(duration) // Will add Send time later?
	// Note: Original code added (process + send) to totalProcessCycles.
	// We need to handle that.
	// Let's increment independently.

	// Trace
	if m.tracer != nil && m.tracer.IsCycleTraced(int64(cycle)) {
		ev := trace.NewCompleteEvent(
			fmt.Sprintf("Process %d", cycle), "node", "good",
			m.nodeID, trace.TidProcess,
			float64(start), float64(end),
			map[string]interface{}{
				"cycle": cycle,
			},
		)
		m.localTraceBuffer = append(m.localTraceBuffer, ev)
	}
}

// OnSendStart is called at the beginning of the Send phase.
func (m *NodeMonitor) OnSendStart() uint64 {
	return GetCPUCycles()
}

// OnSendEnd is called at the end of the Send phase.
func (m *NodeMonitor) OnSendEnd(start uint64, cycle uint64, sentCount int) {
	end := GetCPUCycles()
	duration := end - start

	m.sendCycles.Add(duration)
	m.totalProcessCycles.Add(duration) // Add send duration to total as per original logic

	// Trace
	if m.tracer != nil && m.tracer.IsCycleTraced(int64(cycle)) && sentCount > 0 {
		ev := trace.NewCompleteEvent(
			"Send", "node", "olive",
			m.nodeID, trace.TidSend,
			float64(start), float64(end),
			map[string]interface{}{
				"cycle": cycle,
				"sent":  sentCount,
			},
		)
		m.localTraceBuffer = append(m.localTraceBuffer, ev)
	}

	// Increment process count at the end of the full tick
	m.processCount.Add(1)
}

// ===== Profiling Getters =====

func (m *NodeMonitor) TotalProcessCycles() uint64 {
	return m.totalProcessCycles.Load()
}

func (m *NodeMonitor) ProcessCount() uint64 {
	return m.processCount.Load()
}

func (m *NodeMonitor) ReceiveCycles() uint64 {
	return m.receiveCycles.Load()
}

func (m *NodeMonitor) ProcessCycles() uint64 {
	return m.processCycles.Load()
}

func (m *NodeMonitor) SendCycles() uint64 {
	return m.sendCycles.Load()
}

// GetProcessProfile returns raw process execution stats (time, count).
func (m *NodeMonitor) GetProcessProfile() (uint64, uint64) {
	return m.processExecTime.Load(), m.processCount.Load()
}

// GetAvgProcessExecTime returns the average execution time of the Handler.Process only.
func (m *NodeMonitor) GetAvgProcessExecTime() uint64 {
	count := m.processCount.Load()
	if count == 0 {
		return 0
	}
	return m.processExecTime.Load() / count
}

func (m *NodeMonitor) AvgProcessCycles() uint64 {
	count := m.processCount.Load()
	if count == 0 {
		return 0
	}
	return m.totalProcessCycles.Load() / count
}

func (m *NodeMonitor) AvgReceiveCycles() uint64 {
	count := m.processCount.Load()
	if count == 0 {
		return 0
	}
	return m.receiveCycles.Load() / count
}

func (m *NodeMonitor) AvgProcessCyclesDetailed() uint64 {
	count := m.processCount.Load()
	if count == 0 {
		return 0
	}
	return m.processCycles.Load() / count
}

func (m *NodeMonitor) AvgSendCycles() uint64 {
	count := m.processCount.Load()
	if count == 0 {
		return 0
	}
	return m.sendCycles.Load() / count
}
