# Handler 和 State 架构详解

## 目录

1. [架构概览](#架构概览)
2. [Handler 接口体系](#handler-接口体系)
3. [State 结构体系](#state-结构体系)
4. [数据流路径](#数据流路径)
5. [API 使用示例](#api-使用示例)
6. [Handler 实现详解](#handler-实现详解)

---

## 架构概览

### 设计原则

**Schema-First**: OpenAPI Schema 是单一数据源
- OpenAPI YAML → Protocol 类型（自动生成）
- Handler 统计导出 → NodeState
- NodeState → Protocol（Adapter 转换）

**统一接口**: 所有 Handler 实现相同接口
- `NodeHandler.Process()` - 必需，处理每周期逻辑
- `StatsExporter.ExportStats()` - 可选，导出统计

**往返一致性**: 状态数据完整保留
- `ExportState()` → NodeState → Protocol
- `StateToFlowSimNetwork()` → Protocol → （未来）恢复到 NodeState

---

## Handler 接口体系

### 1. 核心接口定义

位置：`internal/core/node/node.go`

```go
// NodeHandler - 必需接口
type NodeHandler interface {
    // 每周期处理逻辑
    Process(cycle uint64, inputs [][]queue.PacketRef) error
}

// StatsExporter - 可选接口（Phase 4 引入）
type StatsExporter interface {
    // 导出运行时统计（字段对齐 OpenAPI Schema）
    ExportStats() map[string]interface{}
}
```

### 2. Handler 实现类型

| Handler 类型 | 文件位置 | 对应节点类型 | 实现接口 |
|-------------|----------|-------------|---------|
| **CPUNodeHandler** | `internal/nodes/cpu/champsim/flowsim/cpu_node.go` | `cpu` | NodeHandler + StatsExporter |
| **DRAMNodeHandler** | `internal/nodes/cpu/champsim/flowsim/dram_node.go` | `memory_controller` | NodeHandler + StatsExporter |
| **L2CacheNodeHandler** | `internal/nodes/cpu/champsim/flowsim/l2_cache_node.go` | `generic` | NodeHandler + StatsExporter |
| **MemoryControllerHandler** | `internal/nodes/cpu/champsim/flowsim/memory_controller_node.go` | `memory_controller` | NodeHandler + StatsExporter |

### 3. BaseNode 与 Handler 的关系

```go
type BaseNode struct {
    id      int
    handler NodeHandler  // 多态行为
    // ... 其他字段
}

// BaseNode.Tick() 调用 handler.Process()
func (n *BaseNode) Tick(cycle uint64, duration time.Duration) error {
    // 1. 收集输入队列数据
    // 2. 调用 handler.Process(cycle, inputs)
    // 3. 更新输出队列
}
```

**关键点**：
- BaseNode 提供通用逻辑（队列管理、统计、监控）
- Handler 提供专用逻辑（CPU 流水线、DRAM 调度等）
- 通过组合模式实现多态

---

## Handler 实现详解

### CPUNodeHandler

**职责**: ChampSim O3 CPU + L1D Cache

**内部组件**:
```go
type CPUNodeHandler struct {
    cpu           *cpu.O3CPU                   // 乱序 CPU
    l1dCache      *cache.SetAssociativeCache   // L1D Cache
    memoryAdapter *FlowSimMemoryAdapter        // Cache ↔ 网络适配器
    nodeID, dramID int
    outputQueue   *queue.OutputQueue
}
```

**Process() 逻辑**:
1. 处理来自 DRAM 的响应 → 填充 Cache
2. 执行 CPU Tick（指令执行、访存）
3. 发送 Cache miss 请求到 DRAM

**ExportStats() 导出**:
```go
{
    "ipc": 1.25,                    // Instructions Per Cycle
    "total_instructions": 1000000,
    "total_cycles": 800000,
    "branch_mispredictions": 1234,
    "fetch_stalls": 500,
    "decode_stalls": 300,
    // ... 流水线统计
    "l1d_cache_stats": {...}        // L1D 统计
}
```

### DRAMNodeHandler

**职责**: DRAM Channel 管理

**内部组件**:
```go
type DRAMNodeHandler struct {
    dramChannel      *dram.DRAMChannel  // DRAM 通道
    nodeID, cpuID    int
    outputQueue      *queue.OutputQueue
    pendingResponses []MemoryResponsePayload
}
```

**Process() 逻辑**:
1. 处理来自 CPU/L2 的内存请求 → DRAM Channel
2. 执行 DRAM Tick（Bank 调度、Row Buffer 管理）
3. 发送完成的响应回 CPU/L2

**ExportStats() 导出**:
```go
{
    "read_requests": 50000,
    "write_requests": 10000,
    "row_buffer_hits": 30000,
    "row_buffer_misses": 20000,
    "rq_row_buffer_hits": 25000,
    "wq_row_buffer_hits": 5000,
    // ... 详细统计
}
```

### L2CacheNodeHandler

**职责**: 共享 L2 Cache（多 CPU 访问）

**Process() 逻辑**:
1. 处理来自多个 CPU 的请求
2. Cache 访问（命中/未命中）
3. 发送响应到 CPU 或请求到 Memory Controller
4. 处理一致性消息（Coherence）

**ExportStats() 导出**:
```go
{
    "accesses": 100000,
    "hits": 80000,
    "misses": 20000,
    "invalidates_sent": 500,
    "writebacks": 1000,
    "coherence_stats": {...}
}
```

### MemoryControllerHandler

**职责**: 地址映射 + 多 DRAM Channel 管理

**Process() 逻辑**:
1. 处理来自上游（L2/L3）的请求
2. 地址映射（选择 DRAM Channel）
3. 路由到对应 DRAM Channel
4. 聚合响应

**ExportStats() 导出**:
```go
{
    "total_requests": 100000,
    "responses": 95000,
    "requests_per_channel": [25000, 24000, 26000, 25000]
}
```

---

## State 结构体系

### 1. NodeState 结构

位置：`internal/core/state/state.go`

```go
type NodeState struct {
    // === 基础信息 ===
    ID           int
    Type         string  // Handler 类型名（如 "*flowsim.CPUNodeHandler"）
    CurrentCycle int

    // === 队列状态（运行时） ===
    Inputs  []QueueState
    Outputs []QueueState

    // === 统计数据（运行时动态产生）===
    Stats map[string]interface{}
    // Stats["ipc"] = 1.25
    // Stats["total_instructions"] = 1000000
    // Stats["cache"] = []CacheState{...}

    // === 配置信息（静态）===
    Features          map[string]map[string]interface{}  // cache/directory 配置
    CoherenceDomainID *int

    // === Phase 5: 节点类型配置（对应 OpenAPI Schema）===
    NodeType     *string                // "cpu", "memory_controller", "generic", "router"
    CPUConfig    map[string]interface{} // CPU 专用配置和统计
    MemoryConfig map[string]interface{} // 内存专用配置和统计

    // === 可视化信息 ===
    DisplayData map[string]interface{}  // position, data, style

    // === 废弃字段（兼容性保留）===
    Caches      []CacheState
    Directories []DirectoryState
    CustomData  map[string]interface{}
}
```

### 2. NetworkState 结构

```go
type NetworkState struct {
    CurrentCycle int
    Nodes        []NodeState
    Links        []LinkState
    DisplayData  map[string]interface{}  // zoom, pan
}
```

### 3. LinkState 结构

```go
type LinkState struct {
    SourceID, TargetID       int
    SourcePortID, TargetPortID int
    CurrentCycle             int
    Latency, Bandwidth       int
    Occupancy                []int     // 时间槽占用情况
    PacketTypes              []string
    EdgeID                   int
    DisplayData              map[string]interface{}
}
```

### 4. QueueState 结构

```go
type QueueState struct {
    Type        string    // "Input" or "Output"
    Length      int       // 当前队列长度
    Capacity    int       // 队列容量
    Bandwidth   int
    Bitmap      string    // "1010..." 槽位占用
    PacketTypes []string
    Packets     []PacketState  // 包详情（可选）
}
```

---

## 数据流路径

### 完整数据流（从 Handler 到 API）

```
┌─────────────────────────────────────────────────────────────┐
│ 1. 仿真运行时                                                │
│    Handler.Process() → 内部状态更新                          │
│    (CPU 执行指令、DRAM 处理请求等)                            │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. 状态导出 (Phase 4 + Phase 5)                             │
│    if handler implements StatsExporter:                      │
│        stats := handler.ExportStats()                        │
│        → NodeState.Stats["ipc"] = stats["ipc"]              │
│        → NodeState.Stats["total_instructions"] = ...        │
│                                                              │
│    BaseNode.ExportState():                                   │
│        → 推断 NodeType (从 handler 类型)                     │
│        → extractCPUConfig(stats) → NodeState.CPUConfig      │
│        → extractMemoryConfig(stats) → NodeState.MemoryConfig│
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ 3. State → Protocol 转换 (Phase 5)                          │
│    adapter.StateToFlowSimNetwork(networkState):              │
│        → node.NodeType = protocol.NodeNodeType(*nodeState.NodeType)  │
│        → node.CpuConfig = mapToCPUConfig(nodeState.CPUConfig)       │
│        → node.MemoryConfig = mapToMemoryConfig(nodeState.MemoryConfig) │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ 4. OpenAPI Protocol (types.gen.go)                          │
│    type Node struct {                                        │
│        NodeType     *NodeNodeType   // "cpu", "memory_controller" │
│        CpuConfig    *CPUConfig      // 对齐 Schema          │
│        MemoryConfig *MemoryConfig   // 对齐 Schema          │
│        // ... 其他字段                                       │
│    }                                                         │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ 5. HTTP API 响应                                             │
│    GET /current_state                                        │
│    → 返回 FlowSimNetwork (JSON)                             │
│    → 前端可视化                                              │
└─────────────────────────────────────────────────────────────┘
```

### 反向数据流（从 API 到 Handler）- Phase 6 将实现

```
┌─────────────────────────────────────────────────────────────┐
│ 1. HTTP API 请求                                             │
│    POST /build_network                                       │
│    {                                                         │
│        "nodes": [{                                           │
│            "node_type": "cpu",                               │
│            "cpu_config": {                                   │
│                "type": "champsim_o3",                        │
│                "trace_file": "...",                          │
│                "rob_size": 256                               │
│            }                                                 │
│        }]                                                    │
│    }                                                         │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. Protocol → Builder (Phase 6 TODO)                        │
│    builder.BuildFromFlowSimNetwork(protocol):                │
│        for each node:                                        │
│            switch node.NodeType:                             │
│                case "cpu":                                   │
│                    handler = createCPUHandler(node.CpuConfig)│
│                case "memory_controller":                     │
│                    handler = createDRAMHandler(node.MemoryConfig) │
│                default:                                      │
│                    handler = createGenericHandler()         │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ 3. 创建 BaseNode + Handler                                   │
│    baseNode := NewBaseNode(id, handler)                      │
│    → handler.Process() 开始仿真                              │
└─────────────────────────────────────────────────────────────┘
```

---

## API 使用示例

### 示例 1: 获取当前状态（包含节点类型和统计）

**请求**:
```bash
curl http://localhost:8080/current_state
```

**响应** (JSON):
```json
{
  "version": "1.0.0",
  "cycle": 100000,
  "nodes": [
    {
      "node_id": 0,
      "node_name": "CPU_0",
      "node_type": "cpu",
      "cpu_config": {
        "ipc": 1.25,
        "total_instructions": 100000,
        "total_cycles": 80000,
        "branch_mispredictions": 1234
      },
      "in_ports": [...],
      "out_ports": [...]
    },
    {
      "node_id": 1,
      "node_name": "DRAM_0",
      "node_type": "memory_controller",
      "memory_config": {
        "read_requests": 50000,
        "write_requests": 10000,
        "row_buffer_hits": 30000,
        "row_buffer_misses": 20000
      },
      "in_ports": [...],
      "out_ports": [...]
    }
  ],
  "edges": [...]
}
```

### 示例 2: 创建网络（Phase 6 将支持）

**请求**:
```bash
curl -X POST http://localhost:8080/build_network \
  -H "Content-Type: application/json" \
  -d @config.json
```

**config.json**:
```json
{
  "nodes": [
    {
      "node_id": 0,
      "node_type": "cpu",
      "cpu_config": {
        "type": "champsim_o3",
        "trace_file": "/path/to/trace.champsimtrace",
        "rob_size": 256,
        "lq_size": 128,
        "sq_size": 72
      }
    },
    {
      "node_id": 1,
      "node_type": "memory_controller",
      "memory_config": {
        "type": "ddr4",
        "channels": 4,
        "ranks": 2,
        "banks": 8
      }
    }
  ],
  "edges": [
    {
      "src_node_id": 0,
      "dst_node_id": 1,
      "latency": 100
    }
  ]
}
```

---

## 关键设计模式

### 1. 策略模式 (Strategy Pattern)

```go
// BaseNode 是 Context
type BaseNode struct {
    handler NodeHandler  // Strategy
}

// 不同的 Handler 是不同的策略
// - CPUNodeHandler: CPU 执行策略
// - DRAMNodeHandler: DRAM 调度策略
// - L2CacheNodeHandler: Cache 管理策略
```

### 2. 适配器模式 (Adapter Pattern)

```go
// FlowSimMemoryAdapter 将 Cache 的内存接口适配到网络包
type FlowSimMemoryAdapter struct {
    pendingRequests []MemoryRequest
}

// Cache miss → 记录到 pendingRequests
// CPUNodeHandler → 从 pendingRequests 创建网络包
```

### 3. 模板方法模式 (Template Method)

```go
// BaseNode.Tick() 是模板方法
func (n *BaseNode) Tick(cycle uint64, duration time.Duration) error {
    // 1. 准备输入（固定）
    inputs := n.prepareInputs()

    // 2. 调用子类实现（可变）
    err := n.handler.Process(cycle, inputs)

    // 3. 处理输出（固定）
    n.processOutputs()

    return err
}
```

---

## 未来扩展

### Phase 6: Builder 扩展

**目标**: 根据 `node_type` 自动创建对应的 Handler

```go
func createHandlerByType(nodeType protocol.NodeNodeType, config *protocol.Node) NodeHandler {
    switch nodeType {
    case protocol.Cpu:
        return createCPUHandler(config.CpuConfig)
    case protocol.MemoryController:
        return createDRAMHandler(config.MemoryConfig)
    case protocol.Generic:
        return createGenericHandler(config)
    default:
        return createGenericHandler(config)
    }
}
```

### 新增 Handler 类型的步骤

1. **定义 OpenAPI Schema** (`web/openapi.yaml`)
   - 添加新的 `node_type` 枚举值
   - 定义对应的 Config schema（如 `RouterConfig`）

2. **生成 Protocol 类型**
   ```bash
   ./scripts/generate_go_types.sh
   ```

3. **实现 Handler**
   ```go
   type RouterNodeHandler struct {
       // 内部组件
   }

   func (h *RouterNodeHandler) Process(cycle uint64, inputs [][]queue.PacketRef) error {
       // 路由逻辑
   }

   func (h *RouterNodeHandler) ExportStats() map[string]interface{} {
       return map[string]interface{}{
           "packets_routed": h.stats.PacketsRouted,
           "routing_conflicts": h.stats.Conflicts,
       }
   }
   ```

4. **扩展 State 提取**
   - 更新 `extractXXXConfig()` 函数
   - 更新 `mapToXXXConfig()` 函数

5. **扩展 Builder** (Phase 6)
   - 在 `createHandlerByType()` 添加新分支

---

## 总结

**当前架构优势**:
1. ✅ **统一接口**: 所有 Handler 遵循相同契约
2. ✅ **自动化导出**: 实现 StatsExporter 即自动填充到 API
3. ✅ **Schema 对齐**: 字段命名完全对应 OpenAPI
4. ✅ **往返一致**: State ↔ Protocol 数据不丢失
5. ✅ **可扩展**: 新增节点类型只需实现接口

**当前限制**:
- ⚠️ Builder 尚未支持根据 Protocol 创建 Handler（Phase 6）
- ⚠️ 配置恢复逻辑未完整实现（依赖 Builder）
- ⚠️ 通用 Generic Handler 未实现（需要动态配置能力）

**下一步**（Phase 6）:
- 实现 Builder 根据 `node_type` 创建 Handler
- 实现配置 → Handler 参数的映射
- 支持完整的 JSON 驱动配置
