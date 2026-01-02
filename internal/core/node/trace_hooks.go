//go:build trace

package node

import "github.com/Readm/flow_sim/internal/core/trace"

// traceReceive 在 Receive 阶段记录 trace（编译时启用）
func (n *BaseNode) traceReceive(start, end int64, cycle uint64, packetCount int) {
	if n.tracer != nil {
		n.tracer.RecordComplete("Receive", trace.CategoryNode, n.id, trace.TidReceive,
			start, end, map[string]interface{}{
				"cycle":   cycle,
				"packets": packetCount,
			})
	}
}

// traceProcess 在 Process 阶段记录 trace（编译时启用）
func (n *BaseNode) traceProcess(start, end int64, cycle uint64) {
	if n.tracer != nil {
		n.tracer.RecordComplete("Process", trace.CategoryNode, n.id, trace.TidProcess,
			start, end, map[string]interface{}{"cycle": cycle})
	}
}

// traceSend 在 Send 阶段记录 trace（编译时启用）
func (n *BaseNode) traceSend(start, end int64, cycle uint64, sentCount int) {
	if n.tracer != nil {
		n.tracer.RecordComplete("Send", trace.CategoryNode, n.id, trace.TidSend,
			start, end, map[string]interface{}{
				"cycle": cycle,
				"sent":  sentCount,
			})
	}
}
