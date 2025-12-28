# Flow_Sim 自动路由设计方案

## 设计目标

1. **最小配置**：用户只需配置必要参数（如地址交错粒度）
2. **自动推断**：系统自动识别节点角色和层次关系
3. **智能路由**：根据拓扑自动生成路由规则
4. **灵活扩展**：支持不同的 Directory 位置和 Cache 层次

---

## 核心概念

### 1. 节点自描述 (Node Self-Description)

每个节点声明自己的**类型**和**能力**：

```go
// NodeRole 节点角色
type NodeRole string

const (
    RoleCompute      NodeRole = "compute"       // CPU, GPU - 发起请求
    RoleCache        NodeRole = "cache"         // L1, L2, L3 - 缓存
    RoleDirectory    NodeRole = "directory"     // 一致性目录
    RoleMemoryCtrl   NodeRole = "memory_ctrl"   // Memory Controller / Home Agent
    RoleMemory       NodeRole = "memory"        // DRAM, NVM
    RoleInterconnect NodeRole = "interconnect"  // Router, Switch
)

// NodeCapability 节点能力描述
type NodeCapability struct {
    Role           NodeRole
    CacheLevel     int              // Cache 级别 (1, 2, 3, ...)
    HasDirectory   bool             // 是否有 Directory
    IsAddressable  bool             // 是否是地址空间的一部分（Memory 是，Router 不是）
    CanInitiate    bool             // 是否能发起请求（CPU 能，DRAM 不能）
    CanRespond     bool             // 是否能响应请求（所有节点都能）
    AddressRange   *AddressRange    // 负责的地址范围（如果是 Memory）
}

// NodeDescriptor 节点描述符（每个节点提供）
type NodeDescriptor struct {
    NodeID       int
    Capability   NodeCapability
    Metadata     map[string]interface{}  // 额外的元数据
}
```

**示例：**

```go
// CPU Node
cpuDesc := NodeDescriptor{
    NodeID: 0,
    Capability: NodeCapability{
        Role:        RoleCompute,
        CanInitiate: true,
        CanRespond:  true,
    },
}

// L2 Cache Node (无 Directory)
l2Desc := NodeDescriptor{
    NodeID: 4,
    Capability: NodeCapability{
        Role:         RoleCache,
        CacheLevel:   2,
        HasDirectory: false,
        CanRespond:   true,
    },
}

// L3 Cache Node (带 Directory)
l3Desc := NodeDescriptor{
    NodeID: 5,
    Capability: NodeCapability{
        Role:         RoleCache,
        CacheLevel:   3,
        HasDirectory: true,  // L3 有 Directory
        CanRespond:   true,
    },
}

// Memory Controller (Home Agent with Directory)
haDesc := NodeDescriptor{
    NodeID: 8,
    Capability: NodeCapability{
        Role:         RoleMemoryCtrl,
        HasDirectory: true,  // HA 有 Directory
        IsAddressable: true,
        CanRespond:   true,
    },
}
```

---

### 2. 拓扑分析器 (Topology Analyzer)

根据节点描述符和连接关系，自动分析拓扑结构：

```go
// TopologyAnalyzer 拓扑分析器
type TopologyAnalyzer struct {
    nodes       map[int]*NodeDescriptor  // 所有节点
    connections map[int][]int            // 连接关系: nodeID -> [neighborIDs]
}

// AnalysisResult 分析结果
type AnalysisResult struct {
    // 节点分类
    ComputeNodes   []int
    CacheNodes     map[int][]int  // level -> [nodeIDs]
    MemoryNodes    []int
    DirectoryNodes []int

    // 层次关系
    Hierarchy      *CacheHierarchy

    // 地址映射
    AddressMapper  AddressMapper

    // 路由信息
    RoutingHints   map[int]*RoutingHint
}

// CacheHierarchy 缓存层次结构
type CacheHierarchy struct {
    // 每个节点的上游缓存（更低级别）
    UpperLevel map[int][]int  // nodeID -> [upper cache nodeIDs]

    // 每个节点的下游缓存（更高级别）
    LowerLevel map[int][]int  // nodeID -> [lower cache nodeIDs]

    // Directory 位置
    DirectoryLevel int  // 哪一级有 Directory (3 表示 L3, -1 表示 Memory Controller)
    DirectoryNodes []int
}

// RoutingHint 路由提示（为每个节点生成）
type RoutingHint struct {
    // 上游路由：当前节点 miss 时，应该去哪里
    // key: 请求类型 ("read", "write", "upgrade")
    // value: 下一跳节点 ID
    UpstreamRoute map[string]int

    // 下游路由：响应应该发送给谁
    // 通常从 pending request table 查询，这里提供默认规则
    DownstreamRoute map[int]int  // requester nodeID -> next hop nodeID

    // Directory 路由：需要一致性操作时，去哪个 Directory
    DirectoryRoute int
}
```

**分析过程：**

```go
func (a *TopologyAnalyzer) Analyze() (*AnalysisResult, error) {
    result := &AnalysisResult{
        CacheNodes: make(map[int][]int),
        RoutingHints: make(map[int]*RoutingHint),
    }

    // Step 1: 节点分类
    a.classifyNodes(result)

    // Step 2: 构建缓存层次结构
    result.Hierarchy = a.buildHierarchy()

    // Step 3: 识别 Directory 位置
    a.identifyDirectoryLevel(result)

    // Step 4: 生成地址映射器
    result.AddressMapper = a.generateAddressMapper(result)

    // Step 5: 生成路由提示
    a.generateRoutingHints(result)

    return result, nil
}

// Step 2: 构建缓存层次结构
func (a *TopologyAnalyzer) buildHierarchy() *CacheHierarchy {
    hierarchy := &CacheHierarchy{
        UpperLevel: make(map[int][]int),
        LowerLevel: make(map[int][]int),
    }

    // 算法：从连接关系推断层次
    // 规则1：Compute 节点的邻居是 L1 Cache（如果有）或 L2 Cache
    // 规则2：Ln Cache 的下游邻居是 Ln+1 Cache 或 Memory Controller
    // 规则3：使用广度优先搜索 (BFS) 从 Compute 节点开始，确定层次

    visited := make(map[int]bool)
    queue := []int{}

    // 从所有 Compute 节点开始 BFS
    for nodeID, desc := range a.nodes {
        if desc.Capability.Role == RoleCompute {
            queue = append(queue, nodeID)
            visited[nodeID] = true
        }
    }

    // BFS 遍历
    for len(queue) > 0 {
        currentID := queue[0]
        queue = queue[1:]

        currentDesc := a.nodes[currentID]

        // 查找下游节点（更深层次）
        for _, neighborID := range a.connections[currentID] {
            if visited[neighborID] {
                continue
            }

            neighborDesc := a.nodes[neighborID]

            // 判断是否是下游
            if a.isDownstream(currentDesc, neighborDesc) {
                hierarchy.LowerLevel[currentID] = append(hierarchy.LowerLevel[currentID], neighborID)
                hierarchy.UpperLevel[neighborID] = append(hierarchy.UpperLevel[neighborID], currentID)

                queue = append(queue, neighborID)
                visited[neighborID] = true
            }
        }
    }

    return hierarchy
}

// 判断 neighbor 是否是 current 的下游
func (a *TopologyAnalyzer) isDownstream(current, neighbor *NodeDescriptor) bool {
    // Compute -> Cache: 下游
    if current.Capability.Role == RoleCompute && neighbor.Capability.Role == RoleCache {
        return true
    }

    // Cache Ln -> Cache Ln+1: 下游
    if current.Capability.Role == RoleCache && neighbor.Capability.Role == RoleCache {
        return neighbor.Capability.CacheLevel > current.Capability.CacheLevel
    }

    // Cache -> Memory Controller: 下游
    if current.Capability.Role == RoleCache && neighbor.Capability.Role == RoleMemoryCtrl {
        return true
    }

    // Memory Controller -> Memory: 下游
    if current.Capability.Role == RoleMemoryCtrl && neighbor.Capability.Role == RoleMemory {
        return true
    }

    return false
}

// Step 3: 识别 Directory 位置
func (a *TopologyAnalyzer) identifyDirectoryLevel(result *AnalysisResult) {
    // 查找所有有 Directory 的节点
    for nodeID, desc := range a.nodes {
        if desc.Capability.HasDirectory {
            result.Hierarchy.DirectoryNodes = append(result.Hierarchy.DirectoryNodes, nodeID)

            // 确定 Directory 在哪一级
            if desc.Capability.Role == RoleCache {
                level := desc.Capability.CacheLevel
                if result.Hierarchy.DirectoryLevel == 0 || level < result.Hierarchy.DirectoryLevel {
                    result.Hierarchy.DirectoryLevel = level
                }
            } else if desc.Capability.Role == RoleMemoryCtrl {
                result.Hierarchy.DirectoryLevel = -1  // -1 表示在 Memory Controller
            }
        }
    }
}
```

---

### 3. 自动地址映射生成

根据拓扑分析结果，自动生成 AddressMapper：

```go
// 用户配置（最小化）
type AddressMappingConfig struct {
    Granularity uint64  // 交错粒度，如 1024 (1KB)
    Strategy    string  // "interleaved", "range", "hash"
}

// 自动生成 AddressMapper
func (a *TopologyAnalyzer) generateAddressMapper(result *AnalysisResult) AddressMapper {
    // 收集所有 Memory 节点或有 Directory 的节点（作为地址空间的终点）
    var targetNodes []int

    if result.Hierarchy.DirectoryLevel == -1 {
        // Directory 在 Memory Controller，用 Memory Controller 作为目标
        for nodeID, desc := range a.nodes {
            if desc.Capability.Role == RoleMemoryCtrl {
                targetNodes = append(targetNodes, nodeID)
            }
        }
    } else {
        // Directory 在 Cache 层，用 Directory 节点作为目标
        targetNodes = result.Hierarchy.DirectoryNodes
    }

    // 创建 AddressMapper
    return &InterleavedAddressMapper{
        targetNodeIDs: targetNodes,
        granularity:   config.Granularity,  // 用户配置
    }
}

type InterleavedAddressMapper struct {
    targetNodeIDs []int
    granularity   uint64
}

func (m *InterleavedAddressMapper) MapAddress(addr uint64) int {
    // 根据地址和粒度，计算应该去哪个目标节点
    index := (addr / m.granularity) % uint64(len(m.targetNodeIDs))
    return m.targetNodeIDs[index]
}
```

---

### 4. 自动路由生成

根据层次结构，为每个节点生成路由提示：

```go
func (a *TopologyAnalyzer) generateRoutingHints(result *AnalysisResult) {
    for nodeID, desc := range a.nodes {
        hint := &RoutingHint{
            UpstreamRoute:   make(map[string]int),
            DownstreamRoute: make(map[int]int),
        }

        // 1. 上游路由（miss 时去哪里）
        lowerNodes := result.Hierarchy.LowerLevel[nodeID]
        if len(lowerNodes) > 0 {
            // 简单情况：只有一个下游，直接路由
            if len(lowerNodes) == 1 {
                hint.UpstreamRoute["default"] = lowerNodes[0]
            } else {
                // 复杂情况：多个下游（如多个 L3 slice）
                // 需要根据地址决定，这里标记为需要 AddressMapper
                hint.UpstreamRoute["default"] = -1  // -1 表示需要地址映射
            }
        }

        // 2. Directory 路由（需要一致性操作时）
        hint.DirectoryRoute = a.findNearestDirectory(nodeID, result)

        result.RoutingHints[nodeID] = hint
    }
}

// 查找最近的 Directory 节点
func (a *TopologyAnalyzer) findNearestDirectory(nodeID int, result *AnalysisResult) int {
    // BFS 查找最近的有 Directory 的节点
    visited := make(map[int]bool)
    queue := []int{nodeID}
    visited[nodeID] = true

    for len(queue) > 0 {
        currentID := queue[0]
        queue = queue[1:]

        desc := a.nodes[currentID]
        if desc.Capability.HasDirectory {
            return currentID
        }

        // 继续向下游搜索
        for _, neighborID := range result.Hierarchy.LowerLevel[currentID] {
            if !visited[neighborID] {
                queue = append(queue, neighborID)
                visited[neighborID] = true
            }
        }
    }

    return -1  // 未找到
}
```

---

### 5. 节点使用自动路由

每个节点使用生成的 RoutingHint 进行路由决策：

```go
type AutoDecoder struct {
    myNodeID      int
    routingHint   *RoutingHint
    addressMapper AddressMapper
    linkMapper    *LinkMapper  // next_hop nodeID -> queue index
}

func (d *AutoDecoder) DecodeForMiss(addr uint64) (queueIndex int, err error) {
    var nextHop int

    // 1. 检查是否需要地址映射
    if d.routingHint.UpstreamRoute["default"] == -1 {
        // 需要地址映射（多个下游的情况）
        nextHop = d.addressMapper.MapAddress(addr)
    } else {
        // 直接使用路由提示
        nextHop = d.routingHint.UpstreamRoute["default"]
    }

    // 2. 转换为队列索引
    queueIndex = d.linkMapper.GetQueueIndex(nextHop)
    return queueIndex, nil
}

func (d *AutoDecoder) DecodeForCoherence(addr uint64) (queueIndex int, err error) {
    // 一致性操作：发送到 Directory
    nextHop := d.routingHint.DirectoryRoute
    queueIndex = d.linkMapper.GetQueueIndex(nextHop)
    return queueIndex, nil
}
```

---

## 使用流程

### Step 1: 用户创建节点并声明能力

```go
// 创建 CPU 节点
cpuNode0 := NewCPUNode(0)
cpuNode0.SetCapability(NodeCapability{
    Role:        RoleCompute,
    CanInitiate: true,
})

// 创建 L2 Cache 节点（无 Directory）
l2Node := NewL2CacheNode(4)
l2Node.SetCapability(NodeCapability{
    Role:         RoleCache,
    CacheLevel:   2,
    HasDirectory: false,
})

// 创建 L3 Cache 节点（带 Directory）
l3Node := NewL3CacheNode(5)
l3Node.SetCapability(NodeCapability{
    Role:         RoleCache,
    CacheLevel:   3,
    HasDirectory: true,  // ✅ 用户指定 Directory 在 L3
})

// 创建 Memory Controller（也可以有 Directory）
haNode := NewMemoryControllerNode(8)
haNode.SetCapability(NodeCapability{
    Role:         RoleMemoryCtrl,
    HasDirectory: false,  // ❌ 这个系统 Directory 在 L3，不在 HA
})
```

### Step 2: 用户连接节点

```go
// 使用 Network API 连接节点
network := NewNetwork()

// CPU <-> L2
network.Connect(cpuNode0, l2Node, LinkConfig{Latency: 1})
network.Connect(cpuNode1, l2Node, LinkConfig{Latency: 1})
// ...

// L2 <-> L3
network.Connect(l2Node, l3Node, LinkConfig{Latency: 5})

// L3 <-> Memory Controller
network.Connect(l3Node, haNode, LinkConfig{Latency: 10})

// Memory Controller <-> DRAM
network.Connect(haNode, dramNode0, LinkConfig{Latency: 20})
network.Connect(haNode, dramNode1, LinkConfig{Latency: 20})
```

### Step 3: 自动分析和生成路由

```go
// 用户提供最小配置
config := AutoRoutingConfig{
    AddressMapping: AddressMappingConfig{
        Granularity: 1024,  // 1KB 交错
        Strategy:    "interleaved",
    },
}

// 🔍 自动分析拓扑
analyzer := NewTopologyAnalyzer(network)
analysis, err := analyzer.Analyze()

// ✅ 分析结果：
// - 发现 4 个 CPU 节点
// - 发现 1 个 L2 节点，1 个 L3 节点
// - 识别 Directory 在 L3 (level=3)
// - 发现 2 个 DRAM 节点
// - 自动生成地址映射：addr → DRAM 0 或 DRAM 1

// 🚀 自动生成路由
decoders := GenerateDecoders(analysis, config)

// 为每个节点设置 Decoder
for nodeID, decoder := range decoders {
    nodes[nodeID].SetDecoder(decoder)
}
```

### Step 4: 节点使用自动路由

```go
// CPU Node 发送请求
func (cpu *CPUNode) SendRequest(addr uint64) {
    // 使用自动生成的 Decoder
    queueIndex, _ := cpu.decoder.DecodeForMiss(addr)

    // 发送到对应队列（自动路由到 L2）
    cpu.outputQueues[queueIndex].InjectPacket(pkt)
}

// L2 Cache Node 处理 miss
func (l2 *L2CacheNode) HandleMiss(addr uint64) {
    // 使用自动生成的 Decoder
    queueIndex, _ := l2.decoder.DecodeForMiss(addr)

    // 自动路由到 L3（因为 L3 有 Directory）
    l2.outputQueues[queueIndex].InjectPacket(pkt)
}

// L3 Cache Node 处理一致性
func (l3 *L3CacheNode) HandleCoherenceRequest(addr uint64) {
    // L3 自己就是 Directory，本地处理
    if l3.decoder.IsLocalDirectory() {
        l3.directory.HandleRequest(addr)
    }
}
```

---

## 配置示例

### 示例 1: Directory 在 L3

```go
// 用户只需指定 L3 有 Directory
l3Node.SetCapability(NodeCapability{
    Role:         RoleCache,
    CacheLevel:   3,
    HasDirectory: true,  // ✅
})

// 自动推断：
// - CPU miss → L2 → L3 (Directory) → Memory
// - 一致性请求在 L3 处理
```

### 示例 2: Directory 在 Home Agent (Memory Controller)

```go
// 用户指定 HA 有 Directory
haNode.SetCapability(NodeCapability{
    Role:         RoleMemoryCtrl,
    HasDirectory: true,  // ✅
})

// L3 没有 Directory
l3Node.SetCapability(NodeCapability{
    Role:         RoleCache,
    CacheLevel:   3,
    HasDirectory: false,  // ❌
})

// 自动推断：
// - CPU miss → L2 → L3 → HA (Directory) → Memory
// - 一致性请求在 HA 处理
```

### 示例 3: 分布式 L3 with Directory

```go
// 4 个 L3 Slice，每个都有 Directory
for i := 0; i < 4; i++ {
    l3Slice := NewL3CacheNode(10 + i)
    l3Slice.SetCapability(NodeCapability{
        Role:         RoleCache,
        CacheLevel:   3,
        HasDirectory: true,
    })
}

// 配置地址映射粒度
config.AddressMapping.Granularity = 64  // 64B (cache line)

// 自动推断：
// - 地址 X → L3 Slice (X / 64) % 4
// - 每个 L3 Slice 是一部分地址的 Home Node
// - CPU/L2 根据地址自动路由到对应的 L3 Slice
```

---

## 配置清单

### ✅ 需要用户配置

1. **节点能力**：
   - 节点类型 (Compute, Cache, Memory)
   - Cache 级别 (1, 2, 3)
   - 是否有 Directory ⭐

2. **地址映射**：
   - 交错粒度 (Granularity) ⭐
   - 映射策略 (Interleaved, Range, Hash)

3. **连接关系**：
   - 哪些节点连接到哪些节点
   - 链路延迟、带宽

### ✨ 自动推断

1. **拓扑层次**：
   - 缓存层次结构 (L1 → L2 → L3)
   - 上游/下游关系

2. **路由规则**：
   - 每个节点 miss 时去哪里
   - 一致性请求去哪个 Directory
   - 地址如何映射到 Memory/HA

3. **地址空间划分**：
   - 自动发现所有 Memory 节点
   - 根据交错粒度自动分配地址

---

## 优势

### 🎯 易用性
- 用户只需配置最少的必要信息
- 大部分路由规则自动生成
- 声明式的节点能力描述

### 🔧 灵活性
- 支持 Directory 在任意层级（L3, HA）
- 支持分布式 Cache/Directory
- 支持复杂的多层次拓扑

### 🧩 可扩展性
- 新增节点类型只需实现 NodeCapability
- 新增路由策略只需扩展 RoutingHint
- 新增地址映射只需实现 AddressMapper

### ⚡ 性能
- 路由在初始化时生成，运行时查表
- 简单拓扑（单 L2/L3）零开销
- 复杂拓扑（多 Slice）仅在必要时计算

---

## 实现路线图

### Phase 1: 基础框架
1. ✅ NodeDescriptor 和 NodeCapability 定义
2. ✅ TopologyAnalyzer 实现
3. ✅ 简单的层次结构推断（CPU → L2 → L3 → Memory）

### Phase 2: 自动路由
1. ✅ RoutingHint 生成
2. ✅ AutoDecoder 实现
3. ✅ LinkMapper（next_hop → queue index）

### Phase 3: 地址映射
1. ✅ InterleavedAddressMapper
2. ✅ 自动发现 Memory/HA 节点
3. ✅ 支持分布式 L3/HA

### Phase 4: 高级特性
1. ⬜ 动态路由（根据拥塞）
2. ⬜ 虚拟通道支持
3. ⬜ NoC 拓扑（Mesh, Ring）

---

## 开放问题

### Q1: 如何处理不规则拓扑？
例如：某些 CPU 连接到 L2，某些 CPU 直接连接到 L3？

**方案A**: BFS 自动发现最近的 Cache
**方案B**: 要求用户显式指定（减少自动推断的复杂度）

### Q2: 地址映射的一致性
L3 Slice 的地址映射是否应该和 Memory 的地址映射一致？

**方案A**: 一致（同一个 AddressMapper）- 简单，局部性好
**方案B**: 独立（两个 AddressMapper）- 灵活，但可能复杂

### Q3: 多路径路由
如果有多条路径到达目标（如 Ring 拓扑），如何选择？

**方案A**: 静态选择（如顺时针优先）
**方案B**: 动态选择（根据拥塞）

---

## 下一步

是否：
1. 先实现 Phase 1 (基础框架)，验证设计？
2. 针对 ChampSim 的简单拓扑实现一个原型？
3. 讨论开放问题的具体方案？
