# 旧版 Schema 迁移分析与 Protocol-Core 合并可行性研究

## 问题 1: 旧版 API (NodeSchema/EdgeSchema) 迁移分析

### 当前状态

**旧版 Schema 定义**:
- 文件: `internal/core/network/network.go`
- 结构体: `NodeSchema`, `EdgeSchema`, `NetworkSchema`
- 使用场景: `Network.Reset()` 方法

**新版 Schema**:
- 文件: `internal/core/visualization/protocol/types.gen.go` (自动生成)
- 结构体: `protocol.Node`, `protocol.Edge`, `protocol.FlowSimNetwork`
- 使用场景: HTTP API (`/build_network`, `/reset_network`)

### 使用情况调查

#### 生产代码使用

```bash
$ grep -r "NodeSchema\|EdgeSchema\|NetworkSchema" --include="*.go" --exclude-dir=docs | grep -v "test.go"

internal/core/network/network.go:52:type NodeSchema struct {
internal/core/network/network.go:65:type EdgeSchema struct {
internal/core/network/network.go:75:type NetworkSchema struct {
internal/core/network/network.go:379:func (n *Network) Reset(schema *NetworkSchema) error {
```

**结论**: 旧版 Schema **仅在** `Network.Reset()` 方法中使用。

#### 测试代码使用

```bash
$ grep -r "NodeSchema\|EdgeSchema" internal/core/network/network_reset_test.go | wc -l
44
```

**network_reset_test.go** 中有 44 处引用，涵盖 18 个测试用例。

### 迁移阻碍分析

#### 1. **字段差异**

##### NodeSchema vs protocol.Node

| 字段 | NodeSchema | protocol.Node | 兼容性 |
|-----|-----------|--------------|-------|
| ID | `NodeID int` | `NodeId int` | ✓ 名称不同但语义相同 |
| Name | - | `NodeName string` | ✓ 可选字段 |
| Features | `NodeFeatures []string` | `NodeFeatures *[]string` | ✓ 指针差异 |
| Cache | `*CacheConfigSchema` | `*CacheConfig` | ⚠️ 结构体名称不同 |
| Directory | `*DirectoryConfigSchema` | `*DirectoryConfig` | ⚠️ 结构体名称不同 |
| CoherenceDomainID | `*int` | `*int` | ✓ 完全兼容 |
| Ports | `[]PortSchema` | `*[]Port` | ⚠️ 指针差异 |
| **可视化字段** | - | `Data`, `Position`, `Style` | ✗ **缺失** |

**关键差异**:
- `NodeSchema` 缺少可视化字段 (`data`, `position`, `style`)
- `PortSchema` 字段不同（见下表）

##### PortSchema vs protocol.Port

| 字段 | PortSchema | protocol.Port | 兼容性 |
|-----|-----------|--------------|-------|
| ID | `PortID *int` | `PortId int` | ⚠️ 一个指针，一个值 |
| PacketTypes | `PacketTypes []int` | `PacketTypes *[]int` | ⚠️ 指针差异 |
| BufferSize | `BufferSize int` | `BufferSize *int` | ⚠️ 可选性不同 |
| **带宽** | `InBandwidth int` | `Bandwidth int` | ✗ **字段名不同** |
| **带宽** | `OutBandwidth int` | - | ✗ **缺失** |
| **运行时状态** | - | `BufferLength`, `Capacity`, `Bitmap` | ✗ **缺失** |

**关键问题**:
- `PortSchema` 使用 `InBandwidth` + `OutBandwidth`，而 `protocol.Port` 只有 `Bandwidth`
- `PortSchema` 缺少运行时状态字段

##### EdgeSchema vs protocol.Edge

| 字段 | EdgeSchema | protocol.Edge | 兼容性 |
|-----|-----------|--------------|-------|
| ID | `EdgeID int` | `EdgeId int` | ✓ 名称差异 |
| Source | `SrcNodeID int` | `SrcNodeId int` | ✓ 名称差异 |
| Source Port | `SrcPortID int` | `SrcPortId *int` | ⚠️ 可选性不同 |
| Target | `DstNodeID int` | `DstNodeId int` | ✓ 名称差异 |
| Target Port | `DstPortID int` | `DstPortId *int` | ⚠️ 可选性不同 |
| PacketTypes | `PacketTypes []int` | `PacketTypes *[]int` | ⚠️ 指针差异 |
| **可视化字段** | - | `Data`, `LinkStatus` | ✗ **缺失** |
| **链路参数** | - | `Latency *int`, `Bandwidth *int` | ✗ **缺失** |

**关键问题**:
- `EdgeSchema` 缺少链路参数（latency, bandwidth）
- `EdgeSchema` 缺少可视化字段

#### 2. **API 兼容性**

当前 `Network.Reset()` 签名:
```go
func (n *Network) Reset(schema *NetworkSchema) error
```

如果直接改为:
```go
func (n *Network) Reset(schema *protocol.FlowSimNetwork) error
```

**破坏性变更**:
- 所有 `network_reset_test.go` 的 18 个测试需要重写
- 任何外部调用 `Reset()` 的代码会编译失败

#### 3. **设计意图差异**

**NodeSchema/EdgeSchema**:
- 设计意图: 最小化配置 API
- 使用场景: 编程式构建网络（如测试、benchmark）
- 特点:
  - 只包含必要的拓扑和配置信息
  - 没有可视化字段
  - 简洁的端口定义

**protocol.FlowSimNetwork**:
- 设计意图: 完整的数据交换格式
- 使用场景: HTTP API、前端可视化
- 特点:
  - 包含完整的可视化信息
  - 支持运行时状态
  - 兼容 CyEditor 格式

### 迁移方案

#### 方案 A: 彻底移除 NodeSchema（推荐）

**步骤**:

1. **新增 Builder 函数**
   ```go
   // 从 protocol.FlowSimNetwork 构建（已存在）
   func BuildFromFlowSimNetwork(flowNet protocol.FlowSimNetwork) (*Network, error)

   // 新增：从简化配置构建（用于测试）
   type SimpleNetworkConfig struct {
       Nodes []struct {
           ID       int
           InPorts  int  // 端口数量
           OutPorts int
           Cache    *CacheConfig
       }
       Edges []struct {
           SrcNodeID int
           SrcPort   int
           DstNodeID int
           DstPort   int
           Latency   int
           Bandwidth int
       }
   }

   func BuildSimpleNetwork(cfg SimpleNetworkConfig) (*Network, error)
   ```

2. **迁移 Reset() 方法**
   ```go
   // 删除旧的 Reset()
   // func (n *Network) Reset(schema *NetworkSchema) error

   // 新增：使用 protocol
   func (n *Network) RebuildFromProtocol(flowNet protocol.FlowSimNetwork) error {
       // 清理现有网络
       n.stopWorkers()
       n.nodes = make(map[int]*NodeHandle)
       n.links = make([]*link.Link, 0)
       n.nodeList = nil
       n.frozen = false

       // 调用 builder
       newNet, err := builder.BuildFromFlowSimNetwork(flowNet)
       if err != nil {
           return err
       }

       // 复制内部状态
       n.nodes = newNet.nodes
       n.links = newNet.links
       return nil
   }
   ```

3. **重写测试**

   迁移 `network_reset_test.go` 的 18 个测试，使用 `protocol.FlowSimNetwork` 或 `SimpleNetworkConfig`。

4. **删除旧代码**

   删除 `NodeSchema`, `EdgeSchema`, `NetworkSchema` 定义。

**优点**:
- ✅ 统一到 OpenAPI schema，减少维护负担
- ✅ 自动获得新字段支持（可视化、状态等）
- ✅ 符合 Schema-First 原则

**缺点**:
- ❌ 需要重写所有测试
- ❌ 破坏性变更（如果有外部依赖）

**工作量估计**: 2-3 小时

#### 方案 B: 保留 NodeSchema 但标记废弃

**步骤**:

1. 添加废弃注释
   ```go
   // Deprecated: Use protocol.FlowSimNetwork instead.
   // NodeSchema will be removed in v2.0.
   type NodeSchema struct { ... }
   ```

2. 提供迁移路径文档

3. 新代码禁止使用

**优点**:
- ✅ 向后兼容
- ✅ 给用户迁移缓冲期

**缺点**:
- ❌ 维护两套 API
- ❌ 容易混淆

#### 方案 C: 转换适配器

**步骤**:

1. 保留 `NodeSchema`
2. 添加转换函数
   ```go
   func NodeSchemaToProtocol(ns NodeSchema) protocol.Node { ... }
   func EdgeSchemaToProtocol(es EdgeSchema) protocol.Edge { ... }
   ```
3. `Reset()` 内部转换后调用 builder

**优点**:
- ✅ API 兼容
- ✅ 内部统一

**缺点**:
- ❌ 额外的转换开销
- ❌ 字段映射复杂（端口带宽问题）

### 推荐决策

**推荐方案 A（彻底移除）**，理由：

1. **使用范围极小**: 只有 `Reset()` 方法和测试使用
2. **字段差异大**: 端口带宽设计不兼容，转换困难
3. **测试代码易改**: 测试代码修改成本可控
4. **长远收益**: 统一数据结构，减少未来维护成本

---

## 问题 2: Protocol 与 Core 实体合并可行性分析

### 当前架构回顾

**分层设计**:
```
protocol.Node (JSON 层)
    ↓ BuildFromFlowSimNetwork()
node.BaseNode (实体层) + features/displayData 字段
    ↓ ExportState()
state.NodeState (DTO 层)
    ↓ StateToFlowSimNetwork()
protocol.Node (JSON 层)
```

### 合并方案探讨

#### 方案 1: BaseNode 直接使用 protocol.Node

**设计**:
```go
type BaseNode struct {
    // 嵌入 protocol 数据
    config protocol.Node  // 包含所有配置和可视化

    // 运行时对象（不能序列化）
    inputs  []*queue.InputQueue   // 实际队列实例
    outputs []*queue.OutputQueue
    caches  []cache.Cache         // 实际组件实例

    // 运行时状态
    handler      NodeHandler
    monitor      *monitor.NodeMonitor
    currentCycle uint64
    dataMu       sync.RWMutex
}
```

**访问方式**:
```go
// 配置访问
node.config.NodeId
node.config.Cache.Capacity
node.config.Position.X

// 运行时访问
node.inputs[0].Length()
node.handler.Process(...)
```

#### 评估：技术可行性

##### 优点

1. **✅ 数据统一**
   - 只维护一份配置数据
   - 自动支持所有 OpenAPI 字段
   - 往返序列化无需映射

2. **✅ 代码简化**
   - 删除 `features`, `coherenceDomainID`, `displayData` 字段
   - 删除 SetFeature/GetFeature 等方法
   - ExportState 简化: 直接读取 `config`

3. **✅ 类型安全**
   - 编译时检查字段存在性
   - 自动获得 OpenAPI schema 更新

##### 缺点

1. **❌ 耦合严重**

   **问题**: Core 层依赖 Visualization 层

   ```go
   package node  // internal/core/node

   import (
       "github.com/Readm/flow_sim/internal/core/visualization/protocol"  // ❌ 跨层依赖
   )
   ```

   **影响**:
   - 违反分层架构原则（Core 不应依赖 Visualization）
   - 循环依赖风险: `visualization → core → visualization`
   - 单元测试困难: 测试 Node 需要 mock protocol 结构

2. **❌ 配置与状态混淆**

   **问题**: `protocol.Node` 同时包含配置和运行时状态

   ```go
   type Node struct {
       // 配置（静态）
       NodeId   int
       Cache    *CacheConfig { Capacity, NumSets }

       // 运行时状态（动态）
       InPorts  []Port { BufferLength, Bitmap }
       Cache    *CacheConfig { Hits, Misses }  // 同一个对象！
   }
   ```

   **影响**:
   - BaseNode 需要不断更新 `config` 的运行时字段
   - 并发访问问题: 仿真时修改 `config.InPorts[0].BufferLength` 需要锁
   - 语义不清: 哪些字段是配置？哪些是状态？

3. **❌ 指针字段处理复杂**

   **问题**: Protocol 使用大量指针表示可选字段

   ```go
   type Node struct {
       NodeFeatures      *[]string           // 可选
       Cache             *CacheConfig        // 可选
       CoherenceDomainId *int                // 可选
       InPorts           *[]Port             // 可选
       Style             *map[string]interface{} // 可选
   }
   ```

   **影响**:
   - 访问前需要 nil 检查: `if node.config.Cache != nil { ... }`
   - 修改时需要初始化: `node.config.Cache = &protocol.CacheConfig{...}`
   - 容易引发 nil panic

4. **❌ JSON tag 污染**

   **问题**: Protocol 结构体包含 JSON tag

   ```go
   type Node struct {
       NodeId   int    `json:"node_id"`
       NodeName string `json:"node_name"`
       Cache    *CacheConfig `json:"cache,omitempty"`
   }
   ```

   **影响**:
   - BaseNode 不需要 JSON 序列化（通过 ExportState）
   - Tag 信息无用但占用内存
   - 误导开发者直接序列化 BaseNode

5. **❌ 字段冗余**

   **问题**: Protocol 包含运行时状态字段，BaseNode 已有实际对象

   ```go
   type BaseNode struct {
       config  protocol.Node
       // config.InPorts[0].BufferLength = 5

       inputs  []*queue.InputQueue
       // inputs[0].Length() = 5

       // 两处存储相同信息！
   }
   ```

   **影响**:
   - 数据冗余，增加内存占用
   - 同步问题: 需要保证 `config` 和实际对象一致
   - 维护成本: 每次修改需要更新两处

#### 方案 2: 轻量级引用

**设计**:
```go
type BaseNode struct {
    // 只保存配置（不包含运行时状态）
    nodeID            int
    coherenceDomainID *int

    // 可视化信息（轻量）
    displayData struct {
        dataID   string    // protocol.Node.Data.Id
        position struct{ X, Y float32 }
        style    map[string]interface{}
    }

    // 运行时对象
    inputs   []*queue.InputQueue
    outputs  []*queue.OutputQueue
    caches   []cache.Cache
    handler  NodeHandler
    monitor  *monitor.NodeMonitor
}

// ExportState 时动态构建 protocol.Node
func (n *BaseNode) ToProtocol() protocol.Node {
    return protocol.Node{
        NodeId: n.nodeID,
        Position: struct{...}{X: n.displayData.position.X, ...},
        Data: protocol.Node_Data{Id: n.displayData.dataID},
        // ... 动态填充
    }
}
```

**优点**:
- ✅ 避免跨层依赖
- ✅ 配置与状态分离
- ✅ 控制数据冗余

**缺点**:
- ❌ 仍需维护字段映射
- ❌ 新字段需要手动添加

#### 方案 3: 组合模式（推荐）

**设计**:
```go
// 分离不可变配置
type NodeConfig struct {
    ID                int
    Name              string
    Features          []string
    CoherenceDomainID *int

    // Cache/Directory 配置（不含统计）
    CacheConfig     *CacheConfigStatic
    DirectoryConfig *DirectoryConfigStatic
}

// 分离可视化信息
type NodeDisplayInfo struct {
    DataID   string
    Label    string
    Position struct{ X, Y float32 }
    Style    map[string]interface{}
}

type BaseNode struct {
    // 不可变配置（来自 protocol，构建后不变）
    config  *NodeConfig
    display *NodeDisplayInfo

    // 运行时对象
    inputs   []*queue.InputQueue
    outputs  []*queue.OutputQueue
    caches   []cache.Cache
    handler  NodeHandler
    monitor  *monitor.NodeMonitor
    currentCycle uint64
}

// 从 protocol 构建
func NewBaseNodeFromProtocol(protoNode protocol.Node) *BaseNode {
    return &BaseNode{
        config: &NodeConfig{
            ID:   protoNode.NodeId,
            Name: protoNode.NodeName,
            // ...
        },
        display: &NodeDisplayInfo{
            DataID: protoNode.Data.Id,
            Position: protoNode.Position,
            // ...
        },
        inputs:  []*queue.InputQueue{},
        outputs: []*queue.OutputQueue{},
    }
}

// 导出到 protocol
func (n *BaseNode) ExportToProtocol() protocol.Node {
    return protocol.Node{
        NodeId:   n.config.ID,
        NodeName: n.config.Name,
        Position: n.display.Position,
        Data: protocol.Node_Data{
            Id:    n.display.DataID,
            Label: &n.display.Label,
        },
        // 动态生成运行时状态
        InPorts: n.buildPortStates(),
        Cache:   n.buildCacheState(),
    }
}
```

**优点**:
- ✅ **清晰的职责分离**: Config（配置）、Display（可视化）、Runtime（运行时）
- ✅ **避免跨层依赖**: NodeConfig 是本地定义，不依赖 protocol
- ✅ **不可变性保证**: Config 构建后只读，线程安全
- ✅ **易于测试**: 可以独立测试 Config、Display、Runtime
- ✅ **灵活性**: 未来可以独立演化各部分

**缺点**:
- ❌ 需要定义中间结构体（NodeConfig, NodeDisplayInfo）
- ❌ 仍需维护 protocol ↔ config 映射

### 推荐决策

**不建议合并 Protocol 和 Core 实体**，理由：

1. **架构原则**
   - Core 层应该独立于 Visualization 层
   - Protocol 是外部契约，Core 是内部实现
   - 分层架构保持清晰边界

2. **职责分离**
   - Protocol: JSON 序列化/反序列化
   - Core: 仿真逻辑、运行时状态
   - 两者关注点完全不同

3. **现有设计已足够好**

   当前方案（方案 3 的变体）:
   ```go
   BaseNode {
       // 配置
       features          map[string]map[string]interface{}
       coherenceDomainID *int
       displayData       map[string]interface{}

       // 运行时
       inputs, outputs, caches, handler, monitor
   }
   ```

   **优势**:
   - ✅ 灵活的 map 存储，支持任意字段
   - ✅ 无跨层依赖
   - ✅ 配置与状态分离
   - ✅ 已通过所有测试验证

4. **合并收益不大**

   **当前的"问题"**:
   - 需要维护字段映射（builder, export, adapter）

   **但这是必要的代价**:
   - Builder: Protocol → Core（构建逻辑）
   - Export: Core → State（状态提取）
   - Adapter: State ↔ Protocol（格式转换）

   每一层都有明确的职责，合并反而会混淆。

### 替代优化建议

如果觉得当前映射代码繁琐，可以考虑：

#### 优化 1: 代码生成

使用 `go generate` 自动生成映射代码:

```go
//go:generate go run scripts/gen_mappers.go

// 自动生成:
func ProtocolNodeToConfig(pn protocol.Node) NodeConfig { ... }
func ConfigToProtocolNode(cfg NodeConfig) protocol.Node { ... }
```

#### 优化 2: 反射辅助函数

```go
func MapFields(src, dst interface{}, fieldMap map[string]string) {
    // 使用反射自动映射字段
}

// 使用
MapFields(protoNode, nodeConfig, map[string]string{
    "NodeId": "ID",
    "NodeName": "Name",
    // ...
})
```

#### 优化 3: 统一 Config 定义

将 NodeConfig 移到独立包:

```go
package config

// 统一的配置定义（OpenAPI schema 的 Go 表示）
type NodeConfig struct { ... }

// protocol.Node → config.NodeConfig
func FromProtocol(pn protocol.Node) NodeConfig { ... }

// config.NodeConfig → protocol.Node
func (c NodeConfig) ToProtocol() protocol.Node { ... }
```

---

## 总结

### 问题 1: 旧版 Schema 迁移

**建议**: **立即迁移到 protocol.FlowSimNetwork**

**行动计划**:
1. 为测试创建 `SimpleNetworkConfig` 辅助结构
2. 重写 `network_reset_test.go` 的 18 个测试
3. 删除 `NodeSchema`, `EdgeSchema`, `NetworkSchema`
4. 删除 `Network.Reset()` 方法

**预期收益**:
- 统一数据结构，减少维护成本
- 自动支持新字段
- 符合 Schema-First 原则

**工作量**: 2-3 小时

### 问题 2: Protocol-Core 合并

**建议**: **不合并，保持当前分层架构**

**理由**:
- 跨层依赖违反架构原则
- 配置与状态混淆
- 收益不明显，成本较高
- 当前设计已足够清晰和灵活

**可选优化**:
- 使用代码生成减少样板代码
- 添加反射辅助函数
- 统一 Config 定义到独立包

---

**文档版本**: 1.0
**最后更新**: 2026-01-08
**作者**: Claude Code
