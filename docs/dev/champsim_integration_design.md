# ChampSim 与框架集成设计

**版本：** v1.0
**日期：** 2025-12-26
**基于：** champsim_review_checklist.md 中的用户反馈

---

## 🎯 集成目标

将 ChampSim O3_CPU 作为框架的 CPU 激励源，生成内存访问请求（Message 级别）。

**核心原则：**
1. **保持 ChampSim 语义**：不强制适配框架的 Transaction
2. **Message 级别集成**：直接生成 CHI/AXI Message
3. **简单优先**：一个 IncentiveHook 对应一个 CPU（1:1）
4. **时钟对齐**：CPU cycle = 框架 cycle（1:1 映射）

---

## 📐 架构设计

### 整体架构图

```
┌─────────────────────────────────────────────────────────────┐
│                      框架 Network                            │
│  ┌───────────┐    ┌───────────┐    ┌───────────┐           │
│  │  Router   │───│  Router   │───│  Cache    │           │
│  └───────────┘    └───────────┘    └───────────┘           │
│         ↑                                  ↑                 │
│         │ CHI/AXI Message                  │                 │
│         │                                  │                 │
└─────────┼──────────────────────────────────┼─────────────────┘
          │                                  │
          │                                  │
┌─────────┼──────────────────────────────────┼─────────────────┐
│         │          ChampSim CPU            │                 │
│  ┌──────┴─────┐                    ┌───────┴────┐           │
│  │  Message   │                    │  Message   │           │
│  │  Generator │                    │  Receiver  │           │
│  └──────┬─────┘                    └───────┬────┘           │
│         │                                  │                 │
│         │        ┌──────────────┐          │                 │
│         └────────│   LSQ        │──────────┘                 │
│                  │ Load Queue   │                            │
│                  │ Store Queue  │                            │
│                  └──────┬───────┘                            │
│                         │                                    │
│                  ┌──────┴───────┐                            │
│                  │   Pipeline   │                            │
│                  │ Fetch→Retire │                            │
│                  └──────┬───────┘                            │
│                         │                                    │
│                  ┌──────┴───────┐                            │
│                  │ TraceReader  │                            │
│                  └──────────────┘                            │
│                         ↑                                    │
└─────────────────────────┼────────────────────────────────────┘
                          │
                   ┌──────┴───────┐
                   │ Trace File   │
                   │  (.xz/.gz)   │
                   └──────────────┘
```

---

## 🔌 接口设计

### 方案：扩展的 IncentiveHook

**原始接口的局限：**
```go
type IncentiveHook interface {
    ShouldCreateTransaction(nodeID int, cycle uint64) bool
    CreateTransaction(nodeID int, cycle uint64) (*transaction.Transaction, error)
}
```

**问题：**
- ❌ 每周期只能返回一个 Transaction
- ❌ 无法传递内存地址等信息
- ❌ Transaction 缺少内存语义字段

**解决方案：按 Message 级别处理**

```go
// ChampSimIncentive 实现 IncentiveHook 接口
// 但内部直接生成 Message，不经过 Transaction
type ChampSimIncentive struct {
    cpu *cpu.O3_CPU

    // Message 生成回调（由框架提供）
    messageCallback func(msg *message.Message) error

    // 待处理的内存请求队列
    pendingRequests map[uint64]*MemoryRequest
}

type MemoryRequest struct {
    InstrID  uint64    // 对应的指令 ID
    Addr     uint64    // 内存地址
    IsWrite  bool      // true=store, false=load
    Size     int       // 数据大小（字节）
    IssuedCycle uint64 // 发出时间
}
```

**核心方法：**

```go
// ShouldCreateTransaction 检查 CPU 是否有待处理的内存请求
func (csi *ChampSimIncentive) ShouldCreateTransaction(nodeID int, cycle uint64) bool {
    // 1. 推进 CPU 一个周期
    csi.cpu.Operate(cycle)

    // 2. 检查 LSQ 中是否有新的内存请求
    return csi.cpu.LSQ.HasPendingMemoryRequest()
}

// CreateTransaction 实际上创建 Message（不是 Transaction）
func (csi *ChampSimIncentive) CreateTransaction(nodeID int, cycle uint64) (*transaction.Transaction, error) {
    // 1. 从 LSQ 获取内存请求
    req := csi.cpu.LSQ.PopNextMemoryRequest()
    if req == nil {
        return nil, nil // 没有待处理请求
    }

    // 2. 生成 CHI/AXI Message
    msg := csi.generateMessage(req, nodeID, cycle)

    // 3. 通过回调发送 Message
    if err := csi.messageCallback(msg); err != nil {
        return nil, err
    }

    // 4. 记录待处理请求（等待响应）
    csi.pendingRequests[req.InstrID] = req

    // 5. 返回 nil（我们不使用 Transaction）
    return nil, nil
}

// generateMessage 将内存请求转换为 CHI/AXI Message
func (csi *ChampSimIncentive) generateMessage(req *MemoryRequest, nodeID int, cycle uint64) *message.Message {
    // 根据请求类型选择 CHI Opcode
    var opcode chi.Opcode
    if req.IsWrite {
        opcode = chi.WriteNoSnpPtl // Store
    } else {
        opcode = chi.ReadNoSnp      // Load
    }

    return &message.Message{
        Type:       message.TypeRequest,
        SrcNode:    nodeID,
        DestNode:   /* 通过地址映射确定 */,
        Addr:       req.Addr,
        Size:       req.Size,
        Opcode:     int(opcode),
        TxnID:      req.InstrID, // 用于关联响应
        Cycle:      cycle,
    }
}
```

---

## 🔄 数据流设计

### 1. 内存请求流程

```
[Trace] → [CPU Pipeline] → [LSQ] → [Message Generator] → [Network]
   │          每周期           │         │                     │
   │          Operate()        │      PopNext()          SendMessage()
   │                           │         │                     │
   └──────> [Instruction] ───> │         └──> [MemoryRequest]  │
                               │                               │
                          [Load Queue]                         │
                          [Store Queue] ─────────────────────────┘
```

**详细步骤：**

1. **CPU 执行**（每个 cycle）：
   ```go
   cpu.Operate(cycle)
   ```
   - Fetch → Decode → Dispatch → Execute
   - Execute 阶段：内存指令进入 LSQ

2. **LSQ 管理**：
   ```go
   // Load 指令准备好时
   if load.ReadyTime <= cycle && !load.FetchIssued {
       lsq.MarkReadyForIssue(load)
   }

   // Store 指令准备好时
   if store.ReadyTime <= cycle && !store.FetchIssued {
       lsq.MarkReadyForIssue(store)
   }
   ```

3. **Message 生成**：
   ```go
   for _, req := range lsq.GetReadyRequests() {
       msg := generateMessage(req)
       messageCallback(msg)
   }
   ```

---

### 2. 内存响应流程

```
[Network] → [Response Handler] → [LSQ] → [CPU Pipeline]
    │              │                 │          │
    │         OnResponse()      UpdateState() CompleteInstr()
    │              │                 │          │
    └──> [Message] └──> [InstrID] ───┴──────────┘
```

**详细步骤：**

1. **响应到达**：
   ```go
   func (csi *ChampSimIncentive) OnMemoryResponse(msg *message.Message, cycle uint64) {
       instrID := msg.TxnID // 从 Message 中获取指令 ID

       // 查找对应的请求
       req, ok := csi.pendingRequests[instrID]
       if !ok {
           // 错误：未找到对应请求
           return
       }

       // 通知 CPU
       csi.cpu.HandleMemoryResponse(instrID, cycle)

       // 清理
       delete(csi.pendingRequests, instrID)
   }
   ```

2. **CPU 更新状态**：
   ```go
   func (cpu *O3_CPU) HandleMemoryResponse(instrID uint64, cycle uint64) {
       // 在 LSQ 中查找对应条目
       entry := cpu.LSQ.FindByInstrID(instrID)
       if entry == nil {
           return
       }

       // 标记内存操作完成
       entry.Completed = true
       entry.CompleteCycle = cycle

       // 在 ROB 中查找对应指令
       instr := cpu.ROB.FindByInstrID(instrID)
       if instr != nil {
           instr.CompletedMemOps++

           // 如果所有内存操作都完成，标记指令完成
           if instr.CompletedMemOps == instr.NumMemOps() {
               instr.Completed = true
           }
       }
   }
   ```

---

## ⏱️ 时钟同步

**决策：1:1 映射（CPU cycle = 框架 cycle）**

```go
// 框架的仿真循环
for cycle := uint64(0); cycle < maxCycles; cycle++ {
    // 1. 推进 CPU
    if incentive.ShouldCreateTransaction(nodeID, cycle) {
        incentive.CreateTransaction(nodeID, cycle)
    }

    // 2. 推进网络
    network.AdvanceCycle()

    // 3. 处理响应
    for _, msg := range network.GetCompletedMessages() {
        incentive.OnMemoryResponse(msg, cycle)
    }
}
```

**关键点：**
- CPU 和网络使用**相同的 cycle 计数器**
- 每个 cycle 先执行 CPU，再执行网络
- 响应在同一个 cycle 内反馈给 CPU

---

## 🏗️ 多核支持

**架构：1 IncentiveHook ↔ 1 CPU（一对一）**

```go
// 创建多个 CPU
cpus := make([]*ChampSimIncentive, numCores)
for i := 0; i < numCores; i++ {
    cpus[i] = NewChampSimIncentive(
        fmt.Sprintf("trace_%d.xz", i),
        uint8(i), // CPU ID
        messageCallback,
    )
}

// 每个 cycle，所有 CPU 并行执行
for cycle := uint64(0); cycle < maxCycles; cycle++ {
    for _, cpu := range cpus {
        if cpu.ShouldCreateTransaction(cpu.NodeID, cycle) {
            cpu.CreateTransaction(cpu.NodeID, cycle)
        }
    }
}
```

**优点：**
- 简单：每个 CPU 独立
- 并行：可以用 goroutine 并行推进多个 CPU
- 隔离：CPU 之间互不影响

---

## 📝 实现细节

### LSQ 的 Message 接口

```go
// cpu/lsq.go

type LoadStoreQueue struct {
    loadQueue  []*LSQEntry
    storeQueue []*LSQEntry

    // 待发送的内存请求队列
    pendingIssues []*LSQEntry
}

// GetReadyRequests 返回准备好发送的内存请求
func (lsq *LoadStoreQueue) GetReadyRequests(currentCycle uint64) []*MemoryRequest {
    var requests []*MemoryRequest

    // 检查 Load Queue
    for _, entry := range lsq.loadQueue {
        if entry.IsReady(currentCycle) && !entry.FetchIssued {
            requests = append(requests, &MemoryRequest{
                InstrID:     entry.InstrID,
                Addr:        entry.VirtualAddr,
                IsWrite:     false,
                Size:        8, // 假设 8 字节
                IssuedCycle: currentCycle,
            })
            entry.FetchIssued = true
        }
    }

    // 检查 Store Queue
    for _, entry := range lsq.storeQueue {
        if entry.IsReady(currentCycle) && !entry.FetchIssued {
            requests = append(requests, &MemoryRequest{
                InstrID:     entry.InstrID,
                Addr:        entry.VirtualAddr,
                IsWrite:     true,
                Size:        8,
                IssuedCycle: currentCycle,
            })
            entry.FetchIssued = true
        }
    }

    return requests
}

// HandleResponse 处理内存响应
func (lsq *LoadStoreQueue) HandleResponse(instrID uint64, cycle uint64) {
    // 在 LQ 中查找
    for _, entry := range lsq.loadQueue {
        if entry.InstrID == instrID {
            entry.Completed = true
            entry.CompleteCycle = cycle
            return
        }
    }

    // 在 SQ 中查找
    for _, entry := range lsq.storeQueue {
        if entry.InstrID == instrID {
            entry.Completed = true
            entry.CompleteCycle = cycle
            return
        }
    }
}
```

---

## 🧪 测试策略

### 单元测试

1. **Message 生成测试**：
   ```go
   func TestGenerateMessage_Load(t *testing.T) {
       req := &MemoryRequest{
           InstrID: 100,
           Addr:    0x1000,
           IsWrite: false,
           Size:    8,
       }

       msg := generateMessage(req, 0, 500)

       assert.Equal(t, chi.ReadNoSnp, msg.Opcode)
       assert.Equal(t, 0x1000, msg.Addr)
   }
   ```

2. **响应处理测试**：
   ```go
   func TestHandleResponse(t *testing.T) {
       // 创建 CPU 和 LSQ
       // 发送请求
       // 模拟响应
       // 验证状态更新
   }
   ```

### 集成测试

1. **端到端测试**：
   ```
   [小 Trace] → [CPU] → [Message] → [Mock Network] → [Response]
                                          ↓
                                    验证 Message 正确性
   ```

---

## 📊 性能考虑

### 内存开销

- **LSQ 大小**：128 (LQ) + 72 (SQ) = 200 条目
- **Pending Requests**：最多 200 个
- **每个 MemoryRequest**：~40 字节
- **总计**：~8 KB / CPU

### 吞吐量

- **每个 cycle**：最多发送 `LQ_WIDTH + SQ_WIDTH` 个请求（默认 ~10）
- **Message 生成**：O(1) 每个请求
- **响应处理**：O(1) 哈希表查找

---

## ✅ 下一步实施

1. **实现 LSQ**（阶段 3）
2. **实现 Message 生成**（阶段 6）
3. **集成测试**（阶段 7）

---

## 参考

- ChampSim 源码：`inc/ooo_cpu.h`, `src/ooo_cpu.cc`
- 框架接口：`pkg/hook/incentive.go`, `internal/dataflow/message/`
