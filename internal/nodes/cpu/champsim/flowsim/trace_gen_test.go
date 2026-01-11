//go:build trace

package flowsim

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	champsimtrace "github.com/Readm/flow_sim/internal/nodes/cpu/champsim/trace"
	"github.com/Readm/flow_sim/internal/core/trace"
)

// TestGenerateChampSimTrace 生成 ChampSim 64-CPU 的 Chrome trace
// 运行 1000 个 cycles，生成可视化的 trace 文件
func TestGenerateChampSimTrace(t *testing.T) {
	const numSimCPUs = 64
	const maxCycles = 1000 // 运行 1000 cycles 用于性能分析
	// Use environment variable if provided, otherwise fallback to repo's small trace
	traceFile := os.Getenv("CHAMPSIM_TRACE")
	if traceFile == "" {
		largeTrace := "../../../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
		if _, err := os.Stat(largeTrace); err == nil {
			traceFile = largeTrace
		} else {
			traceFile = "../../../../../testdata/traces/small.champsimtrace"
		}
	}

	// Check if trace file is available
	testReader, err := champsimtrace.NewTraceReader(traceFile, 0, champsimtrace.FormatStandard)
	if err != nil {
		t.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE=%s)", err, traceFile)
	}
	testReader.Close()

	// 设置 16 核运行
	oldMaxProcs := runtime.GOMAXPROCS(16)
	defer runtime.GOMAXPROCS(oldMaxProcs)

	t.Logf(" 生成 ChampSim 64-CPU Chrome Trace...")
	t.Logf(" 配置: %d CPUs, %d cycles, 16 physical cores", numSimCPUs, maxCycles)

	// 1. 创建 tracer
	// 追踪所有 RingRouter 节点 (200-215)
	ringRouterIDs := make([]int, 0, 16)
	for i := 200; i < 216; i++ {
		ringRouterIDs = append(ringRouterIDs, i)
	}

	// 也追踪几个 L3 和 MemCtrl
	l3IDs := []int{96, 97, 98, 99}          // 前 4 个 L3
	memCtrlIDs := []int{104, 105, 106, 107} // 前 4 个 MemCtrl

	nodeFilter := append(ringRouterIDs, l3IDs...)
	nodeFilter = append(nodeFilter, memCtrlIDs...)

	config := trace.TracerConfig{
		Enabled:        true,
		EndCycle:       maxCycles,
		SampleRate:     1,      // 每个 cycle 都记录
		MinDuration:    0,      // 记录所有事件
		NodeFilter:     nil,    // 记录所有节点！
		BlockThreshold: 100000, // 阻塞超过 100us 才记录
	}
	tracer := trace.NewTraceRecorder(config)

	t.Logf(" Node filter: %v", nodeFilter)

	// 2. 构建系统
	net, handlers, err := buildChampSimSystem(numSimCPUs, traceFile)
	if err != nil {
		t.Fatalf("Failed to build system: %v", err)
	}
	defer handlers.Cleanup()

	// 3. 预热 trace readers（不包含在 trace 时间内）
	t.Logf(" 预热 trace readers...")
	for i, reader := range handlers.traceReaders {
		if err := reader.Warmup(); err != nil {
			t.Fatalf("Failed to warmup trace reader %d: %v", i, err)
		}
	}
	t.Logf(" Warmup 完成")

	// 4. 设置 tracer（在 Warmup 之后，确保 trace 不包含预热时间）
	net.SetTracer(tracer)
	t.Logf(" Tracer 已设置，追踪 %d 个节点", len(nodeFilter))

	// 5. 运行仿真
	t.Logf(" 开始仿真 %d cycles...", maxCycles)
	if err := net.AdvanceTo(maxCycles); err != nil {
		t.Fatalf("Simulation failed: %v", err)
	}

	// 5. 导出 trace (带元数据)
	nodeNames := make(map[int]string)

	// CPU 命名 (0-63)
	for i := 0; i < 64; i++ {
		nodeNames[i] = fmt.Sprintf("CPU_%d", i)
	}

	// L2 命名 (64-95)
	for i := 64; i < 96; i++ {
		nodeNames[i] = fmt.Sprintf("L2_%d", i-64)
	}

	// L3 命名 (96-103)
	for i := 96; i < 104; i++ {
		nodeNames[i] = fmt.Sprintf("L3_%d", i-96)
	}

	// MemCtrl 命名 (104-111)
	for i := 104; i < 112; i++ {
		nodeNames[i] = fmt.Sprintf("MemCtrl_%d", i-104)
	}

	// DRAM 命名 (112-119)
	for i := 112; i < 120; i++ {
		nodeNames[i] = fmt.Sprintf("DRAM_%d", i-112)
	}

	// RingRouter 命名 (200-215)
	for i := 200; i < 216; i++ {
		nodeNames[i] = fmt.Sprintf("RingRouter_%d", i-200)
	}

	threadNames := map[int]string{
		trace.TidReceive:  "Receive",
		trace.TidProcess:  "Process",
		trace.TidSend:     "Send",
		trace.TidTransfer: "Transfer",
	}

	outputFile := "/tmp/champsim_64cpu_trace.json"
	if err := tracer.ExportWithMetadata(outputFile, nodeNames, threadNames); err != nil {
		t.Fatalf("Failed to export trace: %v", err)
	}

	// 6. 报告结果
	events := tracer.GetEvents()
	eventCount := len(events)
	t.Logf(" Trace 生成成功!")
	t.Logf(" 生成了 %d 个事件", eventCount)
	t.Logf(" 文件位置: %s", outputFile)
	t.Logf(" 查看方式: 在 Chrome 中打开 chrome://tracing")
	t.Logf(" 操作指南:")
	t.Logf("   1. 打开 Chrome 浏览器")
	t.Logf("   2. 访问 chrome://tracing")
	t.Logf("   3. 点击 'Load' 按钮")
	t.Logf("   4. 选择 %s", outputFile)
	t.Logf("   5. 使用 WASD 导航，鼠标点击查看详情")
	t.Logf("")
	t.Logf(" 重点关注:")
	t.Logf("   - RingRouter 的 Receive 阶段是否长时间阻塞")
	t.Logf("   - Process 阶段的执行时间分布")
	t.Logf("   - Send 阶段是否被下游背压阻塞")
}

// TestGenerateFullSystemTrace 生成完整系统的 trace（所有节点）
// 注意：会生成大量数据，只运行前 50 个 cycles
func TestGenerateFullSystemTrace(t *testing.T) {
	t.Skip("跳过完整系统 trace 生成，使用 TestGenerateChampSimTrace 查看关键节点")

	const numSimCPUs = 64
	const maxCycles = 50 // 更少的 cycles
	// Use environment variable if provided, otherwise fallback to repo's small trace
	traceFile := os.Getenv("CHAMPSIM_TRACE")
	if traceFile == "" {
		largeTrace := "../../../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
		if _, err := os.Stat(largeTrace); err == nil {
			traceFile = largeTrace
		} else {
			traceFile = "../../../../../testdata/traces/small.champsimtrace"
		}
	}

	testReader, err := champsimtrace.NewTraceReader(traceFile, 0, champsimtrace.FormatStandard)
	if err != nil {
		t.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE=%s)", err, traceFile)
	}
	testReader.Close()

	oldMaxProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(oldMaxProcs)

	// 不过滤节点，记录所有节点
	config := trace.TracerConfig{
		Enabled:        true,
		EndCycle:       maxCycles,
		SampleRate:     2, // 每 2 个 cycles 采样以减少数据量
		MinDuration:    0,
		NodeFilter:     nil, // 记录所有节点
		BlockThreshold: 100000,
	}
	tracer := trace.NewTraceRecorder(config)

	net, handlers, err := buildChampSimSystem(numSimCPUs, traceFile)
	if err != nil {
		t.Fatalf("Failed to build system: %v", err)
	}
	defer handlers.Cleanup()

	net.SetTracer(tracer)

	if err := net.AdvanceTo(maxCycles); err != nil {
		t.Fatalf("Simulation failed: %v", err)
	}

	outputFile := "/tmp/champsim_64cpu_full_trace.json.gz" // 使用 gzip 压缩
	if err := tracer.Export(outputFile); err != nil {
		t.Fatalf("Failed to export trace: %v", err)
	}

	t.Logf(" 完整系统 Trace 生成成功!")
	t.Logf(" 生成了 %d 个事件", len(tracer.GetEvents()))
	t.Logf(" 文件位置: %s (gzip 压缩)", outputFile)
}
