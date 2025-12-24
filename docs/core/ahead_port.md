# AheadPort 同步机制

`AheadPort` 是 `flow_sim` 的核心同步原语，位于 `internal/core/ahead_port`。它提供了一种基于周期的、高效的异步同步机制，连接不同的仿真组件（如 Node, Link）。

## 核心接口

为了保证类型安全和清晰的职责划分，`AheadPort` 被划分为两个视图：

### 1. InPort (上游/发送方视角)

上游组件（如 OutputQueue 或 Link 的输出端）使用此接口发送数据。

```go
type InPort interface {
    // [标准 API]
    // TrySend 尝试向发送一个数据包。
    // 该调用会阻塞，直到下游组件确定了其就绪状态 (Ready)。
    // 返回 true 表示发送成功，false 表示下游该周期不就绪。
    TrySend(cycle int, pkt PacketWithCycle) bool

    // MarkDone 标记上游已完成指定周期的处理。
    // 这通知下游可以安全读取该周期及其之前的所有数据。
    MarkDone(cycle int)

    // [高级 API]
    // PeekReady 非阻塞地检查下游是否就绪。
    PeekReady(cycle int) (ready bool, decided bool)
    // IsReady 阻塞直到下游确定就绪状态。
    IsReady(cycle int) bool
}
```

### 2. OutPort (下游/接收方视角)

下游组件（如 InputQueue 或 Link 的输入端）使用此接口接收数据。

```go
type OutPort interface {
    // [标准 API]
    // Receive 获取指定周期的所有数据包。
    // 这是一个阻塞调用，内部会自动等待上游发出 MarkDone(cycle) 信号。
    Receive(cycle int) []packet.Packet

    // UpdateReady 更新下游在指定周期是否就绪。
    // 必须在每个周期尽早调用，以解除上游 TrySend 的阻塞。
    UpdateReady(cycle int, ready bool)

    // [高级 API]
    // WaitDone 阻塞直到上游完成指定周期。
    WaitDone(cycle int)
    // PeekDone 非阻塞地获取上游已完成的最高周期。
    PeekDone() int
}
```

## 设计原景

### 1. 独立实体 (Independent Entity)
Port 既不属于上游也不属于下游，它是两者之间的连接桥梁。一个 `AheadPort` 实例同时实现了 `InPort` 和 `OutPort` 接口，通过 `AsInPort()` 和 `AsOutPort()` 进行类型安全的转换。

### 2. 同步逻辑中心化
所有的线程同步（等待、通知、反压）都封装在 `AheadPort` 及其内部的 `ComponentSync` 中。组件本身（Node, Link）只需要关注自身的业务逻辑（如路由、延迟）。

### 3. 数据一致性保证
- **阻塞式接收**: `Receive(cycle)` 确保在读取数据前，上游已经显式地调用了 `MarkDone(cycle)`。
- **防止漂移**: 通过 `drainChannel` 机制，确保即便是异步环境下，所有属于 Cycle N 的包都能被准确捕获，不会丢失在 Channel 中。

## 执行时序

典型的组件执行循环如下：

1. **Wait/Receive**: 下游调用 `OutPort.Receive(cycle)` 获取输入。
2. **Process**: 组件根据输入进行逻辑处理。
3. **Check Ready**: 组件计算下一周期的存储/处理余量。
4. **Update Ready**: 下游调用 `OutPort.UpdateReady(cycle+1, ready)`。
5. **Send**: 上游尝试调用 `InPort.TrySend(cycle+latency, pkt)`。
6. **Mark Done**: 上游处理完当前周期，调用 `InPort.MarkDone(cycle)`。

## 避免死锁

为了避免全系统的循环等待死锁：
- **顺序一致性**: 调度器通常会先运行 Link 增加数据的流动性。
- **提前 Ready**: 接收方应尽可能早地通过 `UpdateReady` 告知上游其状态，不必等逻辑全部跑完。
