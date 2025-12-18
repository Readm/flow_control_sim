# 强大的异步同步机制 (Robust Asynchronous Synchronization)

本文档介绍了 `flow_sim` 中实现的高可靠异步同步机制，以及用于验证该机制稳健性的测试用例。

## 1. 设计背景

在异步仿真模型中，不同组件（Node, Link）运行在不同的 goroutine 中，且前进速度不一致。为了保证仿真的确定性和数据完整性，必须解决以下挑战：
- **时间漂移 (Sync Drift)**：发送方完成周期 `N` 并标记 `MarkDone`，但由于下游缓冲区满或调度延迟，实际数据包在较晚的时间才到达。
- **丢包风险**：非阻塞的接收逻辑可能在数据包到达前就认为该周期已结束。
- **确定性保证**：无论组件运行速度如何，特定周期 `N` 的输入包集合必须是确定的。

## 2. 核心机制

为了解决上述问题，我们将同步逻辑集中在 `Port` 组件中，并利用 `ComponentSync` 原语实现了以下机制：

### 2.1 阻塞式接收 (Blocking Receive)
`Port.Receive(cycle)` 不再是简单的非阻塞读取，而是一个复合的阻塞过程：
1. **预排空 (Pre-drain)**：将当前 Channel 中所有可用的包（可能是未来的包）读入内部 `pendingPackets` 缓存。
2. **等待标记 (Wait Done)**：调用 `upstreamSync.WaitDone(cycle)`，阻塞当前 goroutine，直到上游明确调用了 `MarkDone(cycle)`。
3. **后排空 (Post-drain)**：在上游标记完成后，再次执行 Channel 排空，确保在 `MarkDone` 调用前一刻发送的包也被捕获。
4. **确定性返回**：从缓存中提取并返回属于该周期的所有包。

### 2.2 阻塞式就绪检查 (Blocking Ready)
`Port.IsReady(cycle)` 会导致发送方阻塞，直到下游组件通过 `UpdateReady(cycle, bool)` 明确了其就绪状态。
- 这一机制强制发送方在下游未准备好时“原地等待”，防止发送方跑得太快导致数据在网络中过度堆积或因为逻辑错误而丢失。

### 2.3 零丢包保证 (Zero-Drop Guarantee)
在异步模式下，`Link` 和 `OutputQueue` 强制使用阻塞式的 `TrySend`。这意味着：
- **无静默丢包**：如果下游不可用且没有缓冲区，发送方将阻塞。
- **协议闭环**：`MarkDone` 只能在所有数据成功递交给 `Port`（或存入 `Port` 缓存）后发送。

## 3. 稳健性验证 (Robustness Tests)

我们在 `internal/core/ahead_port/sync_robustness_test.go` 中实现了专门的压力测试，模拟极端的同步挑战：

### 3.1 异步漂移测试 (`TestAsynchronousDrift`)
- **场景**：上游节点以极速瞬间跑完 100 个周期，而下游节点每周期人为增加 2ms 延迟。
- **验证**：确保 100 个周期的包能够正确累积在 `Port` 的 `pendingPackets` 缓存中，且下游在延迟到达对应周期时，能 100% 正确读取属于该周期的包。

### 3.2 协议违规容错测试 (`TestProtocolViolation_LateReceive`)
- **场景**：下游组件跳过 `WaitUpstreamDone` 直接调用 `Receive`，或者上游数据发送极度滞后。
- **验证**：验证 `Receive` 内部的阻塞逻辑是否能代为完成同步等待，确保不会因为调用顺序的微小偏差导致读不到数据。

### 3.3 死锁弹性测试 (`TestBackpressureDeadlockResilience`)
- **场景**：人为缩小 `Port` 的 Channel 容量（如仅为 4），并让发送方连续发送远超容量的包，而接收方处于忙碌状态。
- **验证**：验证发送方在 Channel 满时阻塞，而接收方苏醒后能平滑排空 Channel 并解锁发送方，确保在大规模突发流量下系统不会由于循环等待而死锁。

## 4. 结论

通过引入“阻塞-排空-缓存”三位一体的同步逻辑，`flow_sim` 能够在维持高并发性能的同时，提供等同于同步锁步仿真的确定性保证。
