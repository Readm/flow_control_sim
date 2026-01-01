//go:build trace

package trace_test

import (
	"fmt"
	"testing"

	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/core/trace"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// MockHandler 简单的测试 handler
type MockHandler struct{}

func (h *MockHandler) Process(cycle uint64, inputs [][]queue.PacketRef) error {
	// 简单处理：什么都不做
	return nil
}

// TestTraceBasic 测试基本的 trace 功能
func TestTraceBasic(t *testing.T) {
	// 1. 创建 tracer
	config := trace.TracerConfig{
		Enabled:        true,
		MaxCycles:      10, // 只记录前 10 个 cycles
		SampleRate:     1,  // 每个 cycle 都记录
		MinDuration:    0,
		BlockThreshold: 1000,
	}
	tracer := trace.NewTraceRecorder(config)

	// 2. 创建简单的 network: Node1 -> Node2
	net := network.New()
	net.SetTracer(tracer)

	// Node 1
	node1 := node.NewBaseNode(1, &MockHandler{})
	iq1 := queue.NewInputQueue(10, 1)
	oq1 := queue.NewOutputQueue(10, 1)
	node1.AddInputQueue(iq1)
	node1.AddOutputQueue(oq1)

	// Node 2
	node2 := node.NewBaseNode(2, &MockHandler{})
	iq2 := queue.NewInputQueue(10, 1)
	oq2 := queue.NewOutputQueue(10, 1)
	node2.AddInputQueue(iq2)
	node2.AddOutputQueue(oq2)

	// 添加到 network
	net.AddNode(&network.NodeHandle{Node: node1, Inputs: []*queue.InputQueue{iq1}, Outputs: []*queue.OutputQueue{oq1}})
	net.AddNode(&network.NodeHandle{Node: node2, Inputs: []*queue.InputQueue{iq2}, Outputs: []*queue.OutputQueue{oq2}})

	// 3. 连接节点 (sourceID, sourceOutputIdx, targetID, targetInputIdx, latency, bandwidth)
	_, err := net.Connect(1, 0, 2, 0, 1, 1)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// 4. 注入一些包
	node1.InjectPacket(packet.Packet{
		SourceID: 1,
		TargetID: 2,
		Payload:  "test",
	})

	// 5. 运行几个 cycles
	if err := net.AdvanceTo(5); err != nil {
		t.Fatalf("AdvanceTo failed: %v", err)
	}

	// 6. 导出 trace
	outputFile := "/tmp/trace_test.json"
	if err := tracer.Export(outputFile); err != nil {
		t.Fatalf("Failed to export trace: %v", err)
	}

	// 7. 验证
	eventCount := tracer.EventCount()
	if eventCount == 0 {
		t.Errorf("Expected some events, got 0")
	}

	fmt.Printf("✅ Trace test passed! Generated %d events\n", eventCount)
	fmt.Printf("📊 Trace file: %s\n", outputFile)
	fmt.Printf("🌐 View in Chrome: chrome://tracing\n")
}

// TestTraceWithMetadata 测试带元数据的导出
func TestTraceWithMetadata(t *testing.T) {
	config := trace.DefaultConfig()
	config.MaxCycles = 5
	tracer := trace.NewTraceRecorder(config)

	// 创建简单的 network
	net := network.New()
	net.SetTracer(tracer)

	node1 := node.NewBaseNode(100, &MockHandler{})
	iq := queue.NewInputQueue(10, 1)
	oq := queue.NewOutputQueue(10, 1)
	node1.AddInputQueue(iq)
	node1.AddOutputQueue(oq)

	net.AddNode(&network.NodeHandle{Node: node1, Inputs: []*queue.InputQueue{iq}, Outputs: []*queue.OutputQueue{oq}})

	// 运行
	if err := net.AdvanceTo(3); err != nil {
		t.Fatalf("AdvanceTo failed: %v", err)
	}

	// 带元数据导出
	nodeNames := map[int]string{
		100: "TestCPU",
	}
	threadNames := map[int]string{
		trace.TidReceive:  "Receive",
		trace.TidProcess:  "Process",
		trace.TidSend:     "Send",
		trace.TidTransfer: "Transfer",
	}

	outputFile := "/tmp/trace_with_metadata.json"
	if err := tracer.ExportWithMetadata(outputFile, nodeNames, threadNames); err != nil {
		t.Fatalf("Failed to export with metadata: %v", err)
	}

	fmt.Printf("✅ Metadata test passed!\n")
	fmt.Printf("📊 Trace file: %s\n", outputFile)
}
