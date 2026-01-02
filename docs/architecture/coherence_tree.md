# 一致性责任树 (Coherence Tree)

## 1. 概述 (Overview)

**一致性责任树 (Coherence Tree)** 是 FlowSim 中用于解决复杂物理拓扑下一致性维护问题的核心机制。它通过建立一个逻辑上的责任层级结构，明确了每个地址的一致性由谁负责（Home Node），以及节点间如何进行一致性通信。

### 核心问题：物理拓扑 ≠ 一致性责任

在一个复杂的众核系统中，物理连接（如 Mesh、Ring）并不直接等同于一致性维护的责任关系。
例如，考虑以下拓扑：
```text
          L3[0] (Dir)          L3[1] (Dir)
              |                    |
        +-----+-----+        +-----+-----+
        |           |        |           |
     CPU[0]      CPU[1]   CPU[2]      CPU[3]
```
- 地址 X 的一致性由谁负责？
- 如果 CPU[0] 和 CPU[2] 共享数据，谁来协调？
- 仅靠简单的 BFS 物理路由无法回答这些一致性逻辑问题。

### 解决方案

Coherence Tree 定义了一致性维护的**层次责任**：
```text
Root: System-level Directory (如 Home Agent)
  |
  +-- Branch: L3[0] Directory
  |     |
  |     +-- Leaf: CPU[0], CPU[1] (由 L3[0] 负责)
  |
  +-- Branch: L3[1] Directory
        |
        +-- Leaf: CPU[2], CPU[3] (由 L3[1] 负责)
```

---

## 2. 核心概念 (Core Concepts)

### 2.1 Coherence Node (一致性节点)
表示一致性树中的一个节点，通常对应系统中的 Directory 或 Agent。

```go
type CoherenceNode struct {
    NodeID   int            // 物理节点 ID
    Role     NodeRole       // 角色 (RoleCache, RoleMemoryCtrl 等)
    Domain   []int          // 该节点管理的下级节点 ID 集合 (Coherence Domain)
    Parent   *CoherenceNode // 上级 Directory
    Children []*CoherenceNode // 下级 Directory
}
```

### 2.2 Coherence Tree (一致性树)
描述整个系统的一致性责任划分。

```go
type CoherenceTree struct {
    Root           *CoherenceNode            // 根节点
    DirectoryNodes map[int]*CoherenceNode    // 所有参与一致性维护的节点索引
    AddressMapper  HomeNodeMapper            // 地址到 Home Node 的映射器
}
```

### 2.3 关键术语
- **Home Node**: 负责维护特定地址一致性的节点（通常是 Directory）。
- **Coherence Domain**: 一个 Directory 直接管理的一组节点。
- **Leaf Node**: 树的叶子（通常是 CPU 或 L1 Cache），不维护其他节点的一致性。

---

## 3. 构建策略 (Building Strategies)

FlowSim 支持三种构建策略，以适应不同复杂度的系统。

### 策略 1: 自动推断 (Auto-Inference)
**适用场景**: 简单层级结构（如 L2 -> L3 -> Memory），单层 Directory。

自动推断基于 BFS 和启发式规则：
1.  **识别 Directory**: 找出所有 `HasDirectory=true` 的节点。
2.  **推断层级**:
    *   Memory Controller (HA) 通常是 L3 Cache 的父节点。
    *   High-Level Cache (L3) 是 Low-Level Cache (L2) 的父节点。
    *   距离 Memory 更近的通常是父节点。

### 策略 2: 用户显式指定 (Explicit)
**适用场景**: 复杂拓扑，混合 Directory，多根结构。

用户通过 Builder API 明确定义每个 Directory 及其管理的 Domain。

### 策略 3: 混合方案 (Hybrid) - *推荐*
先尝试自动推断，对于推断不准确的部分使用用户配置进行覆盖。

### 示例代码

#### 自动推断示例
```go
// 1. 定义拓扑 (略)
topology := ... 

// 2. 配置
config := coherence.AddressMappingConfig{
    Granularity: 64,  // 64B cache line
    Strategy:    coherence.MappingInterleaved,
}

// 3. 构建
tree, err := coherence.BuildCoherenceTree(topology, config, nil)
```

#### 显式指定示例
```go
builder := coherence.NewCoherenceTreeBuilder(config)

// L3[0] 管理 CPU 0, 1
builder.AddDirectory(10, coherence.RoleCache, coherence.CoherenceDomain{
    ManagedNodes: []int{0, 1},
    AddressRange: &coherence.AddressRange{Start: 0x0000, End: 0x7FFF},
})

// HA 管理 L3[0]
builder.AddDirectory(20, coherence.RoleMemoryCtrl, coherence.CoherenceDomain{
    ManagedNodes: []int{10},
})
builder.SetParent(10, 20)

tree, _ := builder.Build()
```

---

## 4. 路由机制 (Routing)

**CoherenceRouter** 利用一致性树进行路由决策，而不是基于物理跳数。

```go
// RouteForCoherence 返回一致性请求的下一跳
func (r *CoherenceRouter) RouteForCoherence(addr uint64) int {
    // 1. 给定地址，查找负责该地址的 Home Node
    homeNodeID := r.tree.GetHomeNode(addr)

    // 2. 如果我就在 Home Node，本地处理
    if homeNodeID == r.myNodeID { return LOCAL_PROCESSING }

    // 3. 计算在一致性树上的路径 (Requester -> ... -> Home)
    // 注意：路径是逻辑上的，物理传输可能经过其他节点
    path := r.tree.GetCoherencePath(r.myNodeID, addr)
    
    return path[1] // 返回下一跳
}
```

### 典型路由路径
- **本地 Domain**: CPU[0] (L3[0]域) 访问地址 A (L3[0]负责) -> **CPU[0] -> L3[0]**
- **跨 Domain**: CPU[0] (L3[0]域) 访问地址 B (L3[1]负责) -> **CPU[0] -> L3[0] -> HA -> L3[1]**
(请求必须向上汇聚到公共祖先，再向下转发)

---

## 5. 地址映射策略 (Address Mapping)

决定了特定的物理地址由哪个 Directory (Home Node) 负责。

### 5.1 扁平映射 (Flat)
所有 Home Node 在同一层（如多个 L3 Slice）。
- 依据地址位选（Interleaved）将地址均匀分布到各 L3 Slice。
- 公式: `HomeID = DirectoryNodes[(Addr / Granularity) % Count]`

### 5.2 层次映射 (Hierarchical)
地址范围被划分给不同层级的 Directory。
- Root 目录负责全地址空间。
- 子目录负责特定子范围。
- 路由时查找负责该地址的**最底层** Directory。

### 配置建议
- **Granularity**: 必须是 Cache Line Size (通常 64B) 的倍数。

---

## 6. ChampSim 集成

FlowSim 提供了专门用于集成 ChampSim 模型的辅助函数：

```go
tree, err := flowsim.BuildChampSimCoherenceTree(
    cpuNodeIDs,
    l2NodeID,      // 假设 L2 是共享 Directory
    memCtrlNodeID, // 内存控制器
    dramNodeIDs,
    config,
)
```
此函数会自动处理 ChampSim 常见的层级结构。

---

## 7. 最佳实践与故障排查

### 最佳实践
1.  **优先自动推断**: 对于标准层级结构，自动推断最简单且不易出错。
2.  **验证树结构**: 构建后调用 `tree.Validate()` 确保没有环路和孤立节点。
3.  **调试**: 使用 `router.PrintRoutingInfo()` 打印每个节点的路由视图。

### 常见问题
- **自动推断失败 (No Directory found)**: 检查物理节点的 `HasDirectory` 属性是否设置为 `true`。
- **无法找到路径**: 检查拓扑连接是否完整，确保叶子节点包含在某个 Directory 的 Domain 中。
- **地址映射错误**: 确保 `Granularity` 设置正确（如 64）。

---

## 8. 附录：典型架构模式

### 模式 A: 单层 Directory (Shared L3)
所有 CPU 共享一组 L3 Slice，无上层 Directory。每个 L3 Slice 负责一部分地址。
```text
Root: [L3[0], L3[1]] (并列)
L3[0] Domain: [CPU 0-1] (Addr A)
L3[1] Domain: [CPU 2-3] (Addr B)
```

### 模式 B: 分层 Directory (Private L2 + Shared L3)
```text
Root: L3 (Shared)
  |-- L2[0] (Private Dir) -> CPU 0
  |-- L2[1] (Private Dir) -> CPU 1
```
L2 负责过滤本地一致性流量，L3 负责全局。
