# Link 实现细节 (Link Implementation Details)

`Link` 是连接仿真节点的物理通道建模，负责处理延迟、带宽和反压。

## 核心结构 (`internal/core/link/link.go`)

`Link` 采用**策略模式 (Strategy Pattern)**，将核心的包处理逻辑委托给 `LinkHandler`：

```go
type Link struct {
    handler      LinkHandler
    latency      int
    bandwidth    int
    fromUpstream ahead_port.OutPort
    toDownstream ahead_port.InPort
}
```

## LinkHandler 策略

目前支持两种主要的链路行为：

### 1. BufferedLinkHandler (带缓冲链路)
- **行为**: 模拟具有固定延迟和有限缓冲的链路。
- **内部实现**: 使用一个循环缓冲区（Ring Buffer）来存储在途（In-flight）的数据包。
- **反压**: 当内部缓冲区满时，通过执行 `UpdateUpstreamReady(cycle, false)` 向上传递反压信号。
- **延迟控制**: 数据包根据其 `TargetCycle` 在内部队列中移动，确保经过 `Latency` 个周期后才输出。

### 2. BufferlessLinkHandler (无缓冲链路)
- **行为**: 用于逻辑连接或零缓冲透传。
- **反压**: 始终向上游报告 `Ready`。
- **特点**: 不占用物理内存，适合高性能拓扑（如 Bufferless Router 的内部连接）。

## 通信与同步流程 (以 Buffered 为例)

1. **上游接收 (Phase 1)**:
   - `Link.Tick(cycle)` 会计算 `waitCycle = cycle - latency`。
   - 调用 `fromUpstream.Receive(waitCycle)`。
2. **逻辑处理 (Phase 2)**:
   - 将收到的数据推入 `LinkHandler`。
   - Handler 检查下游是否就绪 (`toDownstream.IsReady(cycle)`)。
   - 如果就绪且有到期包，通过 `toDownstream.TrySend(cycle, pkt)` 发送。
3. **完成信号 (Phase 3)**:
   - 调用 `toDownstream.MarkDone(cycle)`，解锁下游。
4. **反压回信**:
   - Link 计算下一周期是否有空余 Slot。
   - 调用 `UpdateUpstreamReady(cycle+1, ready)`。

## 反压机制 (Backpressure)

反压是基于 `AheadPort` 的 `UpdateReady` 建立的：
1. **下传**: Link 会阻塞在 `toDownstream.TrySend`，如果下游节点不 Ready。
2. **缓冲**: 在阻塞期间，Link 可以继续从上游收包并放入内部 Buffer。
3. **上传**: 当 Link 的内部 Buffer 达到阈值时，它会停止调用 `UpdateUpstreamReady(cycle, true)`，上游节点继而会在其 `TrySend` 处阻塞。

## 这种设计的目的
- **周期精确**: 强制数据必须经历 `Latency` 的等待。
- **解耦延迟与带宽**: 只有在 `BufferedLinkHandler` 中才严格计算 Slot，简化了核心 `Link` 结构。
- **高性能**: 支持无锁快速路径（如果 Buffer 足够或无缓冲）。

