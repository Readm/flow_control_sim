# FlowSimNetwork 集成测试详细说明

## 概述

本文档详细解释每个集成测试的行为、检查目标和验证逻辑。这些测试确保 FlowSimNetwork 架构在前后端之间的数据流转是正确和完整的。

---

## 测试文件位置

- **测试文件**: `/internal/core/visualization/flowsim_integration_test.go`
- **运行命令**: `go test -timeout=3s -v ./internal/core/visualization`
- **CI 配置**: `.github/workflows/ci.yml` (已包含)

---

## Test 1: TestBenchmarkToFlowSimNetwork

### 测试目标
验证现有的 Benchmark 网络能够完整、正确地导出为 FlowSimNetwork 格式。

### 测试行为

```go
1. 使用 loadbench.BuildBidirectionalRing(8) 创建一个8节点双向环网络
   ↓
2. 调用 net.ExportState() 导出仿真状态
   ↓
3. 调用 visualization.StateToFlowSimNetwork() 转换为前端格式
   ↓
4. 验证结果
```

### 检查点详解

| 检查项 | 目的 | 验证方法 |
|--------|------|----------|
| **节点数量** | 确保所有节点都被导出 | `len(flowNet.Nodes) == 8` |
| **边数量** | 确保所有链路都被导出 | `len(flowNet.Edges) == 16` (双向环: 每个节点2条出边) |
| **端口完整性** | 确保节点的输入/输出端口都存在 | 每个节点检查 `InPorts != nil && len(*InPorts) > 0` |
| **JSON 序列化** | 确保可以序列化为 JSON | `json.Marshal(flowNet)` 无错误 |
| **JSON 反序列化** | 确保 JSON 可以解析回结构体 | `json.Unmarshal()` 后结构完整 |

### 为什么重要

这个测试验证了**数据导出的完整性**。如果 Benchmark 网络无法正确导出,说明:
- StateToFlowSimNetwork 转换逻辑有bug
- 某些字段没有被正确映射
- JSON schema 定义不完整

### 输出示例

```
✓ Benchmark network successfully exported to FlowSimNetwork format
  - Nodes: 8
  - Edges: 16
  - JSON size: 9005 bytes
```

### 数据流图

```
BuildBidirectionalRing(8)
         ↓
    [Network对象]
    - 8个WorkerNode
    - 16个Link (双向连接)
    - 每个节点2个输入队列、2个输出队列
         ↓
   ExportState()
         ↓
   [NetworkState]
   - CurrentCycle: 0
   - Nodes: []NodeState (8个)
   - Links: []LinkState (16个)
         ↓
StateToFlowSimNetwork()
         ↓
  [FlowSimNetwork]
  {
    "version": "1.0.0",
    "cycle": 0,
    "nodes": [
      {
        "node_id": 0,
        "node_name": "Node_0",
        "in_ports": [{...}],
        "out_ports": [{...}],
        "data": {"id": "node-0", ...},
        "position": {"x": 100, "y": 200}
      },
      ...
    ],
    "edges": [...]
  }
```

---

## Test 2: TestFlowSimNetworkBuildAndSimulate

### 测试目标
验证从 FlowSimNetwork JSON 可以构建出可执行的仿真网络,并且仿真能够正常推进。

### 测试行为

```go
1. 创建一个简单的 FlowSimNetwork (2节点, 1边)
   ↓
2. 调用 builder.BuildFromFlowSimNetwork(flowNet) 构建网络
   ↓
3. 验证构建的网络结构
   ↓
4. 调用 net.AdvanceTo(10) 执行仿真
   ↓
5. 验证仿真成功推进
```

### 检查点详解

| 检查项 | 目的 | 验证方法 |
|--------|------|----------|
| **网络构建成功** | 确保 BuildFromFlowSimNetwork 无错误 | `err == nil` |
| **节点数量匹配** | 确保所有节点都被创建 | `len(state.Nodes) == len(flowNet.Nodes)` |
| **链路数量匹配** | 确保所有链路都被创建 | `len(state.Links) == len(flowNet.Edges)` |
| **仿真可执行** | 确保网络可以运行 | `net.AdvanceTo(10)` 无错误 |
| **周期推进** | 确保仿真真的在执行 | `finalState.CurrentCycle >= targetCycle` |

### 为什么重要

这个测试验证了**网络构建的正确性**。如果构建失败,说明:
- BuildFromFlowSimNetwork 有bug
- FlowSimNetwork 缺少必要字段
- 节点/链路创建逻辑有问题

### 输出示例

```
✓ FlowSimNetwork successfully built and simulated
  - Initial cycle: 0
  - Final cycle: 11
```

### 数据流图

```
FlowSimNetwork (JSON)
{
  "nodes": [
    {"node_id": 0, "in_ports": [...], ...},
    {"node_id": 1, "in_ports": [...], ...}
  ],
  "edges": [
    {"src_node_id": 0, "dst_node_id": 1, ...}
  ]
}
         ↓
BuildFromFlowSimNetwork()
         ↓
内部处理:
1. 缓存 display 信息
   visualization.CacheNodeDisplay(nodeId, data, position, style)

2. 创建节点
   for each node in flowNet.Nodes:
     - newNode = node.NewWorkerNode(nodeId)
     - 添加 cache (如果有)
     - 添加 directory (如果有)
     - 创建 InputQueue 从 in_ports
     - 创建 OutputQueue 从 out_ports
     - net.AddNode(handle)

3. 创建链路
   for each edge in flowNet.Edges:
     - net.Connect(src, srcPort, dst, dstPort, latency, bandwidth)
         ↓
    [Network对象]
    可以执行仿真
         ↓
   AdvanceTo(10)
         ↓
    [运行仿真]
    周期推进到 10+
```

---

## Test 3: TestFlowSimNetworkCyEditorCompatibility

### 测试目标
验证导出的 FlowSimNetwork 包含 CyEditor 可视化所需的所有必要字段。

### 测试行为

```go
1. 构建 Benchmark 网络 (4节点环)
   ↓
2. 导出为 FlowSimNetwork
   ↓
3. 逐个检查每个节点和边的 CyEditor 必需字段
```

### 检查点详解

#### 节点字段检查

| 字段 | 必要性 | 用途 | 验证方法 |
|------|--------|------|----------|
| **data.id** | ✅ 必需 | CyEditor 唯一标识符 | `node.Data.Id != ""` |
| **position.x/y** | ✅ 必需 | 节点在画布上的位置 | `position.X != 0 || position.Y != 0` (非全零) |
| **node_name** | ✅ 必需 | 节点显示名称 | `node.NodeName != ""` |
| **node_id** | ✅ 必需 | 仿真系统中的节点ID | `node.NodeId >= 0` |

#### 边字段检查

| 字段 | 必要性 | 用途 | 验证方法 |
|------|--------|------|----------|
| **data.id** | ✅ 必需 | CyEditor 边标识符 | `edge.Data.Id != ""` |
| **data.source** | ✅ 必需 | 源节点的 data.id | `edge.Data.Source != ""` |
| **data.target** | ✅ 必需 | 目标节点的 data.id | `edge.Data.Target != ""` |
| **src_node_id** | ✅ 必需 | 源节点的仿真ID | `edge.SrcNodeId >= 0` |
| **dst_node_id** | ✅ 必需 | 目标节点的仿真ID | `edge.DstNodeId >= 0` |

### 为什么重要

这个测试验证了**前端兼容性**。CyEditor 需要特定的字段才能渲染:
- 没有 `data.id` → CyEditor 无法识别元素
- 没有 `position` → 节点会堆叠在原点
- 没有 `source/target` → 边无法绘制

### CyEditor 数据格式要求

CyEditor (基于 Cytoscape.js) 期望的格式:

```javascript
{
  nodes: [
    {
      data: {
        id: "node-0",      // 必需: 唯一ID
        label: "Node 0"    // 可选: 显示标签
      },
      position: {
        x: 100,            // 必需: X坐标
        y: 200             // 必需: Y坐标
      }
    }
  ],
  edges: [
    {
      data: {
        id: "edge-1",      // 必需: 唯一ID
        source: "node-0",  // 必需: 源节点ID
        target: "node-1"   // 必需: 目标节点ID
      }
    }
  ]
}
```

### 输出示例

```
✓ FlowSimNetwork contains all required CyEditor fields
  - All nodes have data.id and position
  - All edges have data.id, source, and target
```

---

## Test 4: TestCyEditorEditedFlowSimNetwork

### 测试目标
验证用户在 CyEditor 中编辑后生成的 FlowSimNetwork 是有效的,可以被后端接受并执行。

### 测试行为

```go
1. 模拟用户在 CyEditor 中创建的网络数据
   - 手动构造 FlowSimNetwork 对象
   - 包含所有 CyEditor 会生成的字段
   ↓
2. JSON 序列化 (模拟前端发送)
   ↓
3. JSON 反序列化 (模拟后端接收)
   ↓
4. 调用 BuildFromFlowSimNetwork 构建网络
   ↓
5. 验证构建成功并可以仿真
```

### 模拟的用户操作

测试模拟了以下用户操作序列:

```
用户在 CyEditor 中:
1. 拖拽添加 Node 0 到位置 (100, 100)
2. 拖拽添加 Node 1 到位置 (300, 100)
3. 连接 Node 0 → Node 1
4. 配置端口: 输入/输出各1个,buffer_size=64
5. 点击 "Build & Deploy" 按钮
```

生成的数据结构:

```go
FlowSimNetwork{
  Nodes: []Node{
    {
      NodeId: 0,
      NodeName: "Node_0",
      Data: {Id: "node-0", Label: "N0", Type: "WorkerNode"},
      Position: {X: 100, Y: 100},  // 用户拖拽的位置
      InPorts: [{PortId: 0, Bandwidth: 1, BufferSize: 64}],
      OutPorts: [{PortId: 0, Bandwidth: 1, BufferSize: 64}]
    },
    {
      NodeId: 1,
      // ...类似结构
    }
  },
  Edges: []Edge{
    {
      EdgeId: 1,
      SrcNodeId: 0, SrcPortId: 0,
      DstNodeId: 1, DstPortId: 0,
      Latency: 5, Bandwidth: 1,
      Data: {Id: "edge-1", Source: "node-0", Target: "node-1"}
    }
  }
}
```

### 检查点详解

| 检查项 | 目的 | 验证方法 |
|--------|------|----------|
| **JSON 序列化** | 确保数据可以发送到后端 | `json.Marshal()` 无错误 |
| **JSON 反序列化** | 确保后端可以解析 | `json.Unmarshal()` 无错误 |
| **结构验证** | 确保解析后数据完整 | 节点/边数量匹配 |
| **网络构建** | 确保可以构建仿真网络 | `BuildFromFlowSimNetwork()` 无错误 |
| **仿真执行** | 确保网络可以运行 | `AdvanceTo(5)` 无错误 |

### 为什么重要

这个测试验证了**用户工作流的有效性**。它模拟了完整的用户操作流程:

```
用户编辑 → 发送JSON → 后端接收 → 构建网络 → 执行仿真
```

如果这个测试失败,说明:
- CyEditor 生成的数据格式不被后端接受
- 必需字段缺失
- 数据验证逻辑有问题

### 输出示例

```
✓ CyEditor-edited FlowSimNetwork is valid and executable
  - JSON roundtrip: successful
  - Network build: successful
  - Simulation: successful
```

---

## Test 5: TestFlowSimNetworkRoundTrip

### 测试目标
验证数据在前后端之间往返转换时保持结构一致性,不会丢失信息。

### 测试行为

```go
1. 创建原始 FlowSimNetwork (A)
   ↓
2. 构建网络: BuildFromFlowSimNetwork(A) → Network
   ↓
3. 导出状态: Network.ExportState() → NetworkState
   ↓
4. 转换回前端: StateToFlowSimNetwork(NetworkState) → FlowSimNetwork (B)
   ↓
5. 验证 A 和 B 的结构一致性
```

### 检查点详解

| 检查项 | 目的 | 验证方法 |
|--------|------|----------|
| **节点数量保持** | 确保没有节点丢失或增加 | `len(A.Nodes) == len(B.Nodes)` |
| **边数量保持** | 确保没有边丢失或增加 | `len(A.Edges) == len(B.Edges)` |
| **节点ID保持** | 确保节点标识符不变 | `A.Nodes[i].NodeId == B.Nodes[i].NodeId` |
| **结构完整性** | 确保核心字段都存在 | 节点有端口、边有连接信息 |

### 数据流图

```
FlowSimNetwork (A)          往返转换          FlowSimNetwork (B)
{                                             {
  nodes: [                                      nodes: [
    {id:0, name:"N0"}        ←→                   {id:0, name:"N0"}
  ],                                            ],
  edges: [                                      edges: [
    {src:0, dst:1}           ←→                   {src:0, dst:1}
  ]                                             ]
}                                             }

         ↓                                             ↑
BuildFromFlowSimNetwork()                 StateToFlowSimNetwork()
         ↓                                             ↑
    [Network]              ExportState()          [NetworkState]
    可执行仿真    ←―――――――――――――――――――――――→    包含运行时状态
```

### JSON 大小差异说明

测试输出显示:
```
Original JSON size: 716 bytes
Rebuilt JSON size: 1076 bytes
```

**为什么重建后变大了?**

原始 FlowSimNetwork 只包含配置信息:
```json
{
  "nodes": [{
    "node_id": 0,
    "in_ports": [{"buffer_size": 64}]  // 只有配置
  }]
}
```

重建后包含运行时状态:
```json
{
  "nodes": [{
    "node_id": 0,
    "in_ports": [{
      "buffer_size": 64,      // 配置
      "buffer_length": 0,     // 运行时状态
      "bitmap": "00000..."    // 运行时状态
    }]
  }]
}
```

这是**预期行为**,因为网络运行后会有运行时状态。

### 为什么重要

这个测试验证了**数据一致性**。确保:
- 用户编辑的网络 → 仿真执行 → 返回给用户,结构不变
- Display 信息(位置、样式)在往返中被保留
- 不会出现数据丢失或损坏

### 输出示例

```
✓ Round-trip test successful
  - Original nodes: 2, Rebuilt nodes: 2
  - Original edges: 1, Rebuilt edges: 1
  - Original JSON size: 716 bytes, Rebuilt JSON size: 1076 bytes
```

---

## Test 6: TestStateToFlowSimNetwork

### 测试目标
验证基础的状态转换功能,确保 NetworkState 可以正确转换为 FlowSimNetwork。

### 测试行为

```go
1. 手动构造一个 NetworkState
   - CurrentCycle: 100
   - 2个节点 (WorkerNode, HubNode)
   - 1条链路,带流量占用信息
   ↓
2. 调用 StateToFlowSimNetwork(state)
   ↓
3. 验证所有字段正确转换
```

### 检查点详解

| 检查项 | 目的 | 验证内容 |
|--------|------|----------|
| **节点转换** | 基础结构 | 节点数量、ID、类型 |
| **边转换** | 连接关系 | 边的源、目标节点 |
| **周期信息** | 时间状态 | `flowNet.Cycle == 100` |
| **流量状态** | 运行时数据 | `LinkStatus` 包含占用信息 `[0,1,0,5]` |
| **显示信息** | UI字段 | `data.id`, `position` 等字段存在 |
| **JSON序列化** | 完整性 | 可以序列化为合法JSON |

### 详细验证逻辑

#### 1. 节点转换验证

```go
// 输入 NodeState
NodeState{
  ID: 1,
  Type: "WorkerNode",
  Inputs: []QueueState{...},
  Outputs: []QueueState{...}
}

// 期望输出
Node{
  NodeId: 1,                           // ✓ ID匹配
  NodeName: "Node_1",                  // ✓ 自动生成名称
  NodeFeatures: ["WorkerNode"],        // ✓ 类型转换
  Data: {
    Id: "node-1",                      // ✓ CyEditor ID
    Type: "WorkerNode",                // ✓ 类型
    Label: "N1"                        // ✓ 标签
  },
  Position: {X: 600, Y: 300}          // ✓ 自动布局
}
```

#### 2. 链路流量状态验证

```go
// 输入 LinkState
LinkState{
  SourceID: 1,
  TargetID: 2,
  Occupancy: [0, 1, 0, 5]  // 每个时间槽的占用情况
}

// 期望输出
Edge{
  SrcNodeId: 1,
  DstNodeId: 2,
  LinkStatus: [{
    Name: "occupancy",
    Values: [0, 1, 0, 5]    // ✓ 流量数据保留
  }]
}
```

这个流量数据用于前端可视化:
- CyEditor 可以显示链路的实时占用情况
- 可以绘制流量热力图
- 用户可以看到哪些链路拥塞

### JSON 输出示例

测试会打印完整的 JSON:

```json
{
  "cycle": 100,
  "nodes": [
    {
      "node_id": 1,
      "node_name": "Node_1",
      "node_features": ["WorkerNode"],
      "data": {
        "id": "node-1",
        "label": "N1",
        "type": "WorkerNode"
      },
      "position": {
        "x": 600,
        "y": 300
      }
    },
    {
      "node_id": 2,
      "node_name": "Node_2",
      "node_features": ["HubNode"],
      "data": {
        "id": "node-2",
        "label": "N2",
        "type": "HubNode"
      },
      "position": {
        "x": 200,
        "y": 300
      }
    }
  ],
  "edges": [
    {
      "edge_id": 1,
      "src_node_id": 1,
      "dst_node_id": 2,
      "latency": 10,
      "bandwidth": 1,
      "link_status": [
        {
          "name": "occupancy",
          "values": [0, 1, 0, 5]
        }
      ],
      "data": {
        "id": "edge-1",
        "source": "node-1",
        "target": "node-2",
        "lineType": "solid"
      }
    }
  ],
  "version": "1.0.0"
}
```

### 为什么重要

这是最基础的测试,验证了**核心转换逻辑**。如果这个测试失败,所有其他测试也会失败。

---

## 测试覆盖矩阵

| 测试 | Benchmark导出 | JSON构建 | CyEditor兼容 | 用户编辑 | 往返一致 | 状态转换 |
|------|--------------|----------|-------------|---------|---------|---------|
| Test 1 | ✅ | - | ✅ | - | - | - |
| Test 2 | - | ✅ | - | - | - | - |
| Test 3 | - | - | ✅ | - | - | - |
| Test 4 | - | ✅ | ✅ | ✅ | - | - |
| Test 5 | - | ✅ | - | - | ✅ | ✅ |
| Test 6 | - | - | - | - | - | ✅ |

---

## CI/CD 集成

### CI 配置位置
`.github/workflows/ci.yml`

### 测试步骤

```yaml
- name: Run Unit Tests
  run: |
    go test -timeout=3s -v ./...

- name: Run FlowSimNetwork Integration Tests
  run: |
    go test -timeout=3s -v ./internal/core/visualization -run "TestFlowSim.*|TestStateToFlowSimNetwork"
```

### CI 执行流程

```
GitHub Push/PR
      ↓
检出代码 + 初始化子模块
      ↓
设置 Go 1.21 环境
      ↓
运行所有单元测试 (go test ./...)
      ↓
运行 FlowSimNetwork 集成测试
      ↓
运行性能基准测试
      ↓
生成测试报告
```

### 失败处理

如果任何测试失败,CI 会:
1. 标记构建为失败 ❌
2. 在 GitHub Actions 页面显示详细错误
3. 阻止代码合并到 main 分支

---

## 本地开发测试命令

### 运行所有集成测试
```bash
go test -timeout=3s -v ./internal/core/visualization
```

### 运行单个测试
```bash
go test -timeout=3s -v ./internal/core/visualization -run TestBenchmarkToFlowSimNetwork
```

### 运行并查看详细输出
```bash
go test -timeout=3s -v ./internal/core/visualization | tee test_output.txt
```

### 检查测试覆盖率
```bash
go test -timeout=3s -cover ./internal/core/visualization
```

### 生成覆盖率报告
```bash
go test -timeout=3s -coverprofile=coverage.out ./internal/core/visualization
go tool cover -html=coverage.out -o coverage.html
```

---

## 测试维护指南

### 何时更新测试

1. **添加新字段到 FlowSimNetwork**
   - 更新 Test 3 验证新字段
   - 更新 Test 6 检查字段转换

2. **修改 BuildFromFlowSimNetwork 逻辑**
   - 确保 Test 2, 4, 5 仍然通过
   - 添加针对新逻辑的测试用例

3. **修改 StateToFlowSimNetwork 逻辑**
   - 确保 Test 1, 5, 6 仍然通过
   - 验证新的转换逻辑

4. **更改 CyEditor 要求**
   - 更新 Test 3 的字段检查
   - 更新 Test 4 的模拟数据

### 测试数据维护

测试使用的辅助函数:
```go
// 创建简单测试网络
func createSimpleFlowSimNetwork() protocol.FlowSimNetwork

// 字符串指针辅助
func stringPtr(s string) *string

// 整数指针辅助
func intPtr(i int) *int
```

在添加新测试时可以复用这些函数。

---

## 总结

这6个测试共同验证了 FlowSimNetwork 架构的:

1. ✅ **数据完整性** - 所有字段都被正确导出和解析
2. ✅ **功能正确性** - 网络可以构建和执行
3. ✅ **前端兼容性** - CyEditor 可以正确显示
4. ✅ **用户体验** - 编辑流程端到端可用
5. ✅ **往返一致性** - 数据不会在转换中损坏
6. ✅ **基础功能** - 核心转换逻辑正确

所有测试都已集成到 CI 中,确保每次代码提交都经过验证。
