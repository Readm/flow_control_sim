<!-- 7d4857ef-f23d-48df-9683-70b27d3bf9cc 26eafd32-3058-4c21-96dd-8f4dac039f1a -->
# Channel Pipeline Slots 与背压机制实现计划

## 核心设计理解

### 1. Pipeline Slot 模型

- **每个 Edge（fromID->toID）有独立的 pipeline**
- **Pipeline 结构**：latency 个 slot，每个 slot 容量 = bandwidthLimit
- **Slot 索引**：Slot[0] 是第一个 stage（刚发送），Slot[latency-1] 是最后一个 stage（即将到达）
- **Ring Buffer 实现**：使用固定大小的 slice，通过索引偏移实现循环

### 2. 工作流程（每个 Tick）

对于每个 Edge 的 Pipeline：

1. **Slot[0] 的 packets 尝试到达接收端**

   - 调用接收端的 `CanReceive(edgeKey)` 或 `TryReceive(edgeKey, packets)` 方法
   - 如果接收端可以接收：packets 被接收，Slot[0] 清空
   - 如果接收端无法接收：**背压触发**，Slot[0] 保持，后续步骤暂停

2. **如果 Slot[0] 成功清空（或本来就空）**：

   - Slot[1] 的 packets → Slot[0]
   - Slot[2] 的 packets → Slot[1]
   - ...
   - Slot[latency-1] 的 packets → Slot[latency-2]
   - 新的 packets 尝试进入 Slot[latency-1]（如果带宽允许）

3. **如果 Slot[0] 无法清空（背压）**：

   - **所有 slot 保持不动**，packets 不移动
   - 新的 packets 无法进入 pipeline（发送端阻塞）

### 3. 背压机制

- **触发条件**：接收端无法接收 Slot[0] 的 packets
- **影响范围**：整个 Edge 的 pipeline 暂停
- **表现形式**：
  - Slot[0] 保持有 packets
  - 所有 slot 的 packets 不移动
  - 新的发送请求被阻塞或排队

### 4. 接收端接口

需要在 Node 接口中添加方法：

```go
// CanReceive 检查节点是否可以接收来自指定 edge 的 packets
CanReceive(edgeKey EdgeKey, packetCount int) bool

// 或者更主动的方式：
// TryReceive 尝试接收 packets，返回实际接收的数量
TryReceive(edgeKey EdgeKey, packets []*Packet) int
```

### 5. 带宽限制

- **发送端限制**：每个 cycle，Slot[latency-1] 最多接收 bandwidthLimit 个新 packets
- **如果超过限制**：多余的 packets 排队等待（发送队列）

## 实现细节

### Channel 结构

```go
type Channel struct {
    bandwidthLimit int
    pipelines map[EdgeKey]*Pipeline
}

type Pipeline struct {
    latency int
    slots []Slot  // 固定大小：latency 个 slot
    sendQueue []pendingPacket  // 发送队列（超过带宽限制的 packets）
}

type Slot struct {
    packets []*InFlightMessage
    capacity int  // 等于 bandwidthLimit
}
```

### EdgeKey 结构

```go
type EdgeKey struct {
    FromID int
    ToID   int
}
```

### 核心方法

#### Channel.Tick(cycle int)

```go
func (c *Channel) Tick(cycle int) {
    for edgeKey, pipeline := range c.pipelines {
        // 1. 尝试接收 Slot[0] 的 packets
        if len(pipeline.slots[0].packets) > 0 {
            // 获取接收端节点
            receiver := getReceiverNode(edgeKey.ToID)
            
            // 检查是否可以接收
            if receiver.CanReceive(edgeKey, len(pipeline.slots[0].packets)) {
                // 接收 packets
                arrivals := pipeline.slots[0].packets
                pipeline.slots[0].packets = nil
                receiver.OnPackets(arrivals, cycle)
                
                // 2. 移动所有 slot（向前移动）
                pipeline.advanceSlots()
                
                // 3. 从发送队列填充 Slot[latency-1]
                pipeline.fillFromQueue(cycle)
            } else {
                // 背压：不移动，不接收新 packets
                // 所有 slot 保持不动
            }
        } else {
            // Slot[0] 是空的，正常移动
            pipeline.advanceSlots()
            pipeline.fillFromQueue(cycle)
        }
    }
}
```

#### Channel.Send(packet, fromID, toID, currentCycle, latency)

```go
func (c *Channel) Send(...) {
    edgeKey := EdgeKey{FromID: fromID, ToID: toID}
    pipeline := c.getOrCreatePipeline(edgeKey, latency)
    
    // 检查 Slot[latency-1] 是否有空间
    lastSlot := pipeline.slots[latency-1]
    if len(lastSlot.packets) < lastSlot.capacity {
        // 直接进入 pipeline
        lastSlot.packets = append(lastSlot.packets, msg)
    } else {
        // 加入发送队列
        pipeline.sendQueue = append(pipeline.sendQueue, pendingPacket{...})
    }
}
```

### 接收端实现（Node 接口）

需要在每个 Node 类型（Master/Relay/Slave）中实现：

```go
func (n *HomeNode) CanReceive(edgeKey EdgeKey, packetCount int) bool {
    // 检查是否可以接收
    // 例如：检查队列容量、处理能力等
    return len(n.queue) + packetCount <= n.maxCapacity
}

func (n *HomeNode) OnPackets(messages []*InFlightMessage, cycle int) {
    for _, msg := range messages {
        msg.Packet.ReceivedAt = cycle
        n.OnPacket(msg.Packet, cycle, ...)
    }
}
```

## 可视化支持

### Pipeline 状态获取

```go
func (c *Channel) GetPipelineState(cycle int) map[EdgeKey][]PipelineStageInfo {
    result := make(map[EdgeKey][]PipelineStageInfo)
    for edgeKey, pipeline := range c.pipelines {
        stages := make([]PipelineStageInfo, pipeline.latency)
        for i, slot := range pipeline.slots {
            stages[i] = PipelineStageInfo{
                StageIndex: i,
                PacketCount: len(slot.packets),
            }
        }
        result[edgeKey] = stages
    }
    return result
}
```

### EdgeSnapshot 扩展

```go
type EdgeSnapshot struct {
    Source        int
    Target        int
    Label         string
    Latency       int
    PipelineStages []PipelineStageInfo
}

type PipelineStageInfo struct {
    StageIndex int  // 0 到 latency-1
    PacketCount int
}
```

## 关键问题确认

1. **接收端处理能力**：

   - 每个节点每个 cycle 可以接收多少个 packets？
   - 是固定值还是动态计算？
   - 是否需要考虑不同 edge 的优先级？

2. **背压传播**：

   - 如果 Slot[0] 被阻塞，发送端是否立即感知？
   - 是否需要额外的信号机制？

3. **Slot 容量**：

   - 每个 slot 的容量 = bandwidthLimit？
   - 还是可以有更大的容量（允许多个 packets 在同一 stage）？

请确认以上理解是否正确，以及需要澄清的问题。