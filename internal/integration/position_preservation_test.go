package integration

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/builder"
	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

// TestPositionPreservation 验证完整工作流：
// 1. 加载网络
// 2. 修改节点坐标
// 3. Build网络
// 4. Advance 100 cycles
// 5. 导出状态并转换为FlowSimNetwork
// 6. 验证：cycle已推进，节点坐标保持不变
func TestPositionPreservation(t *testing.T) {
	// ===== Step 1: 创建初始FlowSimNetwork =====
	version := "1.0.0"
	cycle := 0

	// 创建简单的2节点网络
	bufferSize := 64
	bandwidth := 1
	latency := 10

	flowNet := protocol.FlowSimNetwork{
		Version: &version,
		Cycle:   &cycle,
		Nodes: []protocol.Node{
			{
				NodeId:       0,
				NodeName:     "Node_0",
				NodeFeatures: &[]string{"*node.WorkerNode"},
				InPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: bandwidth, BufferSize: &bufferSize},
				},
				OutPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: bandwidth},
				},
				Data: protocol.Node_Data{
					Id: "node-0",
				},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{
					X: 100.0, // 初始位置
					Y: 200.0,
				},
			},
			{
				NodeId:       1,
				NodeName:     "Node_1",
				NodeFeatures: &[]string{"*node.WorkerNode"},
				InPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: bandwidth, BufferSize: &bufferSize},
				},
				OutPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: bandwidth},
				},
				Data: protocol.Node_Data{
					Id: "node-1",
				},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{
					X: 300.0, // 初始位置
					Y: 400.0,
				},
			},
		},
		Edges: []protocol.Edge{
			{
				EdgeId:    1,
				SrcNodeId: 0,
				SrcPortId: ptrInt(0),
				DstNodeId: 1,
				DstPortId: ptrInt(0),
				Latency:   &latency,
				Bandwidth: &bandwidth,
				Data: protocol.Edge_Data{
					Id:     "edge-0-p0-1-p0",
					Source: "node-0",
					Target: "node-1",
				},
			},
		},
	}

	// ===== Step 2: 修改节点坐标（模拟前端用户拖动） =====
	t.Log("Step 2: 修改节点坐标")
	flowNet.Nodes[0].Position.X = 150.0
	flowNet.Nodes[0].Position.Y = 250.0
	flowNet.Nodes[1].Position.X = 350.0
	flowNet.Nodes[1].Position.Y = 450.0

	// 记录修改后的坐标
	expectedNode0X := flowNet.Nodes[0].Position.X
	expectedNode0Y := flowNet.Nodes[0].Position.Y
	expectedNode1X := flowNet.Nodes[1].Position.X
	expectedNode1Y := flowNet.Nodes[1].Position.Y

	t.Logf("  Node 0 新坐标: (%.1f, %.1f)", expectedNode0X, expectedNode0Y)
	t.Logf("  Node 1 新坐标: (%.1f, %.1f)", expectedNode1X, expectedNode1Y)

	// ===== Step 3: Build网络 =====
	t.Log("Step 3: Build网络")
	net, err := builder.BuildFromFlowSimNetwork(flowNet)
	if err != nil {
		t.Fatalf("Build失败: %v", err)
	}
	t.Logf("  网络构建成功: %d 节点, %d 链路", len(flowNet.Nodes), len(flowNet.Edges))

	// ===== Step 4: Advance 100 cycles =====
	targetCycle := 100
	t.Logf("Step 4: Advance到 cycle %d", targetCycle)
	if err := net.AdvanceTo(targetCycle); err != nil {
		t.Fatalf("AdvanceTo失败: %v", err)
	}
	t.Log("  仿真推进成功")

	// ===== Step 5: 导出状态并转换为FlowSimNetwork =====
	t.Log("Step 5: 导出状态")
	exportedState := net.ExportState(state.ExportConfig{
		DetailLevel: state.DetailLevelSummary,
	})

	// 转换为FlowSimNetwork
	resultFlowNet := visualization.StateToFlowSimNetwork(exportedState)

	// ===== Step 6: 验证 =====
	t.Log("Step 6: 验证结果")

	// 6.1 验证cycle已推进
	if resultFlowNet.Cycle == nil {
		t.Fatal("结果中cycle为nil")
	}
	// AdvanceTo(100) 会推进到cycle 100（包含），下一个cycle是101
	expectedCycle := targetCycle + 1
	if *resultFlowNet.Cycle != expectedCycle {
		t.Errorf("Cycle未正确推进: 期望=%d, 实际=%d", expectedCycle, *resultFlowNet.Cycle)
	}
	t.Logf("  ✓ Cycle已正确推进到 %d", *resultFlowNet.Cycle)

	// 6.2 验证节点数量
	if len(resultFlowNet.Nodes) != 2 {
		t.Fatalf("节点数量错误: 期望=2, 实际=%d", len(resultFlowNet.Nodes))
	}

	// 6.3 验证节点坐标保持不变
	for _, node := range resultFlowNet.Nodes {
		var expectedX, expectedY float32
		if node.NodeId == 0 {
			expectedX = expectedNode0X
			expectedY = expectedNode0Y
		} else if node.NodeId == 1 {
			expectedX = expectedNode1X
			expectedY = expectedNode1Y
		} else {
			t.Errorf("未知的节点ID: %d", node.NodeId)
			continue
		}

		actualX := node.Position.X
		actualY := node.Position.Y

		if actualX != expectedX || actualY != expectedY {
			t.Errorf("节点 %d 坐标改变了:\n  期望=(%.1f, %.1f)\n  实际=(%.1f, %.1f)",
				node.NodeId, expectedX, expectedY, actualX, actualY)
		} else {
			t.Logf("  ✓ Node %d 坐标保持不变: (%.1f, %.1f)", node.NodeId, actualX, actualY)
		}
	}

	// 6.4 验证边信息完整
	if len(resultFlowNet.Edges) != 1 {
		t.Fatalf("边数量错误: 期望=1, 实际=%d", len(resultFlowNet.Edges))
	}
	edge := resultFlowNet.Edges[0]
	if edge.SrcNodeId != 0 || edge.DstNodeId != 1 {
		t.Errorf("边连接错误: src=%d, dst=%d", edge.SrcNodeId, edge.DstNodeId)
	}
	if edge.SrcPortId == nil || *edge.SrcPortId != 0 {
		t.Error("边的源端口ID错误")
	}
	if edge.DstPortId == nil || *edge.DstPortId != 0 {
		t.Error("边的目标端口ID错误")
	}
	t.Log("  ✓ 边信息完整且正确")

	// 6.5 验证Display data信息保留
	for _, node := range resultFlowNet.Nodes {
		if node.Data.Id == "" {
			t.Errorf("Node %d 的 Data.Id 为空", node.NodeId)
		}
		expectedId := "node-" + string(rune('0'+node.NodeId))
		if node.Data.Id != expectedId {
			t.Errorf("Node %d 的 Data.Id 错误: 期望=%s, 实际=%s",
				node.NodeId, expectedId, node.Data.Id)
		}
	}
	t.Log("  ✓ Display数据保留正确")

	t.Log("\n========== 测试通过 ==========")
	t.Log("完整工作流验证成功:")
	t.Log("  1. ✓ 加载网络")
	t.Log("  2. ✓ 修改节点坐标")
	t.Log("  3. ✓ Build网络")
	t.Logf("  4. ✓ Advance %d cycles", targetCycle)
	t.Log("  5. ✓ 导出状态")
	t.Log("  6. ✓ 坐标保持不变，cycle正确推进")
}

// ptrInt 返回int的指针
func ptrInt(i int) *int {
	return &i
}
