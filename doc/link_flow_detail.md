# Link 反压与跨 Cycle 并行实现总结

## 接口定义

### Link 接口 (`internal/core/link/link.go`)

#### 新增字段
- `currentCycle uint64` - Link 的内部 cycle 计数器（只有 Link 写）
- `sendFinishedCycle uint64` - SFC：Link 已准备好发送到哪个 cycle（只有 Link 写）
- `noBackpressureUntil uint64` - 接收方告知：到哪个 cycle 之前不会反压（只有 Flow 写）

#### 新增方法
- `CurrentCycle() uint64` - 获取当前内部 cycle（只读）
- `SendFinishedCycle() uint64` - 获取 SFC（只读）
- `SetSendFinishedCycle(cycle uint64)` - 设置 SFC（只有 Link 写）
- `SetNoBackpressureUntil(cycle uint64)` - 接收方告知不会反压到哪个 cycle（只有 Flow 写）
- `NoBackpressureUntil() uint64` - 获取不会反压的 cycle 边界（只读）
- `ReadFromFlow(f flow.Flow)` - 从 Flow out_queue 读取数据（基于 Flow SFC）

#### 修改的方法
- `Transmit(cycle uint64, pkt packet.Packet)` - 优化路径：如果 `noBackpressureUntil >= targetCycle`，直接发送到 channel；否则使用 ring buffer
- `Advance(cycle uint64)` - 只处理 ring buffer 路径的 packet，反压时暂停 cycle 计数

### Flow 接口 (`internal/dataflow/flow/flow.go`)

#### 新增字段
- `currentCycle uint64` - 当前推进到的 cycle（只有 Flow 写）
- `outQueueSendFinishedCycle uint64` - out_queue 的 SFC（供 Link 读取，只有 Flow 写）
- `noBackpressureUntil uint64` - 告知上游：到哪个 cycle 之前不会反压（只有 Flow 写）
- `upstreamLink interface{}` - 上游 Link（用于告知反压信号）

#### 新增方法
- `CurrentCycle() uint64` - 获取 Flow 当前推进到的 cycle（只读）
- `OutQueueSendFinishedCycle() uint64` - 获取 out_queue 的 SFC（只读）
- `SetNoBackpressureUntil(cycle uint64)` - 告知上游 Link：到哪个 cycle 之前不会反压（只有 Flow 写）
- `NoBackpressureUntil() uint64` - 获取不会反压的 cycle 边界（只读）
- `AdvanceTo(cycle uint64, linkSFC uint64) error` - 推进到指定 cycle（需要 Link SFC 作为参数）
- `SetUpstreamLink(link interface{})` - 设置上游 Link（用于告知反压信号）

#### 修改的方法
- `Tick(ctx context.Context, cycle uint64) error` - 更新 `currentCycle`
- `Emit(pkts ...packet.Packet)` - 更新 `outQueueSendFinishedCycle`

## 核心机制

### 1. SFC (Send Finished Cycle) 机制
- **Link SFC**：Link 已准备好发送到哪个 cycle 的所有数据
- **Flow out_queue SFC**：Flow 已准备好发送到哪个 cycle 的所有数据
- Flow 执行条件：`currentCycle <= Link.SFC`
- Link 读取条件：`currentCycle <= Flow.out_queue.SFC`

### 2. 反压信号机制
- Flow 的 in_queue 计算 `noBackpressureUntil`（基于容量和带宽）
- Flow 告知上游 Link："我在 cycle K 之前不会反压"
- Link 根据 `noBackpressureUntil` 决定可以安全发送到哪个 cycle
- **重要**：`noBackpressureUntil` 只是说前面不会有反压，但不意味着后面一定有反压

### 3. 直接发送路径优化
- 如果 `noBackpressureUntil >= currentCycle + latency`，直接发送到 channel
- 不需要经过 ring buffer，减少延迟和内存开销
- 如果 channel 满了，fallback 到 ring buffer 路径

### 4. Ring Buffer 路径（有反压风险）
- 如果 `noBackpressureUntil < currentCycle + latency`，使用 ring buffer 缓冲
- 反压时：`currentCycle` 不增加，ring buffer 指针不移动，所有 packet 保持在原 slot
- 不反压时：`currentCycle` 增加，ring buffer 指针移动，处理对应 slot 的 packet

### 5. 无锁设计
- cycle 都是递增的，不需要锁保护
- 只有一方写：
  - `currentCycle`：只有 Link/Flow 自己写
  - `sendFinishedCycle`：只有 Link 写
  - `outQueueSendFinishedCycle`：只有 Flow 写
  - `noBackpressureUntil`：只有 Flow 写，Link 只读

## 测试覆盖

### Link 单元测试 (`internal/core/link/link_test.go`)

1. **TestLinkBasicFunctionality** - 基础功能：固定延迟后 packet 从入口传播到出口
2. **TestLinkRingBufferMechanism** - Ring Buffer 机制：验证 packet 在正确的 slot 中
3. **TestLinkSFC** - SFC 机制：验证 Link SFC 正确更新
4. **TestLinkBackpressurePausesCycle** - 反压暂停 Cycle：验证反压时 cycle 不增加
5. **TestLinkDirectSendPath** - 直接发送路径：验证优化路径工作正常
6. **TestLinkRingBufferPath** - Ring Buffer 路径：验证有反压风险时使用 ring buffer
7. **TestLinkReadFromFlow** - 从 Flow 读取：验证 Link 可以基于 Flow SFC 读取数据
8. **TestLinkMultiplePackets** - 多个 Packet 处理：验证多个 packet 按顺序处理

### 跨 Cycle 并行测试 (`internal/core/parallel_test.go`)

1. **TestIndependentFlowParallelAdvance** - 独立 Flow 并行推进：验证多个 Flow 可以独立推进到不同 cycle
2. **TestBidirectionalLinkParallel** - 双向 Link 并行：验证双向通信可以并行推进
3. **TestSFCBasedAdvance** - 基于 SFC 的推进：验证 Flow 基于 SFC + Link Delay 计算可推进的最大 cycle
4. **TestBackpressureSignalMechanism** - 反压信号机制：验证 Flow 计算并告知 Link noBackpressureUntil
5. **TestBackpressureParallel** - 反压并行：验证一个 Link 反压时，其他 Link 仍可正常推进

## 测试结果

所有测试通过：
- Link 单元测试：8/8 通过
- 跨 Cycle 并行测试：5/5 通过

## 关键设计决策

1. **使用 interface{} 避免循环依赖**：Flow 的 `upstreamLink` 使用 `interface{}`，通过类型断言调用 Link 的方法
2. **直接发送路径优化**：当接收方保证不会反压到足够远的 cycle 时，直接发送，减少延迟
3. **反压时暂停 cycle 计数**：反压时 ring buffer 指针不移动，所有 packet 保持在原 slot
4. **SFC 机制**：允许 Flow 和 Link 独立推进，基于 SFC 计算可推进的最大 cycle
5. **无锁设计**：cycle 递增特性保证无锁并发安全

## 使用示例

```go
// 创建 Flow 和 Link
f := flow.NewFIFO(1, 8, 0, 0)
l := link.NewLink(0, f, 2, 0)

// 设置反压信号
f.SetUpstreamLink(l)
l.SetNoBackpressureUntil(10)

// 发送 packet
pkt := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}
l.Transmit(0, pkt)

// Flow 推进（基于 Link SFC）
linkSFC := l.SendFinishedCycle()
f.AdvanceTo(5, linkSFC+10)

// Link 从 Flow 读取
l.ReadFromFlow(f)
```

