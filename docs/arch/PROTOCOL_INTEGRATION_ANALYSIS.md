# Protocol 集成方案分析

## 当前架构复杂度分析

### 1. 当前 Builder 的复杂度 (builder.go: 235 行)

**数据转换操作统计**:
- **Protocol → Core 转换**: 约 140 行 (60%)
- **Display 数据转换**: 约 30 行 (13%)
- **Config 数据拷贝**: 约 40 行 (17%)
- **State 初始化**: 约 25 行 (10%)

**具体转换细节**:

```go
// 1. Node 创建 (19-137行, 118行代码)
for _, nodeProto := range flowNet.Nodes {
    // 1.1 创建 Core 实体
    newNode = node.NewWorkerNode(nodeProto.NodeId)

    // 1.2 添加组件 (Cache/Directory)
    if nodeProto.Cache != nil {
        c := cache.NewFullyAssociativeCache(nodeProto.Cache.Capacity)
        newNode.AddCache(c)
    }

    // 1.3 拷贝 Config 数据到 Features (54-74行)
    cacheConfig := map[string]interface{}{
        "capacity":           nodeProto.Cache.Capacity,
        "num_sets":           nodeProto.Cache.NumSets,
        "replacement_policy": string(nodeProto.Cache.ReplacementPolicy),
        "states":             nodeProto.Cache.States,
    }
    newNode.SetFeature("cache", cacheConfig)

    // 1.4 拷贝 Display 数据 (76-83行)
    displayData := make(map[string]interface{})
    displayData["position"] = nodeProto.Position
    displayData["data"] = nodeProto.Data
    newNode.SetAllDisplayData(displayData)

    // 1.5 创建队列 (85-125行)
    for _, port := range *nodeProto.InPorts {
        q := queue.NewInputQueue(bufferSize, port.Bandwidth)
        inputs = append(inputs, q)
    }
}

// 2. Edge 创建 (140-190行, 50行代码)
for _, edgeProto := range flowNet.Edges {
    // 2.1 提取参数
    srcPort := 0
    if edgeProto.SrcPortId != nil {
        srcPort = *edgeProto.SrcPortId
    }

    // 2.2 连接节点
    linkInstance, err := net.Connect(...)

    // 2.3 保存 Display 数据
    linkDisplayData := make(map[string]interface{})
    linkDisplayData["data"] = edgeProto.Data
    linkInstance.SetAllDisplayData(linkDisplayData)
}
```

### 2. 反向转换复杂度 (adapter.go: StateToFlowSimNetwork, 约 300 行)

**State → Protocol 转换**:
- **Node 转换**: 约 150 行
- **Link 转换**: 约 80 行
- **Display 数据恢复**: 约 70 行

**关键问题**:
```go
// 反向转换时，需要从 map[string]interface{} 恢复强类型数据
if cacheConfig, ok := nodeState.Features["cache"]; ok {
    capacity, _ := cacheConfig["capacity"].(int)  // 类型断言
    numSets, _ := cacheConfig["num_sets"].(int)
    replacementPolicy, _ := cacheConfig["replacement_policy"].(string)

    cacheConfigProto := &protocol.CacheConfig{
        Capacity:          capacity,
        NumSets:           numSets,
        ReplacementPolicy: protocol.CacheConfigReplacementPolicy(replacementPolicy),
        States:            states,
    }
}
```

---

## 提议方案：数据分组 + 直接引用

### 核心思想

**将 Protocol 数据分为三类,采用不同的处理策略**:

1. **Display 数据**: 完全透传,不在 Core 层操作
2. **Config 数据**: 构建时只读,直接引用 Protocol 结构
3. **State 数据**: 通过统一接口读写,支持仿真运行

### 数据分类详细定义

#### 1. Display 数据 (完全透传)

**定义**: 仅用于前端展示,Core 层不关心的数据

**Protocol 字段**:
```yaml
Node:
  - position: {x, y}           # 节点位置
  - data.id                    # 前端节点ID
  - data.label                 # 显示标签
  - style                      # 样式信息

Edge:
  - data.id                    # 前端边ID
  - data.source               # 前端source ID
  - data.target               # 前端target ID
  - data.lineType             # 线型

Network:
  - zoom                       # 缩放级别
  - pan: {x, y}               # 平移位置
```

**处理方式**:
- Builder: 不处理,保持在 protocol.FlowSimNetwork 中
- Core: 不存储,不访问
- Adapter: 直接从 protocol 读取并返回

#### 2. Config 数据 (只读引用)

**定义**: 构建时确定,仿真过程中不变的配置

**Protocol 字段**:
```yaml
Node:
  - node_id                    # 节点ID
  - node_name                  # 节点名称
  - node_features              # 节点类型
  - cache: {capacity, num_sets, replacement_policy, states}
  - directory: {capacity, num_sets, replacement_policy, states}
  - coherence_domain_id        # 一致性域ID
  - in_ports[].buffer_size     # 输入端口缓冲大小
  - in_ports[].bandwidth       # 输入端口带宽
  - out_ports[].buffer_size    # 输出端口缓冲大小
  - out_ports[].bandwidth      # 输出端口带宽

Edge:
  - edge_id                    # 边ID
  - src_node_id, dst_node_id   # 源节点/目标节点
  - src_port_id, dst_port_id   # 源端口/目标端口
  - latency                    # 延迟
  - bandwidth                  # 带宽
  - packet_types               # 包类型
```

**处理方式**:
- Builder: 直接引用 `&nodeProto.Cache` (指针)
- Core: 只读访问,不修改
- Adapter: 直接从 protocol 读取

#### 3. State 数据 (运行时修改)

**定义**: 仿真运行过程中动态变化的状态

**Protocol 字段**:
```yaml
Node:
  - in_ports[].buffer_length   # 当前缓冲区占用
  - in_ports[].bitmap          # 位图
  - out_ports[].buffer_length  # 当前缓冲区占用
  - out_ports[].bitmap         # 位图
  - cache.hits                 # 缓存命中数
  - cache.misses               # 缓存未命中数
  - cache.accesses             # 缓存访问数

Edge:
  - link_status                # 链路状态

Network:
  - cycle                      # 当前周期
```

**处理方式**:
- Builder: 初始化为0或默认值
- Core: 通过统一接口读写
- Adapter: 从 Core 获取最新状态写入

---

## 方案优势分析

### 1. Builder 简化 (预计减少 60% 代码)

**当前**:
```go
// 需要拷贝 Cache 配置到 Features
cacheConfig := map[string]interface{}{
    "capacity":           nodeProto.Cache.Capacity,
    "num_sets":           nodeProto.Cache.NumSets,
    "replacement_policy": string(nodeProto.Cache.ReplacementPolicy),
    "states":             nodeProto.Cache.States,
}
newNode.SetFeature("cache", cacheConfig)

// 需要拷贝 Display 数据到 displayData
displayData := make(map[string]interface{})
displayData["position"] = nodeProto.Position
displayData["data"] = nodeProto.Data
newNode.SetAllDisplayData(displayData)
```

**新方案**:
```go
// Config: 直接引用,不拷贝
newNode.SetConfigRef(nodeProto)  // 保存整个 protocol.Node 的指针

// Display: 不处理,保持在 FlowSimNetwork 中

// State: 只初始化队列
for _, port := range *nodeProto.InPorts {
    q := queue.NewInputQueue(port.BufferSize, port.Bandwidth)
    inputs = append(inputs, q)
}
```

**代码量对比**:
- 当前: ~140 行转换代码
- 新方案: ~40 行 (减少 70%)

### 2. Adapter 简化 (预计减少 50% 代码)

**当前**:
```go
// 需要从 map 恢复 Cache 配置
if cacheConfig, ok := nodeState.Features["cache"]; ok {
    capacity, _ := cacheConfig["capacity"].(int)
    numSets, _ := cacheConfig["num_sets"].(int)
    replacementPolicy, _ := cacheConfig["replacement_policy"].(string)
    states, _ := cacheConfig["states"].(string)

    cacheConfigProto := &protocol.CacheConfig{
        Capacity:          capacity,
        NumSets:           numSets,
        ReplacementPolicy: protocol.CacheConfigReplacementPolicy(replacementPolicy),
        States:            states,
    }
    node.Cache = cacheConfigProto
}

// 需要从 displayData 恢复 Position
if pos, ok := nodeState.DisplayData["position"]; ok {
    posMap := pos.(map[string]interface{})
    x := float32(posMap["x"].(float64))
    y := float32(posMap["y"].(float64))
    node.Position = struct{X float32; Y float32}{X: x, Y: y}
}
```

**新方案**:
```go
// Config: 直接读取已保存的引用
node.Cache = nodeCore.GetConfigRef().Cache  // 已经是正确的类型

// Display: 直接从 FlowSimNetwork 读取 (无需转换)
node.Position = flowNet.Nodes[i].Position

// State: 只更新运行时数据
if cacheStats := nodeCore.GetCacheStats(); cacheStats != nil {
    node.Cache.Hits = &cacheStats.Hits
    node.Cache.Misses = &cacheStats.Misses
}
```

**代码量对比**:
- 当前: ~150 行 Node 转换代码
- 新方案: ~60 行 (减少 60%)

### 3. 消除类型转换和数据拷贝

**当前问题**:
- Protocol → map[string]interface{} → Protocol (双向转换)
- 类型断言容易出错 (`capacity, _ := cacheConfig["capacity"].(int)`)
- 数据多次拷贝,内存开销大

**新方案优势**:
- Protocol → Protocol (零拷贝)
- 类型安全,编译期检查
- 内存开销降低 50%+

### 4. 维护性提升

**当前维护成本**:
- 修改 OpenAPI Schema → 修改 Protocol (自动生成) → 修改 Builder 转换逻辑 → 修改 Adapter 反向转换 → 修改 State 结构
- 5 处修改点,容易遗漏

**新方案维护成本**:
- 修改 OpenAPI Schema → 修改 Protocol (自动生成) → 完成
- 1-2 处修改点

---

## 方案挑战与解决

### 挑战 1: Network/Node/Link 需要持有 Protocol 引用

**问题**: Protocol 是整个 FlowSimNetwork,Node 只需要自己的 protocol.Node

**解决方案 A: 单独引用**
```go
type BaseNode struct {
    id   int
    // Config 引用 (只读)
    configRef *protocol.Node

    // State (读写)
    inputs  []InputQueue
    outputs []OutputQueue
    caches  []cache.Cache

    // Display 数据不存储
}

// Builder
newNode.SetConfigRef(&nodeProto)  // 保存节点配置引用
```

**解决方案 B: 保留整个 FlowSimNetwork 引用**
```go
type Network struct {
    // 原始配置 (只读)
    config *protocol.FlowSimNetwork

    // 运行时结构
    nodes map[int]*NodeHandle
    links []*link.Link
}

type BaseNode struct {
    id   int
    // 通过索引访问配置
    networkConfig *protocol.FlowSimNetwork
    nodeIndex     int

    // State
    inputs  []InputQueue
    outputs []OutputQueue
}

// 访问配置
func (n *BaseNode) GetCacheConfig() *protocol.CacheConfig {
    return n.networkConfig.Nodes[n.nodeIndex].Cache
}
```

**推荐**: 方案 A (单独引用)
- 优点: 访问更简单,不需要维护索引
- 缺点: 每个 Node/Link 多一个指针 (8字节)

### 挑战 2: 如何处理 Display 数据

**当前问题**: Display 数据分散在 Node/Link 的 displayData map 中

**解决方案: Network 保留原始 FlowSimNetwork**
```go
type Network struct {
    // 原始配置和 Display 数据
    sourceConfig *protocol.FlowSimNetwork

    // 运行时结构
    nodes map[int]*NodeHandle
    links []*link.Link
}

// Adapter 导出时
func (n *Network) ExportState(cfg ExportConfig) state.NetworkState {
    ns := state.NetworkState{}

    // Config: 从 sourceConfig 读取
    // Display: 从 sourceConfig 读取
    // State: 从 nodes/links 读取

    for i, handle := range n.nodeList {
        nodeProto := n.sourceConfig.Nodes[i]

        ns.Nodes[i].ID = nodeProto.NodeId           // Config
        ns.Nodes[i].Name = nodeProto.NodeName       // Config
        ns.Nodes[i].Position = nodeProto.Position   // Display
        ns.Nodes[i].Inputs = handle.ExportInputs()  // State
    }
}
```

**优点**:
- Display 数据零拷贝
- Config 数据零拷贝
- 只需要读取 State 数据

**缺点**:
- Network 需要维护 sourceConfig 引用
- 需要保证 Node/Link 索引与 sourceConfig 一致

### 挑战 3: State 数据的读写接口

**当前问题**: State 分散在 queue, cache, directory 等组件中

**解决方案: 统一 State 接口**
```go
// State 接口
type Stateful interface {
    ExportState() interface{}
    ImportState(state interface{}) error
}

// InputQueue 实现
func (q *InputQueue) ExportState() *protocol.Port {
    length := q.Length()
    bitmap := q.GetBitmap()
    return &protocol.Port{
        BufferLength: &length,
        Bitmap:       &bitmap,
    }
}

// Node 实现
func (n *BaseNode) ExportState() *state.NodeState {
    ns := &state.NodeState{}

    // Config: 从 configRef 读取
    ns.ID = n.configRef.NodeId
    ns.Name = n.configRef.NodeName

    // State: 从组件读取
    for i, q := range n.inputs {
        ns.Inputs[i] = q.ExportState()
    }

    for _, c := range n.caches {
        ns.CacheStats = append(ns.CacheStats, c.ExportState())
    }

    return ns
}
```

---

## 实施计划

### Phase 1: 添加 Config 引用支持

1. **修改 BaseNode/Link 结构**
   ```go
   type BaseNode struct {
       // ... existing fields ...
       configRef *protocol.Node  // 新增
   }

   type Link struct {
       // ... existing fields ...
       configRef *protocol.Edge  // 新增
   }
   ```

2. **修改 Builder**
   ```go
   func BuildFromFlowSimNetwork(flowNet protocol.FlowSimNetwork) (*network.Network, error) {
       net := network.New()
       net.SetSourceConfig(&flowNet)  // 保存原始配置

       for i, nodeProto := range flowNet.Nodes {
           newNode := node.NewWorkerNode(nodeProto.NodeId)
           newNode.SetConfigRef(&flowNet.Nodes[i])  // 引用,不拷贝

           // 只处理 State 初始化
           // ...
       }
   }
   ```

3. **测试验证**: 确保 Config 数据可以正确读取

### Phase 2: 移除 Features/DisplayData map

1. **删除 BaseNode.features 和 displayData**
2. **修改所有读取 Features 的代码**
   ```go
   // Before
   cacheConfig := node.GetFeature("cache")
   capacity := cacheConfig["capacity"].(int)

   // After
   capacity := node.GetConfigRef().Cache.Capacity
   ```

3. **修改 Adapter**
   ```go
   // Before
   if pos, ok := nodeState.DisplayData["position"]; ok {
       // 复杂的类型转换
   }

   // After
   node.Position = net.sourceConfig.Nodes[i].Position
   ```

### Phase 3: 简化 State 导出

1. **实现 Stateful 接口**
2. **重构 ExportState 方法**
3. **性能测试**: 确保导出性能不下降

### Phase 4: 文档和清理

1. **更新架构文档**
2. **删除未使用的辅助函数** (nodeDataToMap, edgeDataToMap)
3. **性能基准测试**

---

## 预期收益

### 代码复杂度
- Builder: 235行 → ~80行 (减少 65%)
- Adapter: 300行 → ~120行 (减少 60%)
- 总计: 减少约 350 行代码

### 内存开销
- 当前: Protocol + Features map + DisplayData map (3份数据)
- 新方案: Protocol + State (1.5份数据)
- 减少约 50% 内存开销

### 维护成本
- Schema 修改传播路径: 5步 → 1-2步
- 类型安全: 运行时断言 → 编译期检查

### 性能
- 数据拷贝: 大量 → 几乎零
- 类型转换: 频繁 → 无

---

## 风险评估

### 低风险
- ✅ 不影响仿真逻辑
- ✅ 不改变 API 接口
- ✅ 可以渐进式实施

### 中风险
- ⚠️  需要修改大量现有代码
- ⚠️  需要仔细处理索引对应关系
- **缓解**: 充分的单元测试覆盖

### 可控风险
- ⚠️  Protocol 结构必须稳定 (不能频繁修改指针指向的内容)
- **缓解**: 明确标记只读字段,code review 把关

---

## 总结

### 方案可行性: ★★★★★

**优势**:
1. 大幅简化代码 (减少 60%+)
2. 消除类型转换和数据拷贝
3. 提升类型安全
4. 降低维护成本
5. 提升性能

**挑战**:
1. 需要重构现有代码
2. 需要保证索引一致性
3. 需要明确只读语义

**建议**:
- ✅ 强烈推荐实施
- 采用渐进式迁移 (Phase 1-4)
- 每个 Phase 完成后进行充分测试
- 优先实施 Phase 1-2 (收益最大)
