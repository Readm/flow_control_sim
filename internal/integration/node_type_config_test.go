package integration

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/builder"
	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

// TestCPUMemoryNodeTypeConfig 测试 CPU+Memory 节点配置和统计导出
//
// Phase 7 集成测试：验证 Phase 4-6 实现的完整数据流
// 1. 使用 node_type 创建 CPU 和 Memory 节点
// 2. 配置 cpu_config 和 memory_config
// 3. 运行仿真
// 4. 验证统计数据正确导出
func TestCPUMemoryNodeTypeConfig(t *testing.T) {
	t.Log("========== 测试 CPU+Memory 节点配置和统计导出 ==========")

	// Step 1: 创建 CPU+Memory 网络配置
	t.Log("Step 1: 创建 FlowSimNetwork 配置（CPU+Memory）")

	version := "1.0.0"
	cycle := 0
	bufferSize := 128
	bandwidth := 1

	// CPU 配置
	traceFile := "../../testdata/traces/small.champsimtrace"
	robSize := 256
	lqSize := 64
	sqSize := 48
	cpuNodeType := protocol.Cpu

	// Memory 配置
	tcas := 16
	trcd := 16
	trp := 16
	tras := 38
	channels := 1
	ranks := 1
	banks := 8
	memNodeType := protocol.MemoryController

	flowNet := protocol.FlowSimNetwork{
		Version: &version,
		Cycle:   &cycle,
		Nodes: []protocol.Node{
			{
				NodeId:   0,
				NodeName: "CPU_0",
				NodeType: &cpuNodeType,
				CpuConfig: &protocol.CPUConfig{
					TraceFile: &traceFile,
					RobSize:   &robSize,
					LqSize:    &lqSize,
					SqSize:    &sqSize,
				},
				InPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: bandwidth, BufferSize: &bufferSize},
				},
				OutPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: bandwidth, BufferSize: &bufferSize},
				},
				Data: protocol.Node_Data{Id: "node-0"},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: 100.0, Y: 100.0},
			},
			{
				NodeId:   1,
				NodeName: "Memory_0",
				NodeType: &memNodeType,
				MemoryConfig: &protocol.MemoryConfig{
					TCAS:     &tcas,
					TRCD:     &trcd,
					TRP:      &trp,
					TRAS:     &tras,
					Channels: &channels,
					Ranks:    &ranks,
					Banks:    &banks,
				},
				InPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: bandwidth, BufferSize: &bufferSize},
				},
				OutPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: bandwidth, BufferSize: &bufferSize},
				},
				Data: protocol.Node_Data{Id: "node-1"},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: 300.0, Y: 100.0},
			},
		},
		Edges: []protocol.Edge{
			{
				EdgeId:    1,
				SrcNodeId: 0,
				SrcPortId: ptrInt(0),
				DstNodeId: 1,
				DstPortId: ptrInt(0),
				Data: protocol.Edge_Data{
					Id:     "edge-0-1",
					Source: "node-0",
					Target: "node-1",
				},
			},
			{
				EdgeId:    2,
				SrcNodeId: 1,
				SrcPortId: ptrInt(0),
				DstNodeId: 0,
				DstPortId: ptrInt(0),
				Data: protocol.Edge_Data{
					Id:     "edge-1-0",
					Source: "node-1",
					Target: "node-0",
				},
			},
		},
	}

	// Step 2: 从配置构建网络
	t.Log("Step 2: 使用 Builder 从配置构建网络")

	net, err := builder.BuildFromFlowSimNetwork(flowNet)
	if err != nil {
		t.Fatalf("构建网络失败: %v", err)
	}
	defer func() {
		if err := net.Close(); err != nil {
			t.Errorf("清理网络资源失败: %v", err)
		}
	}()

	// Step 3: 运行仿真
	targetCycle := 100
	t.Logf("Step 3: 运行仿真到 cycle %d", targetCycle)

	// AdvanceTo(N) 会推进到 cycle N，然后 CurrentCycle 变成 N+1
	if err := net.AdvanceTo(targetCycle - 1); err != nil {
		t.Fatalf("仿真失败: %v", err)
	}

	// Step 4: 导出状态
	t.Log("Step 4: 导出网络状态")

	networkState := net.ExportState(state.ExportConfig{DetailLevel: state.DetailLevelSummary})
	resultFlowNet := visualization.StateToFlowSimNetwork(networkState)

	// Step 5: 验证结果
	t.Log("Step 5: 验证结果")

	// 5.1 验证 cycle
	if resultFlowNet.Cycle == nil {
		t.Fatal("Cycle 为 nil")
	}
	actualCycle := *resultFlowNet.Cycle
	if actualCycle != targetCycle {
		t.Fatalf("Cycle 错误: 期望=%d, 实际=%d", targetCycle, actualCycle)
	}
	t.Logf("  ✓ Cycle 正确: %d", actualCycle)

	// 5.2 验证节点数量
	if len(resultFlowNet.Nodes) != 2 {
		t.Fatalf("节点数量错误: 期望=2, 实际=%d", len(resultFlowNet.Nodes))
	}

	// 5.3 验证 CPU 节点
	var cpuNode *protocol.Node
	for i := range resultFlowNet.Nodes {
		if resultFlowNet.Nodes[i].NodeId == 0 {
			cpuNode = &resultFlowNet.Nodes[i]
			break
		}
	}
	if cpuNode == nil {
		t.Fatal("未找到 CPU 节点")
	}

	// 验证 node_type
	if cpuNode.NodeType == nil || *cpuNode.NodeType != protocol.Cpu {
		t.Errorf("CPU node_type 错误: 期望=cpu, 实际=%v", cpuNode.NodeType)
	} else {
		t.Log("  ✓ CPU node_type 正确: cpu")
	}

	// 验证 cpu_config 存在
	if cpuNode.CpuConfig == nil {
		t.Fatal("CPU cpu_config 为 nil")
	}

	// 验证统计数据（CPU 应该执行了一些指令）
	if cpuNode.CpuConfig.TotalInstructions == nil {
		t.Error("CPU total_instructions 为 nil")
	} else if *cpuNode.CpuConfig.TotalInstructions <= 0 {
		t.Errorf("CPU total_instructions 应该 > 0, 实际=%d", *cpuNode.CpuConfig.TotalInstructions)
	} else {
		t.Logf("  ✓ CPU total_instructions: %d", *cpuNode.CpuConfig.TotalInstructions)
	}

	if cpuNode.CpuConfig.TotalCycles != nil {
		t.Logf("  ✓ CPU total_cycles: %d", *cpuNode.CpuConfig.TotalCycles)
	}

	if cpuNode.CpuConfig.Ipc != nil {
		t.Logf("  ✓ CPU IPC: %.4f", *cpuNode.CpuConfig.Ipc)
	}

	// 5.4 验证 Memory 节点
	var memNode *protocol.Node
	for i := range resultFlowNet.Nodes {
		if resultFlowNet.Nodes[i].NodeId == 1 {
			memNode = &resultFlowNet.Nodes[i]
			break
		}
	}
	if memNode == nil {
		t.Fatal("未找到 Memory 节点")
	}

	// 验证 node_type
	if memNode.NodeType == nil || *memNode.NodeType != protocol.MemoryController {
		t.Errorf("Memory node_type 错误: 期望=memory_controller, 实际=%v", memNode.NodeType)
	} else {
		t.Log("  ✓ Memory node_type 正确: memory_controller")
	}

	// 验证 memory_config 存在
	if memNode.MemoryConfig == nil {
		t.Fatal("Memory memory_config 为 nil")
	}

	// 验证 Memory 配置参数（应该从 configRef 恢复）
	if memNode.MemoryConfig.TCAS == nil {
		t.Error("Memory TCAS 为 nil，配置参数未正确导出")
	} else if *memNode.MemoryConfig.TCAS != tcas {
		t.Errorf("Memory TCAS 错误: 期望=%d, 实际=%d", tcas, *memNode.MemoryConfig.TCAS)
	} else {
		t.Logf("  ✓ Memory TCAS: %d", *memNode.MemoryConfig.TCAS)
	}

	// 验证 CPU 配置参数
	if cpuNode.CpuConfig.RobSize == nil {
		t.Error("CPU rob_size 为 nil，配置参数未正确导出")
	} else if *cpuNode.CpuConfig.RobSize != robSize {
		t.Errorf("CPU rob_size 错误: 期望=%d, 实际=%d", robSize, *cpuNode.CpuConfig.RobSize)
	} else {
		t.Logf("  ✓ CPU rob_size: %d", *cpuNode.CpuConfig.RobSize)
	}

	// 验证统计数据（如果 CPU 发送了请求，Memory 应该有统计）
	if memNode.MemoryConfig.ReadRequests != nil {
		t.Logf("  ✓ Memory read_requests: %d", *memNode.MemoryConfig.ReadRequests)
	}
	if memNode.MemoryConfig.WriteRequests != nil {
		t.Logf("  ✓ Memory write_requests: %d", *memNode.MemoryConfig.WriteRequests)
	}

	t.Log("\n========== CPU+Memory 配置和统计导出测试通过 ==========")
}

// TestNodeTypeRoundTrip 测试往返一致性：Config → Build → Simulate → Export → Config
//
// 验证导出的 Protocol 可以重新构建网络，且配置保持不变
func TestNodeTypeRoundTrip(t *testing.T) {
	t.Log("========== 测试往返一致性 ==========")

	// Step 1: 创建初始配置
	t.Log("Step 1: 创建初始配置")

	version := "1.0.0"
	cycle := 0
	bufferSize := 64
	bandwidth := 1

	traceFile := "../../testdata/traces/small.champsimtrace"
	robSize := 128
	cpuNodeType := protocol.Cpu
	memNodeType := protocol.MemoryController

	// Memory 配置
	tcas := 16
	trcd := 16
	trp := 16
	tras := 38

	initialNet := protocol.FlowSimNetwork{
		Version: &version,
		Cycle:   &cycle,
		Nodes: []protocol.Node{
			{
				NodeId:   0,
				NodeName: "CPU_0",
				NodeType: &cpuNodeType,
				CpuConfig: &protocol.CPUConfig{
					TraceFile: &traceFile,
					RobSize:   &robSize,
				},
				InPorts:  &[]protocol.Port{{PortId: 0, Bandwidth: bandwidth, BufferSize: &bufferSize}},
				OutPorts: &[]protocol.Port{{PortId: 0, Bandwidth: bandwidth, BufferSize: &bufferSize}},
				Data:     protocol.Node_Data{Id: "node-0"},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: 100.0, Y: 100.0},
			},
			{
				NodeId:   1,
				NodeName: "Memory_0",
				NodeType: &memNodeType,
				MemoryConfig: &protocol.MemoryConfig{
					TCAS: &tcas,
					TRCD: &trcd,
					TRP:  &trp,
					TRAS: &tras,
				},
				InPorts:  &[]protocol.Port{{PortId: 0, Bandwidth: bandwidth, BufferSize: &bufferSize}},
				OutPorts: &[]protocol.Port{{PortId: 0, Bandwidth: bandwidth, BufferSize: &bufferSize}},
				Data:     protocol.Node_Data{Id: "node-1"},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: 300.0, Y: 100.0},
			},
		},
		Edges: []protocol.Edge{
			{
				EdgeId:    1,
				SrcNodeId: 0,
				SrcPortId: ptrInt(0),
				DstNodeId: 1,
				DstPortId: ptrInt(0),
				Data: protocol.Edge_Data{
					Id:     "edge-0-1",
					Source: "node-0",
					Target: "node-1",
				},
			},
			{
				EdgeId:    2,
				SrcNodeId: 1,
				SrcPortId: ptrInt(0),
				DstNodeId: 0,
				DstPortId: ptrInt(0),
				Data: protocol.Edge_Data{
					Id:     "edge-1-0",
					Source: "node-1",
					Target: "node-0",
				},
			},
		},
	}

	// Step 2: 构建网络
	t.Log("Step 2: 构建网络")
	net, err := builder.BuildFromFlowSimNetwork(initialNet)
	if err != nil {
		t.Fatalf("构建网络失败: %v", err)
	}
	defer func() {
		if err := net.Close(); err != nil {
			t.Errorf("清理网络资源失败: %v", err)
		}
	}()

	// Step 3: 运行仿真
	t.Log("Step 3: 运行仿真")
	if err := net.AdvanceTo(50); err != nil {
		t.Fatalf("仿真失败: %v", err)
	}

	// Step 4: 导出状态
	t.Log("Step 4: 导出状态")
	exportedState := net.ExportState(state.ExportConfig{DetailLevel: state.DetailLevelSummary})
	exportedNet := visualization.StateToFlowSimNetwork(exportedState)

	// Step 5: 验证配置保持不变
	t.Log("Step 5: 验证配置保持不变")

	if len(exportedNet.Nodes) != 2 {
		t.Fatalf("节点数量错误: 期望=2, 实际=%d", len(exportedNet.Nodes))
	}

	// 验证 CPU 节点
	var cpuNode *protocol.Node
	for i := range exportedNet.Nodes {
		if exportedNet.Nodes[i].NodeId == 0 {
			cpuNode = &exportedNet.Nodes[i]
			break
		}
	}
	if cpuNode == nil {
		t.Fatal("未找到 CPU 节点")
	}

	// 验证 node_type
	if cpuNode.NodeType == nil || *cpuNode.NodeType != protocol.Cpu {
		t.Errorf("CPU node_type 改变了: 期望=cpu, 实际=%v", cpuNode.NodeType)
	} else {
		t.Log("  ✓ CPU node_type 保持不变: cpu")
	}

	// 验证 cpu_config 存在（配置应该保留，统计应该添加）
	if cpuNode.CpuConfig == nil {
		t.Fatal("cpu_config 丢失")
	}

	// 验证 ROB size 配置保留（Phase 2 实现：从 configRef 读取）
	if cpuNode.CpuConfig.RobSize == nil {
		t.Error("CPU rob_size 为 nil，配置参数未正确导出")
	} else if *cpuNode.CpuConfig.RobSize != robSize {
		t.Errorf("CPU rob_size 改变了: 期望=%d, 实际=%d", robSize, *cpuNode.CpuConfig.RobSize)
	} else {
		t.Logf("  ✓ CPU rob_size 保持不变: %d", *cpuNode.CpuConfig.RobSize)
	}

	// 验证统计数据被正确添加
	if cpuNode.CpuConfig.TotalInstructions == nil {
		t.Error("统计数据未添加: total_instructions 为 nil")
	} else {
		t.Logf("  ✓ 统计数据已添加: total_instructions=%d", *cpuNode.CpuConfig.TotalInstructions)
	}

	// 验证 Position 保持不变
	if cpuNode.Position.X != 100.0 || cpuNode.Position.Y != 100.0 {
		t.Errorf("CPU Position 改变了: 期望=(100.0, 100.0), 实际=(%.1f, %.1f)",
			cpuNode.Position.X, cpuNode.Position.Y)
	} else {
		t.Log("  ✓ CPU Position 保持不变: (100.0, 100.0)")
	}

	// 验证 Memory 节点
	var memNode *protocol.Node
	for i := range exportedNet.Nodes {
		if exportedNet.Nodes[i].NodeId == 1 {
			memNode = &exportedNet.Nodes[i]
			break
		}
	}
	if memNode == nil {
		t.Fatal("未找到 Memory 节点")
	}

	// 验证 Memory 配置参数保持不变
	if memNode.MemoryConfig == nil {
		t.Fatal("Memory memory_config 丢失")
	}

	if memNode.MemoryConfig.TCAS == nil {
		t.Error("Memory TCAS 为 nil，配置参数未正确导出")
	} else if *memNode.MemoryConfig.TCAS != tcas {
		t.Errorf("Memory TCAS 改变了: 期望=%d, 实际=%d", tcas, *memNode.MemoryConfig.TCAS)
	} else {
		t.Logf("  ✓ Memory TCAS 保持不变: %d", *memNode.MemoryConfig.TCAS)
	}

	// 验证 Memory Position 保持不变
	if memNode.Position.X != 300.0 || memNode.Position.Y != 100.0 {
		t.Errorf("Memory Position 改变了: 期望=(300.0, 100.0), 实际=(%.1f, %.1f)",
			memNode.Position.X, memNode.Position.Y)
	} else {
		t.Log("  ✓ Memory Position 保持不变: (300.0, 100.0)")
	}

	t.Log("\n========== 往返一致性测试通过 ==========")
}
