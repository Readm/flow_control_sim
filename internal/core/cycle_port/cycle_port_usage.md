# CyclePort 使用指南

## 核心优势

**写代码时不需要区分单上游还是多上游！** 所有操作都通过 `CyclePort` 接口完成，聚合器自动处理多上游场景。

## 使用方式

### 单上游场景

```go
// 创建单个端口
flow0OutPort := cycle_port.NewCyclePort(8)
flow0.AddOutPort(flow0OutPort)

// 创建 Link（直接使用端口）
link := link.NewLink(0, 1, flow0OutPort, flow1InPort, 2, 1)

// Flow 操作（完全透明）
flow0OutPort.Chan() <- env
flow0OutPort.SetDone(cycle)
flow0OutPort.Ready(cycle)
```

### 多上游场景

```go
// 创建共享端口组（工厂函数）
upstreams, aggregator := cycle_port.NewSharedPortGroup(3, 8)
flow0.AddOutPort(upstreams[0])
flow1.AddOutPort(upstreams[1])
flow2.AddOutPort(upstreams[2])

// 创建 Link（使用聚合器）
link := link.NewLink(0, 3, aggregator, flow3InPort, 1, 10)

// Flow 操作（完全一致，每个 Flow 操作自己的端口）
upstreams[0].Chan() <- env
upstreams[0].SetDone(cycle)
upstreams[0].Ready(cycle)
```

## 设计优势

### 1. 上游（Flow）完全透明

无论单上游还是多上游，Flow 的代码完全一样：

```go
outPort.Chan() <- env        // 发送数据
outPort.SetDone(cycle)       // 设置完成
outPort.Ready(cycle)         // 检查就绪
```

### 2. 下游（Link）接口统一

Link 通过 `CyclePort` 接口操作，不需要区分单/多上游：

```go
upstreamPort.WaitForDone(cycle)      // 等待完成
<-upstreamPort.ReceiveChan()          // 接收数据
upstreamPort.UpdateReady(cycle, ready) // 更新就绪
```

### 3. Done 配置规则

- **设置 Done**：在每个上游端口上分别调用 `port.SetDone(n)`
- **读取 Done**：通过聚合器调用 `aggregator.GetDone()`（返回最小值）
- **等待 Done**：通过聚合器调用 `aggregator.WaitForDone(n)`（等待所有上游）

```go
// ✅ 正确：在每个端口上设置
upstreams[0].SetDone(5)
upstreams[1].SetDone(5)
upstreams[2].SetDone(5)

// ✅ 正确：通过聚合器读取
done := aggregator.GetDone()  // 返回最小值

// ❌ 错误：不能在聚合器上设置
aggregator.SetDone(5)  // panic!
```

## 架构对比

**单上游：**
```
Flow0 ──> port ──> Link ──> Flow1
```

**多上游：**
```
Flow0 ──> upstreams[0] ──┐
                        ├──> [sharedChan] ──> aggregator ──> Link ──> Flow3
Flow1 ──> upstreams[1] ──┤
                        │
Flow2 ──> upstreams[2] ──┘
```

## 关键特性

| 特性 | 单上游 | 多上游 |
|------|--------|--------|
| **数据通道** | 单个 channel | 共享 channel |
| **Done 同步** | 单个端口 | 聚合所有上游（最小值） |
| **Ready 同步** | 单个端口 | 传播到所有上游 |
| **代码复杂度** | 相同 | 相同（接口统一） |

## 使用建议

- **单个上游 Flow** → `NewCyclePort(8)`
- **多个上游 Flow** → `NewSharedPortGroup(count, 8)`

两种方式对 Link 来说接口完全一致，聚合器自动处理多上游的同步和聚合。

