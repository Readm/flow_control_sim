package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
	"github.com/Readm/flow_sim/internal/testing/mocks"
	"github.com/Readm/flow_sim/internal/visualization/mockserver"
)

// TestHTTPWorkflow 测试完整的HTTP工作流：
// 1. 启动MockServer
// 2. 通过HTTP POST /build_network 提交网络（带修改的坐标）
// 3. 通过HTTP POST /advance_to 推进仿真到100周期
// 4. 通过HTTP GET /load_networks 获取当前状态
// 5. 验证：cycle已推进，节点坐标保持不变
func TestHTTPWorkflow(t *testing.T) {
	// ===== Step 1: 启动MockServer =====
	t.Log("Step 1: 启动MockServer")

	ctrl := mocks.NewController()
	srv, err := mockserver.New(mockserver.Options{
		Controller:         ctrl,
		StaticDir:          "../../web/static",
		DefaultTotalCycles: 1000,
	})
	if err != nil {
		t.Fatalf("创建MockServer失败: %v", err)
	}
	defer srv.Close()

	baseURL := srv.BaseURL()
	t.Logf("  MockServer启动成功: %s", baseURL)

	// 给服务器一点时间启动
	time.Sleep(100 * time.Millisecond)

	// ===== Step 2: 准备网络定义（带修改的坐标） =====
	t.Log("Step 2: 准备FlowSimNetwork")

	version := "1.0.0"
	cycle := 0
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
					X: 150.0, // 修改后的坐标
					Y: 250.0,
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
					X: 350.0, // 修改后的坐标
					Y: 450.0,
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

	// 记录期望的坐标
	expectedNode0X := flowNet.Nodes[0].Position.X
	expectedNode0Y := flowNet.Nodes[0].Position.Y
	expectedNode1X := flowNet.Nodes[1].Position.X
	expectedNode1Y := flowNet.Nodes[1].Position.Y

	t.Logf("  Node 0 坐标: (%.1f, %.1f)", expectedNode0X, expectedNode0Y)
	t.Logf("  Node 1 坐标: (%.1f, %.1f)", expectedNode1X, expectedNode1Y)

	// ===== Step 3: POST /build_network =====
	t.Log("Step 3: POST /build_network")

	buildPayload, err := json.Marshal(flowNet)
	if err != nil {
		t.Fatalf("序列化FlowSimNetwork失败: %v", err)
	}

	resp, err := http.Post(baseURL+"/build_network", "application/json", bytes.NewReader(buildPayload))
	if err != nil {
		t.Fatalf("POST /build_network 失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Build返回错误状态 %d: %s", resp.StatusCode, string(body))
	}

	var buildResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&buildResp); err != nil {
		t.Fatalf("解析Build响应失败: %v", err)
	}
	t.Logf("  Build响应: %v", buildResp)

	// 等待网络构建完成
	time.Sleep(200 * time.Millisecond)

	// ===== Step 4: POST /advance_to =====
	targetCycle := 100
	t.Logf("Step 4: POST /advance_to (cycle=%d)", targetCycle)

	advancePayload := map[string]int{"cycle": targetCycle}
	advanceData, err := json.Marshal(advancePayload)
	if err != nil {
		t.Fatalf("序列化advance请求失败: %v", err)
	}

	resp, err = http.Post(baseURL+"/advance_to", "application/json", bytes.NewReader(advanceData))
	if err != nil {
		t.Fatalf("POST /advance_to 失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("AdvanceTo返回错误状态 %d: %s", resp.StatusCode, string(body))
	}

	var advanceResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&advanceResp); err != nil {
		t.Fatalf("解析AdvanceTo响应失败: %v", err)
	}
	t.Logf("  AdvanceTo响应: %v", advanceResp)

	// 等待仿真完成
	time.Sleep(500 * time.Millisecond)

	// ===== Step 5: GET /load_networks =====
	t.Log("Step 5: GET /load_networks")

	resp, err = http.Get(baseURL + "/load_networks")
	if err != nil {
		t.Fatalf("GET /load_networks 失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("LoadNetworks返回错误状态 %d: %s", resp.StatusCode, string(body))
	}

	var networks []protocol.FlowSimNetwork
	if err := json.NewDecoder(resp.Body).Decode(&networks); err != nil {
		t.Fatalf("解析LoadNetworks响应失败: %v", err)
	}

	if len(networks) == 0 {
		t.Fatal("LoadNetworks返回空数组")
	}
	resultFlowNet := networks[0]

	// ===== Step 6: 验证结果 =====
	t.Log("Step 6: 验证结果")

	// 6.1 验证cycle已推进
	if resultFlowNet.Cycle == nil {
		t.Fatal("结果中cycle为nil")
	}
	actualCycle := *resultFlowNet.Cycle
	// MockController的RunCycles会推进到targetCycle，当前cycle就是targetCycle
	expectedCycle := targetCycle
	if actualCycle != expectedCycle {
		t.Fatalf("Cycle未正确推进: 期望=%d, 实际=%d", expectedCycle, actualCycle)
	}
	t.Logf("  ✓ Cycle已正确推进到 %d", actualCycle)

	// 6.2 验证节点数量
	if len(resultFlowNet.Nodes) != 2 {
		t.Fatalf("节点数量错误: 期望=2, 实际=%d", len(resultFlowNet.Nodes))
	}

	// 6.3 验证节点坐标保持不变
	coordsMatch := true
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
			coordsMatch = false
			t.Errorf("节点 %d 坐标改变了:\n  期望=(%.1f, %.1f)\n  实际=(%.1f, %.1f)",
				node.NodeId, expectedX, expectedY, actualX, actualY)
		} else {
			t.Logf("  ✓ Node %d 坐标保持不变: (%.1f, %.1f)", node.NodeId, actualX, actualY)
		}
	}

	if !coordsMatch {
		t.Error("坐标验证失败")
	}

	// 6.4 验证边信息
	if len(resultFlowNet.Edges) != 1 {
		t.Fatalf("边数量错误: 期望=1, 实际=%d", len(resultFlowNet.Edges))
	}
	edge := resultFlowNet.Edges[0]
	if edge.SrcNodeId != 0 || edge.DstNodeId != 1 {
		t.Errorf("边连接错误: src=%d, dst=%d", edge.SrcNodeId, edge.DstNodeId)
	}
	t.Log("  ✓ 边信息完整且正确")

	t.Log("\n========== HTTP工作流测试通过 ==========")
	t.Log("完整流程验证成功:")
	t.Log("  1. ✓ 启动MockServer")
	t.Log("  2. ✓ POST /build_network（带修改坐标）")
	t.Log("  3. ✓ POST /advance_to (100 cycles)")
	t.Log("  4. ✓ GET /load_networks")
	t.Logf("  5. ✓ 验证：Cycle=%d，坐标保持不变", actualCycle)
}

// TestHTTPWorkflowWithReset 测试带重置的工作流
func TestHTTPWorkflowWithReset(t *testing.T) {
	t.Log("========== 测试带重置的HTTP工作流 ==========")

	// 启动服务器
	ctrl := mocks.NewController()
	srv, err := mockserver.New(mockserver.Options{
		Controller:         ctrl,
		StaticDir:          "../../web/static",
		DefaultTotalCycles: 1000,
	})
	if err != nil {
		t.Fatalf("创建MockServer失败: %v", err)
	}
	defer srv.Close()

	baseURL := srv.BaseURL()
	time.Sleep(100 * time.Millisecond)

	// 构建初始网络
	initialNet := createSimpleNetwork(100.0, 200.0, 300.0, 400.0)
	buildNetwork(t, baseURL, initialNet)
	time.Sleep(200 * time.Millisecond)

	// Advance到50周期
	advanceTo(t, baseURL, 50)
	time.Sleep(300 * time.Millisecond)

	// 验证状态
	state1 := loadNetwork(t, baseURL)
	if state1.Cycle == nil || *state1.Cycle != 50 {
		t.Fatalf("第一次推进后cycle错误: 期望=50, 实际=%v", state1.Cycle)
	}
	t.Logf("  ✓ 第一次推进到 cycle %d", *state1.Cycle)

	// Reset网络
	t.Log("Step: POST /reset_network")
	resp, err := http.Post(baseURL+"/reset_network", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /reset_network 失败: %v", err)
	}
	resp.Body.Close()
	time.Sleep(200 * time.Millisecond)

	// 重新构建网络（修改坐标）
	modifiedNet := createSimpleNetwork(150.0, 250.0, 350.0, 450.0)
	buildNetwork(t, baseURL, modifiedNet)
	time.Sleep(200 * time.Millisecond)

	// Advance到100周期
	advanceTo(t, baseURL, 100)
	time.Sleep(300 * time.Millisecond)

	// 验证状态
	state2 := loadNetwork(t, baseURL)
	if state2.Cycle == nil || *state2.Cycle != 100 {
		t.Fatalf("重置后推进cycle错误: 期望=100, 实际=%v", state2.Cycle)
	}
	t.Logf("  ✓ 重置后推进到 cycle %d", *state2.Cycle)

	// 验证坐标是修改后的值
	if len(state2.Nodes) != 2 {
		t.Fatalf("节点数量错误: %d", len(state2.Nodes))
	}

	for _, node := range state2.Nodes {
		if node.NodeId == 0 {
			if node.Position.X != 150.0 || node.Position.Y != 250.0 {
				t.Errorf("Node 0 坐标错误: (%.1f, %.1f)", node.Position.X, node.Position.Y)
			} else {
				t.Logf("  ✓ Node 0 坐标正确: (%.1f, %.1f)", node.Position.X, node.Position.Y)
			}
		} else if node.NodeId == 1 {
			if node.Position.X != 350.0 || node.Position.Y != 450.0 {
				t.Errorf("Node 1 坐标错误: (%.1f, %.1f)", node.Position.X, node.Position.Y)
			} else {
				t.Logf("  ✓ Node 1 坐标正确: (%.1f, %.1f)", node.Position.X, node.Position.Y)
			}
		}
	}

	t.Log("\n========== 带重置的HTTP工作流测试通过 ==========")
}

// 辅助函数：创建简单网络
func createSimpleNetwork(node0X, node0Y, node1X, node1Y float32) protocol.FlowSimNetwork {
	version := "1.0.0"
	cycle := 0
	bufferSize := 64
	bandwidth := 1
	latency := 10

	return protocol.FlowSimNetwork{
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
				Data: protocol.Node_Data{Id: "node-0"},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: node0X, Y: node0Y},
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
				Data: protocol.Node_Data{Id: "node-1"},
				Position: struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: node1X, Y: node1Y},
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
}

// 辅助函数：构建网络
func buildNetwork(t *testing.T, baseURL string, flowNet protocol.FlowSimNetwork) {
	payload, _ := json.Marshal(flowNet)
	resp, err := http.Post(baseURL+"/build_network", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /build_network 失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Build失败 %d: %s", resp.StatusCode, string(body))
	}
}

// 辅助函数：推进仿真
func advanceTo(t *testing.T, baseURL string, cycle int) {
	payload, _ := json.Marshal(map[string]int{"cycle": cycle})
	resp, err := http.Post(baseURL+"/advance_to", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /advance_to 失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("AdvanceTo失败 %d: %s", resp.StatusCode, string(body))
	}
}

// 辅助函数：加载网络状态
func loadNetwork(t *testing.T, baseURL string) protocol.FlowSimNetwork {
	resp, err := http.Get(baseURL + "/load_networks")
	if err != nil {
		t.Fatalf("GET /load_networks 失败: %v", err)
	}
	defer resp.Body.Close()

	var networks []protocol.FlowSimNetwork
	if err := json.NewDecoder(resp.Body).Decode(&networks); err != nil {
		t.Fatalf("解析LoadNetworks响应失败: %v", err)
	}
	if len(networks) == 0 {
		t.Fatal("LoadNetworks返回空数组")
	}
	return networks[0]
}
