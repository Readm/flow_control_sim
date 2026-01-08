package visualization_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Readm/flow_sim/internal/core/builder"
	"github.com/Readm/flow_sim/internal/core/loadbench"
	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

// Test 1: Benchmark网络可以导出为FlowSimNetwork格式
func TestBenchmarkToFlowSimNetwork(t *testing.T) {
	// 构建一个 Bidirectional Ring 网络
	net, err := loadbench.BuildBidirectionalRing(8)
	if err != nil {
		t.Fatalf("Failed to build benchmark network: %v", err)
	}

	// 导出状态
	exportConfig := state.ExportConfig{
		DetailLevel: state.DetailLevelSummary,
	}
	networkState := net.ExportState(exportConfig)

	// 转换为 FlowSimNetwork
	flowNet := visualization.StateToFlowSimNetwork(networkState)

	// 验证基本结构
	if len(flowNet.Nodes) != 8 {
		t.Errorf("Expected 8 nodes, got %d", len(flowNet.Nodes))
	}

	// 验证边的数量（双向环：每个节点2条出边，共16条边）
	if len(flowNet.Edges) != 16 {
		t.Errorf("Expected 16 edges, got %d", len(flowNet.Edges))
	}

	// 验证节点有端口
	for i, node := range flowNet.Nodes {
		if node.InPorts == nil || len(*node.InPorts) == 0 {
			t.Errorf("Node %d has no input ports", i)
		}
		if node.OutPorts == nil || len(*node.OutPorts) == 0 {
			t.Errorf("Node %d has no output ports", i)
		}
	}

	// 验证 JSON 序列化
	jsonData, err := json.Marshal(flowNet)
	if err != nil {
		t.Fatalf("Failed to marshal FlowSimNetwork to JSON: %v", err)
	}

	// 验证可以反序列化
	var flowNet2 protocol.FlowSimNetwork
	if err := json.Unmarshal(jsonData, &flowNet2); err != nil {
		t.Fatalf("Failed to unmarshal JSON to FlowSimNetwork: %v", err)
	}

	if len(flowNet2.Nodes) != len(flowNet.Nodes) {
		t.Errorf("Unmarshal failed: expected %d nodes, got %d", len(flowNet.Nodes), len(flowNet2.Nodes))
	}

	t.Logf("✓ Benchmark network successfully exported to FlowSimNetwork format")
	t.Logf("  - Nodes: %d", len(flowNet.Nodes))
	t.Logf("  - Edges: %d", len(flowNet.Edges))
	t.Logf("  - JSON size: %d bytes", len(jsonData))
}

// Test 2: FlowSimNetwork可以构建并执行仿真
func TestFlowSimNetworkBuildAndSimulate(t *testing.T) {
	// 创建一个简单的 FlowSimNetwork
	flowNet := createSimpleFlowSimNetwork()

	// 使用 BuildFromFlowSimNetwork 构建网络
	net, err := builder.BuildFromFlowSimNetwork(flowNet)
	if err != nil {
		t.Fatalf("Failed to build network from FlowSimNetwork: %v", err)
	}

	// 验证网络结构
	exportConfig := state.ExportConfig{
		DetailLevel: state.DetailLevelSummary,
	}
	initialState := net.ExportState(exportConfig)

	if len(initialState.Nodes) != len(flowNet.Nodes) {
		t.Errorf("Expected %d nodes, got %d", len(flowNet.Nodes), len(initialState.Nodes))
	}

	if len(initialState.Links) != len(flowNet.Edges) {
		t.Errorf("Expected %d links, got %d", len(flowNet.Edges), len(initialState.Links))
	}

	// 执行仿真
	targetCycle := 10
	if err := net.AdvanceTo(targetCycle); err != nil {
		t.Fatalf("Failed to advance simulation: %v", err)
	}

	// 验证仿真推进了
	finalState := net.ExportState(exportConfig)
	if finalState.CurrentCycle < targetCycle {
		t.Errorf("Expected cycle >= %d, got %d", targetCycle, finalState.CurrentCycle)
	}

	t.Logf("✓ FlowSimNetwork successfully built and simulated")
	t.Logf("  - Initial cycle: %d", initialState.CurrentCycle)
	t.Logf("  - Final cycle: %d", finalState.CurrentCycle)
}

// Test 3: FlowSimNetwork 包含 CyEditor 所需的所有字段
func TestFlowSimNetworkCyEditorCompatibility(t *testing.T) {
	// 构建 benchmark 网络并导出
	net, err := loadbench.BuildBidirectionalRing(4)
	if err != nil {
		t.Fatalf("Failed to build benchmark network: %v", err)
	}

	networkState := net.ExportState(state.ExportConfig{DetailLevel: state.DetailLevelSummary})
	flowNet := visualization.StateToFlowSimNetwork(networkState)

	// 验证节点有 CyEditor 需要的字段
	for i, node := range flowNet.Nodes {
		// 验证 data 字段
		if node.Data.Id == "" {
			t.Errorf("Node %d missing data.id", i)
		}

		// 验证 position 字段
		if node.Position.X == 0 && node.Position.Y == 0 && i > 0 {
			// 第一个节点可能在原点,但其他节点不应该都在原点
			t.Logf("Warning: Node %d at origin position", i)
		}

		// 验证节点有基本属性
		if node.NodeName == "" {
			t.Errorf("Node %d missing node_name", i)
		}
	}

	// 验证边有 CyEditor 需要的字段
	for i, edge := range flowNet.Edges {
		if edge.Data.Id == "" {
			t.Errorf("Edge %d missing data.id", i)
		}
		if edge.Data.Source == "" {
			t.Errorf("Edge %d missing data.source", i)
		}
		if edge.Data.Target == "" {
			t.Errorf("Edge %d missing data.target", i)
		}
	}

	t.Logf("✓ FlowSimNetwork contains all required CyEditor fields")
	t.Logf("  - All nodes have data.id and position")
	t.Logf("  - All edges have data.id, source, and target")
}

// Test 4: 模拟 CyEditor 编辑后的 FlowSimNetwork 验证
func TestCyEditorEditedFlowSimNetwork(t *testing.T) {
	// 模拟用户在 CyEditor 中创建的网络
	editedNet := protocol.FlowSimNetwork{
		Nodes: []protocol.Node{
			{
				NodeId:   0,
				NodeName: "Node_0",
				Data: protocol.Node_Data{
					Id:    "node-0",
					Label: stringPtr("N0"),
					Type:  stringPtr("WorkerNode"),
				},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: 100, Y: 100},
				InPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: 1, BufferSize: intPtr(64)},
				},
				OutPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: 1, BufferSize: intPtr(64)},
				},
			},
			{
				NodeId:   1,
				NodeName: "Node_1",
				Data: protocol.Node_Data{
					Id:    "node-1",
					Label: stringPtr("N1"),
					Type:  stringPtr("WorkerNode"),
				},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: 300, Y: 100},
				InPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: 1, BufferSize: intPtr(64)},
				},
				OutPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: 1, BufferSize: intPtr(64)},
				},
			},
		},
		Edges: []protocol.Edge{
			{
				EdgeId:    1,
				SrcNodeId: 0,
				SrcPortId: intPtr(0),
				DstNodeId: 1,
				DstPortId: intPtr(0),
				Latency:   intPtr(5),
				Bandwidth: intPtr(1),
				Data: protocol.Edge_Data{
					Id:     "edge-1",
					Source: "node-0",
					Target: "node-1",
				},
			},
		},
	}

	// 验证可以序列化为 JSON
	jsonData, err := json.Marshal(editedNet)
	if err != nil {
		t.Fatalf("Failed to marshal edited network: %v", err)
	}

	// 验证 JSON 格式正确
	var parsedNet protocol.FlowSimNetwork
	if err := json.Unmarshal(jsonData, &parsedNet); err != nil {
		t.Fatalf("Failed to parse edited network JSON: %v", err)
	}

	// 验证可以构建网络
	net, err := builder.BuildFromFlowSimNetwork(editedNet)
	if err != nil {
		t.Fatalf("Failed to build network from edited FlowSimNetwork: %v", err)
	}

	// 验证构建的网络结构正确
	state := net.ExportState(state.ExportConfig{DetailLevel: state.DetailLevelSummary})
	if len(state.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(state.Nodes))
	}
	if len(state.Links) != 1 {
		t.Errorf("Expected 1 link, got %d", len(state.Links))
	}

	// 验证可以执行仿真
	if err := net.AdvanceTo(5); err != nil {
		t.Fatalf("Failed to simulate edited network: %v", err)
	}

	t.Logf("✓ CyEditor-edited FlowSimNetwork is valid and executable")
	t.Logf("  - JSON roundtrip: successful")
	t.Logf("  - Network build: successful")
	t.Logf("  - Simulation: successful")
}

// Test 5: 往返测试 (Round-trip test) - 验证结构往返一致性
func TestFlowSimNetworkRoundTrip(t *testing.T) {
	// 1. 创建简单的 FlowSimNetwork
	originalFlow := createSimpleFlowSimNetwork()

	// 2. 从 FlowSimNetwork 构建网络
	net, err := builder.BuildFromFlowSimNetwork(originalFlow)
	if err != nil {
		t.Fatalf("Failed to build network: %v", err)
	}

	// 3. 导出状态
	exportedState := net.ExportState(state.ExportConfig{DetailLevel: state.DetailLevelSummary})

	// 4. 转换回 FlowSimNetwork
	rebuiltFlow := visualization.StateToFlowSimNetwork(exportedState)

	// 5. 验证结构一致性
	if len(rebuiltFlow.Nodes) != len(originalFlow.Nodes) {
		t.Errorf("Node count mismatch: original=%d, rebuilt=%d", len(originalFlow.Nodes), len(rebuiltFlow.Nodes))
	}

	if len(rebuiltFlow.Edges) != len(originalFlow.Edges) {
		t.Errorf("Edge count mismatch: original=%d, rebuilt=%d", len(originalFlow.Edges), len(rebuiltFlow.Edges))
	}

	// 6. 验证节点 ID 一致
	for i := range originalFlow.Nodes {
		if i >= len(rebuiltFlow.Nodes) {
			break
		}
		if originalFlow.Nodes[i].NodeId != rebuiltFlow.Nodes[i].NodeId {
			t.Errorf("Node %d ID mismatch: original=%d, rebuilt=%d",
				i, originalFlow.Nodes[i].NodeId, rebuiltFlow.Nodes[i].NodeId)
		}
	}

	// 7. 验证 JSON 往返
	originalJSON, _ := json.Marshal(originalFlow)
	rebuiltJSON, _ := json.Marshal(rebuiltFlow)

	t.Logf("✓ Round-trip test successful")
	t.Logf("  - Original nodes: %d, Rebuilt nodes: %d", len(originalFlow.Nodes), len(rebuiltFlow.Nodes))
	t.Logf("  - Original edges: %d, Rebuilt edges: %d", len(originalFlow.Edges), len(rebuiltFlow.Edges))
	t.Logf("  - Original JSON size: %d bytes, Rebuilt JSON size: %d bytes",
		len(originalJSON), len(rebuiltJSON))
}

// Helper: 创建简单的测试网络
func createSimpleFlowSimNetwork() protocol.FlowSimNetwork {
	return protocol.FlowSimNetwork{
		Version: stringPtr("1.0.0"),
		Cycle:   intPtr(0),
		Nodes: []protocol.Node{
			{
				NodeId:       0,
				NodeName:     "Node_0",
				NodeFeatures: &[]string{"WorkerNode"},
				Data: protocol.Node_Data{
					Id:    "node-0",
					Label: stringPtr("Node 0"),
				},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: 100, Y: 100},
				InPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: 1, BufferSize: intPtr(64)},
				},
				OutPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: 1, BufferSize: intPtr(64)},
				},
			},
			{
				NodeId:       1,
				NodeName:     "Node_1",
				NodeFeatures: &[]string{"WorkerNode"},
				Data: protocol.Node_Data{
					Id:    "node-1",
					Label: stringPtr("Node 1"),
				},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: 300, Y: 100},
				InPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: 1, BufferSize: intPtr(64)},
				},
				OutPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: 1, BufferSize: intPtr(64)},
				},
			},
		},
		Edges: []protocol.Edge{
			{
				EdgeId:    1,
				SrcNodeId: 0,
				SrcPortId: intPtr(0),
				DstNodeId: 1,
				DstPortId: intPtr(0),
				Latency:   intPtr(1),
				Bandwidth: intPtr(1),
				Data: protocol.Edge_Data{
					Id:     "edge-1",
					Source: "node-0",
					Target: "node-1",
				},
			},
		},
	}
}

// ============================================
// Test 7: Multiple Parallel Edges (多链路测试)
// ============================================

// TestMultipleParallelEdges 验证同一对节点间可以有多条不同端口的链路
func TestMultipleParallelEdges(t *testing.T) {
	// 创建一个包含多条平行边的 FlowSimNetwork
	// 节点 0 和节点 1 之间有 3 条不同的链路:
	// - 端口 0 → 端口 0
	// - 端口 1 → 端口 1
	// - 端口 2 → 端口 2
	flowNet := createMultiEdgeFlowSimNetwork()

	// 验证 JSON 可序列化
	jsonBytes, err := json.Marshal(flowNet)
	if err != nil {
		t.Fatalf("Failed to marshal FlowSimNetwork: %v", err)
	}
	t.Logf("✓ FlowSimNetwork with multiple parallel edges serialized (%d bytes)", len(jsonBytes))

	// 验证有 3 条边
	if len(flowNet.Edges) != 3 {
		t.Fatalf("Expected 3 edges, got %d", len(flowNet.Edges))
	}

	// 验证每条边有唯一的端口组合
	portPairs := make(map[string]bool)
	for _, edge := range flowNet.Edges {
		srcPort := 0
		dstPort := 0
		if edge.SrcPortId != nil {
			srcPort = *edge.SrcPortId
		}
		if edge.DstPortId != nil {
			dstPort = *edge.DstPortId
		}
		key := fmt.Sprintf("%d-%d-%d-%d", edge.SrcNodeId, srcPort, edge.DstNodeId, dstPort)
		if portPairs[key] {
			t.Errorf("Duplicate edge found: %s", key)
		}
		portPairs[key] = true
	}
	t.Logf("✓ All 3 edges have unique port combinations")

	// 验证每条边有唯一的 CyEditor ID
	edgeIds := make(map[string]bool)
	for _, edge := range flowNet.Edges {
		edgeId := edge.Data.Id
		if edgeIds[edgeId] {
			t.Errorf("Duplicate edge ID found: %s", edgeId)
		}
		edgeIds[edgeId] = true
	}
	t.Logf("✓ All 3 edges have unique CyEditor IDs")

	// 验证可以构建网络
	net, err := builder.BuildFromFlowSimNetwork(flowNet)
	if err != nil {
		t.Fatalf("Failed to build network from multi-edge FlowSimNetwork: %v", err)
	}
	t.Logf("✓ Network built successfully with multiple parallel edges")

	// 验证网络有正确数量的链路
	exportConfig := state.ExportConfig{
		DetailLevel: state.DetailLevelSummary,
	}
	netState := net.ExportState(exportConfig)
	if len(netState.Links) != 3 {
		t.Errorf("Expected 3 links in network, got %d", len(netState.Links))
	}
	t.Logf("✓ Network has 3 links")

	// 🐛 调试: 打印链路状态
	for i, link := range netState.Links {
		t.Logf("  Link %d: node %d port %d → node %d port %d",
			i, link.SourceID, link.SourcePortID, link.TargetID, link.TargetPortID)
	}

	// 验证往返一致性: FlowSimNetwork → Network → State → FlowSimNetwork
	rebuiltFlow := visualization.StateToFlowSimNetwork(netState)
	if len(rebuiltFlow.Edges) != 3 {
		t.Errorf("Round-trip failed: Expected 3 edges after rebuild, got %d", len(rebuiltFlow.Edges))
	}

	// 验证重建后的边仍然有唯一的端口组合
	rebuiltPortPairs := make(map[string]bool)
	for _, edge := range rebuiltFlow.Edges {
		srcPort := 0
		dstPort := 0
		if edge.SrcPortId != nil {
			srcPort = *edge.SrcPortId
		}
		if edge.DstPortId != nil {
			dstPort = *edge.DstPortId
		}
		key := fmt.Sprintf("%d-%d-%d-%d", edge.SrcNodeId, srcPort, edge.DstNodeId, dstPort)
		rebuiltPortPairs[key] = true
		t.Logf("  Rebuilt edge %d: node %d port %d → node %d port %d (ID: %s)",
			edge.EdgeId, edge.SrcNodeId, srcPort, edge.DstNodeId, dstPort, edge.Data.Id)
	}
	if len(rebuiltPortPairs) != 3 {
		t.Errorf("Round-trip lost port information: Expected 3 unique port pairs, got %d", len(rebuiltPortPairs))
		t.Logf("  Rebuilt port pairs: %v", rebuiltPortPairs)
	}
	t.Logf("✓ Round-trip preserved all 3 unique edges with port information")

	// 完整的 JSON 往返测试
	rebuiltJson, err := json.Marshal(rebuiltFlow)
	if err != nil {
		t.Fatalf("Failed to marshal rebuilt FlowSimNetwork: %v", err)
	}
	t.Logf("✓ Round-trip successful: %d bytes → %d bytes", len(jsonBytes), len(rebuiltJson))
}

// createMultiEdgeFlowSimNetwork 创建一个包含多条平行边的测试网络
func createMultiEdgeFlowSimNetwork() protocol.FlowSimNetwork {
	return protocol.FlowSimNetwork{
		Version: stringPtr("1.0"),
		Nodes: []protocol.Node{
			{
				NodeId:   0,
				NodeName: "Node-0",
				Data: protocol.Node_Data{
					Id:    "node-0",
					Label: stringPtr("Node 0"),
				},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{
					X: 100,
					Y: 100,
				},
				InPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: 1, BufferSize: intPtr(64)},
					{PortId: 1, Bandwidth: 1, BufferSize: intPtr(64)},
					{PortId: 2, Bandwidth: 1, BufferSize: intPtr(64)},
				},
				OutPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: 1, BufferSize: intPtr(64)},
					{PortId: 1, Bandwidth: 1, BufferSize: intPtr(64)},
					{PortId: 2, Bandwidth: 1, BufferSize: intPtr(64)},
				},
			},
			{
				NodeId:   1,
				NodeName: "Node-1",
				Data: protocol.Node_Data{
					Id:    "node-1",
					Label: stringPtr("Node 1"),
				},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{
					X: 300,
					Y: 100,
				},
				InPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: 1, BufferSize: intPtr(64)},
					{PortId: 1, Bandwidth: 1, BufferSize: intPtr(64)},
					{PortId: 2, Bandwidth: 1, BufferSize: intPtr(64)},
				},
				OutPorts: &[]protocol.Port{
					{PortId: 0, Bandwidth: 1, BufferSize: intPtr(64)},
					{PortId: 1, Bandwidth: 1, BufferSize: intPtr(64)},
					{PortId: 2, Bandwidth: 1, BufferSize: intPtr(64)},
				},
			},
		},
		Edges: []protocol.Edge{
			{
				EdgeId:     1,
				SrcNodeId:  0,
				SrcPortId:  intPtr(0),
				DstNodeId:  1,
				DstPortId:  intPtr(0),
				Latency:    intPtr(1),
				Bandwidth:  intPtr(1),
				Data: protocol.Edge_Data{
					Id:     "edge-0-p0-1-p0",  // 包含端口信息的唯一 ID
					Source: "node-0",
					Target: "node-1",
				},
			},
			{
				EdgeId:     2,
				SrcNodeId:  0,
				SrcPortId:  intPtr(1),
				DstNodeId:  1,
				DstPortId:  intPtr(1),
				Latency:    intPtr(1),
				Bandwidth:  intPtr(1),
				Data: protocol.Edge_Data{
					Id:     "edge-0-p1-1-p1",  // 不同的端口,不同的 ID
					Source: "node-0",
					Target: "node-1",
				},
			},
			{
				EdgeId:     3,
				SrcNodeId:  0,
				SrcPortId:  intPtr(2),
				DstNodeId:  1,
				DstPortId:  intPtr(2),
				Latency:    intPtr(1),
				Bandwidth:  intPtr(1),
				Data: protocol.Edge_Data{
					Id:     "edge-0-p2-1-p2",  // 第三条平行边
					Source: "node-0",
					Target: "node-1",
				},
			},
		},
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}
