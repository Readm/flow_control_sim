# DataFlow 数据格式设计文档

## 概述

本文档定义了 flow_sim 项目中三层数据流的数据格式：
- **Packet**: 最小传输单元，在节点间通过链路传输
- **Message**: 业务消息单元，可能由多个 Packet 承载
- **Transaction**: 完整事务，包含多个 Message，支持多种协议（AXI, CHI, CXL等）

## 设计原则

1. **并发安全**: TransactionID 由 {NodeID, TxnID} 组成，每个节点独立计数，避免全局锁
2. **协议无关**: Transaction 支持多种协议（AXI, CHI, CXL等），通过 Protocol 字段区分
3. **完整追踪**: 能够追踪所有 Message 及其 Packet 在哪些节点、哪个 Cycle 被处理
4. **类型安全**: 使用枚举和结构体而非字符串，提高类型安全性

---

## 一、Packet 层

### 1.1 Packet

```go
type Packet struct {
    SourceID      int
    TargetID      int
    Payload       string
    TransactionID TransactionID  // 关联的 Transaction ID
    MessageID     MessageID       // 关联的 Message ID
    Sequence      int             // 在 Message 中的序列号（用于多包消息）
}
```

**说明**:
- `SourceID`/`TargetID`: 源节点和目标节点 ID
- `Payload`: 数据载荷（字符串格式）
- `TransactionID`: 关联的事务 ID（结构体，见下文）
- `MessageID`: 关联的消息 ID（结构体，见下文）
- `Sequence`: 当 Message 被分片成多个 Packet 时，用于标识顺序

### 1.2 PacketWithCycle

```go
type PacketWithCycle struct {
    Cycle  int
    Packet Packet
}
```

**说明**: 将 Packet 与可见周期关联，用于链路传输。

---

## 二、ID 类型定义（公共包）

### 2.1 TransactionID

```go
// 定义在 internal/dataflow/types.go
type TransactionID struct {
    NodeID int  // 创建该 Transaction 的节点 ID
    TxnID  int  // 节点内唯一的事务 ID（单增）
}
```

**说明**: TransactionID 由节点 ID 和节点内事务 ID 组成，支持并发创建，避免全局锁。

### 2.2 MessageID

```go
// 定义在 internal/dataflow/types.go
type MessageID struct {
    NodeID    int  // 创建该 Message 的节点 ID
    MessageID int  // 节点内唯一的消息 ID（单增）
}
```

**说明**: MessageID 由节点 ID 和节点内消息 ID 组成，支持并发创建。

**注意**: TransactionID 和 MessageID 定义在 `internal/dataflow` 包中，避免循环依赖。

---

## 三、Message 层

### 3.1 ProcessedInfo

```go
type ProcessedInfo struct {
    Cycle     uint64  // 处理时间（cycle）
    NodeID    int     // 处理该消息的节点 ID
    PacketIDs []int   // 涉及的 Packet Sequence 列表（可选，用于追踪具体哪些包被处理）
    Info      string  // 处理信息（可选）
}
```

**说明**: 记录消息在某个节点的处理信息，支持记录涉及的 Packet。

### 3.2 Message

```go
type Message struct {
    ID            MessageID        // 唯一标识符
    TransactionID TransactionID   // 所属的 Transaction
    Type          MessageType      // 消息类型（协议相关）
    SourceNodeID  int              // 源节点
    TargetNodeID  int              // 目标节点
    LinkType      string           // 链路类型（可选，用于路由）
    Payload       interface{}      // 消息载荷
    Packets       []Packet         // 关联的 Packet 列表
    CreatedCycle  uint64           // 创建时间（cycle）
    ProcessedInfo []ProcessedInfo  // 处理历史（多个节点可能处理）
}
```

**说明**:
- `ID`: 消息的唯一标识，由节点 ID 和节点内 ID 组成
- `TransactionID`: 关联的事务 ID
- `Type`: 消息类型，在不同协议下含义不同（见协议定义）
- `Packets`: 承载该消息的所有 Packet
- `ProcessedInfo`: 处理历史，记录在哪些节点、哪个 Cycle 被处理

---

## 四、Transaction 层

### 4.1 Protocol

```go
type Protocol string

const (
    ProtocolAXI Protocol = "AXI"
    ProtocolCHI Protocol = "CHI"
    ProtocolCXL Protocol = "CXL"
    // 可以扩展更多协议
)
```

**说明**: 协议枚举，定义支持的事务协议类型。

### 4.2 TransactionState

```go
type TransactionState string

const (
    TransactionStatePending    TransactionState = "Pending"
    TransactionStateInProgress TransactionState = "InProgress"
    TransactionStateCompleted  TransactionState = "Completed"
    TransactionStateFailed     TransactionState = "Failed"
)
```

**说明**: 事务状态枚举。

### 3.4 Event

```go
type Event struct {
    Cycle     uint64      // 发生时间（cycle）
    NodeID    int         // 发生位置（节点）
    EventType string      // 事件类型（Created, MessageSent, MessageReceived, Processed, Completed等）
    MessageID *MessageID // 关联的 Message ID（如果有）
    PacketSeq *int        // 关联的 Packet Sequence（如果有）
    Details   string      // 详细信息
}
```

**说明**: 记录事务生命周期中的事件，可以关联到具体的 Message 和 Packet。

### 4.5 Transaction

```go
type Transaction struct {
    ID              TransactionID      // 唯一标识符
    Protocol        Protocol           // 协议类型（AXI, CHI, CXL等）
    Type            int                // 事务类型（协议相关，不同协议下含义不同）
    InitiatorNodeID int                // 发起节点
    State           TransactionState   // 当前状态
    CreatedCycle    uint64             // 创建时间（cycle）
    CompletedCycle  uint64             // 完成时间（cycle，0 表示未完成）
    Messages        []*Message         // 关联的消息列表
    Events          []Event            // 追踪事件列表
}
```

**说明**:
- `ID`: 事务的唯一标识，由节点 ID 和节点内 ID 组成
- `Protocol`: 协议类型，决定 `Type` 字段的含义
- `Type`: 事务类型，在不同协议下有不同的定义（见协议定义章节）
- `Messages`: 该事务包含的所有消息
- `Events`: 事务生命周期中的所有事件

---

## 五、协议定义

### 5.1 AXI 协议

**Transaction.Type 定义**:
- `0`: Read
- `1`: Write
- `2`: ReadExclusive
- `3`: WriteNoSnoop
- 等等...

**Message.Type 定义**:
- `0`: AR (Address Read)
- `1`: AW (Address Write)
- `2`: R (Read Data)
- `3`: W (Write Data)
- `4`: B (Write Response)
- 等等...

### 5.2 CHI 协议

**Transaction.Type 定义**:
- `0`: ReadNoSnp
- `1`: ReadOnce
- `2`: ReadClean
- `3`: ReadShared
- 等等...

**Message.Type 定义**:
- `0`: Req
- `1`: Rsp
- `2`: Data
- `3`: Snp
- 等等...

### 5.3 CXL 协议

**Transaction.Type 定义**:
- `0`: MemRead
- `1`: MemWrite
- `2`: MemReadLock
- 等等...

**Message.Type 定义**:
- `0`: Request
- `1`: Response
- `2`: Data
- 等等...

**注意**: 具体的类型定义可以根据实际需求扩展。

---

## 六、追踪机制

### 6.1 Message 追踪

每个 Message 通过 `ProcessedInfo` 数组记录处理历史：

```go
// 示例：Message 在节点 2 的 Cycle 5 被处理
msg.ProcessedInfo = append(msg.ProcessedInfo, ProcessedInfo{
    Cycle:     5,
    NodeID:    2,
    PacketIDs:  []int{0, 1},  // 处理了 Sequence 0 和 1 的 Packet
    Info:      "Received and processed",
})
```

### 6.2 Transaction 追踪

Transaction 通过 `Events` 数组记录所有事件：

```go
// 示例：在节点 2 的 Cycle 5 收到 Message
txn.Events = append(txn.Events, Event{
    Cycle:     5,
    NodeID:    2,
    EventType: "MessageReceived",
    MessageID: &msg.ID,
    Details:   "Received request message",
})
```

### 6.3 Packet 追踪

Packet 的追踪通过以下方式实现：
1. **通过 Message**: Packet 属于某个 Message，Message 的 `ProcessedInfo` 可以记录涉及的 Packet Sequence
2. **通过 Transaction Events**: Transaction 的 `Events` 可以记录 Packet 级别的事件

### 6.4 完整追踪示例

```
Transaction (NodeID=1, TxnID=100, Protocol=AXI, Type=0)
  ├─ Message (NodeID=1, MessageID=1, Type=0) [AR]
  │   ├─ Packet (Sequence=0) → 在节点 2, Cycle 5 被处理
  │   └─ Packet (Sequence=1) → 在节点 2, Cycle 5 被处理
  ├─ Message (NodeID=2, MessageID=1, Type=2) [R]
  │   └─ Packet (Sequence=0) → 在节点 1, Cycle 10 被处理
  └─ Events:
      - Cycle 0, Node 1: Created
      - Cycle 3, Node 1: MessageSent (MessageID=1)
      - Cycle 5, Node 2: MessageReceived (MessageID=1)
      - Cycle 7, Node 2: MessageSent (MessageID=2)
      - Cycle 10, Node 1: MessageReceived (MessageID=2)
      - Cycle 10, Node 1: Completed
```

---

## 七、ID 唯一性保证

### 7.1 TransactionID

- **全局唯一**: `{NodeID, TxnID}` 组合保证全局唯一
- **节点内单增**: 每个节点的 `TxnID` 在该节点内单调递增
- **并发安全**: 不同节点可以并发创建 Transaction，无需全局锁

### 7.2 MessageID

- **全局唯一**: `{NodeID, MessageID}` 组合保证全局唯一
- **节点内单增**: 每个节点的 `MessageID` 在该节点内单调递增
- **并发安全**: 不同节点可以并发创建 Message，无需全局锁

### 7.3 Packet Sequence

- **Message 内唯一**: 在同一个 Message 内，Sequence 从 0 开始递增
- **无需全局唯一**: Packet 通过 `TransactionID + MessageID + Sequence` 组合唯一标识

---

## 八、数据关系图

```
Transaction (ID: {NodeID, TxnID}, Protocol, Type)
    │
    ├─ Message (ID: {NodeID, MessageID}, Type, TransactionID)
    │   │
    │   ├─ Packet (TransactionID, MessageID, Sequence)
    │   ├─ Packet (TransactionID, MessageID, Sequence)
    │   └─ ...
    │
    ├─ Message (ID: {NodeID, MessageID}, Type, TransactionID)
    │   │
    │   ├─ Packet (TransactionID, MessageID, Sequence)
    │   └─ ...
    │
    └─ Events[]
        ├─ Event (MessageID, PacketSeq)
        └─ ...
```

---

## 九、使用示例

### 9.1 创建 Transaction

```go
import (
    "github.com/Readm/flow_sim/internal/dataflow"
    "github.com/Readm/flow_sim/internal/dataflow/transaction"
)

// 在节点 1 创建 AXI Read 事务
txn := &transaction.Transaction{
    ID: dataflow.TransactionID{
        NodeID: 1,
        TxnID:  100,  // 节点 1 内的第 100 个事务
    },
    Protocol:        ProtocolAXI,
    Type:            0,  // AXI Read
    InitiatorNodeID: 1,
    State:           TransactionStatePending,
    CreatedCycle:    0,
    Messages:        []*Message{},
    Events:          []Event{},
}
```

### 9.2 创建 Message

```go
// 在节点 1 创建 AR (Address Read) 消息
msg := &Message{
    ID: MessageID{
        NodeID:    1,
        MessageID: 1,  // 节点 1 内的第 1 个消息
    },
    TransactionID: txn.ID,
    Type:          0,  // AXI AR
    SourceNodeID:  1,
    TargetNodeID:  2,
    CreatedCycle:  3,
    Packets:       []Packet{},
    ProcessedInfo: []ProcessedInfo{},
}
```

### 9.3 创建 Packet

```go
// 创建承载消息的 Packet
pkt := Packet{
    SourceID:      1,
    TargetID:      2,
    Payload:       "address=0x1000",
    TransactionID: txn.ID,
    MessageID:     msg.ID,
    Sequence:      0,
}
```

### 9.4 记录处理信息

```go
// 在节点 2 的 Cycle 5 处理消息
msg.ProcessedInfo = append(msg.ProcessedInfo, ProcessedInfo{
    Cycle:    5,
    NodeID:    2,
    PacketIDs: []int{0},  // 处理了 Sequence 0 的 Packet
    Info:     "Received AR request",
})

// 在 Transaction 中记录事件
txn.Events = append(txn.Events, Event{
    Cycle:     5,
    NodeID:    2,
    EventType: "MessageReceived",
    MessageID: &msg.ID,
    Details:   "Received AR request",
})
```

---

## 十、设计决策说明

### 10.1 为什么 TransactionID 和 MessageID 使用结构体？

- **并发安全**: 每个节点独立计数，避免全局锁
- **可读性**: 结构体比复合整数更清晰
- **扩展性**: 未来可以添加更多字段（如时间戳、版本等）

### 10.2 为什么 Protocol 和 Type 分离？

- **灵活性**: 不同协议有不同的事务类型定义
- **类型安全**: Protocol 使用枚举，Type 使用整数，便于协议特定逻辑处理
- **可扩展性**: 新增协议只需添加 Protocol 枚举值，无需修改 Type 定义

### 10.3 为什么 Message.Type 也是整数？

- **协议相关**: 不同协议下 Message 类型不同
- **性能**: 整数比较比字符串快
- **一致性**: 与 Transaction.Type 保持一致

### 10.4 为什么 Packet 中保留 TransactionID 和 MessageID？

- **链路层需求**: Packet 在链路传输时可能需要路由决策
- **调试追踪**: 可以直接从 Packet 追溯到 Transaction 和 Message
- **性能**: 避免在链路层查找 Message 和 Transaction

---

## 十一、待实现功能

1. **序列化/反序列化**: 如果需要持久化或网络传输，需要实现序列化逻辑
2. **ID 生成器**: 需要实现节点内的 ID 生成器，保证单增
3. **协议扩展**: 根据实际需求扩展协议定义
4. **追踪工具**: 实现查询和可视化追踪信息的工具

---

## 十二、版本历史

- **v1.0** (当前): 初始设计，支持 AXI/CHI/CXL 协议，完整的追踪机制

