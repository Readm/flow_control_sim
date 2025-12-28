# Flow_Sim 一致性责任树 (Coherence Tree) 使用指南

## 概述

**一致性责任树 (Coherence Tree)** 是 flow_sim 提供的自动化路由和一致性管理机制。它通过分析系统拓扑，自动推断出：

- 每个地址的一致性由哪个 Directory 节点负责（Home Node）
- 节点之间的一致性层次关系
- 跨 Domain 的路由路径

## 核心概念

### 1. Coherence Node (一致性节点)

表示一个 Directory 节点，负责管理一组节点的一致性：

```go
type CoherenceNode struct {
    NodeID   int            // Directory 节点 ID
    Role     NodeRole       // 节点角色 (Cache, MemoryCtrl)
    Domain   []int          // 管理的节点 IDs (CPU, Cache)
    Parent   *CoherenceNode // 上层 Directory
    Children []*CoherenceNode // 下层 Directory
}
```

### 2. Coherence Tree (一致性树)

描述整个系统的一致性责任划分：

```go
type CoherenceTree struct {
    Root           *CoherenceNode            // 根节点（最高层 Directory）
    DirectoryNodes map[int]*CoherenceNode    // 所有 Directory 节点
    AddressMappingConfig AddressMappingConfig // 地址映射配置
}
```

### 3. CoherenceRouter (一致性路由器)

每个节点的路由决策器：

```go
type CoherenceRouter struct {
    myNodeID int
    tree     *CoherenceTree
}
```

---

## 快速开始

### 示例 1：自动推断（单层 Directory）

```go
package main

import (
    "fmt"
    "github.com/Readm/flow_sim/internal/components/coherence"
)

func main() {
    // Step 1: 构建拓扑描述
    topology := &coherence.Topology{
        Nodes: map[int]*coherence.NodeDescriptor{
            // CPU 节点
            0: {NodeID: 0, Capability: coherence.NodeCapability{
                Role: coherence.RoleCompute, CanInitiate: true}},
            1: {NodeID: 1, Capability: coherence.NodeCapability{
                Role: coherence.RoleCompute, CanInitiate: true}},

            // L2 Cache 节点（带 Directory）
            2: {NodeID: 2, Capability: coherence.NodeCapability{
                Role: coherence.RoleCache, CacheLevel: 2, HasDirectory: true}},

            // Memory Controller 节点
            3: {NodeID: 3, Capability: coherence.NodeCapability{
                Role: coherence.RoleMemoryCtrl}},

            // DRAM 节点
            4: {NodeID: 4, Capability: coherence.NodeCapability{
                Role: coherence.RoleMemory}},
        },
        Connections: map[int][]int{
            0: {2}, 1: {2},
            2: {0, 1, 3},
            3: {2, 4},
            4: {3},
        },
    }

    // Step 2: 配置地址映射
    config := coherence.AddressMappingConfig{
        Granularity: 64,  // 64B cache line 交错
        Strategy:    coherence.MappingInterleaved,
    }

    // Step 3: 自动构建一致性树
    tree, err := coherence.BuildCoherenceTree(topology, config, nil)
    if err != nil {
        panic(err)
    }

    // Step 4: 为每个节点创建路由器
    router0 := coherence.NewCoherenceRouter(0, tree)  // CPU 0 的路由器

    // Step 5: 使用路由器
    homeNode, _ := router0.GetHomeNode(0x1000)
    nextHop, _ := router0.RouteForCoherence(0x1000)

    fmt.Printf("地址 0x1000 的 Home Node: %d\n", homeNode)
    fmt.Printf("CPU 0 的下一跳: %d\n", nextHop)
}
```

**输出：**
```
地址 0x1000 的 Home Node: 2  (L2 Cache)
CPU 0 的下一跳: 2
```

---

### 示例 2：用户显式指定（复杂拓扑）

```go
package main

import (
    "github.com/Readm/flow_sim/internal/components/coherence"
)

func main() {
    // 配置
    config := coherence.AddressMappingConfig{
        Granularity: 128,  // 128B 交错
        Strategy:    coherence.MappingInterleaved,
    }

    // 使用 Builder 显式构建
    builder := coherence.NewCoherenceTreeBuilder(config)

    // 添加 L3 Slice（每个管理 2 个 CPU）
    builder.AddDirectory(10, coherence.RoleCache, coherence.CoherenceDomain{
        ManagedNodes: []int{0, 1},  // L3[0] 管理 CPU 0, 1
        AddressRange: &coherence.AddressRange{Start: 0x0000, End: 0x7FFF},
    })

    builder.AddDirectory(11, coherence.RoleCache, coherence.CoherenceDomain{
        ManagedNodes: []int{2, 3},  // L3[1] 管理 CPU 2, 3
        AddressRange: &coherence.AddressRange{Start: 0x8000, End: 0xFFFF},
    })

    // 添加 Home Agent（管理两个 L3）
    builder.AddDirectory(20, coherence.RoleMemoryCtrl, coherence.CoherenceDomain{
        ManagedNodes: []int{10, 11},
    })

    // 设置层次关系
    builder.SetParent(10, 20)  // L3[0] 的父节点是 HA
    builder.SetParent(11, 20)  // L3[1] 的父节点是 HA

    // 构建
    tree, _ := builder.Build()

    // 测试跨 Domain 路由
    router := coherence.NewCoherenceRouter(0, tree)  // CPU 0
    path, _ := tree.GetCoherencePath(0, 0x9000)      // 访问 L3[1] 的地址

    fmt.Printf("跨 Domain 路径: %v\n", path)
    // 输出: [0, 10, 20, 11]  (CPU 0 → L3[0] → HA → L3[1])
}
```

---

## ChampSim 集成示例

### 使用场景：四核 ChampSim + L2 Directory

```go
package main

import (
    "github.com/Readm/flow_sim/internal/champsim/flowsim"
    "github.com/Readm/flow_sim/internal/components/coherence"
)

func main() {
    // 节点 ID 定义
    cpuNodeIDs := []int{0, 1, 2, 3}
    l2NodeID := 4
    memCtrlNodeID := 8
    dramNodeIDs := []int{9, 10}

    // 配置
    config := coherence.AddressMappingConfig{
        Granularity: 64,  // 64B cache line
        Strategy:    coherence.MappingInterleaved,
    }

    // 自动构建一致性树
    tree, err := flowsim.BuildChampSimCoherenceTree(
        cpuNodeIDs,
        l2NodeID,
        memCtrlNodeID,
        dramNodeIDs,
        config,
    )
    if err != nil {
        panic(err)
    }

    // 为每个 CPU 创建路由器
    routers := make(map[int]*coherence.CoherenceRouter)
    for _, cpuID := range cpuNodeIDs {
        routers[cpuID] = coherence.NewCoherenceRouter(cpuID, tree)
    }

    // 在 CPU Node 中使用路由器
    cpuRouter := routers[0]
    nextHop, _ := cpuRouter.RouteForCoherence(0x1000)
    // nextHop == l2NodeID (4)
}
```

---

## 配置选项

### 1. 地址映射粒度

**规则：** 必须是 64 的倍数

```go
config := coherence.AddressMappingConfig{
    Granularity: 64,    // ✅ 有效 (cache line)
    // Granularity: 128,   // ✅ 有效
    // Granularity: 1024,  // ✅ 有效 (1KB)
    // Granularity: 4096,  // ✅ 有效 (page)
    // Granularity: 100,   // ❌ 无效（不是 64 的倍数）
    Strategy: coherence.MappingInterleaved,
}
```

### 2. Directory 位置选择

**选项 A：Directory 在 L2/L3**

```go
l2Descriptor := &coherence.NodeDescriptor{
    NodeID: 2,
    Capability: coherence.NodeCapability{
        Role:         coherence.RoleCache,
        CacheLevel:   2,
        HasDirectory: true,  // ✅ L2 有 Directory
    },
}
```

**选项 B：Directory 在 Home Agent (Memory Controller)**

```go
haDescriptor := &coherence.NodeDescriptor{
    NodeID: 8,
    Capability: coherence.NodeCapability{
        Role:         coherence.RoleMemoryCtrl,
        HasDirectory: true,  // ✅ HA 有 Directory
    },
}
```

**选项 C：两层 Directory（L3 + HA）**

```go
l3Descriptor := &coherence.NodeDescriptor{
    NodeID: 3,
    Capability: coherence.NodeCapability{
        Role:         coherence.RoleCache,
        CacheLevel:   3,
        HasDirectory: true,  // ✅ L3 有 Directory
    },
}

haDescriptor := &coherence.NodeDescriptor{
    NodeID: 8,
    Capability: coherence.NodeCapability{
        Role:         coherence.RoleMemoryCtrl,
        HasDirectory: true,  // ✅ HA 也有 Directory
    },
}
// 自动推断会识别为两层结构：L3 → HA
```

---

## API 参考

### CoherenceTree

#### `GetHomeNode(addr uint64) (int, error)`

返回负责该地址的 Directory 节点 ID（Home Node）。

**示例：**
```go
homeNodeID, err := tree.GetHomeNode(0x1000)
```

#### `GetCoherencePath(requesterID int, addr uint64) ([]int, error)`

返回从 requester 到 Home Node 的一致性路径。

**示例：**
```go
path, err := tree.GetCoherencePath(0, 0x9000)
// path = [0, 10, 20, 11]  (CPU 0 → L3[0] → HA → L3[1])
```

### CoherenceRouter

#### `RouteForCoherence(addr uint64) (int, error)`

返回一致性请求的下一跳节点 ID。

**示例：**
```go
router := coherence.NewCoherenceRouter(0, tree)
nextHop, err := router.RouteForCoherence(0x1000)
```

#### `IsHomeNode(addr uint64) bool`

检查当前节点是否是某个地址的 Home Node。

**示例：**
```go
if router.IsHomeNode(0x1000) {
    // 本地处理一致性请求
}
```

#### `GetHomeNode(addr uint64) (int, error)`

获取某个地址的 Home Node（与 tree.GetHomeNode 相同）。

---

## 最佳实践

### 1. 自动推断 vs 显式指定

**推荐策略：**
- 简单拓扑（单层或两层 Directory）：使用**自动推断** ✅
- 复杂拓扑（多个 L3 Slice，混合 Directory）：使用**显式指定** ✅

**示例：**
```go
// 尝试自动推断
tree, err := coherence.BuildCoherenceTree(topology, config, nil)
if err != nil {
    // 自动推断失败，使用显式指定
    tree = buildExplicitTree()
}
```

### 2. 验证一致性树

```go
tree, _ := builder.Build()

// 验证树的正确性
if err := tree.Validate(); err != nil {
    panic(fmt.Sprintf("一致性树验证失败: %v", err))
}
```

### 3. 调试路由

```go
router := coherence.NewCoherenceRouter(0, tree)
router.PrintRoutingInfo()  // 打印路由信息
```

**输出示例：**
```
========== Routing Info for Node 0 ==========
My Directory: 10
Directory Nodes: [10 11 20]
Root Node: 20
==============================================
```

---

## 故障排查

### 问题 1：自动推断失败

**错误信息：**
```
⚠️ 自动推断一致性树失败: 没有找到任何 Directory 节点
请使用 CoherenceTreeBuilder 显式指定一致性树
```

**解决方案：**
- 检查是否有节点的 `HasDirectory = true`
- 使用 `CoherenceTreeBuilder` 显式指定

### 问题 2：地址映射配置错误

**错误信息：**
```
Granularity 必须是 64 的倍数，当前值: 100
```

**解决方案：**
- 使用 64 的倍数：64, 128, 256, 512, 1024, 4096

### 问题 3：无法找到路径

**错误信息：**
```
无法找到从 0 到 Home Node 10 的路径
```

**解决方案：**
- 检查拓扑连接是否正确
- 检查 Domain 是否正确设置
- 验证节点 0 是否属于某个 Directory 的 Domain

---

## 完整示例：四核 + MESI + 双通道 DRAM

```go
package main

import (
    "fmt"
    "github.com/Readm/flow_sim/internal/champsim/flowsim"
    "github.com/Readm/flow_sim/internal/components/coherence"
)

func main() {
    // 节点 ID
    cpuNodeIDs := []int{0, 1, 2, 3}
    l2NodeID := 4
    memCtrlNodeID := 8
    dramNodeIDs := []int{9, 10}

    // 配置：64B cache line 交错
    config := coherence.AddressMappingConfig{
        Granularity: 64,
        Strategy:    coherence.MappingInterleaved,
    }

    // 自动构建一致性树
    tree, err := flowsim.BuildChampSimCoherenceTree(
        cpuNodeIDs, l2NodeID, memCtrlNodeID, dramNodeIDs, config,
    )
    if err != nil {
        panic(err)
    }

    // 验证树结构
    fmt.Printf("Directory 节点数: %d\n", len(tree.DirectoryNodes))
    fmt.Printf("根节点: %d\n", tree.Root.NodeID)
    fmt.Printf("L2 Domain: %v\n", tree.DirectoryNodes[l2NodeID].Domain)

    // 创建路由器
    routers := make(map[int]*coherence.CoherenceRouter)
    for _, cpuID := range cpuNodeIDs {
        routers[cpuID] = coherence.NewCoherenceRouter(cpuID, tree)
    }

    // 测试路由
    for _, cpuID := range cpuNodeIDs {
        router := routers[cpuID]
        nextHop, _ := router.RouteForCoherence(0x1000)
        fmt.Printf("CPU %d → %d (L2)\n", cpuID, nextHop)
    }
}
```

**输出：**
```
Directory 节点数: 1
根节点: 4
L2 Domain: [0 1 2 3]
CPU 0 → 4 (L2)
CPU 1 → 4 (L2)
CPU 2 → 4 (L2)
CPU 3 → 4 (L2)
```

---

## 总结

- ✅ **自动推断** - 适用于简单拓扑，零配置
- ✅ **显式指定** - 适用于复杂拓扑，完全控制
- ✅ **灵活配置** - 地址映射粒度、Directory 位置
- ✅ **层次路由** - 必须经过公共父节点
- ✅ **ChampSim 集成** - 提供便捷的集成函数

更多信息，请参考测试用例：
- `coherence_test.go` - 单元测试
- `coherence_integration_test.go` - ChampSim 集成测试
