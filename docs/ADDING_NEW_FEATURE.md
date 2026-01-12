# 添加新的节点配置字段指南

本文档说明如何为节点（如 CPU、Memory）添加新的配置字段，涵盖从 Schema 定义到代码实现的完整流程。

---

## 目录

1. [概述](#概述)
2. [添加新字段的完整流程](#添加新字段的完整流程)
3. [配置字段 vs 统计字段](#配置字段-vs-统计字段)
4. [默认值处理](#默认值处理)
5. [必填字段设置](#必填字段设置)
6. [示例：添加 CPU 分支预测器配置](#示例添加-cpu-分支预测器配置)
7. [常见问题](#常见问题)

---

## 概述

### 当前架构

项目采用 **Schema-First** 设计：

```
                    OpenAPI Schema (web/openapi.yaml)
                                |
                    ┌───────────┴───────────┐
                    |                       |
            【后端代码生成】           【前端代码生成】
                    ↓                       ↓
        Go Protocol Types           TypeScript Types
    (types.gen.go)                  (web_dev/src/types/api.ts)
                    ↓                       ↓
        ┌───────────┴──────┐               |
        |                  |               |
    Builder 读取      HTTP API         前端使用
    (类型安全)        (JSON验证)      (类型检查)
        ↓
    Handler 配置
    (champsim/...)
        ↓
    通用转换器 (configconv 包)
        ↓
    往返一致性保留 ✅
```

**关键特性：**
- ✅ **零假设原则**：框架代码不知道具体字段（反射自动处理）
- ✅ **自动往返保留**：新字段无需修改转换代码
- ✅ **类型安全**：后端 Go + 前端 TypeScript 都有编译期检查
- ✅ **单一数据源**：OpenAPI Schema 同时驱动后端和前端

---

## 添加新字段的完整流程

### 步骤 1：修改 OpenAPI Schema

**文件：** `web/openapi.yaml`

找到对应的配置对象（如 `CPUConfig`），添加新字段：

```yaml
CPUConfig:
  type: object
  description: "CPU 配置和统计"
  properties:
    # === 现有字段 ===
    rob_size:
      type: integer
      description: "ROB 大小"
      minimum: 0

    # === 新增字段 ===
    branch_predictor_type:
      type: string
      enum: ["perceptron", "gshare", "bimodal", "tage"]
      description: "分支预测器类型"
      default: "perceptron"  # 可选：设置默认值

    branch_predictor_size:
      type: integer
      description: "分支预测器表大小（条目数）"
      minimum: 1
      maximum: 65536
      default: 8192
```

**字段类型支持：**
- `string`：字符串
- `integer`：整数
- `number`：浮点数
- `boolean`：布尔值
- `enum`：枚举（限定可选值）
- `$ref`：引用其他 Schema（嵌套对象）

### 步骤 2：重新生成类型定义

> **💡 为什么需要生成代码？**
>
> 项目采用 **Schema-First** 设计，`openapi.yaml` 是类型的唯一数据源。
> 修改 Schema 后，需要生成两种类型定义：
>
> 1. **Go 类型**：给后端 Builder/HTTP API 使用（类型安全）
> 2. **TypeScript 类型**：给前端 React/Vue 使用（类型安全）
>
> **注意：** 这里生成的是**类型定义本身**（`protocol.CPUConfig` 结构体），
> 不是生成转换器函数。项目使用反射方案，转换器由 `configconv` 包自动处理。

#### 方式 1：手动运行生成脚本

```bash
# 生成 Go 类型（必需）
./scripts/generate_go_types.sh

# 生成 TypeScript 类型（如果有前端开发环境）
./scripts/generate_ts_types.sh
```

**生成内容：**

| 脚本 | 输出文件 | 用途 |
|------|---------|------|
| `generate_go_types.sh` | `internal/core/visualization/protocol/types.gen.go` | 后端 Builder 读取配置 |
| `generate_ts_types.sh` | `web_dev/src/types/api.ts` | 前端 TypeScript 类型检查 |

**生成的 Go 代码示例：**

```go
type CPUConfig struct {
    // ...
    BranchPredictorType *string `json:"branch_predictor_type,omitempty"`
    BranchPredictorSize *int    `json:"branch_predictor_size,omitempty"`
}
```

**生成的 TypeScript 代码示例：**

```typescript
export interface CPUConfig {
  branch_predictor_type?: "perceptron" | "gshare" | "bimodal" | "tage";
  branch_predictor_size?: number;
  // ...
}
```

#### 方式 2：通过 Git Hooks 自动生成（推荐）

配置 Git hooks（只需一次）：

```bash
git config core.hooksPath .githooks
```

配置后，**每次提交 `openapi.yaml` 时会自动生成**：

```bash
vim web/openapi.yaml   # 修改 Schema
git add web/openapi.yaml
git commit             # 自动触发生成 ✅
```

**Hook 执行流程：**

```
git commit
    ↓
检测到 openapi.yaml 变化
    ↓
自动运行 ./scripts/generate_go_types.sh
    ↓
自动运行 ./scripts/generate_ts_types.sh（如果 web_dev 存在）
    ↓
自动 git add types.gen.go
    ↓
提交完成 ✅
```

**注意：**
- 所有字段都是指针类型（`*string`, `*int`），表示可选
- 如果字段未在 JSON 中提供，指针为 `nil`

### 步骤 3：在 Builder 中读取配置

**文件：** `internal/core/builder/builder.go`

在 `createCPUHandler` 函数中添加读取逻辑：

```go
func createCPUHandler(nodeID int, cpuConfig *protocol.CPUConfig, outputQueue *queue.OutputQueue, downstreamIDs []int) (node.NodeHandler, trace.TraceReader, error) {
    // ... 现有代码 ...

    // 读取新字段（带默认值）
    branchPredictorType := "perceptron" // 默认值
    if cpuConfig.BranchPredictorType != nil {
        branchPredictorType = *cpuConfig.BranchPredictorType
    }

    branchPredictorSize := 8192 // 默认值
    if cpuConfig.BranchPredictorSize != nil {
        branchPredictorSize = *cpuConfig.BranchPredictorSize
    }

    // 使用配置创建组件
    branchPredictor := cpu.NewBranchPredictor(branchPredictorType, branchPredictorSize)
    o3cpu.SetBranchPredictor(branchPredictor)

    // ...
}
```

### 步骤 4：验证往返一致性

**无需修改转换代码！** 通用转换器自动处理新字段。

运行测试验证：

```bash
go test -v ./internal/integration -run TestNodeTypeRoundTrip
```

测试会验证：
1. 配置通过 JSON 正确导入
2. 仿真运行后配置保留
3. 导出的 JSON 包含原始配置值

---

## 配置字段 vs 统计字段

### 配置字段（Configuration）

- **定义**：用户在启动仿真前设置的参数
- **特点**：静态，不会在运行时改变
- **示例**：`rob_size`, `trace_file`, `branch_predictor_type`

### 统计字段（Statistics）

- **定义**：仿真运行时动态生成的数据
- **特点**：初始为空，运行后填充
- **示例**：`ipc`, `total_instructions`, `branch_mispredictions`

**在 OpenAPI Schema 中的区别：**

```yaml
CPUConfig:
  properties:
    # 配置字段（用户提供）
    rob_size:
      type: integer
      description: "ROB 大小"

    # 统计字段（仿真生成）
    total_instructions:
      type: integer
      description: "总指令数"
```

**在 Builder 中的处理：**

```go
// 配置字段：从 cpuConfig 读取
if cpuConfig.RobSize != nil {
    o3Config.ROBSize = *cpuConfig.RobSize
}

// 统计字段：由 Handler 的 ExportStats 导出
func (h *CPUNodeHandler) ExportStats() map[string]interface{} {
    return map[string]interface{}{
        "total_instructions": h.totalInstructions,
        "ipc": float32(h.totalInstructions) / float32(h.totalCycles),
    }
}
```

---

## 默认值处理

### 方法 1：在 OpenAPI Schema 中定义（推荐）

```yaml
branch_predictor_type:
  type: string
  default: "perceptron"
  description: "分支预测器类型"
```

**优点：**
- 文档清晰：API 文档自动显示默认值
- 前端可读取：前端可从 Schema 获取默认值

**缺点：**
- Go 代码仍需手动实现默认值（OpenAPI 生成器不生成默认值逻辑）

### 方法 2：在 Builder 中定义

```go
// 定义常量
const (
    defaultBranchPredictorType = "perceptron"
    defaultBranchPredictorSize = 8192
)

// 使用默认值
branchPredictorType := defaultBranchPredictorType
if cpuConfig.BranchPredictorType != nil {
    branchPredictorType = *cpuConfig.BranchPredictorType
}
```

**优点：**
- 集中管理：所有默认值在一处定义
- 可复用：常量可在多处使用

### 方法 3：在 Handler 构造函数中定义

```go
func NewBranchPredictor(bpType string, size int) *BranchPredictor {
    // 如果参数为零值，使用默认值
    if bpType == "" {
        bpType = "perceptron"
    }
    if size == 0 {
        size = 8192
    }
    // ...
}
```

**推荐实践：** 结合方法 1 和 2，在 Schema 中文档化，在 Builder 中实现。

---

## 必填字段设置

### 方法 1：使用 `required` 字段（推荐）

在 OpenAPI Schema 中声明必填字段：

```yaml
CPUConfig:
  type: object
  required:
    - trace_file  # 强制要求
    - rob_size
  properties:
    trace_file:
      type: string
      description: "Trace 文件路径（必填）"
    rob_size:
      type: integer
      description: "ROB 大小（必填）"
    branch_predictor_type:
      type: string
      description: "分支预测器类型（可选）"
```

**效果：**
- OpenAPI 验证器会检查必填字段
- API 文档会标记必填字段
- 前端可据此校验用户输入

**注意：** Go 代码生成器仍会生成指针类型，需要手动检查。

### 方法 2：在 Builder 中手动检查

```go
func createCPUHandler(nodeID int, cpuConfig *protocol.CPUConfig, ...) (node.NodeHandler, trace.TraceReader, error) {
    // 检查必填字段
    if cpuConfig.TraceFile == nil {
        return nil, nil, fmt.Errorf("trace_file is required for CPU node %d", nodeID)
    }
    if cpuConfig.RobSize == nil {
        return nil, nil, fmt.Errorf("rob_size is required for CPU node %d", nodeID)
    }

    // 使用字段（已确保非 nil）
    traceFile := *cpuConfig.TraceFile
    robSize := *cpuConfig.RobSize

    // ...
}
```

### 方法 3：使用非指针类型（不推荐）

修改 OpenAPI 生成配置，使某些字段生成为非指针类型。

**缺点：**
- 破坏了 Schema 的一致性
- 无法区分"未设置"和"设置为零值"

---

## 示例：添加 CPU 分支预测器配置

### 完整示例

假设需要添加分支预测器配置，允许用户选择预测器类型和大小。

#### 1. 修改 `web/openapi.yaml`

```yaml
CPUConfig:
  type: object
  required:
    - trace_file  # 现有必填字段
  properties:
    # === 现有字段 ===
    trace_file:
      type: string
      description: "Trace 文件路径"
    rob_size:
      type: integer
      description: "ROB 大小"
      default: 256

    # === 新增：分支预测器配置 ===
    branch_predictor_type:
      type: string
      enum: ["perceptron", "gshare", "bimodal", "tage"]
      description: "分支预测器类型"
      default: "perceptron"

    branch_predictor_size:
      type: integer
      description: "分支预测器表大小（条目数）"
      minimum: 128
      maximum: 65536
      default: 8192

    # === 统计字段 ===
    branch_mispredictions:
      type: integer
      description: "分支预测错误次数（运行时统计）"
```

#### 2. 重新生成代码

```bash
./scripts/generate_go_types.sh
```

#### 3. 更新 Builder (`internal/core/builder/builder.go`)

```go
const (
    // 默认值常量
    defaultBranchPredictorType = "perceptron"
    defaultBranchPredictorSize = 8192
)

func createCPUHandler(nodeID int, cpuConfig *protocol.CPUConfig, outputQueue *queue.OutputQueue, downstreamIDs []int) (node.NodeHandler, trace.TraceReader, error) {
    // ... 现有代码 ...

    // 读取分支预测器配置
    branchPredictorType := defaultBranchPredictorType
    if cpuConfig.BranchPredictorType != nil {
        branchPredictorType = *cpuConfig.BranchPredictorType
    }

    branchPredictorSize := defaultBranchPredictorSize
    if cpuConfig.BranchPredictorSize != nil {
        branchPredictorSize = *cpuConfig.BranchPredictorSize
    }

    // 3. 构建 O3CPUConfig
    o3Config := cpu.DefaultO3CPUConfig()
    // ... 设置其他字段 ...

    // 4. 创建 Branch Predictor
    branchPredictor := cpu.NewBranchPredictor(branchPredictorType, branchPredictorSize)

    // 5. 创建 O3CPU
    o3cpu := cpu.NewO3CPU(o3Config, traceReader, branchPredictor)

    // ... 其余代码不变 ...
}
```

#### 4. 实现分支预测器（如果是新组件）

```go
// internal/nodes/cpu/champsim/branch_predictor.go
package cpu

type BranchPredictor struct {
    predictorType string
    tableSize     int
    // ...
}

func NewBranchPredictor(bpType string, size int) *BranchPredictor {
    return &BranchPredictor{
        predictorType: bpType,
        tableSize:     size,
    }
}
```

#### 5. 测试配置

创建测试用的 JSON 配置：

```json
{
  "version": "1.0.0",
  "cycle": 0,
  "nodes": [
    {
      "node_id": 0,
      "node_name": "CPU_0",
      "node_type": "cpu",
      "cpu_config": {
        "trace_file": "../../testdata/traces/small.champsimtrace",
        "rob_size": 256,
        "branch_predictor_type": "tage",
        "branch_predictor_size": 16384
      },
      "in_ports": [{"port_id": 0, "bandwidth": 1, "buffer_size": 128}],
      "out_ports": [{"port_id": 0, "bandwidth": 1, "buffer_size": 128}],
      "position": {"x": 100, "y": 100},
      "data": {"id": "node-0"}
    }
  ],
  "edges": []
}
```

#### 6. 运行测试验证

```bash
# 验证往返一致性
go test -v ./internal/integration -run TestNodeTypeRoundTrip

# 验证配置正确应用
go test -v ./internal/integration -run TestCPUMemoryNodeTypeConfig
```

#### 7. 验证导出的配置包含新字段

导出后的 JSON 应该包含：

```json
{
  "nodes": [
    {
      "node_id": 0,
      "cpu_config": {
        "branch_predictor_type": "tage",
        "branch_predictor_size": 16384,
        "rob_size": 256,
        "total_instructions": 168,
        "ipc": 1.68
      }
    }
  ]
}
```

**关键验证点：**
- ✅ `branch_predictor_type` 保留原始值 `"tage"`
- ✅ `branch_predictor_size` 保留原始值 `16384`
- ✅ 配置参数和统计数据共存

---

## 常见问题

### Q1: 新字段需要修改 `configconv` 转换器吗？

**A:** ❌ **不需要！**

通用转换器基于反射和 JSON tag 自动处理所有字段，无需修改代码。只要：
1. 在 `openapi.yaml` 中定义字段
2. 重新生成 Protocol 类型

转换器就会自动支持新字段。

---

### Q2: 为什么需要两个代码生成步骤？Go 和 TypeScript 都要生成吗？

**A:** 是的，项目采用 **Schema-First** 架构，`openapi.yaml` 是类型的唯一数据源：

**两个生成步骤：**
1. **Go 类型生成**（必需）：
   - 输出：`internal/core/visualization/protocol/types.gen.go`
   - 用途：后端 Builder 读取配置、HTTP API 验证 JSON
   - 工具：`oapi-codegen`

2. **TypeScript 类型生成**（如果有前端）：
   - 输出：`web_dev/src/types/api.ts`
   - 用途：前端 TypeScript 类型检查、IDE 自动补全
   - 工具：`openapi-typescript`

**为什么不能省略？**

| 省略步骤 | 后果 |
|---------|------|
| 不生成 Go 类型 | Builder 无法读取配置，HTTP API 无法验证 JSON |
| 不生成 TS 类型 | 前端失去类型检查，容易出现字段名拼写错误 |

**好消息：** 配置 Git hooks 后（`git config core.hooksPath .githooks`），
提交 `openapi.yaml` 时会**自动生成两者**，开发者无需手动执行。

**注意：** 这里生成的是**类型定义**（结构体/接口），不是转换器函数。
项目使用反射方案，`configconv` 包自动处理所有类型的转换。

---

### Q3: 如何验证新字段正确工作？

**A:** 运行往返一致性测试：

```bash
go test -v ./internal/integration -run TestNodeTypeRoundTrip
```

测试流程：
1. 创建包含新字段的配置
2. 构建网络并运行仿真
3. 导出状态
4. 验证新字段的值保持不变

---

### Q4: 如果忘记在 Builder 中读取新字段会怎样？

**A:**
- ✅ 配置仍能往返保留（导出的 JSON 会包含原始值）
- ❌ 新字段不会影响仿真行为（因为 Handler 未使用）

**建议：** 添加集成测试验证新字段确实影响仿真结果。

---

### Q5: 如何处理嵌套配置对象？

**A:** 在 Schema 中使用 `$ref` 引用：

```yaml
CPUConfig:
  properties:
    l1d_cache:
      $ref: '#/components/schemas/CacheConfig'
      description: "L1D Cache 配置"

CacheConfig:
  type: object
  properties:
    num_sets:
      type: integer
    capacity:
      type: integer
    replacement_policy:
      type: string
```

通用转换器会递归处理嵌套对象。

---

### Q6: 如何处理数组字段？

**A:** 使用 `array` 类型：

```yaml
CPUConfig:
  properties:
    prefetcher_configs:
      type: array
      description: "预取器配置列表"
      items:
        type: object
        properties:
          type:
            type: string
          degree:
            type: integer
```

生成的 Go 类型：

```go
type CPUConfig struct {
    PrefetcherConfigs *[]struct {
        Type   *string `json:"type,omitempty"`
        Degree *int    `json:"degree,omitempty"`
    } `json:"prefetcher_configs,omitempty"`
}
```

---

### Q7: 如何添加仅统计字段（不是配置）？

**A:** 只在 Schema 中定义，不在 Builder 中读取：

```yaml
CPUConfig:
  properties:
    # 统计字段（仅导出，不影响构建）
    branch_mispredictions:
      type: integer
      description: "分支预测错误次数"
```

在 Handler 中导出统计：

```go
func (h *CPUNodeHandler) ExportStats() map[string]interface{} {
    return map[string]interface{}{
        "branch_mispredictions": h.branchPredictor.GetMispredictions(),
    }
}
```

---

### Q8: 如何强制用户提供某个字段？

**A:** 方法 1（推荐）：在 Builder 中手动检查

```go
if cpuConfig.BranchPredictorType == nil {
    return nil, nil, fmt.Errorf("branch_predictor_type is required for CPU node")
}
```

方法 2：在 Schema 中标记 `required`（仅用于文档和前端校验）

```yaml
CPUConfig:
  required:
    - trace_file
    - branch_predictor_type
```

---

## 总结

### 添加新字段的最小步骤

1. **修改 `web/openapi.yaml`**：定义新字段
2. **运行 `./scripts/generate_go_types.sh`**：生成 Protocol 类型
3. **更新 `internal/core/builder/builder.go`**：读取并使用新字段
4. **运行测试**：`go test ./internal/integration`

### 关键原则

- ✅ **Schema-First**：OpenAPI 是单一数据源
- ✅ **零假设**：框架不知道具体字段，通用转换器自动处理
- ✅ **向后兼容**：新字段默认为 `nil`，不影响现有配置
- ✅ **往返一致**：配置通过 JSON 导入后，导出时保持不变

### 常见模式

| 场景 | 做法 |
|------|------|
| 可选字段 + 默认值 | Schema 定义 `default`，Builder 中实现默认值逻辑 |
| 必填字段 | Schema 标记 `required` + Builder 中手动检查 `nil` |
| 枚举限定 | Schema 使用 `enum` + Builder 中读取字符串 |
| 嵌套对象 | Schema 使用 `$ref` + Builder 递归读取 |
| 统计字段 | Schema 定义 + Handler 的 `ExportStats()` 导出 |

---

## 参考

- **OpenAPI 规范**：https://swagger.io/specification/
- **项目代码生成配置**：`scripts/generate_go_types.sh`
- **通用转换器实现**：`internal/configconv/converter.go`
- **测试示例**：`internal/integration/node_type_config_test.go`
