# 设计文档：基于 **Yield/Resume 模式** 的 Transaction 驱动 Cache 一致性仿真（Go 实现）

本文档面向实现者与架构决策者，详述在 flow_sim 项目中实现 Transaction 驱动的一致性仿真的设计：
**核心思想**：Transaction 以同步、直线式代码编写（使用 `Yield/Resume` 抽象），在 `Node.Tick` 周期中由 `TxnManager` 统一调度，保证所有对 Node 本地状态的修改都在 `Tick` 执行路径中串行化，既保持代码可读性又确保一致性安全。

---

# 一、目标与动机

## 目的

* **可读性**：让事务（Transaction）逻辑写成同步、直线式代码（类似 Python 的 `yield`），便于实现复杂控制流（重试、超时、错误处理）。
* **正确性**：保证所有对 Node 本地元数据（cache line 状态、sharer 列表、pending 列表等）的读写都在 `Node.Tick` 执行路径中串行化，避免并发竞态与锁复杂度。
* **灵活性**：支持 snooping（广播）与 directory（点对点）两类实现，允许读合并、owner forward、update/invalidate 等协议要素。
* **调试友好**：事务控制流集中、事件可追踪（trace），便于可视化与重放。

## 背景

Go 没有语言级 `yield`/generator。我们使用 **goroutine + channel 封装的 Yield/Resume** 来模拟类似行为。结合 `Node.Tick` 的周期驱动模型，既保留 Node 操作的串行性，又能让事务逻辑直观。

---

# 二、整体架构概览

## 参与实体

* **`Transaction`**：保存事务上下文（ID、状态、消息列表等），以同步式代码编写逻辑，在关键点调用 `ctx.Yield(...)` 将控制权交给 `TxnManager`。
* **`TxnContext`**：封装 `yieldCh/resumeCh`，提供 `Yield()`、`Send()` 等 helper 供 Transaction 调用。
* **`TxnManager`**：管理 Transaction 的生命周期、ID 分配、pending 列表（按地址或消息类型），在 `Node.Tick` 中被调用以处理消息路由和 Transaction 恢复。
* **`Node`**：实现 `node.Node` 接口，在 `Tick(ctx, cycle, linkDelay)` 中调用 `TxnManager.Tick()` 处理消息和恢复 Transaction。
* **`Pipeline`**：处理 `Packet` 的流动，Transaction 通过 `TxnContext.Send()` 发送的消息会被转换为 `Packet` 并通过 `Pipeline` 发送。
* **`Network`**：协调多个 Node，在每个周期调用各 Node 的 `Tick`。

## 高层时序

1. **Transaction 创建**：CPU/上层逻辑创建 Transaction 并调用 `TxnManager.Start(txnFunc)`。`Start` 会启动一个 goroutine 执行 `txnFunc`。
2. **Transaction 执行**：`txnFunc` 以同步代码编写，在需要等待外部事件（如收到消息）时调用 `ctx.Yield(WaitFor{...})`，阻塞在 `resumeCh` 上。
3. **消息到达**：`Packet` 通过 `Pipeline` 到达 Node，在 `Node.Tick` 中被转换为 `Message` 并传递给 `TxnManager.Tick(incomingMessages)`。
4. **Transaction 恢复**：`TxnManager` 根据消息的 `TransactionID` 找到对应的 Transaction，通过 `resumeCh` 发送恢复值，Transaction 继续执行。
5. **完成**：Transaction 完成后通知上层，`TxnManager` 清理相关资源。

---

# 三、核心概念与 API 约定

## 1. TxnContext（封装 yield/resume）

```go
type TxnContext struct {
    yieldCh  chan *YieldCommand // Txn -> Manager: yield command
    resumeCh chan interface{}   // Manager -> Txn: resume value
    ctx      context.Context    // cancel/timeout
    nodeID   int                // Node ID
    txnID    dataflow.TransactionID // Transaction ID
}

func (tc *TxnContext) Yield(cmd *YieldCommand) (interface{}, error)
func (tc *TxnContext) Send(msg *message.Message) error
```

**语义**：
- `Yield` 将等待意图（`YieldCommand`）发送给 `TxnManager` 并阻塞直到 `resumeCh` 收到恢复值或超时/取消。
- `Send` 将消息加入发送队列，由 `TxnManager` 在 `Tick` 中处理。

## 2. YieldCommand（等待描述）

```go
type YieldCommand struct {
    Type      YieldType           // WaitForMessage, WaitForTimeout, etc.
    WaitFor   *WaitForMessage     // 等待的消息条件（可选）
    Timeout   time.Duration       // 超时时间（可选）
    SendQueue []*message.Message  // 待发送的消息列表
}
```

## 3. TxnManager（Transaction 管理器）

```go
type TxnManager struct {
    nodeID        int
    activeTxns    map[dataflow.TransactionID]*activeTxn
    pendingByAddr map[Addr][]*activeTxn  // 按地址索引的 pending transactions
    nextTxnID     int                    // 节点内事务 ID 计数器
    mu            sync.Mutex
}

func NewTxnManager(nodeID int) *TxnManager
func (tm *TxnManager) Start(txnFunc func(*TxnContext)) dataflow.TransactionID
func (tm *TxnManager) Tick(cycle uint64, incoming []*message.Message) (outgoing []*message.Message, err error)
```

**职责**：
- `Start`：启动新的 Transaction goroutine，分配 `TransactionID`。
- `Tick`：处理传入消息（路由到等待的 Transaction）、处理 Transaction 的 `Yield` 请求、收集待发送消息。

## 4. activeTxn（活跃 Transaction 记录）

```go
type activeTxn struct {
    txnID    dataflow.TransactionID
    context  *TxnContext
    done     chan struct{}
    txn      *transaction.Transaction
}
```

---

# 四、实现细节（Run-in-Tick 模式）

## 核心设计：Run-in-Tick

本项目采用 **Run-in-Tick** 模式：
- **Transaction 以 goroutine 运行**，使用 `Yield/Resume` 实现暂停与恢复。
- **所有 Node 状态修改在 `Node.Tick` 中执行**：`TxnManager.Tick` 在 `Node.Tick` 中被调用，确保串行化。
- **消息处理同步化**：`TxnManager.Tick` 处理传入消息并立即恢复对应的 Transaction，Transaction 的后续操作（如修改 cache 状态）通过 `YieldCommand` 中的操作队列返回给 `TxnManager`，在下一个 `Tick` 或当前 `Tick` 的后续阶段执行。

## 工作流程示例：Read Transaction

场景：Core 发起 Load（读）；本地 cache miss；发出 ReadReq；等待 DataReply。

1. **Transaction 启动**：
   ```go
   txnID := txnMgr.Start(func(ctx *TxnContext) {
       // Check cache
       if cacheHit(ctx, addr) {
           return // done
       }
       // Send ReadReq
       ctx.Send(&message.Message{
           Type: MsgReadReq,
           TargetNodeID: memoryNodeID,
           // ...
       })
       // Wait for DataReply
       reply, _ := ctx.Yield(&YieldCommand{
           Type: WaitForMessage,
           WaitFor: &WaitForMessage{Type: MsgDataReply},
       })
       // Process reply and update cache
       updateCache(ctx, reply)
   })
   ```

2. **Node.Tick 处理**：
   ```go
   func (n *CoherenceNode) Tick(ctx context.Context, cycle uint64, linkDelay time.Duration) error {
       // 1. 从 Pipeline 接收 Packet，转换为 Message
       incomingMsgs := n.receiveMessages(cycle)
       
       // 2. 调用 TxnManager.Tick 处理消息和恢复 Transaction
       outgoingMsgs, err := n.txnMgr.Tick(cycle, incomingMsgs)
       
       // 3. 将待发送消息转换为 Packet 并通过 Pipeline 发送
       n.sendMessages(cycle, outgoingMsgs)
       
       // 4. 处理 Pipeline
       for _, flow := range n.Flows() {
           if err := flow.Tick(int(cycle)); err != nil {
               return err
           }
       }
       return nil
   }
   ```

3. **TxnManager.Tick 处理**：
   ```go
   func (tm *TxnManager) Tick(cycle uint64, incoming []*message.Message) ([]*message.Message, error) {
       var outgoing []*message.Message
       
       // 1. 处理传入消息：路由到等待的 Transaction
       for _, msg := range incoming {
           if txn := tm.findWaitingTxn(msg); txn != nil {
               // 非阻塞发送恢复值
               select {
               case txn.context.resumeCh <- msg:
               default:
               }
           }
       }
       
       // 2. 处理 Transaction 的 Yield 请求（非阻塞）
       tm.processYields(cycle, &outgoing)
       
       return outgoing, nil
   }
   ```

4. **Transaction 恢复**：Transaction goroutine 从 `resumeCh` 收到消息后继续执行，可能再次 `Yield`（如发送新消息、等待下一个事件）或完成。

---

# 五、详细工作流程（以 Snoop + Read 为例）

场景：Core 发起 Load（读）；本地 cache miss；发出 ReadReq；在等待 DataReply 期间收到一个 SnoopInvalidate 广播。

核心流程：

1. **Transaction 启动**：Transaction 检查 cache miss，发送 ReadReq，`Yield(WaitForMessage{Type: MsgDataReply})`。

2. **SnoopInvalidate 到达**：
   - `Packet` 通过 `Pipeline` 到达，转换为 `Message`。
   - `TxnManager.Tick` 收到消息，发现没有 Transaction 在等待此消息类型（SnoopInvalidate 是广播）。
   - Node 立即处理 SnoopInvalidate：更新本地 cache line 状态为 Invalid。
   - 检查是否有 pending Transaction 可能受影响（如等待同一地址的 DataReply），如果有，通过 `resumeCh` 发送通知。

3. **Transaction 处理 Snoop**：
   - Transaction 收到通知后，可以决定：中止等待、记录 invalidation 并继续等待 Data、或采取其他行动。
   - Transaction 可能再次 `Yield` 等待 DataReply。

4. **DataReply 到达**：
   - `TxnManager.Tick` 收到 DataReply，找到等待的 Transaction，通过 `resumeCh` 发送。
   - Transaction 收到 DataReply，更新 cache（通过 `YieldCommand` 中的操作），完成。

---

# 六、优点与缺点

## 优点

* **一致性安全**：所有 Node 本地状态修改在 `Node.Tick` 中串行化，极大简化并发正确性。
* **代码可读性**：Transaction 逻辑写成同步、直线式代码，易于理解和维护。
* **Snoop 友好**：广播消息到达时可以立即在 `Tick` 中处理并影响所有相关 Transaction。
* **调试友好**：Transaction 事件序列易于记录和追踪。

## 缺点与风险

* **需要 channel 管理**：`Yield/Resume` 需要稳定的 channel 管理与超时策略。
* **goroutine 开销**：每个 Transaction 占用一个 goroutine，大量 Transaction 时可能有开销。
* **死锁风险**：需谨慎设计避免 Node 与 Transaction 互相等待。
* **实现复杂性**：需要处理超时、late message、retry 等边界情况。

---

# 七、实现细节与工程建议

1. **TxnManager.Tick 必须短且非阻塞**：避免在 `Tick` 中做长时间操作，所有阻塞操作都通过 `Yield` 转移到 Transaction goroutine。

2. **Yield/Resume 通道设计**：`yieldCh`、`resumeCh` 使用缓冲通道（至少缓冲 1），`Yield` 使用 `select` 支持超时与 ctx 取消。

3. **pendingMap 机制**：`TxnManager` 维护按地址索引的 pending Transaction 列表，支持多个 Transaction attach（读合并）。

4. **晚到消息处理**：消息携带 `TransactionID`，`TxnManager` 在 Transaction 完成后能识别并丢弃晚到消息。

5. **超时/重试**：Transaction 层实现重试/backoff 策略，`TxnManager` 提供超时支持。

6. **对象池化**：Pool 化 Transaction 对象与缓冲，避免频繁 alloc/GC。

7. **Tracing**：每个 Transaction 记录事件（发/收消息、Yield/Resume、状态变化）以便可视化。

8. **单元测试**：编写测试验证：read hit/miss、concurrent reads、snoop during miss、owner forward 等场景。使用 `-race` 检测并发问题。

---

# 八、示例：Transaction 编写模板

## Yield 风格（推荐）

```go
func ReadTransaction(ctx *TxnContext, addr Addr) error {
    // Check cache
    if ctx.GetCacheState(addr) != Invalid {
        data := ctx.ReadCache(addr)
        ctx.Complete(data)
        return nil
    }
    
    // Cache miss: send ReadReq
    readReq := &message.Message{
        ID:            ctx.NewMessageID(),
        TransactionID: ctx.txnID,
        Type:          MsgReadReq,
        SourceNodeID:  ctx.nodeID,
        TargetNodeID:  memoryNodeID,
        Payload:       ReadReqPayload{Addr: addr},
    }
    if err := ctx.Send(readReq); err != nil {
        return err
    }
    
    // Wait for DataReply
    reply, err := ctx.Yield(&YieldCommand{
        Type: WaitForMessage,
        WaitFor: &WaitForMessage{
            Type: MsgDataReply,
            Addr: addr,
        },
        Timeout: 100 * time.Millisecond,
    })
    if err != nil {
        return err
    }
    
    dataReply := reply.(*message.Message)
    data := dataReply.Payload.(DataReplyPayload).Data
    
    // Update cache
    ctx.UpdateCache(addr, Shared, data)
    ctx.Complete(data)
    return nil
}
```

**注意**：`GetCacheState`、`ReadCache`、`UpdateCache` 等方法需要通过 `YieldCommand` 与 `TxnManager` 通信，在 `Node.Tick` 中执行实际的状态修改。

---

# 九、测试用例

核心场景单元测试：

1. **单核读命中**：Transaction 检查 cache hit，直接返回数据。
2. **单核读缺失**：Transaction 发送 ReadReq，等待 DataReply，更新 cache。
3. **两核并发读合并**：两个 Transaction 同时请求同一地址，验证读合并是否生效。
4. **读缺失期间收到 SnoopInvalidate**：验证 Transaction 如何处理 snoop。
5. **写后读（升级）**：Transaction 先写后读，验证状态升级。
6. **Owner forward 场景（MOESI）**：验证 owner forward 机制。
7. **Update 协议场景**：写广播 Update，其他持有者接收并更新。
8. **NACK/Retry 场景**：目录冲突时的重试机制。
9. **Late message**：保证过期 Transaction 的消息被丢弃。

每个测试都要检查：消息序列、最终状态、bus traffic metrics。

---

# 十、常见陷阱与避免办法

* **Node.Tick 内阻塞**：禁止在 `Node.Tick` 中做长时间阻塞操作，所有阻塞都通过 `Yield` 转移到 Transaction goroutine。

* **死锁**：明确约束：`TxnManager.Tick` 不阻塞等待 Transaction，只通过 channel 非阻塞通信。Transaction 的阻塞通过 `Yield` 实现。

* **goroutine 泄漏**：所有 `Yield` 必须有超时/取消处理，`TxnManager` 在 Transaction 完成或失败时确保 goroutine 退出。

* **backpressure**：当 Transaction 数量暴涨时，channel 可能耗尽；增加 channel 容量并记录 metrics，必要时拒绝新 Transaction。

* **不一致的状态修改**：确保所有 Node 状态修改都通过 `TxnManager` 在 `Tick` 中执行，不在 Transaction goroutine 中直接修改。

---

# 十一、最小用例实现

## 框架组件清单

1. **`internal/dataflow/transaction/types.go`**：
   - `YieldCommand`、`YieldType`、`WaitForMessage` 等类型定义。

2. **`internal/dataflow/transaction/context.go`**：
   - `TxnContext` 实现，`Yield`、`Send` 等方法。

3. **`internal/dataflow/transaction/manager.go`**：
   - `TxnManager` 实现，`Start`、`Tick` 等方法。

4. **测试用例**（`tests/transaction_poc/`）：
   - `PingNode`、`PongNode` 实现 `node.Node`。
   - `PingTransaction`：发送 Ping 消息，等待 Pong 回复。
   - `PongNode`：收到 Ping 后回复 Pong。
   - 端到端测试验证 Transaction 完成。

## 集成到现有 Node

在实现 coherence node 时，`Node.Tick` 中需要：

```go
func (n *CoherenceNode) Tick(ctx context.Context, cycle uint64, linkDelay time.Duration) error {
    // 1. 接收消息
    incomingMsgs := n.receiveMessagesFromPipeline(cycle)
    
    // 2. 处理 Transaction
    outgoingMsgs, err := n.txnMgr.Tick(cycle, incomingMsgs)
    if err != nil {
        return err
    }
    
    // 3. 发送消息
    n.sendMessagesToPipeline(cycle, outgoingMsgs)
    
    // 4. 处理 Pipeline
    for _, flow := range n.Flows() {
        if err := flow.Tick(int(cycle)); err != nil {
            return err
        }
    }
    
    return nil
}
```

---

# 十二、结语与建议路线

* **初始实现**：实现 `TxnContext`、`TxnManager` 核心框架，支持基本的 `Yield/Resume` 和消息路由。
* **最小用例验证**：实现 Ping/Pong Transaction 测试，验证框架基本功能。
* **逐步完善**：添加超时、late message 处理、对象池、metrics、单元测试等。
* **协议实现**：基于框架实现 MESI、MOESI 等一致性协议。
