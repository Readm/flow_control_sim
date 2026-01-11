# ChampSim 目录重构设计文档

## 文档版本

- **版本**: 1.0
- **创建日期**: 2026-01-09
- **作者**: Claude Code
- **状态**: 设计阶段

## 目录

- [背景与动机](#背景与动机)
- [架构约束](#架构约束)
- [设计原则](#设计原则)
- [目标架构](#目标架构)
- [实施计划](#实施计划)
- [性能基准](#性能基准)
- [验收标准](#验收标准)

---

## 背景与动机

### 当前问题

`internal/champsim/` 目录结构不合理：

1. **职责混淆**：只有 CPU 相关代码真正属于 ChampSim，Cache/DRAM 是通用组件
2. **复用困难**：Cache、DRAM 被绑定在 champsim 命名空间下，难以被其他模型复用
3. **架构不清晰**：ChampSim 既是 CPU 模型，又包含了存储子系统

### 重构目标

1. **职责分离**：CPU 模型 vs 通用存储组件
2. **支持多种 Node 类型**：Generic Node（JSON 配置）vs Specialized Node（Go 代码）
3. **Schema-First**：所有配置字段必须先在 OpenAPI 定义
4. **保持性能**：重构不应导致性能下降

---

## 架构约束

### 约束 1：Schema-First 设计

**描述**：OpenAPI 是唯一的真相来源

**具体要求**：
- 所有配置字段必须先在 `web/openapi.yaml` 定义
- Protocol 结构体由 OpenAPI 自动生成（`types.gen.go`），不可手动修改
- 修改流程：`OpenAPI → 生成 Protocol → 更新 Builder/State/Adapter`

**验证方法**：
```bash
# 检查 Protocol 是否为自动生成
grep -r "DO NOT EDIT" internal/core/visualization/protocol/types.gen.go

# 运行生成脚本
cd web && npm run generate:types
```

### 约束 2：数据完整往返

**描述**：所有字段必须在往返过程中完整保留（零丢包原则）

**数据流转路径**：
```
OpenAPI Schema → Protocol → Builder → Core → ExportState → State → Adapter → Protocol
```

**验证方法**：
- 运行往返一致性测试：`go test -run TestFlowSimNetworkRoundTrip`
- 新增字段必须通过完整的往返测试

### 约束 3：Node 是第一公民

**描述**：不引入额外的 Component 抽象层

**具体要求**：
- 所有实体都是 Node（CPU、Cache、Router、Memory Controller）
- Node 内部实现不约束（可以直接函数调用、事件等）
- Node 之间通信永远使用 Packet

### 约束 4：性能保持

**描述**：重构不应导致性能显著下降

**基准测试**：
```bash
# 运行性能基准测试
go test -bench=. -benchmem ./internal/benchmarks/...

# 对比重构前后的性能
# 允许误差范围：±5%
```

**关键指标**：
- CPU Tick 延迟
- Cache Access 延迟
- Packet 处理吞吐量
- 内存分配次数

---

## 设计原则

### 原则 1：按职责组织，而非按来源

**当前**：按来源组织（ChampSim、Framework）
```
internal/champsim/
  ├── cpu/        # 真正属于 ChampSim
  ├── cache/      # 通用组件
  └── dram/       # 通用组件
```

**目标**：按职责组织
```
internal/nodes/cpu/champsim/    # CPU 模型（来源：ChampSim）
internal/capabilities/cache/     # 通用能力
internal/capabilities/memory/    # 通用能力
```

### 原则 2：Generic vs Specialized

**Generic Node**（通过 JSON 配置）：
- 纯粹的 Cache Node
- Directory Node
- Router Node
- 简单的转发节点

**Specialized Node**（通过 Go 代码实现）：
- CPU Node（复杂的流水线逻辑）
- Memory Controller（复杂的调度逻辑）
- 其他有状态、算法密集的节点

### 原则 3：配置优先，代码作为例外

**目标**：90% 的系统用 JSON 配置，10% 用 Go 代码

**实现**：
- Generic Node 完全由 JSON 配置驱动
- Specialized Node 的配置参数也在 JSON 中定义
- 代码仅用于实现复杂的运行时逻辑

---

## 目标架构

### 目录结构

```
internal/
├── nodes/                        # Node 实现（按类型组织）
│   ├── generic_handler.go        # Generic Node Handler
│   │   - 实现 cache/directory/router 能力
│   │   - 完全由 JSON 配置驱动
│   │
│   ├── cpu/                      # CPU Node 实现
│   │   └── champsim/             # ChampSim O3 CPU
│   │       ├── cpu_handler.go    # CPUHandler (implements NodeHandler)
│   │       ├── o3_cpu.go         # O3 CPU 核心逻辑
│   │       ├── pipeline.go       # 流水线阶段
│   │       ├── lsq.go            # Load-Store Queue
│   │       ├── rob.go            # Reorder Buffer
│   │       ├── dib.go            # Decoded Instruction Buffer
│   │       ├── register.go       # 寄存器分配器
│   │       ├── instruction/      # 指令格式
│   │       └── trace/            # Trace 读取器
│   │
│   └── memory/                   # 内存 Node 实现
│       └── dram_handler.go       # DRAM Controller Handler
│
├── capabilities/                 # 可复用能力实现（内部库）
│   ├── cache/                    # Cache 能力
│   │   ├── set_associative.go   # Set-Associative Cache
│   │   ├── fully_associative.go # Fully-Associative Cache
│   │   ├── mshr.go              # MSHR 队列
│   │   └── coherence.go         # 一致性协议辅助
│   │
│   ├── directory/                # Directory 能力
│   │   └── directory.go         # 目录实现
│   │
│   └── memory/                   # 内存能力
│       └── dram/                 # DRAM 实现
│           ├── channel.go        # DRAM Channel
│           ├── bank.go           # Bank 状态
│           ├── scheduler.go      # 调度器
│           └── address_mapping.go # 地址映射
│
├── core/                         # 框架核心（保持不变）
│   ├── node/
│   │   ├── node.go               # BaseNode
│   │   └── handler.go            # NodeHandler 接口
│   ├── builder/
│   │   └── builder.go            # 扩展支持 node_type
│   ├── state/
│   │   └── state.go              # 扩展 NodeState
│   ├── visualization/
│   │   └── adapter.go            # 扩展 Adapter
│   ├── link/
│   ├── network/
│   └── queue/
│
└── benchmarks/                   # 性能基准测试
    └── configs/
        ├── simple_cache.json
        └── champsim_system.json
```

### OpenAPI Schema 扩展

#### Node Schema 新增字段

```yaml
Node:
  properties:
    # === 现有字段（保持不变） ===
    node_id: integer
    node_name: string
    node_features: array<string>
    cache: CacheConfig
    directory: DirectoryConfig
    coherence_domain_id: integer
    data: object
    position: object
    style: object

    # === 新增字段 ===
    node_type:
      type: string
      description: "节点类型"
      enum: ["generic", "cpu", "memory_controller", "router"]
      default: "generic"

    cpu_config:
      $ref: '#/components/schemas/CPUConfig'
      description: "CPU 专用配置（仅 node_type=cpu 时有效）"

    memory_config:
      $ref: '#/components/schemas/MemoryConfig'
      description: "内存控制器专用配置（仅 node_type=memory_controller 时有效）"
```

#### CPUConfig Schema

```yaml
CPUConfig:
  type: object
  description: "CPU 配置和统计"
  properties:
    # === 配置字段 ===
    type:
      type: string
      enum: ["champsim_o3", "simple", "custom"]
      description: "CPU 类型"
    trace_file:
      type: string
      description: "Trace 文件路径"
    rob_size:
      type: integer
      description: "ROB 大小"
    lq_size:
      type: integer
      description: "Load Queue 大小"
    sq_size:
      type: integer
      description: "Store Queue 大小"
    fetch_width:
      type: integer
    decode_width:
      type: integer

    # === L1D Cache 配置 ===
    l1d_cache:
      $ref: '#/components/schemas/CacheConfig'

    # === 运行时统计 ===
    ipc:
      type: number
      description: "Instructions Per Cycle"
    total_instructions:
      type: integer
    total_cycles:
      type: integer
```

#### MemoryConfig Schema

```yaml
MemoryConfig:
  type: object
  description: "DRAM 控制器配置"
  properties:
    type:
      type: string
      enum: ["ddr4", "ddr5", "hbm"]
    channels:
      type: integer
    ranks:
      type: integer
    banks:
      type: integer
    rows:
      type: integer
    columns:
      type: integer

    # 时序参数
    tRCD:
      type: integer
    tRP:
      type: integer
    tCAS:
      type: integer
```

### Builder 扩展

```go
// internal/core/builder/builder.go

func BuildFromFlowSimNetwork(fsn protocol.FlowSimNetwork) (*network.Network, error) {
    for _, protoNode := range fsn.Nodes {
        var handler node.NodeHandler

        // 根据 node_type 创建不同的 Handler
        nodeType := "generic"
        if protoNode.NodeType != nil {
            nodeType = *protoNode.NodeType
        }

        switch nodeType {
        case "cpu":
            handler = createCPUHandler(protoNode)
        case "memory_controller":
            handler = createMemoryHandler(protoNode)
        case "router":
            handler = createRouterHandler(protoNode)
        default: // "generic"
            handler = createGenericHandler(protoNode)
        }

        // 创建 BaseNode
        n := node.NewBaseNode(protoNode.NodeId, handler)

        // 保存所有配置到 BaseNode（用于往返）
        if protoNode.CpuConfig != nil {
            n.SetData("cpu_config", protoNode.CpuConfig)
        }
        if protoNode.MemoryConfig != nil {
            n.SetData("memory_config", protoNode.MemoryConfig)
        }
        if protoNode.Cache != nil {
            n.SetFeature("cache", cacheConfigToMap(protoNode.Cache))
        }

        // ... 保存可视化信息
        n.SetAllDisplayData(extractDisplayData(protoNode))
    }
}
```

### JSON 配置示例

```json
{
  "nodes": [
    {
      "node_id": 0,
      "node_name": "CPU_0",
      "node_type": "cpu",
      "cpu_config": {
        "type": "champsim_o3",
        "trace_file": "traces/600.perlbench_s.xz",
        "rob_size": 256,
        "lq_size": 72,
        "sq_size": 56,
        "fetch_width": 6,
        "decode_width": 6,
        "l1d_cache": {
          "capacity": 32768,
          "num_sets": 64,
          "num_ways": 8,
          "replacement_policy": "LRU",
          "states": "MESI"
        }
      },
      "data": {"id": "cpu-0", "label": "CPU 0"},
      "position": {"x": 100, "y": 100}
    },
    {
      "node_id": 1,
      "node_name": "L2_Cache",
      "node_type": "generic",
      "cache": {
        "capacity": 262144,
        "num_sets": 512,
        "num_ways": 8,
        "replacement_policy": "LRU",
        "states": "MESI"
      },
      "data": {"id": "l2-0", "label": "L2"},
      "position": {"x": 100, "y": 200}
    },
    {
      "node_id": 2,
      "node_name": "DRAM",
      "node_type": "memory_controller",
      "memory_config": {
        "type": "ddr4",
        "channels": 1,
        "ranks": 1,
        "banks": 8
      },
      "data": {"id": "dram-0", "label": "DRAM"},
      "position": {"x": 100, "y": 300}
    }
  ],
  "edges": [...]
}
```

---

## 实施计划

### Phase 1: OpenAPI Schema 扩展

**目标**：定义新的数据结构

**任务**：
- [ ] 在 `web/openapi.yaml` 添加 `node_type` 字段
- [ ] 定义 `CPUConfig` schema
- [ ] 定义 `MemoryConfig` schema
- [ ] 运行 `npm run generate:types` 生成 Protocol
- [ ] 验证生成的 `types.gen.go` 正确

**验收标准**：
- ✅ `protocol.Node` 包含 `NodeType`, `CpuConfig`, `MemoryConfig` 字段
- ✅ 所有字段都是指针类型（表示可选）
- ✅ 枚举值正确定义

**测试**：
```bash
cd web && npm run generate:types
grep -A 10 "type Node struct" internal/core/visualization/protocol/types.gen.go
```

---

### Phase 2: 目录重组（第一步：复制）

**目标**：创建新目录结构，保留旧代码

**任务**：
- [ ] 创建 `internal/nodes/cpu/champsim/`
- [ ] **复制**（不是移动）`internal/champsim/cpu/*` → `internal/nodes/cpu/champsim/`
- [ ] **复制** `internal/champsim/instruction/` → `internal/nodes/cpu/champsim/instruction/`
- [ ] **复制** `internal/champsim/trace/` → `internal/nodes/cpu/champsim/trace/`
- [ ] 创建 `internal/capabilities/cache/`
- [ ] **复制** `internal/champsim/cache/*` → `internal/capabilities/cache/`
- [ ] 创建 `internal/capabilities/memory/dram/`
- [ ] **复制** `internal/champsim/dram/*` → `internal/capabilities/memory/dram/`

**验收标准**：
- ✅ 新旧目录同时存在
- ✅ 所有文件都已复制
- ✅ 旧代码仍然编译通过

**测试**：
```bash
go build ./...
go test -timeout=20s ./internal/champsim/...
```

**性能基准**：运行现有 Benchmark，记录基准值

```bash
go test -bench=. -benchmem ./internal/benchmarks/... > baseline.txt
```

---

### Phase 3: 更新 import 路径

**目标**：新代码使用新的 import 路径

**任务**：
- [ ] 更新 `internal/nodes/cpu/champsim/` 内部 import
  - `github.com/Readm/flow_sim/internal/champsim/instruction` → `github.com/Readm/flow_sim/internal/nodes/cpu/champsim/instruction`
- [ ] 更新 `internal/capabilities/cache/` 的 import
  - 移除对 `internal/champsim/` 的依赖
- [ ] 更新 `internal/capabilities/memory/dram/` 的 import

**验收标准**：
- ✅ 新目录下的代码编译通过
- ✅ 没有循环依赖

**测试**：
```bash
go build ./internal/nodes/...
go build ./internal/capabilities/...
```

---

### Phase 4: 实现 NodeHandler

**目标**：将新代码适配到 NodeHandler 接口

**任务**：
- [ ] 实现 `internal/nodes/cpu/champsim/cpu_handler.go`
  ```go
  type CPUHandler struct {
      cpu   *O3CPU
      l1d   *cache.SetAssociativeCache
      // ...
  }

  func (h *CPUHandler) Process(cycle uint64, inputs [][]queue.PacketRef) error {
      // 1. 处理来自网络的响应
      // 2. CPU Tick
      // 3. 发送 miss 请求
  }
  ```
- [ ] 实现 `internal/nodes/memory/dram_handler.go`
- [ ] 实现 `internal/nodes/generic_handler.go`

**验收标准**：
- ✅ 所有 Handler 实现 `NodeHandler` 接口
- ✅ 编译通过
- ✅ 单元测试通过

**测试**：
```bash
go test -timeout=20s ./internal/nodes/...
```

---

### Phase 5: 扩展 State 和 Adapter

**目标**：支持新字段的往返

**任务**：
- [ ] 扩展 `state.NodeState`
  ```go
  type NodeState struct {
      // 现有字段...
      NodeType     string
      CPUConfig    map[string]interface{}
      MemoryConfig map[string]interface{}
  }
  ```
- [ ] 扩展 `node.ExportState()`
  ```go
  func (n *BaseNode) ExportState(...) state.NodeState {
      ns := state.NodeState{...}

      // 导出 CPU 配置
      if cpuConfig := n.GetData("cpu_config"); cpuConfig != nil {
          ns.CPUConfig = cpuConfigToMap(cpuConfig)
      }

      return ns
  }
  ```
- [ ] 扩展 `adapter.StateToFlowSimNetwork()`
  ```go
  func StateToFlowSimNetwork(ns state.NetworkState) protocol.FlowSimNetwork {
      for _, nodeState := range ns.Nodes {
          // 恢复 CPU 配置
          if nodeState.CPUConfig != nil {
              protoNode.CpuConfig = mapToCPUConfig(nodeState.CPUConfig)
          }
      }
  }
  ```

**验收标准**：
- ✅ 往返测试通过
- ✅ 新字段完整保留

**测试**：
```bash
go test -run TestFlowSimNetworkRoundTrip ./internal/core/visualization/...
```

---

### Phase 6: 扩展 Builder

**目标**：支持 node_type 路由

**任务**：
- [ ] 扩展 `builder.BuildFromFlowSimNetwork()`
  - 添加 `node_type` 路由逻辑
  - 调用不同的 Handler 创建函数
- [ ] 实现 `createCPUHandler(protoNode)`
- [ ] 实现 `createMemoryHandler(protoNode)`
- [ ] 实现 `createGenericHandler(protoNode)`

**验收标准**：
- ✅ 可以从 JSON 构建不同类型的 Node
- ✅ 配置正确传递到 Handler

**测试**：
```bash
go test -run TestBuildFromFlowSimNetwork ./internal/core/builder/...
```

---

### Phase 7: 集成测试

**目标**：端到端验证

**任务**：
- [ ] 创建 ChampSim 系统 JSON 配置
- [ ] 测试 `POST /build_network` API
- [ ] 测试 `POST /advance_to` API
- [ ] 测试 `GET /current_state` API
- [ ] 验证往返一致性

**验收标准**：
- ✅ 完整的 HTTP 工作流测试通过
- ✅ ChampSim CPU 正常运行
- ✅ 统计数据正确

**测试**：
```bash
go test -run TestHTTPWorkflow ./internal/integration/...
```

---

### Phase 8: 迁移现有代码

**目标**：将现有代码切换到新路径

**任务**：
- [ ] 更新所有使用旧 import 路径的代码
  - `internal/champsim/cpu` → `internal/nodes/cpu/champsim`
  - `internal/champsim/cache` → `internal/capabilities/cache`
  - `internal/champsim/dram` → `internal/capabilities/memory/dram`
- [ ] 运行所有测试

**验收标准**：
- ✅ 所有测试通过
- ✅ 没有 import 旧路径

**测试**：
```bash
go test -timeout=20s ./...
```

**性能基准**：
```bash
go test -bench=. -benchmem ./internal/benchmarks/... > after.txt
# 对比 baseline.txt 和 after.txt
```

---

### Phase 9: 删除旧代码

**目标**：清理遗留代码

**任务**：
- [ ] 删除 `internal/champsim/` 目录
- [ ] 删除 `internal/champsim/flowsim/` 相关代码
- [ ] 删除 `internal/champsim/integration/` 相关代码
- [ ] 更新文档

**验收标准**：
- ✅ `internal/champsim/` 目录不存在
- ✅ 所有测试仍然通过
- ✅ 性能基准保持

**测试**：
```bash
go build ./...
go test -timeout=20s ./...
go test -bench=. -benchmem ./internal/benchmarks/...
```

---

### Phase 10: 文档更新

**目标**：更新项目文档

**任务**：
- [ ] 更新 `README.md`
- [ ] 更新 `docs/architecture/DATA_STRUCTURES.md`
- [ ] 创建 `docs/guides/NODE_TYPES.md`（说明不同 Node 类型）
- [ ] 创建 `docs/examples/`（JSON 配置示例）

**验收标准**：
- ✅ 文档准确反映新架构
- ✅ 示例可以运行

---

## 性能基准

### Baseline 测试

**测试命令**：
```bash
go test -bench=. -benchmem -benchtime=3s ./internal/benchmarks/... > baseline.txt
```

**关键指标**：
- `BenchmarkCPUTick`：CPU Tick 延迟
- `BenchmarkCacheAccess`：Cache Access 延迟
- `BenchmarkPacketProcessing`：Packet 处理吞吐量
- `BenchmarkNetworkAdvance`：Network AdvanceTo 延迟

### 允许的性能变化范围

| 指标 | 允许变化 | 说明 |
|------|---------|------|
| CPU Tick 延迟 | ±5% | 主要逻辑未改变，应保持 |
| Cache Access 延迟 | ±5% | 只是移动了目录，不应变化 |
| Packet 处理吞吐量 | ±3% | 通信机制未改变 |
| 内存分配次数 | 0% | 不应增加额外分配 |

### 性能回归处理

如果性能下降超过允许范围：
1. 使用 `go test -cpuprofile=cpu.prof` 生成 CPU profile
2. 使用 `go tool pprof cpu.prof` 分析热点
3. 定位性能瓶颈
4. 优化或回滚该步骤

---

## 验收标准

### 功能完整性

- [ ] 所有现有测试通过
- [ ] 往返一致性测试通过
- [ ] ChampSim CPU 正常运行
- [ ] 统计数据正确
- [ ] JSON 配置可以加载

### 代码质量

- [ ] 没有循环依赖
- [ ] 所有 import 路径正确
- [ ] 没有编译警告
- [ ] 代码符合 Go 规范

### 性能保持

- [ ] 所有 Benchmark 在允许范围内
- [ ] 没有额外的内存分配
- [ ] CPU profile 无异常热点

### 文档完整

- [ ] 设计文档完整
- [ ] API 文档更新
- [ ] 示例可运行
- [ ] CHANGELOG 更新

---

## 风险与缓解

### 风险 1：性能回归

**缓解措施**：
- 每个 Phase 都运行 Benchmark
- 及时发现问题，及时优化
- 保留回滚能力

### 风险 2：测试失败

**缓解措施**：
- 分步进行，每步验证
- 先复制后修改，保留旧代码
- 可以随时回退

### 风险 3：往返不一致

**缓解措施**：
- 严格遵守 Schema-First
- 每个字段都添加往返测试
- 使用 Adapter 统一转换逻辑

---

## 附录

### 相关文件

- `web/openapi.yaml`：OpenAPI Schema 定义
- `internal/core/visualization/protocol/types.gen.go`：自动生成的 Protocol
- `internal/core/builder/builder.go`：Builder 逻辑
- `internal/core/state/state.go`：State 定义
- `internal/core/visualization/adapter.go`：Adapter 逻辑
- `docs/architecture/DATA_STRUCTURES.md`：数据结构文档

### 参考资料

- [OpenAPI 3.0 规范](https://swagger.io/specification/)
- [Go Project Layout](https://github.com/golang-standards/project-layout)
- [ChampSim 官方文档](https://github.com/ChampSim/ChampSim)

---

**文档结束**
