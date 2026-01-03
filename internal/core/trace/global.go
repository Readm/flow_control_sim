package trace

import (
	"flag"
	"fmt"
	"sync"
)

var (
	// traceFileFlag 定义命令行参数 -flow_trace
	// 注意：主程序必须调用 flag.Parse() 才能生效
	traceFileFlag  = flag.String("flow_trace", "", "Output file for flow_sim internal trace (Chrome Trace format)")
	traceStartFlag = flag.Int("flow_trace_start", 0, "Start cycle for tracing (0 = from beginning)")
	traceEndFlag   = flag.Int("flow_trace_end", 0, "End cycle for tracing (0 = unlimited)")

	globalTracer *TraceRecorder
	globalOnce   sync.Once
)

// GetGlobalTracer 获取全局单例 Tracer。
// 如果 -flow_trace 未设置，返回 nil。
// 如果是第一次调用且 -flow_trace 已设置，则初始化 Tracer。
// 线程安全。
func GetGlobalTracer() *TraceRecorder {
	globalOnce.Do(func() {
		if *traceFileFlag != "" {
			fmt.Printf("[FlowSim] Enabling global trace to %s (Start: %d, End: %d)\n", *traceFileFlag, *traceStartFlag, *traceEndFlag)
			config := DefaultConfig()
			config.StartCycle = *traceStartFlag
			config.EndCycle = *traceEndFlag
			globalTracer = NewTraceRecorder(config)
		}
	})
	return globalTracer
}

// FlushGlobal 将全局 Tracer 的数据写入文件（如果已启用）。
// 通常在 main 结束或仿真结束时调用。
func FlushGlobal() error {
	t := GetGlobalTracer()
	if t == nil {
		return nil // 未启用，忽略
	}

	filename := *traceFileFlag
	if filename == "" {
		return nil
	}

	fmt.Printf("[FlowSim] Flushing global trace to %s...\n", filename)
	return t.Export(filename)
}
