# 一致性责任树 (Coherence Tree) 设计

## 问题分析

### 核心问题：物理拓扑 ≠ 一致性责任

考虑这个复杂拓扑：

```
          L3[0] (Dir)          L3[1] (Dir)
              |                    |
        +-----+-----+        +-----+-----+
        |           |        |           |
     CPU[0]      CPU[1]   CPU[2]      CPU[3]
        |           |        |           |
        +-----+-----+        +-----+-----+
              |                    |
          HA[0] (Dir)          HA[1] (Dir)
              |                    |
          DRAM[0]              DRAM[1]
```

**问题：**
1. 地址 X 的一致性由谁负责？L3[0] 还是 HA[0]？
2. L3[0] 和 L3[1] 之间的一致性由谁维护？
3. 如果 CPU[0] 和 CPU[2] 共享一个 cache line，谁负责协调？

**仅靠 BFS 无法回答这些问题！**

---

## 解决方案：一致性责任树 (Coherence Tree)

### 1. 核心概念

**Coherence Tree** 定义了一致性维护的**层次责任**：

```
Root: System-level Directory (如果存在)
  |
  +-- Branch: L3[0] Directory
  |     |
  |     +-- Leaf: CPU[0], CPU[1] (这些 CPU 的一致性由 L3[0] 负责)
  |
  +-- Branch: L3[1] Directory
        |
        +-- Leaf: CPU[2], CPU[3] (这些 CPU 的一致性由 L3[1] 负责)
```

**关键概念：**

- **Home Node**: 负责某个地址的一致性维护的节点
- **Coherence Domain**: 一个 Directory 管理的节点集合
- **Coherence Hierarchy**: Directory 之间的层次关系

---

### 2. Coherence Tree 的定义

```go
// CoherenceTree 一致性责任树
type CoherenceTree struct {
    // 根节点：最高层的 Directory（如果存在）
    Root *CoherenceNode

    // 所有 Directory 节点
    DirectoryNodes map[int]*CoherenceNode

    // 地址到 Home Node 的映射
    AddressMapper HomeNodeMapper
}

// CoherenceNode 一致性树节点
type CoherenceNode struct {
    NodeID int
    Role   NodeRole  // RoleCache (L3), RoleMemoryCtrl (HA), 等

    // 一致性域：这个 Directory 负责哪些节点
    Domain []int  // 管理的节点 IDs (CPU, L1, L2)

    // 子节点：下一层的 Directory
    Children []*CoherenceNode

    // 父节点：上一层的 Directory
    Parent *CoherenceNode

    // 地址责任：这个节点负责哪些地址
    AddressResponsibility *AddressRange
}

// HomeNodeMapper 地址到 Home Node 的映射
type HomeNodeMapper interface {
    // GetHomeNode 返回负责该地址的 Directory 节点 ID
    GetHomeNode(addr uint64) int

    // GetCoherencePath 返回从 requester 到 Home Node 的路径
    GetCoherencePath(requesterID int, addr uint64) []int
}
```

---

### 3. 构建策略：自动推断 + 用户指定

#### 策略 1: 自动推断（基于 BFS + 启发式）

```go
// AutoInferCoherenceTree 自动推断一致性树
func AutoInferCoherenceTree(topology *Topology) (*CoherenceTree, error) {
    tree := &CoherenceTree{
        DirectoryNodes: make(map[int]*CoherenceNode),
    }

    // Step 1: 识别所有 Directory 节点
    dirNodes := findDirectoryNodes(topology)

    // Step 2: 根据拓扑结构推断层次关系
    // 启发式规则：
    // - 如果 Directory A 和 Directory B 之间有路径
    // - 且 A 的 CacheLevel < B 的 CacheLevel (或 A 是 MemCtrl)
    // - 则 B 是 A 的子节点

    for _, dirA := range dirNodes {
        for _, dirB := range dirNodes {
            if isParentChild(topology, dirA, dirB) {
                // dirB 是 dirA 的子节点
                nodeA := tree.getOrCreateNode(dirA)
                nodeB := tree.getOrCreateNode(dirB)
                nodeA.Children = append(nodeA.Children, nodeB)
                nodeB.Parent = nodeA
            }
        }
    }

    // Step 3: 找到根节点（没有父节点的 Directory）
    for _, node := range tree.DirectoryNodes {
        if node.Parent == nil {
            if tree.Root == nil {
                tree.Root = node
            } else {
                // ⚠️ 发现多个根节点 - 无法自动推断！
                return nil, ErrMultipleRoots
            }
        }
    }

    // Step 4: 为每个 Directory 分配 Domain（管理的节点）
    assignDomains(tree, topology)

    return tree, nil
}

// 判断 dirA 是否是 dirB 的父节点
func isParentChild(topology *Topology, dirA, dirB *NodeDescriptor) bool {
    // 启发式 1: 层次关系
    if dirA.Capability.Role == RoleMemoryCtrl && dirB.Capability.Role == RoleCache {
        return true  // HA 是 L3 的父节点
    }

    if dirA.Capability.Role == RoleCache && dirB.Capability.Role == RoleCache {
        if dirA.Capability.CacheLevel > dirB.Capability.CacheLevel {
            return true  // L3 是 L2 的父节点（如果 L2 有 Directory）
        }
    }

    // 启发式 2: 距离 Memory 的远近
    // 离 Memory 更近的是父节点
    distA := topology.GetDistance(dirA.NodeID, findNearestMemory(topology, dirA.NodeID))
    distB := topology.GetDistance(dirB.NodeID, findNearestMemory(topology, dirB.NodeID))
    if distA < distB {
        return true
    }

    return false
}
```

**自动推断的局限性：**
- ✅ 可以处理简单的层次结构（L3 → HA）
- ✅ 可以处理单层 Directory（只在 L3 或只在 HA）
- ❌ 无法处理复杂的分区 Directory（多个 L3 + 多个 HA）
- ❌ 无法处理混合一致性策略

---

#### 策略 2: 用户显式指定

当自动推断失败时，用户可以显式定义一致性树：

```go
// 用户 API 1: 声明式构建
builder := NewCoherenceTreeBuilder()

// 定义 L3 层 Directory（每个 L3 管理 2 个 CPU）
builder.AddDirectory(l3_0_ID, CoherenceDomain{
    ManagedNodes: []int{cpu0_ID, cpu1_ID},
    AddressRange: AddressRange{Start: 0x0000, End: 0x7FFF},  // 地址分区
})

builder.AddDirectory(l3_1_ID, CoherenceDomain{
    ManagedNodes: []int{cpu2_ID, cpu3_ID},
    AddressRange: AddressRange{Start: 0x8000, End: 0xFFFF},
})

// 定义 HA 层 Directory（管理所有 L3）
builder.AddDirectory(ha_0_ID, CoherenceDomain{
    ManagedNodes: []int{l3_0_ID, l3_1_ID},  // HA 管理 L3 层
    AddressRange: AddressRange{Start: 0x0000, End: 0xFFFF},  // 全地址
})

// 定义层次关系
builder.SetParent(l3_0_ID, ha_0_ID)
builder.SetParent(l3_1_ID, ha_0_ID)

tree := builder.Build()
```

```go
// 用户 API 2: 编程式构建
tree := &CoherenceTree{}

// 创建 HA 节点（根）
haNode := &CoherenceNode{
    NodeID: ha_0_ID,
    Role:   RoleMemoryCtrl,
    Domain: []int{l3_0_ID, l3_1_ID},
}
tree.Root = haNode

// 创建 L3[0] 节点
l3_0_Node := &CoherenceNode{
    NodeID:   l3_0_ID,
    Role:     RoleCache,
    Domain:   []int{cpu0_ID, cpu1_ID},
    Parent:   haNode,
}
haNode.Children = append(haNode.Children, l3_0_Node)

// 创建 L3[1] 节点
l3_1_Node := &CoherenceNode{
    NodeID:   l3_1_ID,
    Role:     RoleCache,
    Domain:   []int{cpu2_ID, cpu3_ID},
    Parent:   haNode,
}
haNode.Children = append(haNode.Children, l3_1_Node)
```

---

#### 策略 3: 混合方案（推荐）⭐

```go
// 混合方案：BFS 推断 + 用户覆盖
type CoherenceTreeConfig struct {
    // 自动推断
    AutoInfer bool  // 是否尝试自动推断

    // 用户覆盖（可选）
    ExplicitTree *CoherenceTree  // 用户显式定义的树
    Overrides    map[string]interface{}  // 用户覆盖的部分

    // 地址映射策略
    AddressMapping AddressMappingStrategy
}

func BuildCoherenceTree(topology *Topology, config CoherenceTreeConfig) (*CoherenceTree, error) {
    var tree *CoherenceTree
    var err error

    // Step 1: 尝试自动推断
    if config.AutoInfer {
        tree, err = AutoInferCoherenceTree(topology)
        if err == nil {
            log.Info("✅ 自动推断一致性树成功")
        } else {
            log.Warn("⚠️ 自动推断失败: %v", err)
        }
    }

    // Step 2: 如果自动推断失败，使用用户显式定义
    if tree == nil && config.ExplicitTree != nil {
        tree = config.ExplicitTree
        log.Info("✅ 使用用户显式定义的一致性树")
    }

    // Step 3: 应用用户覆盖
    if config.Overrides != nil {
        applyOverrides(tree, config.Overrides)
    }

    // Step 4: 验证一致性树的正确性
    if err := ValidateCoherenceTree(tree); err != nil {
        return nil, fmt.Errorf("一致性树验证失败: %v", err)
    }

    return tree, nil
}
```

---

### 4. 使用一致性树进行路由

有了一致性树，路由决策变得清晰：

```go
// CoherenceRouter 基于一致性树的路由器
type CoherenceRouter struct {
    tree     *CoherenceTree
    myNodeID int
    topology *Topology
}

// RouteForCoherence 一致性请求路由
// 返回：应该发送到哪个节点（Home Node 或中间节点）
func (r *CoherenceRouter) RouteForCoherence(addr uint64, msgType CoherenceMessageType) int {
    // 1. 找到该地址的 Home Node
    homeNodeID := r.tree.AddressMapper.GetHomeNode(addr)

    // 2. 如果我就是 Home Node，本地处理
    if homeNodeID == r.myNodeID {
        return LOCAL_PROCESSING
    }

    // 3. 找到从当前节点到 Home Node 的路径
    path := r.tree.AddressMapper.GetCoherencePath(r.myNodeID, addr)

    // 4. 返回下一跳（路径的第二个节点）
    if len(path) > 1 {
        return path[1]
    }

    return homeNodeID
}

// RouteForData 数据请求路由（可能不同于一致性路由）
func (r *CoherenceRouter) RouteForData(addr uint64) int {
    // 数据请求可能直接去 Memory，不经过 Directory
    // 取决于协议设计
    // ...
}
```

---

### 5. 地址到 Home Node 的映射策略

#### 策略 A: 扁平映射（单层 Directory）

```go
// 所有地址的 Home Node 在同一层（如都在 L3 层）
type FlatHomeNodeMapper struct {
    directoryNodes []int  // 所有 Directory 节点
    granularity    uint64
}

func (m *FlatHomeNodeMapper) GetHomeNode(addr uint64) int {
    index := (addr / m.granularity) % uint64(len(m.directoryNodes))
    return m.directoryNodes[index]
}

// 示例：
// Directory: [L3[0], L3[1]]
// addr 0x0000 → L3[0]
// addr 0x1000 → L3[1]
// addr 0x2000 → L3[0]
```

#### 策略 B: 层次映射（多层 Directory）

```go
// 地址的 Home Node 可能在不同层
type HierarchicalHomeNodeMapper struct {
    tree *CoherenceTree
}

func (m *HierarchicalHomeNodeMapper) GetHomeNode(addr uint64) int {
    // 查找一致性树，找到负责该地址的最低层 Directory

    // 从根节点开始遍历
    current := m.tree.Root

    for current != nil {
        // 检查是否有子节点负责这个地址
        hasChildResponsible := false
        for _, child := range current.Children {
            if child.AddressResponsibility.Contains(addr) {
                current = child
                hasChildResponsible = true
                break
            }
        }

        // 如果没有子节点负责，当前节点就是 Home Node
        if !hasChildResponsible {
            return current.NodeID
        }
    }

    return -1
}

// 示例：
// Tree:
//   Root: HA[0] (addr 0x0000-0xFFFF)
//     |-- L3[0] (addr 0x0000-0x7FFF)
//     \-- L3[1] (addr 0x8000-0xFFFF)
//
// addr 0x1000 → L3[0] (最低层负责该地址的 Directory)
// addr 0x9000 → L3[1]
```

#### 策略 C: 自适应映射（基于局部性）

```go
// 根据访问局部性动态调整 Home Node
type AdaptiveHomeNodeMapper struct {
    baseMapper    HomeNodeMapper
    accessCounter map[uint64]map[int]int  // addr -> nodeID -> count
}

func (m *AdaptiveHomeNodeMapper) GetHomeNode(addr uint64) int {
    // 1. 检查哪个节点访问这个地址最频繁
    maxCount := 0
    bestNode := -1
    for nodeID, count := range m.accessCounter[addr] {
        if count > maxCount {
            maxCount = count
            bestNode = nodeID
        }
    }

    // 2. 如果有明显的局部性，选择最近的 Directory
    if maxCount > THRESHOLD {
        return findNearestDirectory(bestNode)
    }

    // 3. 否则使用默认映射
    return m.baseMapper.GetHomeNode(addr)
}
```

---

### 6. 一致性树的典型模式

#### 模式 1: 单层 Directory（简单）

```
               HA (no Dir)
                    |
        +-----------+-----------+
        |                       |
    L3[0] (Dir)             L3[1] (Dir)
     |      |                |      |
  CPU[0] CPU[1]           CPU[2] CPU[3]
     |      |                |      |
   DRAM[0]                 DRAM[1]

一致性树：
  Root: L3[0], L3[1] (并列，无父节点)
  L3[0].Domain = [CPU[0], CPU[1]]
  L3[1].Domain = [CPU[2], CPU[3]]

地址映射：
  addr 0x0000-0x7FFF → L3[0] (Home Node)
  addr 0x8000-0xFFFF → L3[1] (Home Node)
```

#### 模式 2: 两层 Directory（分层）

```
            HA[0] (Dir) ← Root
                |
        +-------+-------+
        |               |
    L3[0] (Dir)     L3[1] (Dir)
     |      |        |      |
  CPU[0] CPU[1]   CPU[2] CPU[3]

一致性树：
  Root: HA[0]
    |-- L3[0]
    |     |-- Domain: [CPU[0], CPU[1]]
    |
    \-- L3[1]
          |-- Domain: [CPU[2], CPU[3]]

职责划分：
  - L3[0] 负责 CPU[0] 和 CPU[1] 之间的一致性
  - L3[1] 负责 CPU[2] 和 CPU[3] 之间的一致性
  - HA[0] 负责 L3[0] 和 L3[1] 之间的一致性

路由示例：
  - CPU[0] read addr X (属于 L3[0]) → L3[0] (本地 Directory)
  - CPU[0] read addr Y (属于 L3[1]) → L3[0] → HA[0] → L3[1]
```

#### 模式 3: 混合 Directory（复杂）

```
        HA[0] (Dir)      HA[1] (Dir)
            |                |
        +---+---+        +---+---+
        |       |        |       |
    L3[0]   L3[1]    L3[2]   L3[3]
    (Dir)   (no Dir) (Dir)   (no Dir)

一致性树：
  Root: HA[0], HA[1] (并列)
    HA[0]:
      |-- L3[0] (Dir)
      |     |-- Domain: [CPU[0], CPU[1]]
      |
      \-- L3[1] (no Dir, HA[0] 直接管理)
            |-- Domain: [CPU[2], CPU[3]]

    HA[1]:
      |-- L3[2] (Dir)
      |     |-- Domain: [CPU[4], CPU[5]]
      |
      \-- L3[3] (no Dir, HA[1] 直接管理)
            |-- Domain: [CPU[6], CPU[7]]

⚠️ 这种情况无法自动推断，需要用户显式指定！
```

---

### 7. 验证和调试

```go
// ValidateCoherenceTree 验证一致性树的正确性
func ValidateCoherenceTree(tree *CoherenceTree) error {
    // 检查 1: 每个节点只能属于一个 Domain
    allNodes := make(map[int]bool)
    for _, dirNode := range tree.DirectoryNodes {
        for _, nodeID := range dirNode.Domain {
            if allNodes[nodeID] {
                return fmt.Errorf("节点 %d 属于多个 Coherence Domain", nodeID)
            }
            allNodes[nodeID] = true
        }
    }

    // 检查 2: 地址空间是否完全覆盖且不重叠
    // ...

    // 检查 3: 树结构是否有环
    // ...

    return nil
}

// PrintCoherenceTree 打印一致性树（调试用）
func PrintCoherenceTree(tree *CoherenceTree) {
    fmt.Println("========== Coherence Tree ==========")
    printNode(tree.Root, 0)
}

func printNode(node *CoherenceNode, depth int) {
    indent := strings.Repeat("  ", depth)
    fmt.Printf("%s[Node %d] Role=%s, Domain=%v, AddrRange=%s\n",
        indent, node.NodeID, node.Role, node.Domain, node.AddressResponsibility)

    for _, child := range node.Children {
        printNode(child, depth+1)
    }
}
```

---

## 完整使用示例

### 示例：两层 Directory (L3 + HA)

```go
// Step 1: 创建拓扑（物理连接）
network := NewNetwork()

// 创建节点
cpu0 := NewCPUNode(0)
cpu1 := NewCPUNode(1)
l3_0 := NewL3CacheNode(4, hasDirectory: true)   // ⭐ L3 有 Directory
ha := NewMemoryControllerNode(8, hasDirectory: true)  // ⭐ HA 也有 Directory
dram := NewDRAMNode(9)

// 连接节点
network.Connect(cpu0, l3_0)
network.Connect(cpu1, l3_0)
network.Connect(l3_0, ha)
network.Connect(ha, dram)

// Step 2: 构建一致性树
config := CoherenceTreeConfig{
    AutoInfer: true,  // 先尝试自动推断
    AddressMapping: AddressMappingConfig{
        Granularity: 64,  // 64B cache line
    },
}

tree, err := BuildCoherenceTree(network.GetTopology(), config)
if err != nil {
    // 自动推断失败，用户手动指定
    tree = ManualBuildTree()
}

// Step 3: 生成路由器
routers := GenerateCoherenceRouters(tree, network)

// Step 4: 为每个节点设置路由器
for nodeID, router := range routers {
    nodes[nodeID].SetRouter(router)
}

// Step 5: 运行仿真
// CPU miss → 查询路由器 → 自动路由到 L3 Directory → HA Directory → Memory
```

---

## 总结

### ✅ 优势

1. **清晰的责任划分**：每个地址的一致性责任明确
2. **支持复杂拓扑**：多层 Directory、分区 Directory
3. **灵活的配置**：自动推断 + 用户显式指定
4. **易于调试**：可以打印一致性树查看责任划分

### 🎯 推荐方案

**混合方案：BFS 基础 + 用户覆盖**

1. **简单拓扑**（单层 Directory）：自动推断 ✅
2. **中等复杂**（两层 Directory）：自动推断 + 少量用户覆盖 ✅
3. **复杂拓扑**（多 L3 + 多 HA）：用户显式指定 ✅

### 📋 配置清单

#### 自动推断（无需配置）
- 单层 Directory（只在 L3 或只在 HA）
- 简单的层次结构（L3 → HA）

#### 需要用户指定
- 多根 Directory（多个 HA 各自独立）
- 混合 Directory（部分 L3 有 Dir，部分没有）
- 特殊的地址分区策略

---

## 开放问题

### Q1: 地址分区的粒度
L3 Slice 的地址分区应该基于什么？
- A. Cache line (64B)
- B. Page (4KB)
- C. 用户自定义

### Q2: 跨 Domain 的一致性
如果 CPU[0] (属于 L3[0]) 访问 CPU[2] (属于 L3[1]) 的数据，路由路径是什么？
- A. CPU[0] → L3[0] → HA → L3[1] → CPU[2]
- B. CPU[0] → L3[0] → L3[1] → CPU[2] (直接通信)

### Q3: Directory 的数据存储
如果 L3 有 Directory，数据是存在 L3 还是 Memory？
- A. L3 (包容性 Cache)
- B. Memory (非包容性)
- C. 可配置
