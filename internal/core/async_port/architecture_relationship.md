# 架构关系梳理

## 当前架构关系图

```mermaid
---
config:
  layout: dagre
---
flowchart TB
    subgraph Interfaces["接口层 (Interfaces)"]
        CyclePort["CyclePort<br/>Cycle 同步端口接口"]
        PacketProcessor["PacketProcessor<br/>包处理策略接口"]
    end
    
    subgraph Implementations["实现层 (Implementations)"]
        CyclePortImpl["CyclePortImpl<br/>实现 CyclePort<br/>提供同步机制"]
        DefaultProcessor["DefaultProcessor<br/>实现 PacketProcessor<br/>FIFO 策略"]
        FIFOFlowProcessor["FIFOFlowProcessor<br/>实现 PacketProcessor<br/>嵌入 DefaultProcessor"]
        PriorityFlowProcessor["PriorityFlowProcessor<br/>实现 PacketProcessor<br/>嵌入 DefaultProcessor"]
    end
    
    subgraph Coordinator["协调层 (Coordinator)"]
        CycleProcessor["CycleProcessor<br/>协调 CyclePort 和 Processor"]
    end
    
    subgraph Types["类型别名"]
        PacketWithCycle["PacketWithCycle<br/>= packet.PacketWithCycle"]
    end
    
    CyclePort -->|实现| CyclePortImpl
    PacketProcessor -->|实现| DefaultProcessor
    PacketProcessor -->|实现| FIFOFlowProcessor
    PacketProcessor -->|实现| PriorityFlowProcessor
    
    DefaultProcessor -.->|嵌入| FIFOFlowProcessor
    DefaultProcessor -.->|嵌入| PriorityFlowProcessor
    
    CycleProcessor -->|持有| CyclePort
    CycleProcessor -->|持有| PacketProcessor
    CycleProcessor -->|使用| PacketWithCycle
    
    style CyclePort fill:#E6F3FF
    style PacketProcessor fill:#E6F3FF
    style CyclePortImpl fill:#FFF4E6
    style DefaultProcessor fill:#FFF4E6
    style CycleProcessor fill:#FFE6E6
    style PacketWithCycle fill:#E6FFE6
```

---

## 详细关系说明

### 1. 接口层

#### CyclePort (接口)
- **职责**: 定义基于 cycle 的双向同步通信协议
- **方法**:
  - 上游操作: `SetDoneUntil()`, `Chan()`, `Ready()`, `ReadyNonBlocking()`, `GetDoneUntil()`
  - 下游操作: `ReceiveChan()`, `WaitForDoneUntil()`

#### PacketProcessor (接口)
- **职责**: 定义包处理策略
- **方法**: `ProcessPackets(...)`
- **状态**: 需要存储 `pendingPackets`（因此是接口而不是函数类型）

### 2. 实现层

#### CyclePortImpl (结构体)
- **实现**: `CyclePort` 接口
- **职责**: 提供基于 cycle 的同步通信的具体实现
  - 管理 `doneUntil`（上游完成状态）
  - 管理 `readyUntil` 和 `readyMap`（下游就绪状态）
  - 提供 channel 进行数据传输
  - 提供阻塞/唤醒机制（condition variable）

#### DefaultProcessor (结构体)
- **实现**: `PacketProcessor` 接口
- **职责**: 提供默认的 FIFO 包处理策略
- **状态**: `pendingPackets []PacketWithCycle`（存储未发送的包）

#### FIFOFlowProcessor (结构体)
- **实现**: `PacketProcessor` 接口
- **嵌入**: `*DefaultProcessor`
- **职责**: 在默认处理基础上添加日志

#### PriorityFlowProcessor (结构体)
- **实现**: `PacketProcessor` 接口
- **嵌入**: `*DefaultProcessor`
- **职责**: 优先级处理（当前实现不完整）

### 3. 协调层

#### CycleProcessor (结构体)
- **职责**: 协调 CyclePort 和 Processor
- **持有**:
  - `upstreamPort CyclePort` - 上游端口
  - `downstreamPort CyclePort` - 下游端口
  - `processor PacketProcessor` - 包处理器
- **方法**: `ProcessCycle(cycle int)` - 执行一个 cycle 的处理流程

### 4. 类型别名

#### PacketWithCycle
- **类型**: `packet.PacketWithCycle` 的别名
- **用途**: 表示带 cycle 信息的包

---

## 使用流程

### 创建和使用示例

```go
// 1. 创建端口（实现 CyclePort）
upstreamPort := NewCyclePort(8)      // 返回 *CyclePortImpl，实现 CyclePort
downstreamPort := NewCyclePort(8)    // 返回 *CyclePortImpl，实现 CyclePort

// 2. 创建处理器（实现 PacketProcessor，可选）
processor := &DefaultProcessor{}  // 或 nil（使用默认）

// 3. 创建协调器
cp := NewCycleProcessor(upstreamPort, downstreamPort, processor)

// 4. 处理周期
for cycle := 0; cycle < 10; cycle++ {
    cp.ProcessCycle(cycle)
}
```

### 数据流

```
上游组件
    ↓ SetDoneUntil, Chan(), Ready()
CyclePort (接口)
    ↓ 实现
CyclePortImpl (结构体)
    ↓ packetChan
CycleProcessor
    ↓ ReceiveChan(), ProcessPackets()
PacketProcessor (接口)
    ↓ 实现
DefaultProcessor (结构体)
    ↓ sendPacket()
CyclePort (接口)
    ↓ 实现
CyclePortImpl (结构体)
    ↓ packetChan
下游组件
```

---

## 命名问题总结

### 已解决的问题

重命名已完成：
- ✅ `ASyncPort` → `CyclePort` - 明确表示基于 cycle 的同步端口
- ✅ `Port` → `CyclePortImpl` - 明确是 CyclePort 的实现
- ✅ `NewPort` → `NewCyclePort` - 与类型名一致
- ✅ `packet.Envelope` → `packet.PacketWithCycle` - 命名更清晰

### 命名一致性

- ✅ `CyclePort` (Interface) + `CyclePortImpl` (Struct) - 关系清晰
- ✅ `PacketProcessor` (Interface) + `DefaultProcessor` (Struct) - 关系清晰

---

## 改进后的架构关系（推荐）

```mermaid
---
config:
  layout: dagre
---
flowchart TB
    subgraph Interfaces["接口层 (Interfaces)"]
        CyclePort["CyclePort<br/>Cycle 同步端口接口"]
        PacketProcessor["PacketProcessor<br/>包处理策略接口"]
    end
    
    subgraph Implementations["实现层 (Implementations)"]
        CyclePortImpl["CyclePortImpl<br/>实现 CyclePort"]
        DefaultProcessor["DefaultProcessor<br/>实现 PacketProcessor"]
        FIFOFlowProcessor["FIFOFlowProcessor<br/>实现 PacketProcessor"]
    end
    
    subgraph Coordinator["协调层 (Coordinator)"]
        CycleProcessor["CycleProcessor<br/>协调 CyclePort 和 Processor"]
    end
    
    CyclePort -->|实现| CyclePortImpl
    PacketProcessor -->|实现| DefaultProcessor
    PacketProcessor -->|实现| FIFOFlowProcessor
    
    DefaultProcessor -.->|嵌入| FIFOFlowProcessor
    
    CycleProcessor -->|持有| CyclePort
    CycleProcessor -->|持有| PacketProcessor
    
    style CyclePort fill:#E6F3FF
    style PacketProcessor fill:#E6F3FF
    style CyclePortImpl fill:#FFF4E6
    style DefaultProcessor fill:#FFF4E6
    style CycleProcessor fill:#FFE6E6
```

### 改进后的命名

| 当前 | 推荐 | 理由 |
|------|------|------|
| `ASyncPort` | `CyclePort` | 明确表示基于 cycle 的同步 |
| `Port` | `CyclePortImpl` | 明确是 CyclePort 的实现 |
| `NewPort` | `NewCyclePort` | 与类型名一致 |
| `PacketProcessor` | `PacketProcessor` | ✅ 保持不变 |
| `DefaultProcessor` | `DefaultProcessor` | ✅ 保持不变 |
| `CycleProcessor` | `CycleProcessor` | ✅ 保持不变 |

---

## 总结

### 架构的优点
1. ✅ 职责分离清晰（CyclePortImpl 负责同步，Processor 负责处理策略）
2. ✅ 支持组合模式（Processor 可以嵌入 DefaultProcessor）
3. ✅ 接口设计合理（解耦、可测试、可扩展）

### 命名改进（已完成）
- ✅ `ASyncPort` → `CyclePort` - 明确表示基于 cycle 的同步机制
- ✅ `Port` → `CyclePortImpl` - 明确是 CyclePort 的实现
- ✅ `NewPort` → `NewCyclePort` - 与类型名一致
- ✅ `packet.Envelope` → `packet.PacketWithCycle` - 命名更清晰

改进后的效果：
- ✅ 明确表示基于 cycle 的同步机制
- ✅ 与 `CycleProcessor` 命名一致
- ✅ 接口和实现关系清晰（CyclePort + CyclePortImpl）
- ✅ 避免与异步概念混淆

