# 核心实体设计 (Core Entity Design)

## 设计目标
- **解耦**: 通过 `AheadPort` 接口解耦组件间的直接依赖。
- **并发性**: 在驱动 Cycle 推进时，支持各节点并行更新，提高仿真效率。
- **确定性**: 无论执行顺序如何，通过 `AheadPort` 的阻塞等待机制保证每个 Cycle 的数据一致性。

## 核心组件

### 1. Network (`internal/core/network`)
Network 是仿真的“心脏”，负责：
- **拓扑构建**: 持有 `NodeHandle` 列表，管理所有的节点。
- **连接建立**: 提供 `Connect` 方法，在两个节点间建立 `Link` 并分配 `AheadPort`。
- **周期调度**: 通过 `AdvanceTo(targetCycle)` 驱动全系统推向目标周期。

### 2. Node (`internal/core/node`)
仿真中的主动更新逻辑实体：
- **接口**: 定义了 `Tick(cycle int) error`。
- **组成**: 通常包含输入队列 (`InputQueue`)、输出队列 (`OutputQueue`) 以及业务逻辑（如 `Router`）。
- **同步**: 调用 `InputQueue.Receive(cycle)` 获取输入，在处理完后计算反压并调用 `UpdateReady`。

### 3. Link (`internal/core/link`)
仿真中的时序延迟实体：
- **职责**: 模拟物理链路的延迟 (`latency`) 和带宽限制 (`bandwidth`)。
- **更新**: `Link` 也实现 `Tick(cycle int) error`。它从上游 Port 获取数据，放入内部的延迟管道，并在到期后推送到下游 Port。

### 4. AheadPort (`internal/core/ahead_port`)
组件间通信的唯一通道：
- **双向视图**: 提供 `InPort` (发送方) 和 `OutPort` (接收方) 两个受限界面。
- **同步原语**: 封装了 `ComponentSync`，处理 `MarkDone` 和 `Ready` 信号的阻塞等待逻辑。

## 接口定义示例

```go
// Node 接口
type Node interface {
    ID() int
    Tick(cycle int) error
}

// Network 管理器
type Network struct {
    nodes []*NodeHandle
    // ... Internal state
}

func (n *Network) AdvanceTo(targetCycle int) error {
    // 循环推进直到 targetCycle
    // 每个 cycle 中：
    // 1. 并行执行所有 Link.Tick
    // 2. 并行执行所有 Node.Tick
}
```

## 执行流程 (Cycle N)

1. **Link Update**: 所有 `Link` 先行更新。它们将 N-Latency 周期的包提取出来并打给下游节点。
2. **Node Update**: 所有 `Node` 并行执行 `Tick(N)`。
   - 节点从其 `InputQueue` 调用 `Receive(N)`（此时 Link 已完成 N 周期发送，数据就绪）。
   - 节点进行逻辑运算（如路由选择）。
   - 节点尝试通过 `OutputQueue` 的 `TrySend(N)` 发送数据到链路。
3. **Synchronization**: `AheadPort` 确保：
   - 如果 Link 还没发包，Node 的 `Receive` 会阻塞。
   - 如果 Node 还没准备好接收，Link 的 `TrySend` 会阻塞。

## 并行化收益
由于 `AheadPort` 内部使用了高效的无锁快速路径和 Goroutine 同步，Network 可以安全地在单独的 Goroutine 中运行每个节点，极大地利用了多核 CPU 的计算能力。详见 [AheadPort 设计](../design/ahead_port.md)。