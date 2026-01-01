# Chrome Trace 追踪系统

## 概述

这个包实现了一个兼容 Chrome DevTools 的追踪系统，可以生成可视化的时间线，帮助分析仿真性能瓶颈。

## 功能特性

- ✅ **Chrome 兼容**: 生成标准的 Chrome trace JSON 格式，可直接在 `chrome://tracing` 中查看
- ✅ **细粒度追踪**: 每个 Node 的 3 个阶段（Receive/Process/Send）都单独记录
- ✅ **编译时控制**: 使用 build tag (`-tags trace`) 启用，否则零开销
- ✅ **灵活配置**: 支持采样、过滤、时间阈值等多种配置
- ✅ **元数据支持**: 可添加进程名、线程名等元数据

## 快速开始

### 1. 启用 Trace

编译时添加 `-tags trace` 标志：

```bash
go build -tags trace ./...
go test -tags trace ./...
```

### 2. 基本用法

```go
package main

import (
    "github.com/Readm/flow_sim/internal/core/network"
    "github.com/Readm/flow_sim/internal/core/trace"
)

func main() {
    // 1. 创建 tracer
    config := trace.TracerConfig{
        Enabled:    true,
        MaxCycles:  1000,  // 只记录前 1000 个 cycles
        SampleRate: 1,     // 每个 cycle 都记录
    }
    tracer := trace.NewTraceRecorder(config)

    // 2. 创建 network 并设置 tracer
    net := network.New()
    net.SetTracer(tracer)

    // 3. 添加节点、连接拓扑...
    // ... your network setup ...

    // 4. 运行仿真
    net.AdvanceTo(1000)

    // 5. 导出 trace
    tracer.Export("simulation_trace.json")
}
```

### 3. 在 Chrome 中查看

1. 打开 Chrome 浏览器
2. 访问 `chrome://tracing`
3. 点击 "Load" 按钮
4. 选择生成的 `simulation_trace.json` 文件
5. 使用 WASD 键导航，鼠标点击查看详情

## 配置选项

```go
type TracerConfig struct {
    // 是否启用追踪
    Enabled bool

    // 只记录前 N 个 cycles（0 表示不限制）
    MaxCycles int

    // 采样率：每 N 个 cycles 记录一次（1 表示每个 cycle 都记录）
    SampleRate int

    // 最小持续时间：只记录持续时间超过此阈值的事件（cycles）
    MinDuration int64

    // 节点过滤器：只记录这些节点的事件（空表示记录所有节点）
    NodeFilter []int

    // 记录阻塞事件的阈值（cycles）
    BlockThreshold int64
}
```

### 默认配置

```go
config := trace.DefaultConfig()
// Enabled: true
// MaxCycles: 1000
// SampleRate: 1
// MinDuration: 0
// BlockThreshold: 1000000 (1ms)
```

## 高级用法

### 添加元数据

为节点和线程添加可读的名称：

```go
nodeNames := map[int]string{
    0:   "CPU_0",
    1:   "CPU_1",
    100: "L2_Cache",
    200: "RingRouter",
}

threadNames := map[int]string{
    trace.TidReceive:  "Receive",
    trace.TidProcess:  "Process",
    trace.TidSend:     "Send",
    trace.TidTransfer: "Transfer",
}

tracer.ExportWithMetadata("trace.json", nodeNames, threadNames)
```

### 只追踪特定节点

```go
config := trace.TracerConfig{
    Enabled:    true,
    NodeFilter: []int{100, 101, 102}, // 只追踪这 3 个节点
}
```

### 采样以减少开销

```go
config := trace.TracerConfig{
    Enabled:    true,
    SampleRate: 10, // 每 10 个 cycles 记录一次
}
```

## Chrome Trace 视图解读

### Timeline 视图

```
┌─────────────────────────────────────────────────┐
│ Node 101 (CPU)                                  │
├─────────────────────────────────────────────────┤
│ Receive │ [████████]                            │
│ Process │     [███]                              │
│ Send    │         [██]                           │
└─────────────────────────────────────────────────┘
```

- **每个 Node 显示为一个进程**
- **每个阶段（Receive/Process/Send）显示为一个线程**
- **每个 event 显示为一个矩形块**
- **颜色**：相同名称的 event 使用相同颜色
- **长度**：表示持续时间

### 查看详情

点击任何事件块可以看到：
- **Name**: 事件名称
- **Category**: 分类（node/link/sync）
- **Duration**: 持续时间
- **Args**: 自定义参数（cycle, packets, 等）

### 导航快捷键

- **W/S**: 垂直滚动
- **A/D**: 水平滚动
- **鼠标滚轮**: 缩放
- **1-4**: 切换不同的测量工具

## 事件类型

### Node Events

| Event    | TID | Description              |
|----------|-----|--------------------------|
| Receive  | 1   | 接收数据阶段              |
| Process  | 2   | 处理数据阶段              |
| Send     | 3   | 发送数据阶段              |

### 自定义参数

每个事件都携带额外信息：

**Receive**:
```json
{
  "cycle": 1000,
  "packets": 5
}
```

**Process**:
```json
{
  "cycle": 1000
}
```

**Send**:
```json
{
  "cycle": 1000,
  "sent": 3
}
```

## 性能考虑

### 内存开销

每个 event 约 200 bytes：
- 1000 cycles × 136 nodes × 3 phases = ~82MB

### CPU 开销

- 记录操作需要加锁（sync.Mutex）
- 建议只在调试时启用
- 生产环境建议禁用或使用高采样率

### 优化建议

1. **限制 cycles**: 使用 `MaxCycles` 只记录前 N 个 cycles
2. **采样**: 使用 `SampleRate > 1` 减少记录频率
3. **过滤节点**: 使用 `NodeFilter` 只追踪关键节点
4. **最小持续时间**: 使用 `MinDuration` 过滤短事件

## 示例输出

生成的 JSON 格式：

```json
{
  "traceEvents": [
    {
      "name": "Receive",
      "cat": "node",
      "ph": "X",
      "ts": 1000000,
      "dur": 50000,
      "pid": 101,
      "tid": 1,
      "args": {
        "cycle": 1000,
        "packets": 3
      }
    }
  ],
  "displayTimeUnit": "ns",
  "otherData": {
    "version": "flow_sim v1.0",
    "event_count": 1000
  }
}
```

## 调试技巧

### 1. 找到阻塞点

在 Chrome trace 中：
1. 找到持续时间特别长的 Receive 事件
2. 查看其 args.waiting_for 参数
3. 定位到上游节点进行分析

### 2. 对比不同 cycles

1. 记录多个不同配置的 trace
2. 使用 Chrome 的多 trace 对比功能
3. 分析性能差异

### 3. 识别热点

1. 查找持续时间最长的 Process 事件
2. 检查这些节点的处理逻辑
3. 优化算法或增加并行度

## 常见问题

### Q: 为什么时间戳都是 0？

A: 当前实现使用 `GetCPUCycles()` 返回相对时间。如果显示为 0，说明分辨率不够或未启用 profiling。

### Q: 如何减少文件大小？

A:
1. 使用 `.gz` 后缀导出（自动 gzip 压缩）
2. 减少 MaxCycles
3. 增加 SampleRate
4. 使用 NodeFilter

### Q: 编译时忘记加 `-tags trace` 会怎样？

A: 所有 trace 方法都是空操作，完全零开销，不会生成任何 trace 数据。

## 更多信息

- [Chrome Trace Event Format](https://docs.google.com/document/d/1CvAClvFfyA5R-PhYUmn5OOQtYMH4h6I0nSsKchNAySU)
- [Catapult Project](https://github.com/catapult-project/catapult)
