# Core/Entity 设计说明

## 设计目标

- 聚焦 `config`、`Network`、`Node`、`Link` 四个实体，提供最小可运行骨架，保证后续扩展仍然遵守 KISS 原则。
- 通过接口抽象隔离外部依赖（例如 Hook、插件、数据流），当前阶段使用 Mock 即可完成验证。
- 为并行执行提供可观测手段：放大 `link delay` 可以更容易验证多节点同时运行。
- 保留一个稳定的单元测试，确保未来改动不会破坏并行特性。

## 组件职责

- `config`：`internal/config/entity.go` 维持节点（int ID）与链路基础配置，是 Core/Entity 层与外部配置系统的边界。
- `node`：`internal/core/node/node.go` 规范节点接口（`ID() int`、`Flow() flow.Flow`、`Tick(...)`）。每个节点内部实例化 Flow 并在 Tick 中驱动它。
- `link`：`internal/core/link/link.go` 中的 `Link` 结构使用固定 slot 的 ringbuffer 在逻辑 cycle 间缓存 Packet，并在 `Advance(cycle)` 时把 `(cycle, Packet)` 投递到目标 Flow 的 mailbox channel，无锁实现。
- `dataflow`：`internal/dataflow/packet`/`flow` 提供正式的 Packet（`SourceID`/`TargetID` 均为 int）与 Flow（带 `Tick`、`Emit`、`DrainOutgoing`、`Mailbox` channel）。
- `network`：持有 “节点为点、Link 为边” 的有向图。每个 cycle 先调用所有 Link `Advance` 让 Flow 收包，再并发执行节点 Tick，并在 Tick 结束后顺序路由 Flow 的 `DrainOutgoing()` 结果到对应的 Link。
- `controller`：`pkg/controller` 暴露 `SimulationController` 接口，Interface 层通过它启动/停止 Network，并可在测试中注入 Mock Builder。

## 接口约定

```
// internal/config/entity.go
type EntityConfig struct {
    Nodes []NodeConfig
    Link  LinkConfig
}

// internal/core/node/node.go
type Node interface {
    ID() int
    Flow() flow.Flow
    Tick(ctx context.Context, cycle uint64, linkDelay time.Duration) error
}

// internal/core/network/network.go
type Manager interface {
    Run(ctx context.Context, cycles uint64) error
}
```

说明：

- `Tick` 的 `linkDelay` 参数用于在 Mock 场景下注入真实时间延迟（通过 `network.EnableMockDelay` 控制），以便放大并行信号；正式运行时默认传 0。
- Network 在每个 cycle 中为每个 Node 启动一个 goroutine，并借助 `sync.WaitGroup` 与 `errCh` 收敛错误。

## 并行执行流程

1. `Network.Run` 校验 nodes 非空，并创建 `errCh` 收集执行结果。
2. 每个 cycle：
   - 先调用所有 Link 的 `Advance(cycle)`，把到期的 Packet 写入目标 Flow 的 mailbox channel。
   - 并行调用所有节点的 `Tick`，驱动 Flow 处理并生成新的 Packet。
   - 将节点 Flow 的 `DrainOutgoing()` 结果按图结构投递给对应 Link。
   - 若开启 `network.EnableMockDelay`，在进入下一 cycle 前休眠指定时间；正式运行则直接继续。
3. `Run` 结束时返回第一条错误（若有），确保调用方能感知问题。

## Mock 策略

- **Node Mock**：单元测试实现的 `node.Node` 会在内部创建 `flow.FIFO`，Tick 内驱动 `flow.Tick`、借由 `flow.Emit` 生成待发送的 Packet，并通过工作负载模拟计算耗时。
- **Link Mock**：直接使用生产代码中的 `link.Link`。若未来需要特殊行为，可在测试中扩展独立实现。
- **时间延迟 Mock**：通过 `network.EnableMockDelay(time.Duration)` 注入真实时间等待，只在测试/Mock 时启用，避免污染正式逻辑。
- **Config Mock**：测试可直接构造 `config.EntityConfig` 或者仅依赖 `NewManager` 所需的 graph 信息。
- **Controller Mock**：CLI/Web 层只依赖 `SimulationController`，测试场景可以传入 Fake `ManagerBuilder`，而正式环境使用 Network + Mock Node 组合验证启停。

## 并行性测试

- 位置：`internal/core/network/network_test.go`
- 测试方法：
  1. 构建两个节点（ID 0/1），各自内部持有 `flow.FIFO`，通过共享的 `link.Link` 互发 `packet.Packet`，每条 Link 的 latency 为 1 个 cycle。
  2. `Link.Advance` 在每个 cycle 将 slot 中的 `(cycle, Packet)` 写入目标 Flow 的 `Mailbox`（Go channel），Flow Tick 读取 channel 并记录 `ProcessedCount`。
  3. 节点 Tick 内部调用 `flow.Tick` 消费报文，再使用 `flow.Emit` 生成下一个 cycle 的 Packet（非末尾 cycle），同时执行 35ms workload 来放大并发观察窗口。
  4. Network 在所有节点 Tick 完成后，从 Flow `DrainOutgoing` 中拿到 Packet，并按 graph（Node->Link）顺序调用 `Link.Transmit`。断言：两端 Flow 均处理 `cycles - latency` 个 Packet，`maxActive == 2`，总耗时显著小于串行估计。
- 运行命令：

```
go test ./internal/core/network -run TestNetworkNodesExchangePacketsThroughLink -timeout 5s
```

## 控制器测试

- 位置：`pkg/controller/controller_test.go`
- 测试方法：
  1. 使用正式 `network.Manager` + Mock Node/Link 构建 `ManagerBuilder`，验证 `SimulationController.Run` 的执行与错误处理。
  2. 断言重复 `Start` 会返回 `ErrAlreadyRunning`，未启动就 `Stop` 返回 `ErrNotRunning`，Stop 过程中上下文超时会正确传播。
- 运行命令：

```
go test ./pkg/controller -timeout 5s
```

# 跨cycle并行设计

## 跨Cycle并行网络流模拟设计方案总结

### 1. 基本实体定义

**Node (节点)**
- 处理单元，包含多个独立的Flow
- 每个Flow独立处理，避免互相阻塞

**Link (连接)**
- 双向通信通道
- 每个方向包含独立的in/out queue
- 固定延迟特性

**Flow (流)**
- **定义**: in queue → process → out queue 的完整处理流水线
- **特性**: 每个Flow独立并行推进

### 2. 并行化关键机制

**延迟容忍机制**
```
Node A → Link (延迟=L) → Node B
    ↓                      ↓
Flow A1 (Cycle N)      Flow B1 (可模拟到 N+L)
Flow A2 (Cycle M)      Flow B2 (可模拟到 M+L)
```

**Send Finished Cycle (SFC)**
- 发送方声明已完成发送的cycle边界
- 接收方可安全模拟到 `SFC + Link Delay`

**Queue状态预测**
- 基于带宽和队列容量预测未来状态
- 允许提前模拟多个cycle

### 3. 双向链接的并行优势

**传统阻塞方案**:
```
Node A ⇄ Node B
   ↓       ↓
互相等待 → 串行推进
```

**Flow-Based并行方案**:
```
Node A:
├─ Flow A1 (处理A→B): 推进到Cycle N
└─ Flow A2 (处理B→A): 推进到Cycle M

Node B:
├─ Flow B1 (处理A→B): 可推进到 N + Link_AB_Delay  
└─ Flow B2 (处理B→A): 可推进到 M + Link_BA_Delay
```

### 4. 并行推进算法 (Go实现)

```go
package main

import (
    "sync"
    "atomic"
)

// Flow 表示一个完整的处理流水线
type Flow struct {
    ID          int
    InQueue     *MessageQueue
    Process     ProcessLogic
    OutQueue    *MessageQueue
    LinkDelay   int
    CurrentCycle int64
    SendFinishedCycle int64
}

// MessageQueue 消息队列
type MessageQueue struct {
    messages   []Message
    capacity   int
    bandwidth  int // 每cycle处理的消息数
    mu         sync.RWMutex
}

// ProcessLogic 处理逻辑接口
type ProcessLogic interface {
    Advance(currentCycle int, maxAdvanceCycle int) []Message
}

// ParallelSimulator 并行模拟器
type ParallelSimulator struct {
    flows []*Flow
    wg    sync.WaitGroup
}

// AdvanceFlow 并行推进单个Flow
func (ps *ParallelSimulator) AdvanceFlow(flow *Flow, targetCycle int) {
    defer ps.wg.Done()
    
    for {
        current := atomic.LoadInt64(&flow.CurrentCycle)
        if current >= int64(targetCycle) {
            return
        }
        
        // 尝试推进一个cycle
        if flow.tryAdvanceOneCycle() {
            atomic.AddInt64(&flow.CurrentCycle, 1)
        } else {
            // 无法推进，等待依赖满足
            break
        }
    }
}

// tryAdvanceOneCycle 尝试推进一个cycle
func (f *Flow) tryAdvanceOneCycle() bool {
    // 1. 检查输入队列是否可以处理
    if !f.InQueue.CanAdvanceTo(f.CurrentCycle + 1) {
        return false
    }
    
    // 2. 从输入队列获取消息
    messages := f.InQueue.GetMessagesUpTo(f.CurrentCycle + 1)
    if len(messages) == 0 && !f.InQueue.HasCapacity() {
        return false
    }
    
    // 3. 处理逻辑
    processed := f.Process.Advance(int(f.CurrentCycle), int(f.CurrentCycle)+1)
    
    // 4. 检查输出队列容量
    if f.OutQueue.CanAccept(processed, int(f.CurrentCycle)+1) {
        f.OutQueue.ScheduleSend(processed, int(f.CurrentCycle)+1+f.LinkDelay)
        atomic.StoreInt64(&f.SendFinishedCycle, f.CurrentCycle+1)
        return true
    }
    
    return false
}

// ExecuteParallelAdvance 执行并行推进
func (ps *ParallelSimulator) ExecuteParallelAdvance(maxGlobalCycle int) {
    // 为每个Flow计算最大可推进cycle
    flowTargets := make([]int, len(ps.flows))
    for i, flow := range ps.flows {
        flowTargets[i] = flow.calculateMaxAdvanceCycle(maxGlobalCycle)
    }
    
    // 并行推进所有Flow
    ps.wg.Add(len(ps.flows))
    for i, flow := range ps.flows {
        go ps.AdvanceFlow(flow, flowTargets[i])
    }
    ps.wg.Wait()
}

// calculateMaxAdvanceCycle 计算Flow最大可推进cycle
func (f *Flow) calculateMaxAdvanceCycle(maxGlobalCycle int) int {
    // 基于Send Finished Cycle和链路延迟计算
    sfc := atomic.LoadInt64(&f.SendFinishedCycle)
    maxBySFC := int(sfc) + f.LinkDelay
    
    // 基于队列容量计算
    queueCapacity := f.InQueue.RemainingCapacity()
    maxByQueue := int(f.CurrentCycle) + queueCapacity/f.InQueue.bandwidth
    
    // 取最小值
    maxAdvance := min(maxBySFC, maxByQueue, maxGlobalCycle)
    return max(0, maxAdvance)
}

// 辅助函数
func min(a, b, c int) int {
    if a < b && a < c {
        return a
    }
    if b < c {
        return b
    }
    return c
}

func max(a, b int) int {
    if a > b {
        return a
    }
    return b
}
```

### 5. 并行性收益分析

**最佳情况** (无依赖拓扑):
```
Flow1: Node1→Node2 (延迟L1) - 可推进到 C1
Flow2: Node3→Node4 (延迟L2) - 可推进到 C2  
Flow3: Node5→Node6 (延迟L3) - 可推进到 C3
→ 完全并行，速度提升 ≈ 3x
```

**一般情况** (部分依赖):
```
Flow1: Node1→Node2 → Flow3: Node2→Node3
    独立推进窗口       依赖推进窗口
    [0, T1]          [L1, T1+L1+L3]
→ 部分重叠，仍有并行收益
```

这种Flow-Based并行化方案通过独立的in/out queue解耦双向通信，实现了细粒度的并行控制，特别适合现代多核处理器架构。