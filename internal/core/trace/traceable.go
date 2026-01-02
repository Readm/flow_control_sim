package trace

// Traceable 是节点的可选 trace 能力接口
// 实现此接口的节点可以记录 Chrome trace 事件
// 这不是 Node 接口的一部分，而是一个独立的能力标记
type Traceable interface {
	SetTracer(tracer *TraceRecorder)
	GetTracer() *TraceRecorder
}
