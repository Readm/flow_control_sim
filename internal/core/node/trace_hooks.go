//go:build trace

package node

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/core/trace"
)

// traceReceive 在 Receive 阶段记录 trace（编译时启用）
func (n *BaseNode) traceReceive(start, end int64, cycle uint64, packetCount int) {
	if n.tracer != nil && n.tracer.IsCycleTraced(int64(cycle)) {
		// 无锁操作：直接 append 到本地 buffer
		// 无锁操作：直接 append 到本地 buffer
		// TS 转换: Nanoseconds -> Microseconds (float64 for precision)
		event := trace.NewCompleteEvent("Receive", trace.CategoryNode, "yellow", n.id, 1, // tid=1 (Unified)
			float64(start)/1000.0, float64(end)/1000.0, map[string]interface{}{
				"cycle":   cycle,
				"packets": packetCount,
			})
		n.localTraceBuffer = append(n.localTraceBuffer, event)
	}
}

// traceProcess 在 Process 阶段记录 trace（编译时启用）
func (n *BaseNode) traceProcess(start, end int64, cycle uint64) {
	if n.tracer != nil && n.tracer.IsCycleTraced(int64(cycle)) {
		name := fmt.Sprintf("Process %d", cycle)
		event := trace.NewCompleteEvent(name, trace.CategoryNode, "good", n.id, 1, // tid=1 (Unified)
			float64(start)/1000.0, float64(end)/1000.0, map[string]interface{}{"cycle": cycle})
		n.localTraceBuffer = append(n.localTraceBuffer, event)
	}
}

// traceSend 在 Send 阶段记录 trace（编译时启用）
func (n *BaseNode) traceSend(start, end int64, cycle uint64, sentCount int) {
	if n.tracer != nil && n.tracer.IsCycleTraced(int64(cycle)) {
		event := trace.NewCompleteEvent("Send", trace.CategoryNode, "olive", n.id, 1, // tid=1 (Unified)
			float64(start)/1000.0, float64(end)/1000.0, map[string]interface{}{
				"cycle": cycle,
				"sent":  sentCount,
			})
		n.localTraceBuffer = append(n.localTraceBuffer, event)
	}
}
