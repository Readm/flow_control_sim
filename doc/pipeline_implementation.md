# Pipeline 实现说明

## 数据流设计

```
┌─────────────┐      ┌──────────┐      ┌──────────┐      ┌─────────────┐
│   Upstream  │─────▶│  inPort  │─────▶│ in_queue │─────▶│  Process    │
│    Link     │      │ (Channel)│      │  (Array) │      │  Packets    │
└─────────────┘      └──────────┘      └──────────┘      └─────────────┘
                                                                    │
                                                                    ▼
┌─────────────┐      ┌──────────┐      ┌──────────┐      ┌─────────────┐
│ Downstream  │◀─────│ outPort  │◀─────│out_queue │◀─────│  Forward   │
│    Link     │      │ (Channel)│      │  (Array) │      │  (FIFO)     │
└─────────────┘      └──────────┘      └──────────┘      └─────────────┘
```

## 核心流程

### Tick 处理步骤

1. **接收数据包**：从 `inPort` channel 非阻塞接收，存储到 `in_queue` array
2. **提取数据包**：使用 `Pick()` 从 `in_queue` 取出所有可用数据包（FIFO 顺序）
3. **处理数据包**：默认实现是 FIFO 转发（不做任何修改，直接转发）
4. **记录统计**：将数据包添加到 `processed` 列表（用于 `ProcessedCount()` 统计）
5. **存储到 out_queue**：直接存储到 `out_queue` array（如果满则丢弃）
6. **发送到下游**：
   - 从 `out_queue` 取出数据包（受 `outBandwidth` 限制）
   - 检查 `outPort.Ready(cycle)`
   - Ready：发送到 `outPort` channel
   - Not Ready：放回 `out_queue` array 排队
7. **更新状态**：设置 `outPort.Done` 并通知上游 ready

## 关键特性

- **无阻塞**：使用 array 存储，不会阻塞处理流程
- **自然保序**：`Pick()` 按 cycle 排序，保证 FIFO 顺序
- **带宽限制**：`outBandwidth` 控制每 cycle 最大发送数
- **流控处理**：下游不 ready 时数据包在 `out_queue` 中排队等待

## 数据结构

- `inQueue`: 接收缓冲队列（array 存储）
- `outQueue`: 发送缓冲队列（array 存储，带宽限制）
- `processed`: 统计列表（用于 `ProcessedCount()`）

## 默认处理逻辑

`ProcessPackets` 默认实现是 **FIFO 转发**：从 `in_queue` 取出数据包，不做任何修改，直接转发到 `out_queue`。适用于纯转发场景。

