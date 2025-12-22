# Transaction 统一框架设计：支持多执行模式

**文档版本**: 1.0
**创建日期**: 2025-12-03
**状态**: 设计阶段

---

## 目录

1. [概述](#1-概述)
2. [核心设计理念](#2-核心设计理念)
3. [两种执行模式](#3-两种执行模式)
4. [统一框架架构](#4-统一框架架构)
5. [实现细节](#5-实现细节)
6. [使用示例](#6-使用示例)
7. [实现路线图](#7-实现路线图)

---

## 1. 概述

### 1.1 目标

设计一个**统一的 Transaction 框架**，同时支持两种执行模式：

1. **分段式 (Segmented)**: Transaction 只在发起节点执行，通过消息与其他节点通信
2. **连续式 (Continuous)**: Transaction 可以"迁移"到其他节点继续执行，保持执行流的连续性

### 1.2 核心价值

- ✅ **统一框架**：两种模式共用相同的底层机制（Yield/Resume）
- ✅ **向后兼容**：现有的分段式 Transaction 无需修改
- ✅ **灵活选择**：开发者可根据场景选择合适的模式
- ✅ **可共存**：两种模式可以在同一个系统中并行使用

### 1.3 适用场景

| 模式 | 适用场景 | 优势 | 劣势 |
|------|---------|------|------|
| **分段式** | 简单协议、高并发场景 | 性能好、实现简单 | 逻辑分散、日志不连续 |
| **连续式** | 复杂协议、需要全局视图 | 逻辑集中、日志连续 | 实现复杂、调度开销 |

---

## 2. 核心设计理念

### 2.1 本质相同

两种模式的底层机制完全相同：

```
┌────────────────────────────────────────────────────┐
│         统一的 Transaction 框架                     │
│                                                     │
│  核心机制：                                         │
│  - Goroutine（执行载体）                            │
│  - Yield/Resume（暂停/恢复）                        │
│  - Message Passing（节点间通信）                    │
│  - TxnManager（调度器）                             │
└────────────────────────────────────────────────────┘
                    ▲           ▲
                    │           │
       ┌────────────┴───┐   ┌──┴────────────┐
       │  分段式        │   │  连续式        │
       │  (Segmented)   │   │  (Continuous)  │
       │                │   │                │
       │  特点：        │   │  特点：        │
       │  - 固定在      │   │  - 可迁移到    │
       │    发起节点    │   │    其他节点    │
       │  - 消息驱动    │   │  - 流程连续    │
       └────────────────┘   └────────────────┘
```

### 2.2 唯一区别

**分段式**：
```go
func ReadTxn(ctx *TxnContext, localNode *node.Node, addr uint64) {
    // 1. 在本地节点执行
    cache := GetCHICache(localNode)

    // 2. 发送消息
    ctx.Send(msg)

    // 3. Yield，等待消息
    resp, _ := ctx.Yield(&YieldCommand{Type: YieldTypeWaitForMessage})

    // 4. 继续在本地节点执行
    cache.SetData(addr, resp.Data)
}
```

**连续式**：
```go
func WriteTxn(ctx *TxnContext, addr uint64) {
    // 1. 在 RN 节点执行
    cache := ctx.GetCache()  // 当前节点的 cache

    // 2. 迁移到 HN 节点
    hnCtx, _ := ctx.MigrateTo(homeNodeID)  // 关键差异！

    // 3. 在 HN 节点执行（同一个 goroutine）
    dir := hnCtx.GetDirectory()  // HN 节点的 directory

    // 4. 迁移回 RN 节点
    rnCtx, _ := hnCtx.MigrateTo(rnNodeID)

    // 5. 继续在 RN 节点执行
    cache = rnCtx.GetCache()
}
```

### 2.3 关键洞察

**迁移 ≈ 特殊的 Yield/Resume**

```
分段式 Yield:
    RN: Yield() → 等待消息 → Resume(msg) → 继续在 RN 执行

连续式 Yield (迁移):
    RN: MigrateTo(HN) → 等待迁移完成 → Resume(hnContext) → 继续在 HN 执行
                                                              ↑
                                                        同一个 goroutine！
```

---

## 3. 两种执行模式

### 3.1 分段式执行模式

#### 执行流程

```
┌────────────────────────────────────────┐
│  RN (Request Node)                     │
│                                        │
│  ReadCleanTxn goroutine:               │
│  ┌──────────────────────────────────┐ │
│  │ 1. Check cache (miss)            │ │
│  │ 2. Send ReadClean → HN           │ │
│  │ 3. Yield, wait for CompData      │ │
│  │    [goroutine suspended]         │ │
│  └──────────────────────────────────┘ │
└────────────────────────────────────────┘
            │
            │ Message: ReadClean
            ▼
┌────────────────────────────────────────┐
│  HN (Home Node)                        │
│                                        │
│  HomeNodeHandler (独立函数):           │
│  ┌──────────────────────────────────┐ │
│  │ 1. Receive ReadClean             │ │
│  │ 2. Check directory               │ │
│  │ 3. Send CompData → RN            │ │
│  └──────────────────────────────────┘ │
└────────────────────────────────────────┘
            │
            │ Message: CompData
            ▼
┌────────────────────────────────────────┐
│  RN (Request Node)                     │
│                                        │
│  ReadCleanTxn goroutine (Resume):      │
│  ┌──────────────────────────────────┐ │
│  │ 4. [goroutine resumed]           │ │
│  │ 5. Receive CompData              │ │
│  │ 6. Update cache                  │ │
│  │ 7. Complete                      │ │
│  └──────────────────────────────────┘ │
└────────────────────────────────────────┘
```

#### 代码示例

```go
// RN 上的 Transaction
func ReadCleanTxn(ctx *TxnContext, n *node.Node, addr uint64) ([]byte, error) {
    cache := GetCHICache(n)

    // 1. 本地处理
    if cache.IsPresent(addr) {
        return cache.GetData(addr), nil
    }

    // 2. 发送消息
    ctx.Send(buildReadCleanReq(addr, homeNodeID))

    // 3. Yield 等待
    resp, _ := ctx.Yield(&YieldCommand{
        Type:    YieldTypeWaitForMessage,
        WaitFor: &WaitForMessage{Type: OpcodeCompData},
    })

    // 4. 处理响应
    respMsg := resp.(*message.Message)
    cache.SetData(addr, respMsg.Payload.Data)
    return respMsg.Payload.Data, nil
}

// HN 上的 Handler（独立函数）
func HomeNodeReadCleanHandler(ctx *TxnContext, n *node.Node, req *message.Message) error {
    dir := GetCHIDirectory(n)
    data := loadDataFromMemory(req.Payload.Addr)
    ctx.Send(buildCompDataResp(req.SourceNodeID, data))
    dir.AddSharer(req.Payload.Addr, req.SourceNodeID)
    return nil
}
```

#### 特点

- ✅ **简单**: Transaction 逻辑清晰，Handler 独立
- ✅ **高性能**: 无迁移开销，适合高并发
- ✅ **易调试**: 每个节点的逻辑独立
- ❌ **日志分散**: RN 和 HN 的日志在不同地方
- ❌ **状态传递**: 需要通过消息 Payload 传递状态

### 3.2 连续式执行模式

#### 执行流程

```
┌────────────────────────────────────────┐
│  RN (Request Node)                     │
│                                        │
│  WriteUniqueTxn goroutine:             │
│  ┌──────────────────────────────────┐ │
│  │ 1. Check cache (need upgrade)    │ │
│  │ 2. Send migration request → HN   │ │
│  │ 3. Yield, wait for migration     │ │
│  │    [goroutine suspended]         │ │
│  └──────────────────────────────────┘ │
└────────────────────────────────────────┘
            │
            │ Migration Request
            ▼
┌────────────────────────────────────────┐
│  HN (Home Node)                        │
│                                        │
│  WriteUniqueTxn goroutine (Resume):    │
│  ┌──────────────────────────────────┐ │
│  │ 4. [same goroutine resumed!]     │ │
│  │ 5. Check directory               │ │
│  │ 6. Snoop other sharers           │ │
│  │ 7. Send CompData → RN            │ │
│  │ 8. Update directory              │ │
│  │ 9. Yield, migrate back to RN     │ │
│  └──────────────────────────────────┘ │
└────────────────────────────────────────┘
            │
            │ Migration Response
            ▼
┌────────────────────────────────────────┐
│  RN (Request Node)                     │
│                                        │
│  WriteUniqueTxn goroutine (Resume):    │
│  ┌──────────────────────────────────┐ │
│  │ 10. [same goroutine again!]      │ │
│  │ 11. Update cache                 │ │
│  │ 12. Complete                     │ │
│  └──────────────────────────────────┘ │
└────────────────────────────────────────┘
```

#### 代码示例

```go
// 跨节点连续执行的 Transaction
func WriteUniqueTxnContinuous(ctx *TxnContext, addr uint64, data []byte) error {
    // ===== 阶段 1: 在 RN 上 =====
    cache := ctx.GetCache()

    // Fast path
    if cache.GetState(addr) == cache.StateModified {
        cache.SetData(addr, data)
        return nil
    }

    // Slow path: 需要获取独占权限
    ctx.Send(buildWriteUniqueReq(addr, homeNodeID))

    // 迁移到 HN
    hnCtx, err := ctx.MigrateTo(homeNodeID)
    if err != nil {
        return err
    }

    // ===== 阶段 2: 在 HN 上（同一个 goroutine！）=====
    dir := hnCtx.GetDirectory()

    // Snoop 其他 sharers
    sharers := dir.GetSharers(addr)
    for _, sharerID := range sharers {
        hnCtx.Send(buildSnpInvalidate(addr, sharerID))
    }

    // 等待所有 snoop 完成
    for range sharers {
        _, _ = hnCtx.Yield(&YieldCommand{
            Type:    YieldTypeWaitForMessage,
            WaitFor: &WaitForMessage{Type: OpcodeSnpResp},
        })
    }

    // 发送响应
    hnCtx.Send(buildCompData(addr, rnNodeID))

    // 更新 directory
    dir.ClearSharers(addr)
    dir.SetOwner(addr, rnNodeID)

    // 迁移回 RN
    rnCtx, err := hnCtx.MigrateTo(rnNodeID)
    if err != nil {
        return err
    }

    // ===== 阶段 3: 回到 RN 上 =====
    cache = rnCtx.GetCache()
    cache.SetData(addr, data)
    cache.SetState(addr, cache.StateModified)

    return nil
}
```

#### 特点

- ✅ **全局视图**: 整个 Transaction 流程在一个函数中
- ✅ **日志连续**: 同一个 goroutine，日志自然连续
- ✅ **状态保持**: 局部变量跨节点保持
- ✅ **易理解**: 代码读起来像同步流程
- ❌ **实现复杂**: 需要迁移机制
- ❌ **调度开销**: 迁移有一定开销

---

## 4. 统一框架架构

### 4.1 核心组件

```
┌─────────────────────────────────────────────────────────────┐
│                  Transaction Framework                       │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │  TxnContext (执行上下文)                               │ │
│  │  - txnID: TransactionID                                │ │
│  │  - nodeID: int (当前所在节点)                          │ │
│  │  - nodeAccessor: NodeAccessor (节点资源访问)           │ │
│  │  - yieldCh/resumeCh: channels (Yield/Resume 通信)     │ │
│  │                                                        │ │
│  │  方法:                                                 │ │
│  │  - Yield(cmd) → 暂停执行                              │ │
│  │  - MigrateTo(nodeID) → 迁移到其他节点                 │ │
│  │  - GetCache() → 获取当前节点的 cache                  │ │
│  │  - GetDirectory() → 获取当前节点的 directory          │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │  NodeAccessor (节点资源访问抽象)                       │ │
│  │  - LocalNodeAccessor: 直接访问本地节点                │ │
│  │  - RemoteNodeAccessor: 通过消息访问远程节点(可选)     │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │  TxnManager (调度器)                                   │ │
│  │  - activeTxns: 本地启动的 Transaction                 │ │
│  │  - migratedTxns: 迁移来的 Transaction                 │ │
│  │                                                        │ │
│  │  方法:                                                 │ │
│  │  - Start(txnFunc) → 启动 Transaction                  │ │
│  │  - Tick(msgs) → 调度循环                              │ │
│  │  - handleMigrationRequest() → 处理迁移请求            │ │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### 4.2 消息类型

```go
// 现有消息类型（协议相关）
const (
    MsgTypeReadClean = 0x01
    MsgTypeCompData  = 0x30
    // ...
)

// 新增：框架级消息类型
const (
    MsgTypeMigrationRequest = 0xF0  // Transaction 请求迁移
    MsgTypeMigrationResume  = 0xF1  // Resume 迁移的 Transaction
)
```

### 4.3 执行流程对比

#### 分段式流程

```
RN TxnManager:
    Start(ReadCleanTxn) → goroutine 启动
                          ↓
    goroutine:            Check cache
                          Send ReadClean
                          Yield (WaitForMessage)
                          [suspended]

HN TxnManager:
    Tick(ReadClean msg) → call HomeNodeHandler
                          ↓
    Handler:              Check directory
                          Send CompData
                          Return

RN TxnManager:
    Tick(CompData msg)  → Resume goroutine
                          ↓
    goroutine:            [resumed]
                          Update cache
                          Complete
```

#### 连续式流程

```
RN TxnManager:
    Start(WriteTxn)     → goroutine 启动
                          ↓
    goroutine:            Check cache
                          Send MigrationRequest
                          Yield (MigrateTo HN)
                          [suspended]

HN TxnManager:
    Tick(Migration msg) → Resume goroutine (with HN context)
                          ↓
    goroutine:            [resumed on HN!]
                          Check directory
                          Snoop sharers
                          Send CompData
                          Yield (MigrateTo RN)
                          [suspended]

RN TxnManager:
    Tick(Migration msg) → Resume goroutine (with RN context)
                          ↓
    goroutine:            [resumed on RN!]
                          Update cache
                          Complete
```

---

## 5. 实现细节

### 5.1 TxnContext 扩展

#### 结构体定义

```go
type TxnContext struct {
    // 标识
    txnID  dataflow.TransactionID
    nodeID int  // 当前所在节点

    // Yield/Resume 机制
    yieldCh  chan *YieldCommand
    resumeCh chan interface{}

    // Context 和取消
    ctx    context.Context
    cancel context.CancelFunc

    // +++ 新增：节点资源访问 +++
    nodeAccessor NodeAccessor
}
```

#### MigrateTo 方法

```go
// MigrateTo 迁移到目标节点
func (ctx *TxnContext) MigrateTo(targetNodeID int) (*TxnContext, error) {
    // 1. 构建迁移请求
    migrateCmd := &YieldCommand{
        Type:            YieldTypeMigrateTo,
        MigrateToNodeID: targetNodeID,
    }

    // 2. Yield，等待迁移完成
    resumeVal, err := ctx.Yield(migrateCmd)
    if err != nil {
        return nil, err
    }

    // 3. 提取迁移结果
    migResult := resumeVal.(*MigrationResult)

    // 4. 构建新的 Context
    newCtx := &TxnContext{
        txnID:        ctx.txnID,
        nodeID:       targetNodeID,
        yieldCh:      ctx.yieldCh,   // 复用 channel
        resumeCh:     ctx.resumeCh,
        ctx:          ctx.ctx,
        cancel:       ctx.cancel,
        nodeAccessor: migResult.NodeAccessor,  // 新节点的资源访问器
    }

    return newCtx, nil
}
```

#### 资源访问方法

```go
// GetCache 获取当前节点的 Cache
func (ctx *TxnContext) GetCache() cache.Cache {
    return ctx.nodeAccessor.GetCache()
}

// GetDirectory 获取当前节点的 Directory
func (ctx *TxnContext) GetDirectory() directory.Directory {
    return ctx.nodeAccessor.GetDirectory()
}

// GetDecoder 获取当前节点的 Decoder
func (ctx *TxnContext) GetDecoder() decoder.Decoder {
    return ctx.nodeAccessor.GetDecoder()
}

// GetNode 获取当前节点对象（兼容旧代码）
func (ctx *TxnContext) GetNode() *node.Node {
    return ctx.nodeAccessor.GetNode()
}
```

### 5.2 NodeAccessor 接口

```go
// NodeAccessor 提供访问节点资源的抽象接口
type NodeAccessor interface {
    GetCache() cache.Cache
    GetDirectory() directory.Directory
    GetDecoder() decoder.Decoder
    GetNode() *node.Node
}
```

#### LocalNodeAccessor 实现

```go
// LocalNodeAccessor 直接访问本地节点（零开销）
type LocalNodeAccessor struct {
    node *node.Node
}

func NewLocalNodeAccessor(n *node.Node) *LocalNodeAccessor {
    return &LocalNodeAccessor{node: n}
}

func (a *LocalNodeAccessor) GetCache() cache.Cache {
    caches := a.node.Caches()
    if len(caches) == 0 {
        return nil
    }
    return caches[0]
}

func (a *LocalNodeAccessor) GetDirectory() directory.Directory {
    dirs := a.node.Directories()
    if len(dirs) == 0 {
        return nil
    }
    return dirs[0]
}

func (a *LocalNodeAccessor) GetDecoder() decoder.Decoder {
    // 从 Node.data 获取
    if dec := a.node.GetData("CHI_Decoder"); dec != nil {
        return dec.(decoder.Decoder)
    }
    return nil
}

func (a *LocalNodeAccessor) GetNode() *node.Node {
    return a.node
}
```

### 5.3 YieldCommand 扩展

```go
type YieldType string

const (
    YieldTypeWaitForMessage YieldType = "WaitForMessage"
    YieldTypeWaitForTimeout YieldType = "WaitForTimeout"
    YieldTypeSendOnly       YieldType = "SendOnly"
    YieldTypeComplete       YieldType = "Complete"

    // +++ 新增 +++
    YieldTypeMigrateTo      YieldType = "MigrateTo"
)

type YieldCommand struct {
    Type      YieldType
    WaitFor   *WaitForMessage
    Timeout   time.Duration
    SendQueue []*message.Message

    // +++ 新增：迁移目标 +++
    MigrateToNodeID int
}

// MigrationResult Resume 时传回的迁移结果
type MigrationResult struct {
    NodeAccessor NodeAccessor       // 目标节点的资源访问器
    Message      *message.Message   // 触发 Resume 的消息
}
```

### 5.4 TxnManager 扩展

#### 结构体扩展

```go
type TxnManager struct {
    nodeID int
    node   *node.Node

    // 本地启动的 Transaction
    activeTxns map[dataflow.TransactionID]*activeTxn

    // +++ 新增：迁移来的 Transaction +++
    migratedTxns map[dataflow.TransactionID]*migratedTxn

    mu sync.Mutex
}

type migratedTxn struct {
    txnID        dataflow.TransactionID
    yieldCh      chan *YieldCommand  // 用于接收 Yield
    resumeCh     chan interface{}    // 用于发送 Resume
    sourceNodeID int                 // 来自哪个节点
}
```

#### Tick 方法扩展

```go
func (tm *TxnManager) Tick(cycle uint64, incoming []*message.Message) ([]*message.Message, error) {
    var outgoing []*message.Message

    tm.mu.Lock()
    defer tm.mu.Unlock()

    // 1. 处理传入消息
    for _, msg := range incoming {
        switch {
        case msg.Type == MsgTypeMigrationRequest:
            // 处理迁移请求
            tm.handleMigrationRequest(msg, &outgoing)

        default:
            // 普通消息：路由到等待的 Transaction
            tm.routeMessageToTxn(msg)
        }
    }

    // 2. 处理本地 Transaction 的 Yield
    tm.processLocalYields(cycle, &outgoing)

    // 3. 处理迁移 Transaction 的 Yield
    tm.processMigratedYields(cycle, &outgoing)

    return outgoing, nil
}
```

#### 迁移处理

```go
func (tm *TxnManager) handleMigrationRequest(msg *message.Message, outgoing *[]*message.Message) {
    payload := msg.Payload.(*MigrationPayload)

    // 注册迁移的 Transaction
    tm.migratedTxns[payload.TxnID] = &migratedTxn{
        txnID:        payload.TxnID,
        yieldCh:      payload.YieldCh,
        resumeCh:     payload.ResumeCh,
        sourceNodeID: msg.SourceNodeID,
    }

    // 构建 Resume 值（包含本节点的 NodeAccessor）
    resumeVal := &MigrationResult{
        NodeAccessor: NewLocalNodeAccessor(tm.node),
        Message:      msg,
    }

    // Resume Transaction
    select {
    case payload.ResumeCh <- resumeVal:
        // Transaction 已 Resume，现在在本节点执行
    default:
        // Resume 失败，记录错误
        log.Printf("Failed to resume migrated transaction %v", payload.TxnID)
    }
}

func (tm *TxnManager) processMigratedYields(cycle uint64, outgoing *[]*message.Message) {
    for txnID, mtxn := range tm.migratedTxns {
        select {
        case yieldCmd := <-mtxn.yieldCh:
            switch yieldCmd.Type {
            case YieldTypeMigrateTo:
                // Transaction 要迁移到其他节点
                tm.handleMigrationOut(txnID, mtxn, yieldCmd, outgoing)

            case YieldTypeWaitForMessage:
                // Transaction 在本节点等待消息
                // 处理逻辑与本地 Transaction 相同
                tm.handleWaitForMessage(mtxn.resumeCh, yieldCmd)

            case YieldTypeComplete:
                // Transaction 完成，清理
                delete(tm.migratedTxns, txnID)
            }
        default:
            // Transaction 未 Yield
        }
    }
}
```

### 5.5 消息类型

```go
// 迁移请求 Payload
type MigrationPayload struct {
    TxnID    dataflow.TransactionID
    YieldCh  chan *YieldCommand  // 复用原始 channel
    ResumeCh chan interface{}    // 复用原始 channel
}

// 迁移请求消息构建
func buildMigrationRequest(txnID dataflow.TransactionID, targetNodeID int, ctx *TxnContext) *message.Message {
    payload := &MigrationPayload{
        TxnID:    txnID,
        YieldCh:  ctx.yieldCh,
        ResumeCh: ctx.resumeCh,
    }

    return &message.Message{
        TransactionID: txnID,
        Type:          MsgTypeMigrationRequest,
        SourceNodeID:  ctx.nodeID,
        TargetNodeID:  targetNodeID,
        Payload:       payload,
    }
}
```

---

## 6. 使用示例

### 6.1 分段式 Transaction（现有风格）

```go
// RN 上的 Transaction
func ReadCleanTxn(ctx *transaction.TxnContext, n *node.Node, addr uint64) ([]byte, error) {
    // 1. 检查本地 cache
    cache := chi.GetCHICache(n)
    if cache != nil && cache.IsPresent(addr) {
        return cache.GetData(addr), nil
    }

    // 2. 发送请求到 HN
    decoder, _ := chi.GetCHIDecoder(n)
    result, _ := decoder.DecodeAddress(addr)

    reqMsg := buildReadCleanReq(ctx.TxnID(), n.ID(), result.TargetID, addr)
    ctx.Send(reqMsg)

    // 3. Yield 等待响应
    resp, err := ctx.Yield(&transaction.YieldCommand{
        Type:    transaction.YieldTypeWaitForMessage,
        WaitFor: &transaction.WaitForMessage{Type: chi.OpcodeCompData},
        Timeout: 100 * time.Millisecond,
    })
    if err != nil {
        return nil, err
    }

    // 4. 更新 cache
    respMsg := resp.(*message.Message)
    payload := respMsg.Payload.(*chi.CHIPayload)
    cache.SetData(addr, payload.Data)
    cache.SetState(addr, cache.StateShared)

    return payload.Data, nil
}

// HN 上的 Handler
func HomeNodeReadCleanHandler(ctx *transaction.TxnContext, n *node.Node, req *message.Message) error {
    payload := req.Payload.(*chi.CHIPayload)

    dir := chi.GetCHIDirectory(n)
    data := loadDataFromMemory(payload.Addr)

    respMsg := buildCompDataResp(req.TransactionID, n.ID(), req.SourceNodeID, payload.Addr, data)
    ctx.Send(respMsg)

    dir.AddSharer(payload.Addr, req.SourceNodeID)
    return nil
}
```

### 6.2 连续式 Transaction（新风格）

```go
func WriteUniqueTxnContinuous(ctx *transaction.TxnContext, addr uint64, data []byte) error {
    // ===== 阶段 1: 在 RN 上 =====
    cache := ctx.GetCache()

    // Fast path
    if cache.GetState(addr) == cache.StateModified {
        cache.SetData(addr, data)
        return nil
    }

    // Slow path
    decoder := ctx.GetDecoder()
    result, _ := decoder.DecodeAddress(addr)
    homeNodeID := result.TargetID

    reqMsg := buildWriteUniqueReq(ctx.TxnID(), ctx.NodeID(), homeNodeID, addr)
    ctx.Send(reqMsg)

    // ===== 迁移到 HN =====
    hnCtx, err := ctx.MigrateTo(homeNodeID)
    if err != nil {
        return err
    }

    // ===== 阶段 2: 在 HN 上（同一个 goroutine）=====
    dir := hnCtx.GetDirectory()

    // Snoop 其他 sharers
    sharers := dir.GetSharers(addr)
    for _, sharerID := range sharers {
        snpMsg := buildSnpInvalidate(hnCtx.TxnID(), hnCtx.NodeID(), sharerID, addr)
        hnCtx.Send(snpMsg)
    }

    // 等待所有 snoop 完成
    for range sharers {
        _, err := hnCtx.Yield(&transaction.YieldCommand{
            Type:    transaction.YieldTypeWaitForMessage,
            WaitFor: &transaction.WaitForMessage{Type: chi.OpcodeSnpResp},
            Timeout: 100 * time.Millisecond,
        })
        if err != nil {
            return err
        }
    }

    // 发送 CompData
    compMsg := buildCompData(hnCtx.TxnID(), hnCtx.NodeID(), ctx.NodeID(), addr, data)
    hnCtx.Send(compMsg)

    // 更新 directory
    dir.ClearSharers(addr)
    dir.SetOwner(addr, ctx.NodeID())
    dir.SetState(addr, directory.StateModified)

    // ===== 迁移回 RN =====
    rnCtx, err := hnCtx.MigrateTo(ctx.NodeID())
    if err != nil {
        return err
    }

    // ===== 阶段 3: 回到 RN =====
    cache = rnCtx.GetCache()
    cache.SetData(addr, data)
    cache.SetState(addr, cache.StateModified)

    return nil
}
```

### 6.3 两种模式共存

```go
func TestMixedModes(t *testing.T) {
    // 创建网络
    net := createTestNetwork()
    rn1 := net.GetNode(1)
    rn2 := net.GetNode(2)
    hn := net.GetNode(10)

    // Transaction 1: 使用分段式（简单读）
    txn1 := rn1.TxnManager().Start(func(ctx *transaction.TxnContext) {
        data, err := ReadCleanTxn(ctx, rn1, 0x1000)
        assert.NoError(t, err)
        fmt.Printf("RN1 read: %v\n", data)
    })

    // Transaction 2: 使用连续式（复杂写）
    txn2 := rn2.TxnManager().Start(func(ctx *transaction.TxnContext) {
        err := WriteUniqueTxnContinuous(ctx, 0x1000, []byte{1, 2, 3, 4})
        assert.NoError(t, err)
        fmt.Printf("RN2 write completed\n")
    })

    // 运行仿真
    for cycle := 0; cycle < 100; cycle++ {
        net.Tick(context.Background(), uint64(cycle), 0)
    }

    // 等待完成
    <-txn1
    <-txn2
}
```

---

## 7. 实现路线图

### Phase 1: 核心框架扩展（2-3 天）

**目标**: 扩展 TxnContext 和相关类型，支持迁移

**任务**:
- [ ] 1.1 定义 NodeAccessor 接口
- [ ] 1.2 实现 LocalNodeAccessor
- [ ] 1.3 扩展 TxnContext
  - [ ] 添加 nodeAccessor 字段
  - [ ] 添加 nodeID 字段
  - [ ] 实现 MigrateTo() 方法
  - [ ] 实现 GetCache/GetDirectory/GetDecoder() 方法
- [ ] 1.4 扩展 YieldCommand
  - [ ] 添加 YieldTypeMigrateTo
  - [ ] 添加 MigrateToNodeID 字段
  - [ ] 定义 MigrationResult 类型
- [ ] 1.5 定义 MigrationPayload
- [ ] 1.6 单元测试
  - [ ] 测试 NodeAccessor
  - [ ] 测试 TxnContext.MigrateTo()

**验收标准**:
- TxnContext 可以调用 MigrateTo()
- NodeAccessor 可以访问节点资源
- 所有单元测试通过

### Phase 2: TxnManager 支持（2-3 天）

**目标**: 扩展 TxnManager 支持迁移调度

**任务**:
- [ ] 2.1 扩展 TxnManager 结构体
  - [ ] 添加 migratedTxns map
  - [ ] 定义 migratedTxn 类型
- [ ] 2.2 实现迁移处理
  - [ ] handleMigrationRequest()
  - [ ] processMigratedYields()
  - [ ] handleMigrationOut()
- [ ] 2.3 修改 Tick() 方法
  - [ ] 路由迁移消息
  - [ ] 处理迁移 Transaction 的 Yield
- [ ] 2.4 单元测试
  - [ ] 测试迁移请求处理
  - [ ] 测试迁移 Transaction 调度
  - [ ] 测试迁移来回

**验收标准**:
- TxnManager 可以接收迁移请求
- Transaction 可以在节点间迁移
- 所有单元测试通过

### Phase 3: 集成测试与示例（1-2 天）

**目标**: 验证统一框架可以支持两种模式

**任务**:
- [ ] 3.1 实现示例 Transaction
  - [ ] 分段式：ReadCleanTxn
  - [ ] 连续式：WriteUniqueTxnContinuous
- [ ] 3.2 集成测试
  - [ ] 测试分段式 Transaction
  - [ ] 测试连续式 Transaction
  - [ ] 测试两种模式共存
- [ ] 3.3 性能测试
  - [ ] 对比两种模式的开销
  - [ ] 测试迁移的延迟
- [ ] 3.4 文档更新
  - [ ] 更新 Transaction.md
  - [ ] 添加使用指南
  - [ ] 添加最佳实践

**验收标准**:
- 两种模式都能正常工作
- 两种模式可以共存
- 性能测试通过
- 文档完整

---

## 8. 设计决策记录

### 8.1 为什么使用 NodeAccessor 抽象？

**问题**: 如何在 Transaction 中访问不同节点的资源？

**方案对比**:
1. 直接传递 `*node.Node` → 需要全局 Node 注册表
2. 通过消息访问 → 性能差，代码复杂
3. 使用 NodeAccessor 接口 → ✅ 选择

**优势**:
- 抽象了资源访问方式
- 支持本地直接访问（零开销）
- 支持未来的远程访问扩展
- 保持代码简洁

### 8.2 为什么复用 channel？

**问题**: 迁移时如何通信？

**方案对比**:
1. 每次迁移创建新 channel → 复杂，难以管理
2. 复用原始 channel → ✅ 选择

**优势**:
- 简化实现
- Transaction goroutine 无感知
- Resume 机制统一

### 8.3 为什么支持两种模式？

**问题**: 为什么不只选一种？

**答案**:
- 不同场景需要不同的权衡
- 分段式适合简单、高并发场景
- 连续式适合复杂、需要全局视图的场景
- 统一框架支持两种模式的成本不高

---

## 9. 未来扩展

### 9.1 RemoteNodeAccessor

如果需要在分段式 Transaction 中访问远程节点的信息：

```go
type RemoteNodeAccessor struct {
    nodeID  int
    txnMgr  *TxnManager
}

func (a *RemoteNodeAccessor) GetCache() cache.Cache {
    // 通过消息查询远程节点的 cache 状态
    // 返回一个代理对象
    return newRemoteCacheProxy(a.nodeID, a.txnMgr)
}
```

### 9.2 Transaction 可视化

利用连续式 Transaction 的日志连续性优势：

```go
type TxnTracer struct {
    events []TxnEvent
}

type TxnEvent struct {
    Timestamp time.Time
    NodeID    int
    Phase     string
    Action    string
}

// 自动记录 Transaction 的执行轨迹
// 生成可视化报告
```

### 9.3 DSL 支持

基于连续式 Transaction 实现 DSL：

```go
// DSL 示例
Transaction("WriteUnique").
    OnNode("RN").
        CheckCache(addr).
        SendRequest(hn).
    MigrateTo("HN").
        CheckDirectory(addr).
        SnoopSharers().
        SendResponse(rn).
    MigrateTo("RN").
        UpdateCache(addr).
    Complete()
```

---

## 10. 参考资料

- **Transaction.md**: 基础 Transaction 框架设计
- **CHI_Design.md**: CHI 协议设计文档
- **Go Concurrency Patterns**: Goroutine 和 Channel 最佳实践

---

**文档状态**: 设计完成，待实现
**维护者**: flow_sim 团队
**审阅者**: 待定
