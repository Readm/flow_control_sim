# Flow Simulation 数据结构架构文档

## 目录
- [概述](#概述)
- [结构体映射表](#结构体映射表)
- [数据流转过程](#数据流转过程)
- [Adapter 的作用](#adapter-的作用)
- [多版本结构体说明](#多版本结构体说明)
- [设计原则](#设计原则)

---

## 概述

Flow Simulation 项目采用 **Schema-First** 设计，使用 OpenAPI 3.0 定义统一的数据交换格式。系统中存在多层数据结构，用于不同目的：

1. **OpenAPI Schema** (`web/openapi.yaml`): 定义 HTTP API 的接口契约
2. **Protocol 结构体** (自动生成): 由 OpenAPI 生成的 Go 类型，用于 JSON 序列化
3. **State 结构体** (DTO): 内部状态传输对象，用于导出仿真状态
4. **Core 结构体** (实体): 仿真核心实体（Node, Link），包含运行时逻辑

---

## 结构体映射表

### 网络层级

| OpenAPI Schema | Protocol 结构体 | State 结构体 | Core 结构体 | 说明 |
|---------------|----------------|-------------|------------|------|
| `FlowSimNetwork` | `protocol.FlowSimNetwork` | `state.NetworkState` | `network.Network` | 网络拓扑和状态 |
| `Node` | `protocol.Node` | `state.NodeState` | `node.BaseNode` | 节点 |
| `Edge` | `protocol.Edge` | `state.LinkState` | `link.Link` | 链路/边 |
| `Port` | `protocol.Port` | `state.QueueState` | `queue.InputQueue` / `queue.OutputQueue` | 端口/队列 |
| `CacheConfig` | `protocol.CacheConfig` | `state.CacheState` | `cache.Cache` (接口) | 缓存 |
| `DirectoryConfig` | `protocol.DirectoryConfig` | `state.DirectoryState` | `directory.Directory` (接口) | 目录 |

### Node 字段映射详解

#### OpenAPI `Node` 字段

```yaml
Node:
  properties:
    # 业务逻辑
    node_id: integer
    node_name: string
    node_features: array<string>

    # 端口
    in_ports: array<Port>
    out_ports: array<Port>

    # 组件配置
    cache: CacheConfig
    directory: DirectoryConfig
    coherence_domain_id: integer

    # CyEditor 可视化
    data:              # { id, type, label, ... }
      id: string
      type: string
      label: string
    position:          # { x, y }
      x: number
      y: number
    style: object      # Cytoscape 样式
```

#### Protocol `protocol.Node` 结构体

**文件**: `internal/core/visualization/protocol/types.gen.go` (自动生成)

```go
type Node struct {
    // 业务字段
    NodeId            int             `json:"node_id"`
    NodeName          string          `json:"node_name"`
    NodeFeatures      *[]string       `json:"node_features,omitempty"`

    // 端口
    InPorts           *[]Port         `json:"in_ports,omitempty"`
    OutPorts          *[]Port         `json:"out_ports,omitempty"`

    // 组件配置
    Cache             *CacheConfig    `json:"cache,omitempty"`
    Directory         *DirectoryConfig `json:"directory,omitempty"`
    CoherenceDomainId *int            `json:"coherence_domain_id,omitempty"`

    // 可视化
    Data              Node_Data       `json:"data"`
    Position          struct {
        X float32 `json:"x"`
        Y float32 `json:"y"`
    } `json:"position"`
    Style             *map[string]interface{} `json:"style,omitempty"`
}
```

#### State `state.NodeState` 结构体

**文件**: `internal/core/state/state.go`

```go
type NodeState struct {
    // 核心标识
    ID           int
    Type         string        // 节点类型，如 "WorkerNode"
    CurrentCycle int

    // 端口状态
    Inputs       []QueueState  // 输入队列状态
    Outputs      []QueueState  // 输出队列状态

    // 统计数据（运行时）
    Stats map[string]interface{}
    // Stats["cache"] = []CacheState
    // Stats["directory"] = []DirectoryState

    // 配置信息（静态）
    Features          map[string]map[string]interface{}
    // Features["cache"] = { capacity, num_sets, replacement_policy, states }
    // Features["directory"] = { capacity, num_sets, ... }
    CoherenceDomainID *int

    // 可视化信息
    DisplayData map[string]interface{}
    // DisplayData["position"] = struct{ X, Y float32 }
    // DisplayData["data"] = protocol.Node_Data
    // DisplayData["style"] = map[string]interface{}

    // 已废弃字段（兼容性）
    Caches      []CacheState           // 废弃：使用 Stats["cache"]
    Directories []DirectoryState       // 废弃：使用 Stats["directory"]
    CustomData  map[string]interface{} // 废弃：使用 Features/DisplayData
}
```

#### Core `node.BaseNode` 结构体

**文件**: `internal/core/node/node.go`

```go
type BaseNode struct {
    // 核心标识
    id           int
    name         string
    currentCycle uint64

    // 队列（实际对象）
    inputs  []InputQueue
    outputs []OutputQueue

    // 组件（实际对象）
    caches      []cache.Cache
    directories []directory.Directory

    // 业务逻辑
    handler NodeHandler  // 节点处理器
    monitor *monitor.NodeMonitor

    // 配置和可视化（新增字段）
    features          map[string]map[string]interface{}  // 配置
    coherenceDomainID *int                               // 一致性域
    displayData       map[string]interface{}             // 可视化

    // 自定义数据
    data   map[string]interface{}
    dataMu sync.RWMutex
}
```

**关键区别**:
- `BaseNode` 存储**实际对象**（queues, caches），而不是状态快照
- `BaseNode` 包含**运行时逻辑**（handler, monitor）
- 新增的 `features`, `coherenceDomainID`, `displayData` 字段用于保存配置和可视化信息，使其能够完整往返

---

### Link/Edge 字段映射详解

#### OpenAPI `Edge` 字段

```yaml
Edge:
  properties:
    # 业务逻辑
    edge_id: integer
    src_node_id: integer
    src_port_id: integer
    dst_node_id: integer
    dst_port_id: integer

    # 链路配置
    latency: integer
    bandwidth: integer
    packet_types: array<integer>

    # 可视化
    data:
      id: string
      source: string
      target: string
      lineType: string (enum)

    # 运行时状态
    link_status: array<{ name, values }>
```

#### Protocol `protocol.Edge` 结构体

**文件**: `internal/core/visualization/protocol/types.gen.go`

```go
type Edge struct {
    EdgeId      int                     `json:"edge_id"`
    SrcNodeId   int                     `json:"src_node_id"`
    SrcPortId   *int                    `json:"src_port_id,omitempty"`
    DstNodeId   int                     `json:"dst_node_id"`
    DstPortId   *int                    `json:"dst_port_id,omitempty"`
    Latency     *int                    `json:"latency,omitempty"`
    Bandwidth   *int                    `json:"bandwidth,omitempty"`
    PacketTypes *[]int                  `json:"packet_types,omitempty"`
    Data        Edge_Data               `json:"data"`
    LinkStatus  *[]struct{...}          `json:"link_status,omitempty"`
}
```

#### State `state.LinkState` 结构体

**文件**: `internal/core/state/state.go`

```go
type LinkState struct {
    // 核心标识
    SourceID     int
    SourcePortID int
    TargetID     int
    TargetPortID int
    CurrentCycle int

    // 链路参数
    Latency   int
    Bandwidth int

    // 运行时状态
    Occupancy   []int    // 每个时间槽的占用情况
    PacketTypes []string // 支持的包类型

    // 业务和可视化
    EdgeID      int                       // 业务ID
    DisplayData map[string]interface{}     // 可视化信息
    // DisplayData["data"] = protocol.Edge_Data
}
```

#### Core `link.Link` 结构体

**文件**: `internal/core/link/link.go`

```go
type Link struct {
    // 核心标识
    sourceID     int
    sourcePortID int
    targetID     int
    targetPortID int
    currentCycle int

    // 链路参数
    latency   int
    bandwidth int

    // 端口连接（实际对象）
    fromUpstream ahead_port.OutPort
    toDownstream ahead_port.InPort
    upstreamPort   *ahead_port.Port
    downstreamPort *ahead_port.Port

    // 链路类型处理器
    linkType LinkType  // BufferedLinkType 或 BufferlessLinkType

    // 业务和可视化（新增字段）
    edgeID      int                       // 业务ID
    packetTypes []string                  // 包类型
    displayData map[string]interface{}    // 可视化

    // 监控
    monitor             *monitor.LinkMonitor
    currentProcessStart float64
    tickHook            func(cycle int)
}
```

**关键区别**:
- `Link` 存储**实际端口对象**和**链路处理器**
- 新增的 `edgeID`, `packetTypes`, `displayData` 用于保存业务和可视化信息

---

## 数据流转过程

### 1. 前端 → 后端（构建网络）

```
用户在 CyEditor 中编辑网络
    ↓ HTTP POST /build_network
[JSON] FlowSimNetwork
    ↓ json.Unmarshal
[Protocol] protocol.FlowSimNetwork
    ↓ builder.BuildFromFlowSimNetwork()
[Core] network.Network (包含 BaseNode, Link 实例)
    ↓ 保存配置和可视化信息到 Core 结构体
    - node.SetFeature("cache", config)
    - node.SetAllDisplayData({ position, data, style })
    - node.SetCoherenceDomainID(id)
    - link.SetEdgeID(id)
    - link.SetPacketTypes(types)
    - link.SetAllDisplayData({ data })
```

**关键代码**: `internal/core/builder/builder.go:BuildFromFlowSimNetwork()`

### 2. 仿真运行

```
[Core] network.Network
    ↓ network.AdvanceTo(cycle)
各个 Node 和 Link 执行仿真逻辑
    - node.Tick()
    - link.Tick()
    - 更新内部状态（队列、缓存统计等）
```

### 3. 导出状态

```
[Core] network.Network
    ↓ network.ExportState(config)
遍历所有节点和链路
    ↓ node.ExportState() / link.ExportState()
[State] state.NetworkState
    - 从 node.features → NodeState.Features
    - 从 node.displayData → NodeState.DisplayData
    - 从 node.coherenceDomainID → NodeState.CoherenceDomainID
    - 从 node.caches[].ExportState() → NodeState.Stats["cache"]
    - 从 link.edgeID → LinkState.EdgeID
    - 从 link.displayData → LinkState.DisplayData
```

**关键代码**:
- `internal/core/node/node_export.go:ExportState()`
- `internal/core/link/link_export.go:ExportState()`

### 4. 后端 → 前端（返回状态）

```
[State] state.NetworkState
    ↓ visualization.StateToFlowSimNetwork()
[Protocol] protocol.FlowSimNetwork
    - 从 NodeState.Features["cache"] → Node.Cache (配置)
    - 从 NodeState.Stats["cache"] → Node.Cache (统计)
    - 从 NodeState.DisplayData → Node.Position, Node.Data, Node.Style
    - 从 LinkState.DisplayData["data"] → Edge.Data
    - 从 LinkState.EdgeID → Edge.EdgeId
    ↓ json.Marshal
[JSON] FlowSimNetwork
    ↓ HTTP Response
返回给前端 CyEditor
```

**关键代码**: `internal/core/visualization/adapter.go:StateToFlowSimNetwork()`

### 完整往返示例

```
CyEditor 编辑: Node 0 坐标 (150, 250)
    ↓ JSON
{ "nodes": [{ "node_id": 0, "position": { "x": 150, "y": 250 }, ... }] }
    ↓ Protocol
protocol.Node{ NodeId: 0, Position: {X: 150, Y: 250} }
    ↓ Builder
node.SetAllDisplayData({ "position": {X: 150, Y: 250}, "data": {...}, "style": {...} })
    ↓ 仿真运行 100 周期
BaseNode 内部 displayData 保持不变
    ↓ ExportState
NodeState{ DisplayData: { "position": {X: 150, Y: 250}, ... } }
    ↓ Adapter
protocol.Node{ Position: {X: 150, Y: 250} }
    ↓ JSON
{ "nodes": [{ "position": { "x": 150, "y": 250 }, ... }] }
    ↓ HTTP Response
CyEditor 接收: Node 0 坐标仍为 (150, 250) ✓
```

---

## Adapter 的作用

### 为什么需要 `adapter.go`？

`internal/core/visualization/adapter.go` 负责在 **State 结构体** 和 **Protocol 结构体** 之间进行转换。

#### 主要原因

1. **结构差异**
   - State 结构体是内部 DTO，字段设计服务于仿真逻辑
   - Protocol 结构体严格遵循 OpenAPI schema，服务于 API 契约

2. **字段拆分/合并**
   - State: `Features["cache"]` (配置) + `Stats["cache"]` (统计)
   - Protocol: `CacheConfig` (配置 + 统计合并)

3. **类型转换**
   - State: `PacketTypes []string` (内部使用字符串)
   - Protocol: `PacketTypes []int` (API 定义为整数)
   - State: `Occupancy []int`
   - Protocol: `LinkStatus []struct{ Name string, Values []int }`

4. **默认值生成**
   - 如果 DisplayData 为空，自动生成圆形布局坐标
   - 如果 Edge.Data 为空，自动生成唯一 ID

### Adapter 的核心功能

**文件**: `internal/core/visualization/adapter.go`

#### `StateToFlowSimNetwork(ns state.NetworkState) protocol.FlowSimNetwork`

**功能**: State → Protocol（后端 → 前端）

```go
// 1. 恢复网络级别显示信息
if zoom, ok := ns.DisplayData["zoom"].(float64); ok {
    zoomFloat32 := float32(zoom)
    network.Zoom = &zoomFloat32
}

// 2. 转换节点
for _, nodeState := range ns.Nodes {
    // 从 Features 恢复配置
    if cacheConfig, ok := nodeState.Features["cache"]; ok {
        node.Cache = &protocol.CacheConfig{
            Capacity: cacheConfig["capacity"].(int),
            // ...
        }
    }

    // 从 Stats 恢复统计
    if cacheStats, ok := nodeState.Stats["cache"].([]state.CacheState); ok {
        c := cacheStats[0]
        node.Cache.Hits = &c.Hits
        node.Cache.Misses = &c.Misses
    }

    // 从 DisplayData 恢复可视化
    if pos, ok := nodeState.DisplayData["position"].(struct{X, Y float32}); ok {
        node.Position = pos
    }
    if dataMap, ok := nodeState.DisplayData["data"].(protocol.Node_Data); ok {
        node.Data = dataMap
    }

    // 如果没有 DisplayData，生成默认布局（圆形）
    if node.Data.Id == "" {
        angle := 2 * math.Pi * float64(i) / float64(len(ns.Nodes))
        node.Position = struct{ X, Y float32 }{
            X: float32(centerX + radius*math.Cos(angle)),
            Y: float32(centerY + radius*math.Sin(angle)),
        }
        node.Data = protocol.Node_Data{
            Id: fmt.Sprintf("node-%d", nodeState.ID),
            Label: &label,
        }
    }
}

// 3. 转换边
for _, linkState := range ns.Links {
    // 创建端口副本（避免指针共享 bug）
    srcPortID := linkState.SourcePortID
    dstPortID := linkState.TargetPortID
    edge.SrcPortId = &srcPortID
    edge.DstPortId = &dstPortID

    // PacketTypes 类型转换
    if len(linkState.PacketTypes) > 0 {
        pts := make([]int, 0, len(linkState.PacketTypes))
        for _, pt := range linkState.PacketTypes {
            var ptInt int
            fmt.Sscanf(pt, "%d", &ptInt)
            pts = append(pts, ptInt)
        }
        edge.PacketTypes = &pts
    }

    // 从 DisplayData 恢复
    if dataMap, ok := linkState.DisplayData["data"].(protocol.Edge_Data); ok {
        edge.Data = dataMap
    }

    // 默认 ID 生成
    if edge.Data.Id == "" {
        edge.Data = protocol.Edge_Data{
            Id: fmt.Sprintf("edge-%d-p%d-%d-p%d", ...),
            Source: fmt.Sprintf("node-%d", linkState.SourceID),
            Target: fmt.Sprintf("node-%d", linkState.TargetID),
        }
    }
}
```

**关键特性**:
- 字段合并: Features + Stats → CacheConfig
- 类型转换: `[]string` → `[]int`
- 指针副本: 避免循环变量指针共享（重要 bug 修复）
- 默认值生成: 自动布局、唯一 ID 生成

---

## 多版本结构体说明

### Node 的多个版本

| 版本 | 文件位置 | 用途 | 特点 |
|-----|---------|------|------|
| `protocol.Node` | `internal/core/visualization/protocol/types.gen.go` | JSON 序列化/反序列化 | OpenAPI 自动生成，严格遵循 schema |
| `state.NodeState` | `internal/core/state/state.go` | 状态导出/快照 | 内部 DTO，包含 Stats/Features/DisplayData |
| `node.BaseNode` | `internal/core/node/node.go` | 仿真实体 | 包含运行时逻辑、队列实例、组件实例 |
| `network.NodeHandle` | `internal/core/network/network.go` | 网络管理 | 包装 Node + 队列引用，用于网络拓扑管理 |
| `NodeSchema` | `internal/core/network/network.go` | 旧版构建 API | 遗留代码，逐步被 `protocol.Node` 替代 |

### Link 的多个版本

| 版本 | 文件位置 | 用途 | 特点 |
|-----|---------|------|------|
| `protocol.Edge` | `internal/core/visualization/protocol/types.gen.go` | JSON 序列化/反序列化 | OpenAPI 自动生成，Edge 是 Link 的外部名称 |
| `state.LinkState` | `internal/core/state/state.go` | 状态导出/快照 | 内部 DTO，包含 EdgeID/PacketTypes/DisplayData |
| `link.Link` | `internal/core/link/link.go` | 仿真实体 | 包含链路处理器、端口引用、监控 |
| `EdgeSchema` | `internal/core/network/network.go` | 旧版构建 API | 遗留代码，逐步被 `protocol.Edge` 替代 |

### 为什么存在多个版本？

#### 1. **关注点分离 (Separation of Concerns)**

- **Protocol**: 只关心 API 契约，保持与 OpenAPI 一致
- **State**: 只关心数据传输，优化序列化性能
- **Core**: 只关心仿真逻辑，包含运行时行为

#### 2. **性能优化**

- State 结构体可以灵活调整字段，优化导出性能
- Core 结构体可以包含指针、接口、私有字段，不受 JSON 序列化限制

#### 3. **演化独立**

- OpenAPI schema 变更 → 只需重新生成 Protocol
- 仿真逻辑优化 → 只需修改 Core，不影响 API
- 状态导出优化 → 只需修改 State，不影响其他层

#### 4. **类型安全**

- Protocol 使用指针字段 (`*int`) 表示可选字段，符合 OpenAPI `omitempty`
- State 使用值字段 (`int`) 简化内部处理
- Core 使用私有字段保护封装

### 数据同步策略

#### 往返一致性 (Round-trip Consistency)

```
Protocol → Builder → Core → ExportState → State → Adapter → Protocol
```

**保证**: 所有配置和可视化信息完整保留

**实现**:
1. Builder 阶段: 将 Protocol 的所有字段保存到 Core
   - `node.SetFeature()`, `node.SetDisplayData()`, `node.SetCoherenceDomainID()`
   - `link.SetEdgeID()`, `link.SetPacketTypes()`, `link.SetDisplayData()`

2. ExportState 阶段: 将 Core 的所有字段导出到 State
   - `node.features` → `NodeState.Features`
   - `node.displayData` → `NodeState.DisplayData`
   - `link.edgeID` → `LinkState.EdgeID`

3. Adapter 阶段: 将 State 完整转换为 Protocol
   - `NodeState.Features` + `NodeState.Stats` → `Node.Cache`
   - `NodeState.DisplayData` → `Node.Position`, `Node.Data`, `Node.Style`

---

## 设计原则

### 1. **Schema-First**
- OpenAPI 是唯一的真相来源
- Protocol 结构体由 OpenAPI 自动生成（不可手动修改）
- 所有字段定义优先参考 OpenAPI

### 2. **零丢包原则 (Zero-Drop Data)**
- 所有字段必须在往返过程中完整保留
- 配置信息（Features）、可视化信息（DisplayData）、业务信息（EdgeID）都不能丢失
- 通过测试验证往返一致性（如 `TestMultipleParallelEdges`）

### 3. **清晰的分层边界**
- **Builder**: Protocol → Core（构建时）
- **ExportState**: Core → State（导出时）
- **Adapter**: State ↔ Protocol（转换时）
- 每层只负责自己的转换逻辑，不跨层调用

### 4. **向后兼容**
- State 结构体保留废弃字段（Caches, Directories, CustomData）
- 同时填充新旧字段，确保旧代码不会立即失效
- 标记废弃字段的注释，提示未来迁移

### 5. **明确的字段职责**
- **Stats**: 运行时统计数据（Hits, Misses, Accesses）
- **Features**: 静态配置信息（Capacity, NumSets, ReplacementPolicy）
- **DisplayData**: 可视化元数据（Position, Data.id, Style）
- **CoherenceDomainID**: 业务逻辑字段

---

## 常见问题

### Q1: 为什么 Node 有 `node_id` 和 `data.id` 两个 ID？

**A**:
- `node_id`: 业务逻辑 ID（整数），用于仿真拓扑
- `data.id`: CyEditor 节点 ID（字符串），用于前端渲染
- 两者通常有映射关系: `data.id = "node-{node_id}"`

### Q2: 为什么 Cache 既在 Features 又在 Stats？

**A**:
- `Features["cache"]`: 静态配置（容量、替换策略），构建网络时设置
- `Stats["cache"]`: 运行时统计（命中率、访问次数），仿真过程中更新
- Protocol 的 `CacheConfig` 合并了两者，通过 Adapter 拆分/合并

### Q3: PacketTypes 为什么在 State 用 `[]string` 但 Protocol 用 `[]int`？

**A**:
- 历史原因：内部实现最初用字符串表示包类型
- API 定义使用整数更符合规范
- Adapter 负责类型转换: `fmt.Sscanf(pt, "%d", &ptInt)`

### Q4: 如何添加新的配置字段？

**步骤**:
1. 修改 `web/openapi.yaml`，在 Node/Edge schema 添加字段
2. 重新生成 Protocol: `cd web && npm run generate:types`
3. 在 `state.NodeState` / `state.LinkState` 添加对应字段
4. 在 `node.BaseNode` / `link.Link` 添加存储字段和访问方法
5. 更新 `builder.BuildFromFlowSimNetwork()` 保存字段
6. 更新 `node_export.go` / `link_export.go` 导出字段
7. 更新 `adapter.go` 转换逻辑
8. 编写测试验证往返一致性

### Q5: DisplayCache 是什么？为什么被移除？

**A**:
- **曾经**: 使用全局缓存 `DisplayCache` 存储可视化信息，State 结构体中不保存
- **问题**: 违反了"零丢包"原则，往返过程中信息丢失，测试失败
- **现在**: 所有字段直接存储在 Core 结构体中（features, displayData），通过 ExportState 导出，通过 Adapter 恢复
- **优势**: 数据流清晰，完全无状态，支持并发，易于测试

---

## 参考文件

### 核心文件
- `web/openapi.yaml`: OpenAPI schema 定义
- `internal/core/visualization/protocol/types.gen.go`: 自动生成的 Protocol 结构体
- `internal/core/state/state.go`: State DTO 定义
- `internal/core/node/node.go`: BaseNode 核心实现
- `internal/core/link/link.go`: Link 核心实现

### 转换逻辑
- `internal/core/builder/builder.go`: Protocol → Core
- `internal/core/node/node_export.go`: Core → State (Node)
- `internal/core/link/link_export.go`: Core → State (Link)
- `internal/core/visualization/adapter.go`: State ↔ Protocol

### 测试
- `internal/core/visualization/flowsim_integration_test.go`: 往返一致性测试
- `internal/integration/http_workflow_test.go`: HTTP 工作流测试

---

**文档版本**: 1.0
**最后更新**: 2026-01-08
**作者**: Claude Code
