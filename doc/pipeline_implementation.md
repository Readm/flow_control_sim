# Pipeline 实现说明

## 数据流设计

Pipeline 的数据流只到 `Pick()`，处理后的数据包通过 `GetProcessedPackets()` 获取。

```
┌─────────────┐      ┌──────────┐      ┌──────────┐      ┌─────────────┐
│   Upstream  │─────▶│  inPort  │─────▶│ in_queue │─────▶│  Process    │
│    Link     │      │ (Channel)│      │  (Array) │      │  Packets    │
└─────────────┘      └──────────┘      └──────────┘      └─────────────┘
                                                                    │
                                                                    ▼
                                                          ┌─────────────┐
                                                          │  Pick()     │
                                                          │  (数据流终点)│
                                                          └─────────────┘
                                                                    │
                                                                    ▼
                                                          ┌─────────────┐
                                                          │GetProcessed │
                                                          │  Packets()  │
                                                          └─────────────┘
```

OutputQueue 是独立的组件，负责发送数据包到下游：

```
┌─────────────┐      ┌──────────┐      ┌──────────┐      ┌─────────────┐
│ Downstream  │◀─────│ outPort  │◀─────│out_queue │◀─────│ InjectPackets│
│    Link     │      │ (Channel)│      │  (Array) │      │  (外部注入)  │
└─────────────┘      └──────────┘      └──────────┘      └─────────────┘
```

## 核心流程

### Pipeline Tick 处理步骤

1. **接收数据包**：从 `inPort` channel 非阻塞接收，存储到 `in_queue` array
2. **提取数据包**：使用 `Pick()` 从 `in_queue` 取出所有可用数据包（FIFO 顺序）
3. **处理数据包**：默认实现是 FIFO 转发（不做任何修改）
4. **记录统计**：将数据包添加到 `processed` 列表（用于 `ProcessedCount()` 统计）
5. **存储处理结果**：处理后的数据包存储在 `lastCyclePackets` 中，可通过 `GetProcessedPackets()` 获取
6. **更新状态**：通知上游 ready

### OutputQueue Tick 处理步骤

1. **发送数据包**：
   - 从 `out_queue` 取出数据包（受 `outBandwidth` 限制）
   - 检查 `outPort.Ready(cycle)`
   - Ready：发送到 `outPort` channel
   - Not Ready：放回 `out_queue` array 排队
2. **更新状态**：设置 `outPort.Done`

## 关键特性

- **数据流分离**：Pipeline 只负责处理到 `Pick()`，OutputQueue 独立负责发送
- **无阻塞**：使用 array 存储，不会阻塞处理流程
- **自然保序**：`Pick()` 按 cycle 排序，保证 FIFO 顺序
- **带宽限制**：OutputQueue 的 `outBandwidth` 控制每 cycle 最大发送数
- **流控处理**：下游不 ready 时数据包在 `out_queue` 中排队等待

## 数据结构

### Pipeline
- `inQueue`: 接收缓冲队列（array 存储）
- `processed`: 统计列表（用于 `ProcessedCount()`）
- `lastCyclePackets`: 当前 cycle 处理的数据包（用于 `GetProcessedPackets()`）

### OutputQueue
- `queue`: 发送缓冲队列（array 存储，带宽限制）
- `outPort`: 下游输出端口

## 默认处理逻辑

`ProcessPackets` 默认实现是 **FIFO 转发**：从 `in_queue` 取出数据包，不做任何修改。处理后的数据包通过 `GetProcessedPackets()` 获取，然后可以通过 `OutputQueue.InjectPackets()` 注入到 OutputQueue 进行发送。

