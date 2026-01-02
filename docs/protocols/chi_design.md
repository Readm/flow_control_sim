# CHI 协议仿真设计文档

**文档版本**: 1.0
**最后更新**: 2025-12-02
**适用范围**: flow_sim 项目 CHI (Coherence Hub Interface) 协议实现

---

## 目录

1. [概述](#1-概述)
2. [Transaction 框架设计](#2-transaction-框架设计)
3. [CHI 框架扩展](#3-chi-框架扩展)
4. [CHI 协议实现](#4-chi-协议实现)
5. [测试策略](#5-测试策略)
6. [实现检查清单](#6-实现检查清单)

---

## 1. 概述

### 1.1 CHI 协议简介

CHI (Coherence Hub Interface) 是 ARM AMBA 5 规范的一部分，用于实现多核系统中的缓存一致性。CHI 支持：

- **多种缓存状态**: MESI/MOESI 协议
- **分布式目录**: 基于目录的一致性维护
- **多通道消息**: REQ/RSP/DAT/SNP 四个通道
- **复杂事务流**: 支持 snoop、forward、DMT (Direct Memory Transfer)

### 1.2 设计目标

**核心目标**:
-  **可读性**: Transaction 逻辑用同步代码编写（Yield/Resume 模式）
-  **正确性**: 所有状态修改在 Node.Tick 中串行化
-  **解耦性**: 框架保持协议无关，CHI 零耦合
-  **可测试性**: 完整的单元测试和集成测试覆盖

**非目标**:
-  不实现完整的 CHI 规范（仅实现核心子集）
-  不追求极致性能（以可读性为先）
-  不支持实时硬件仿真

### 1.3 架构概览

```
┌─────────────────────────────────────────────────────────────┐
│                       Application Layer                      │
│  (用户代码：CPU 模拟、一致性测试、性能分析)                    │
└─────────────────────────────────────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    CHI Protocol Layer                        │
│  internal/dataflow/chi/                                      │
│  ├─ transactions.go    - CHI 事务实现                         │
│  ├─ decoder.go         - 地址解码                            │
│  ├─ node_helper.go     - Node 辅助函数                       │
│  ├─ message_builder.go - 消息构建                            │
│  ├─ constants.go       - CHI 操作码                          │
│  └─ types.go           - CHI 特定类型                        │
└─────────────────────────────────────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   Transaction Framework                      │
│  internal/dataflow/transaction/                              │
│  ├─ context.go         - TxnContext (Yield/Resume)          │
│  ├─ manager.go         - TxnManager (调度器)                 │
│  └─ types.go           - YieldCommand, WaitFor 等           │
└─────────────────────────────────────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      Core Framework                          │
│  internal/core/                                              │
│  ├─ node/              - Node 抽象 (+ data map)             │
│  ├─ capability/                                              │
│  │  ├─ cache/         - Cache 接口 (+ snoop 方法)           │
│  │  ├─ directory/     - Directory 接口 (+ writeback)        │
│  │  └─ decoder/       - Decoder 接口 (🆕)                   │
│  ├─ link/             - Link 抽象                           │
│  └─ network/          - Network 管理                        │
└─────────────────────────────────────────────────────────────┘
```

---

## 2. Transaction 框架设计

### 2.1 核心思想: Yield/Resume 模式

**问题**: Go 没有语言级的 `yield`/generator，如何让 Transaction 代码看起来像同步代码？

**解决方案**: 使用 goroutine + channel 封装的 Yield/Resume 模式：

```go
// Transaction 代码示例
func ReadCleanTxn(ctx *transaction.TxnContext, n *node.Node, addr uint64) ([]byte, error) {
    // 1. 检查本地缓存
    c := GetCHICache(n)
    if c != nil && c.IsPresent(addr) {
        state := c.GetState(addr)
        if state != cache.StateInvalid {
            return c.GetData(addr), nil  // 缓存命中，直接返回
        }
    }

    // 2. 缓存缺失，发送请求
    reqMsg := buildReadCleanRequest(ctx, n, addr)
    ctx.Send(reqMsg)

    // 3. Yield 等待响应（关键：看起来像同步代码！）
    result, err := ctx.Yield(&transaction.YieldCommand{
        Type: transaction.YieldTypeWaitForMessage,
        WaitFor: &transaction.WaitForMessage{Type: OpcodeCompData},
        Timeout: 100 * time.Millisecond,
    })
    if err != nil {
        return nil, err
    }

    // 4. 收到响应，更新缓存
    respMsg := result.(*message.Message)
    payload := respMsg.Payload.(*CHIPayload)
    c.SetData(addr, payload.Data)
    c.SetState(addr, cache.StateShared)

    return payload.Data, nil
}
```

**优势**:
- 代码直观，易于理解复杂的状态机逻辑
- 错误处理、超时、重试都是常规的 Go 代码
- 调试友好，调用栈清晰

### 2.2 核心组件

#### 2.2.1 TxnContext

封装 Transaction 的执行上下文：

```go
type TxnContext struct {
    yieldCh  chan *YieldCommand  // Txn -> Manager
    resumeCh chan interface{}    // Manager -> Txn
    ctx      context.Context     // 取消/超时
    nodeID   int
    txnID    dataflow.TransactionID
}

// Yield: 暂停 Transaction，等待外部事件
func (tc *TxnContext) Yield(cmd *YieldCommand) (interface{}, error)

// Send: 发送消息（实际发送在 Tick 中执行）
func (tc *TxnContext) Send(msg *message.Message) error
```

#### 2.2.2 TxnManager

管理所有 Transaction 的生命周期：

```go
type TxnManager struct {
    nodeID        int
    activeTxns    map[dataflow.TransactionID]*activeTxn
    pendingByAddr map[Addr][]*activeTxn
    nextTxnID     int
    mu            sync.Mutex
}

// Start: 启动新 Transaction
func (tm *TxnManager) Start(txnFunc func(*TxnContext)) dataflow.TransactionID

// Tick: 在 Node.Tick 中被调用，处理消息路由和 Transaction 恢复
func (tm *TxnManager) Tick(cycle uint64, incoming []*message.Message) ([]*message.Message, error)
```

#### 2.2.3 YieldCommand

描述 Transaction 的等待意图：

```go
type YieldCommand struct {
    Type      YieldType           // WaitForMessage, WaitForTimeout, etc.
    WaitFor   *WaitForMessage     // 等待条件
    Timeout   time.Duration       // 超时
    SendQueue []*message.Message  // 待发送消息
}

type WaitForMessage struct {
    Type     int      // 消息类型（opcode）
    Addr     *uint64  // 地址过滤（可选）
    SourceID *int     // 源节点过滤（可选）
}
```

### 2.3 Run-in-Tick 模式

**关键设计**: 所有 Node 状态修改必须在 `Node.Tick` 中串行化

```go
func (n *CoherenceNode) Tick(ctx context.Context, cycle uint64, linkDelay time.Duration) error {
    // 1. 从 Pipeline 接收消息
    incomingMsgs := n.receiveMessages(cycle)

    // 2. 调用 TxnManager.Tick（核心！）
    //    - 路由消息到等待的 Transaction
    //    - 恢复 Transaction 执行
    //    - 收集待发送消息
    outgoingMsgs, err := n.txnMgr.Tick(cycle, incomingMsgs)
    if err != nil {
        return err
    }

    // 3. 发送消息
    n.sendMessages(cycle, outgoingMsgs)

    // 4. Tick Pipeline
    for _, flow := range n.Flows() {
        if err := flow.Tick(int(cycle)); err != nil {
            return err
        }
    }

    return nil
}
```

**保证**:
-  所有 cache/directory 状态修改都在 Tick 中执行
-  无并发竞争，无需复杂的锁
-  Transaction goroutine 只负责控制流，不直接修改状态

### 2.4 消息路由与恢复

**TxnManager.Tick 伪代码**:

```go
func (tm *TxnManager) Tick(cycle uint64, incoming []*message.Message) ([]*message.Message, error) {
    var outgoing []*message.Message

    // 1. 路由传入消息到等待的 Transaction
    for _, msg := range incoming {
        if txn := tm.findWaitingTxn(msg); txn != nil {
            // 非阻塞发送恢复值
            select {
            case txn.context.resumeCh <- msg:
                // Transaction 将被恢复
            default:
                // Transaction 未准备好，消息丢失或排队
            }
        }
    }

    // 2. 处理 Transaction 的 Yield 请求
    for _, txn := range tm.activeTxns {
        select {
        case yieldCmd := <-txn.context.yieldCh:
            // 处理 Yield 命令
            tm.handleYield(txn, yieldCmd, &outgoing)
        default:
            // Transaction 未 Yield
        }
    }

    return outgoing, nil
}
```

---

## 3. CHI 框架扩展

### 3.1 设计原则: 零耦合

**核心原则**: 框架必须保持协议无关

**实现方式**:
1. **Node.data map**: 使用字符串键存储协议特定数据
2. **Capability 扩展**: 添加通用方法（非 CHI 特定）
3. **无适配器层**: CHI 直接使用框架接口

```
框架 (协议无关)        CHI (协议特定)
    Node                  GetCHICache(n)
    Cache                 StaticDecoder
    Directory             CHIPayload
    Decoder (接口)         ReadCleanTxn()
```

### 3.2 Node 扩展: data map

**扩展内容** (internal/core/node/node.go):

```go
type Node struct {
    id          int
    inputs      []InputQueue
    outputs     []OutputQueue
    caches      []cache.Cache
    directories []directory.Directory

    // +++ 新增 +++
    dataMu sync.RWMutex
    data   map[string]interface{}  // 协议特定数据

    // ... 其他字段 ...
}

// 协议特定数据访问方法
func (n *Node) SetData(key string, value interface{})
func (n *Node) GetData(key string) interface{}
func (n *Node) HasData(key string) bool
func (n *Node) DeleteData(key string)
func (n *Node) GetAllData() map[string]interface{}
```

**使用示例**:

```go
// CHI 使用字符串键
node.SetData("CHI_Role", "RN")
node.SetData("CHI_Decoder", decoderInstance)
node.SetData("CHI_MessageBuilder", builderInstance)

// 访问（通过 helper 函数提供类型安全）
role, err := GetCHIRole(node)
decoder, err := GetCHIDecoder(node)
```

### 3.3 Cache 接口扩展

**扩展方法** (internal/components/cache/interfaces.go):

```go
// 新增 MOESI 的 Owned 状态
const StateOwned State = "Owned"

// Snoop 响应结构
type SnoopResponse struct {
    ResponseOpcode int
    Data           []byte
    HasData        bool
}

type Cache interface {
    // 原有方法 (7 个)
    GetState(addr uint64) State
    IsPresent(addr uint64) bool
    GetData(addr uint64) []byte
    SetData(addr uint64, data []byte)
    SetState(addr uint64, state State)
    Invalidate(addr uint64)
    SetEvictCallback(callback EvictCallback)

    // +++ 新增 (2 个) +++
    HandleSnoop(snoopOpcode int, addr uint64) (*SnoopResponse, error)
    CanForward(addr uint64) bool
}
```

**语义**:
- `HandleSnoop`: 处理 snoop 请求，返回数据并降级状态
- `CanForward`: 判断是否可以转发数据（M/E/O 可以，S/I 不可以）

**实现** (internal/components/cache/fully_associative.go):

```go
func (c *FullyAssociativeCache) HandleSnoop(snoopOpcode int, addr uint64) (*SnoopResponse, error) {
    c.mu.Lock()
    defer c.mu.Unlock()

    line, exists := c.lines[addr]
    if !exists || line.State == StateInvalid {
        return &SnoopResponse{HasData: false}, nil
    }

    // M/E/O 提供数据
    shouldProvideData := line.State == StateModified ||
        line.State == StateExclusive ||
        line.State == StateOwned

    response := &SnoopResponse{HasData: false}
    if shouldProvideData {
        response.Data = line.Data
        response.HasData = true
    }

    // 降级到 Shared
    if line.State == StateModified || line.State == StateExclusive || line.State == StateOwned {
        line.State = StateShared
    }

    return response, nil
}

func (c *FullyAssociativeCache) CanForward(addr uint64) bool {
    c.mu.RLock()
    defer c.mu.RUnlock()

    line, exists := c.lines[addr]
    if !exists {
        return false
    }

    return line.State == StateModified ||
        line.State == StateExclusive ||
        line.State == StateOwned
}
```

### 3.4 Directory 接口扩展

**扩展方法** (internal/components/directory/interfaces.go):

```go
type Directory interface {
    // 原有方法 (9 个)
    GetState(addr uint64) State
    GetSharers(addr uint64) []int
    AddSharer(addr uint64, nodeID int)
    RemoveSharer(addr uint64, nodeID int)
    ClearSharers(addr uint64)
    GetOwner(addr uint64) int
    SetOwner(addr uint64, nodeID int)
    SetState(addr uint64, state State)
    SetEvictCallback(callback EvictCallback)

    // +++ 新增 (2 个) +++
    MustWaitForWriteback(addr uint64) bool
    HasPendingRequest(addr uint64) bool
}
```

**语义**:
- `MustWaitForWriteback`: Modified 状态需要等待写回
- `HasPendingRequest`: 检测地址冲突（当前返回 false，TODO）

**实现** (internal/components/directory/fully_associative.go):

```go
func (d *FullyAssociativeDirectory) MustWaitForWriteback(addr uint64) bool {
    d.mu.RLock()
    defer d.mu.RUnlock()

    entry, exists := d.entries[addr]
    if !exists {
        return false
    }

    return entry.State == StateModified
}

func (d *FullyAssociativeDirectory) HasPendingRequest(addr uint64) bool {
    // TODO: 需要维护 pending 请求映射
    return false
}
```

### 3.5 Decoder 能力 (新增)

**接口定义** (internal/components/decoder/interfaces.go):

```go
package decoder

// DecodeResult 地址解码结果
type DecodeResult struct {
    Addr       uint64                 // 原始地址
    TargetID   int                    // 目标节点 ID
    Attributes map[string]interface{} // 扩展属性
}

// Decoder 地址解码器接口
type Decoder interface {
    DecodeAddress(addr uint64) (*DecodeResult, error)
}

// 标准属性键
const (
    AttrIsMemory    = "IsMemory"
    AttrIsCacheable = "IsCacheable"
    AttrHomeNodeID  = "HomeNodeID"
    AttrSliceID     = "SliceID"
)
```

**用途**: 将内存地址映射到 Home Node ID，支持：
- 静态映射（所有地址到一个节点）
- 哈希分布（地址均匀分布到多个节点）
- 自定义拓扑（未来扩展）

---

## 4. CHI 协议实现

### 4.1 目录结构

```
internal/dataflow/chi/
├── constants.go         # CHI 操作码定义
├── types.go             # CHIPayload 等类型
├── decoder.go           # StaticDecoder, HashDecoder
├── node_helper.go       # Node.data 辅助函数
├── message_builder.go   # MessageBuilder
├── transactions.go      # Transaction 实现
└── interfaces.go        # 架构文档（纯注释）
```

### 4.2 CHI 操作码 (constants.go)

```go
// REQ 通道
const (
    OpcodeReadClean         = 0x01
    OpcodeReadShared        = 0x00
    OpcodeReadUnique        = 0x03
    // ...
)

// DAT 通道
const (
    OpcodeCompData          = 0x30
    OpcodeSnpRespData       = 0x32
    // ...
)

// SNP 通道
const (
    OpcodeSnpSharedFwd      = 0x40
    OpcodeSnpUniqueFwd      = 0x41
    // ...
)
```

### 4.3 CHI Payload (types.go)

```go
type CHIPayload struct {
    Addr        uint64  // 内存地址
    Opcode      int     // 操作码
    ReturnNID   int     // DMT/Forwarding 目标节点
    ReturnTxnID int     // 原始请求者的 TxnID
    Data        []byte  // 数据
    RespErr     int     // 错误码
    ExtFields   map[string]interface{} // 扩展字段
}

func NewCHIPayload(opcode int, addr uint64) *CHIPayload
func (p *CHIPayload) SetData(data []byte)
func (p *CHIPayload) SetReturnInfo(returnNID int, returnTxnID int)
```

### 4.4 Decoder 实现 (decoder.go)

#### 4.4.1 StaticDecoder

```go
type StaticDecoder struct {
    homeNodeID int
}

func (d *StaticDecoder) DecodeAddress(addr uint64) (*decoder.DecodeResult, error) {
    return &decoder.DecodeResult{
        Addr:     addr,
        TargetID: d.homeNodeID,
        Attributes: map[string]interface{}{
            decoder.AttrIsMemory:    true,
            decoder.AttrIsCacheable: true,
            decoder.AttrHomeNodeID:  d.homeNodeID,
        },
    }, nil
}
```

#### 4.4.2 HashDecoder

```go
type HashDecoder struct {
    numHomeNodes int
    homeNodeBase int
    addressBits  int
}

func (d *HashDecoder) DecodeAddress(addr uint64) (*decoder.DecodeResult, error) {
    // 基于 4KB 页号哈希
    pageNum := addr >> 12
    hash := int(pageNum) % d.numHomeNodes
    homeNodeID := d.homeNodeBase + hash

    return &decoder.DecodeResult{
        Addr:     addr,
        TargetID: homeNodeID,
        Attributes: map[string]interface{}{
            decoder.AttrIsMemory:    true,
            decoder.AttrIsCacheable: true,
            decoder.AttrHomeNodeID:  homeNodeID,
        },
    }, nil
}
```

### 4.5 Node Helper (node_helper.go)

**目的**: 提供类型安全的 Node.data 访问

```go
// 数据键常量
const (
    DataKeyRole           = "CHI_Role"
    DataKeyDecoder        = "CHI_Decoder"
    DataKeyMessageBuilder = "CHI_MessageBuilder"
)

// 节点角色
type NodeRole string
const (
    RoleRN NodeRole = "RN"  // Request Node
    RoleHN NodeRole = "HN"  // Home Node
    RoleSN NodeRole = "SN"  // Slave Node
)

// 配置 CHI 节点
func SetupCHINode(
    n *node.Node,
    role NodeRole,
    dec decoder.Decoder,
    c cache.Cache,
    dir directory.Directory,
)

// 类型安全的访问函数
func GetCHIRole(n *node.Node) (NodeRole, error)
func GetCHIDecoder(n *node.Node) (decoder.Decoder, error)
func GetCHIMessageBuilder(n *node.Node) (*MessageBuilder, error)
func GetCHICache(n *node.Node) cache.Cache
func GetCHIDirectory(n *node.Node) directory.Directory
```

### 4.6 Transaction 实现 (transactions.go)

#### 4.6.1 ReadCleanTxn (RN 视角)

```go
func ReadCleanTxn(
    ctx *transaction.TxnContext,
    n *node.Node,
    addr uint64,
) ([]byte, error) {
    // 1. 获取 CHI 能力
    c := GetCHICache(n)
    decoder, err := GetCHIDecoder(n)
    if err != nil {
        return nil, err
    }
    msgBuilder, err := GetCHIMessageBuilder(n)
    if err != nil {
        return nil, err
    }

    // 2. 检查本地缓存
    if c != nil && c.IsPresent(addr) {
        state := c.GetState(addr)
        if state != cache.StateInvalid {
            return c.GetData(addr), nil
        }
    }

    // 3. 解码地址找到 Home Node
    decodeResult, err := decoder.DecodeAddress(addr)
    if err != nil {
        return nil, fmt.Errorf("decode failed: %w", err)
    }
    homeNodeID := decodeResult.TargetID

    // 4. 构建 ReadClean 请求
    reqPayload := NewCHIPayload(OpcodeReadClean, addr)
    reqMsg := msgBuilder.NewMessage(
        ctx.TxnID(),
        OpcodeReadClean,
        n.ID(),
        homeNodeID,
        reqPayload,
    )

    // 5. 发送并等待 CompData 响应
    if err := ctx.Send(reqMsg); err != nil {
        return nil, err
    }

    result, err := ctx.Yield(&transaction.YieldCommand{
        Type: transaction.YieldTypeWaitForMessage,
        WaitFor: &transaction.WaitForMessage{
            Type: OpcodeCompData,
        },
        Timeout: 100 * time.Millisecond,
    })
    if err != nil {
        return nil, fmt.Errorf("timeout: %w", err)
    }

    // 6. 提取数据并更新缓存
    respMsg := result.(*message.Message)
    payload := respMsg.Payload.(*CHIPayload)

    if c != nil {
        c.SetData(addr, payload.Data)
        c.SetState(addr, cache.StateShared)
    }

    return payload.Data, nil
}
```

#### 4.6.2 HomeNodeReadCleanHandler (HN 视角)

```go
func HomeNodeReadCleanHandler(
    ctx *transaction.TxnContext,
    n *node.Node,
    reqMsg *message.Message,
) error {
    payload := reqMsg.Payload.(*CHIPayload)
    addr := payload.Addr

    // 获取能力
    dir := GetCHIDirectory(n)
    msgBuilder, err := GetCHIMessageBuilder(n)
    if err != nil {
        return err
    }

    if dir == nil {
        return fmt.Errorf("directory not available")
    }

    // 检查目录状态
    dirState := dir.GetState(addr)

    // 简化实现：总是从内存返回数据
    // TODO: 处理 dirty case (snoop owner)
    data := loadDataFromMemory(addr)

    // 构建 CompData 响应
    respPayload := NewCHIPayload(OpcodeCompData, addr)
    respPayload.SetData(data)

    respMsg := msgBuilder.NewMessage(
        reqMsg.TransactionID,
        OpcodeCompData,
        n.ID(),
        reqMsg.SourceNodeID,
        respPayload,
    )

    if err := ctx.Send(respMsg); err != nil {
        return err
    }

    // 更新目录
    if dirState != "Shared" {
        dir.SetState(addr, "Shared")
    }
    dir.AddSharer(addr, reqMsg.SourceNodeID)

    return nil
}
```

#### 4.6.3 SnpSharedFwdHandler (RN Snoop 处理)

```go
func SnpSharedFwdHandler(
    ctx *transaction.TxnContext,
    n *node.Node,
    snpMsg *message.Message,
) error {
    payload := snpMsg.Payload.(*CHIPayload)
    addr := payload.Addr

    // 获取能力
    c := GetCHICache(n)
    msgBuilder, err := GetCHIMessageBuilder(n)
    if err != nil {
        return err
    }

    if c == nil || !c.IsPresent(addr) {
        return fmt.Errorf("snoop miss: addr 0x%x not present", addr)
    }

    // 处理 snoop：获取数据并降级
    resp, err := c.HandleSnoop(OpcodeSnpSharedFwd, addr)
    if err != nil {
        return err
    }

    // 转发数据给请求者
    respPayload := NewCHIPayload(OpcodeSnpRespData, addr)
    respPayload.SetData(resp.Data)
    respPayload.SetReturnInfo(payload.ReturnNID, payload.ReturnTxnID)

    respMsg := msgBuilder.NewMessage(
        snpMsg.TransactionID,
        OpcodeSnpRespData,
        n.ID(),
        payload.ReturnNID,
        respPayload,
    )

    return ctx.Send(respMsg)
}
```

---

## 5. 测试策略

### 5.1 框架扩展测试

#### 5.1.1 Node.data map 测试

**文件**: `internal/core/node/node_test.go`

**覆盖场景**:
-  基本存储/检索/删除
-  多种数据类型（string, int, bool, slice, map, struct）
-  并发安全性（隔离性）
-  删除不存在键的安全性

**示例**:
```go
func TestNodeDataMap(t *testing.T) {
    n := node.New(1)

    // 存储和检索
    n.SetData("test_key", "test_value")
    val := n.GetData("test_key")
    assert.Equal(t, "test_value", val.(string))

    // 检查存在性
    assert.True(t, n.HasData("test_key"))
    assert.False(t, n.HasData("nonexistent"))

    // 删除
    n.DeleteData("test_key")
    assert.False(t, n.HasData("test_key"))
}
```

#### 5.1.2 Cache 新方法测试

**文件**: `internal/components/cache/cache_test.go`

**覆盖场景**:
-  HandleSnoop: 各状态的 snoop 响应
  - M/E/O 提供数据并降级到 S
  - S/I 不提供数据
  - 非存在行返回无数据
-  CanForward: 各状态的转发能力
  - M/E/O 可以转发
  - S/I 不能转发
-  Owned 状态: MOESI 协议支持

**示例**:
```go
func TestFullyAssociativeCache_HandleSnoop(t *testing.T) {
    cache := cache.NewFullyAssociativeCache(4)
    addr := uint64(0x1000)
    testData := []byte{10, 20, 30, 40}

    // 设置为 Modified 状态
    cache.SetData(addr, testData)
    cache.SetState(addr, cache.StateModified)

    // Snoop 应该提供数据
    resp, err := cache.HandleSnoop(0x01, addr)
    assert.NoError(t, err)
    assert.True(t, resp.HasData)
    assert.Equal(t, testData, resp.Data)

    // 状态应该降级到 Shared
    assert.Equal(t, cache.StateShared, cache.GetState(addr))
}
```

#### 5.1.3 Directory 新方法测试

**文件**: `internal/components/directory/directory_test.go`

**覆盖场景**:
-  MustWaitForWriteback: Modified 需要写回
-  HasPendingRequest: 当前实现文档化
-  状态转换表测试
-  Modified 状态完整场景

#### 5.1.4 Decoder 接口测试

**文件**: `internal/components/decoder/decoder_test.go`

**覆盖场景**:
-  接口契约
-  基础字段（Addr, TargetID）
-  标准属性（IsMemory, IsCacheable, HomeNodeID）
-  多地址解码
-  自定义属性扩展

### 5.2 CHI Transaction 测试 (TODO)

**文件**: `internal/dataflow/chi/chi_test.go` (待创建)

**核心场景**:

1. **单核读命中**
   - Transaction 检查 cache hit，直接返回

2. **单核读缺失**
   - RN 发送 ReadClean
   - HN 返回 CompData
   - RN 更新 cache 到 Shared

3. **两核并发读**
   - 两个 RN 同时请求同一地址
   - HN 处理并响应
   - 两个 RN 都获得 Shared 副本

4. **Owner Forward**
   - RN1 持有 Modified 副本
   - RN2 发起 ReadClean
   - HN 发送 SnpSharedFwd 到 RN1
   - RN1 转发数据给 RN2
   - RN1 降级到 Shared

5. **读缺失期间 Snoop**
   - RN1 发送 ReadClean，等待响应
   - 收到 SnpInvalidate 广播
   - 验证 Transaction 如何处理

6. **超时重试**
   - ReadClean 超时
   - Transaction 重试
   - 最终成功或失败

7. **晚到消息**
   - Transaction 完成
   - 晚到的消息被 TxnManager 丢弃

### 5.3 测试辅助工具

#### 5.3.1 MockDecoder

```go
type MockDecoder struct {
    targetID int
}

func (m *MockDecoder) DecodeAddress(addr uint64) (*decoder.DecodeResult, error) {
    return &decoder.DecodeResult{
        Addr:     addr,
        TargetID: m.targetID,
        Attributes: map[string]interface{}{
            decoder.AttrIsMemory:    true,
            decoder.AttrIsCacheable: true,
            decoder.AttrHomeNodeID:  m.targetID,
        },
    }, nil
}
```

#### 5.3.2 MessageCollector

```go
type MessageCollector struct {
    mu       sync.Mutex
    messages []*message.Message
}

func (mc *MessageCollector) Collect(msg *message.Message) {
    mc.mu.Lock()
    defer mc.mu.Unlock()
    mc.messages = append(mc.messages, msg)
}

func (mc *MessageCollector) GetMessages() []*message.Message {
    mc.mu.Lock()
    defer mc.mu.Unlock()
    return append([]*message.Message{}, mc.messages...)
}
```

### 5.4 集成测试

**场景**: 多核 CPU 模拟

```go
func TestMultiCoreCoherence(t *testing.T) {
    // 创建网络
    net := network.New()

    // 创建 2 个 RN + 1 个 HN
    rn1 := createRequestNode(net, 1)
    rn2 := createRequestNode(net, 2)
    hn := createHomeNode(net, 10)

    // 连接节点
    connectNodes(rn1, hn)
    connectNodes(rn2, hn)

    // 场景：RN1 写，RN2 读
    addr := uint64(0x1000)

    // RN1 写
    go func() {
        data := []byte{1, 2, 3, 4}
        err := WriteUniqueTxn(rn1.TxnContext(), rn1, addr, data)
        assert.NoError(t, err)
    }()

    // RN2 读
    go func() {
        data, err := ReadCleanTxn(rn2.TxnContext(), rn2, addr)
        assert.NoError(t, err)
        assert.Equal(t, []byte{1, 2, 3, 4}, data)
    }()

    // 运行仿真
    for cycle := 0; cycle < 100; cycle++ {
        net.Tick(context.Background(), uint64(cycle), 0)
    }

    // 验证最终状态
    assert.Equal(t, cache.StateShared, rn1.Cache().GetState(addr))
    assert.Equal(t, cache.StateShared, rn2.Cache().GetState(addr))
}
```

---

## 6. 实现检查清单

### 6.1 框架扩展 

- [x] Node.data map 实现
  - [x] SetData/GetData/HasData/DeleteData/GetAllData
  - [x] 并发安全（RWMutex）
  - [x] 单元测试覆盖
- [x] Cache 接口扩展
  - [x] StateOwned 常量
  - [x] SnoopResponse 类型
  - [x] HandleSnoop 方法
  - [x] CanForward 方法
  - [x] FullyAssociativeCache 实现
  - [x] 单元测试覆盖
- [x] Directory 接口扩展
  - [x] MustWaitForWriteback 方法
  - [x] HasPendingRequest 方法
  - [x] FullyAssociativeDirectory 实现
  - [x] 单元测试覆盖
- [x] Decoder 能力
  - [x] 接口定义
  - [x] 标准属性常量
  - [x] 单元测试覆盖

### 6.2 CHI 组件 

- [x] constants.go - CHI 操作码
  - [x] REQ/RSP/DAT/SNP 通道操作码
- [x] types.go - CHIPayload
  - [x] CHIPayload 结构体
  - [x] NewCHIPayload/SetData/SetReturnInfo
- [x] decoder.go - Decoder 实现
  - [x] StaticDecoder
  - [x] HashDecoder
- [x] node_helper.go - Helper 函数
  - [x] 数据键常量
  - [x] NodeRole 类型
  - [x] SetupCHINode
  - [x] Get* 辅助函数
- [x] message_builder.go - MessageBuilder
  - [x] NewMessage 方法
- [x] transactions.go - Transaction 实现
  - [x] ReadCleanTxn (RN)
  - [x] ReadSharedTxn (RN)
  - [x] ReadUniqueTxn (RN)
  - [x] HomeNodeReadCleanHandler (HN)
  - [x] HomeNodeReadSharedHandler (HN)
  - [x] SnpSharedFwdHandler (RN Snoop)
- [x] interfaces.go - 架构文档
  - [x] 设计说明
  - [x] 使用示例

### 6.3 测试  (框架) / ⏳ (CHI)

**框架测试** :
- [x] Node.data: 3 个测试（基础、类型、删除）
- [x] Cache: 3 个测试（HandleSnoop、CanForward、Owned）
- [x] Directory: 4 个测试（Writeback、Pending、转换、场景）
- [x] Decoder: 6 个测试（接口、字段、属性、多地址、常量、自定义）

**CHI 测试** ⏳:
- [ ] 单核读命中/缺失
- [ ] 两核并发读
- [ ] Owner forward
- [ ] Snoop 处理
- [ ] 超时重试
- [ ] 晚到消息

### 6.4 文档 

- [x] 综合设计文档 (本文档)
- [x] interfaces.go 架构说明
- [x] 测试覆盖文档

### 6.5 编译与验证 

- [x] 所有包编译通过
- [x] 所有框架测试通过
- [x] 无 race condition（`go test -race`）

---

## 附录 A: 术语表

| 术语 | 说明 |
|------|------|
| **CHI** | Coherence Hub Interface，ARM AMBA 5 一致性协议 |
| **RN** | Request Node，请求节点（通常是 CPU cache） |
| **HN** | Home Node，主节点（目录控制器） |
| **SN** | Slave Node，从节点（内存控制器） |
| **MESI** | Modified, Exclusive, Shared, Invalid 缓存一致性协议 |
| **MOESI** | MESI + Owned 状态 |
| **DMT** | Direct Memory Transfer，直接内存传输 |
| **Snoop** | 缓存嗅探，用于维护一致性 |
| **Yield** | 暂停 Transaction，等待外部事件 |
| **Resume** | 恢复 Transaction 执行 |
| **Run-in-Tick** | 在 Node.Tick 中串行化所有状态修改 |

---

## 附录 B: 参考资料

1. **ARM AMBA CHI Specification**: ARM IHI 0050E
2. **《计算机体系结构：量化研究方法》**: 第 5 章 - 缓存一致性
3. **flow_sim 项目文档**:
   - `doc/core/architecture_relationship.md` - 核心架构关系
   - `internal/core/ahead_port/README.md` - Ahead Port 设计

---

**文档状态**: 已完成
**维护者**: flow_sim 团队
**更新频率**: 随实现迭代更新
