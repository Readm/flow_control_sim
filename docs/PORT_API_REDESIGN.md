# Port API 统一设计方案

## 文档信息
- **创建日期**: 2025-12-18
- **更新日期**: 2025-12-18
- **状态**: 设计方案 - 准备实施
- **相关问题**: Port接口和实现重复，同步逻辑分散在各组件中

---

## 1. 问题背景

### 1.1 当前设计的问题

**接口重复**：
- `InPort` 和 `OutPort` 是两个独立的接口
- `BaseInPort` 和 `BaseOutPort` 提供了几乎相同的实现（channel管理、Plug逻辑）
- 每个组件都要定义自己的port类型（linkInPort、inputQueueInPort等）

**同步逻辑分散**：
- Link和Queue都需要自己处理Ready/Done同步
- ComponentSync逻辑嵌入在各个组件中
- 代码重复，难以维护

**连接复杂**：
- 需要Plug模式来建立连接
- 每个连接需要两个Port实例（upstream的OutPort + downstream的InPort）
- 连接关系不直观

### 1.2 设计目标

1. **Port作为独立实体**：Port是连接两个组件的桥梁，不属于任何组件
2. **一个连接一个Port**：两个组件之间只需要一个Port实例
3. **类型安全**：通过接口视图防止误用API
4. **同步逻辑集中**：所有Ready/Done逻辑由Port管理
5. **组件简化**：Link/Queue只关注业务逻辑，不处理同步

---

## 2. 核心设计理念

### 2.1 概念模型

```
[OutputQueue] ----<Port1>---- [Link] ----<Port2>---- [InputQueue]
      |              |           |             |            |
   上游视角       独立实体      双重角色      独立实体     下游视角
  (InPort)                   (OutPort)                  (OutPort)
                             (InPort)
```

**关键理解**：
- **Port是独立实体**：不属于任何组件，是组件之间的连接
- **Port有两个视角**：
  - 上游组件看到的是 `InPort` 接口（发送数据）
  - 下游组件看到的是 `OutPort` 接口（接收数据）
- **一个Port实例**：同时实现两个接口

### 2.2 架构图

```
                     Port内部结构
          ┌─────────────────────────────────┐
          │   channel: chan PacketWithCycle │
          │   upstreamDone: int             │
          │   downstreamReady: map[int]bool │
          │   pendingPackets: cache         │
          └─────────────────────────────────┘
                      ▲         ▲
                      │         │
          实现InPort接口    实现OutPort接口
                      │         │
          ┌───────────┴─┐   ┌──┴──────────┐
          │  上游视角    │   │   下游视角   │
          │  Send()     │   │  Receive()  │
          │  MarkDone() │   │  WaitDone()  │
          └─────────────┘   └──────────────┘
```

---

## 3. 接口设计

### 3.1 InPort接口（上游组件视角）

```go
// InPort - 上游组件的视角（发送方）
type InPort interface {
    // Send 发送数据到下游
    // 返回 true 表示发送成功，false 表示下游未准备好
    Send(cycle int, pkt PacketWithCycle) bool

    // IsDownstreamReady 检查下游是否ready（非阻塞）
    // 返回 (ready, decided):
    //   - ready: 下游是否准备好
    //   - decided: 是否已做出决策（false表示会阻塞）
    IsDownstreamReady(cycle int) (ready bool, decided bool)

    // MarkDone 标记上游完成了指定周期
    // 用于通知下游可以安全读取该周期的数据
    MarkDone(cycle int)
}
```

### 3.2 OutPort接口（下游组件视角）

```go
// OutPort - 下游组件的视角（接收方）
type OutPort interface {
    // Receive 从上游接收数据
    // 返回属于指定cycle的所有packets
    Receive(cycle int) []packet.Packet

    // WaitUpstreamDone 等待上游完成指定周期
    // 阻塞直到上游调用了 MarkDone(cycle)
    WaitUpstreamDone(cycle int)

    // GetUpstreamDone 获取上游已完成的周期
    // 非阻塞，用于查询状态
    GetUpstreamDone() int

    // UpdateReady 更新下游的ready状态
    // 用于告知上游自己是否准备好接收数据
    UpdateReady(cycle int, ready bool)
}
```

### 3.3 Port结构体（统一实现）

```go
type Port struct {
    // ===== 通信通道 =====
    channel chan PacketWithCycle

    // ===== 上游Done状态 =====
    upstreamDone   int
    upstreamDoneCh chan int
    upstreamDoneMu sync.Mutex

    // ===== 下游Ready状态 =====
    downstreamReady   map[int]bool
    downstreamReadyCh map[int]chan bool
    downstreamReadyMu sync.Mutex

    // ===== 数据缓存（OutPort功能）=====
    pendingPackets map[int][]packet.Packet
    pendingMu      sync.Mutex
}

// Port同时实现InPort和OutPort接口
func (p *Port) Send(cycle int, pkt PacketWithCycle) bool { ... }
func (p *Port) IsDownstreamReady(cycle int) (bool, bool) { ... }
func (p *Port) MarkDone(cycle int) { ... }

func (p *Port) Receive(cycle int) []packet.Packet { ... }
func (p *Port) WaitUpstreamDone(cycle int) { ... }
func (p *Port) GetUpstreamDone() int { ... }
func (p *Port) UpdateReady(cycle int, ready bool) { ... }

// 提供视图转换（类型安全）
func (p *Port) AsInPort() InPort { return p }
func (p *Port) AsOutPort() OutPort { return p }
```

---

## 4. 组件设计

### 4.1 Link结构

```go
type Link struct {
    sourceID, targetID int
    latency, bandwidth int

    // ===== Port引用（接口类型，类型安全）=====
    fromUpstream OutPort  // Link从上游接收数据（只能调用Receive等方法）
    toDownstream InPort   // Link向下游发送数据（只能调用Send等方法）

    // ===== Link自己的业务逻辑 =====
    flowControl    FlowControlStrategy
    pendingPackets []PacketWithCycle
}

// NewLink 创建Link（不创建Port）
func NewLink(sourceID, targetID, latency, bandwidth int) *Link {
    return &Link{
        sourceID:  sourceID,
        targetID:  targetID,
        latency:   latency,
        bandwidth: bandwidth,
        // Port由外部设置
    }
}

// SetUpstreamPort 设置上游Port（Link作为下游）
func (l *Link) SetUpstreamPort(port OutPort) {
    l.fromUpstream = port
}

// SetDownstreamPort 设置下游Port（Link作为上游）
func (l *Link) SetDownstreamPort(port InPort) {
    l.toDownstream = port
}
```

### 4.2 InputQueue结构

```go
type InputQueue struct {
    capacity, inBandwidth int

    // ===== Port引用（只能接收）=====
    fromUpstream OutPort  // 只能调用Receive、WaitUpstreamDone、UpdateReady

    // ===== Queue自己的业务逻辑 =====
    slots      []PacketWithCycle
    freeBitmap []bool
}

func (iq *InputQueue) SetUpstreamPort(port OutPort) {
    iq.fromUpstream = port
}
```

### 4.3 OutputQueue结构

```go
type OutputQueue struct {
    capacity, outBandwidth int

    // ===== Port引用（只能发送）=====
    toDownstream InPort  // 只能调用Send、IsDownstreamReady、MarkDone

    // ===== Queue自己的业务逻辑 =====
    slots []PacketWithCycle
}

func (oq *OutputQueue) SetDownstreamPort(port InPort) {
    oq.toDownstream = port
}
```

---

## 5. 网络构建

### 5.1 手动构建方式

```go
func BuildNetwork() {
    // 1. 创建所有组件
    node0Queue := NewOutputQueue(capacity=32, outBandwidth=8)
    link := NewLink(sourceID=0, targetID=1, latency=5, bandwidth=10)
    node1Queue := NewInputQueue(capacity=32, inBandwidth=8)

    // 2. 创建Port并连接

    // Port1: OutputQueue -> Link
    port1 := NewPort()
    node0Queue.SetDownstreamPort(port1.AsInPort())   // OutputQueue作为上游
    link.SetUpstreamPort(port1.AsOutPort())          // Link作为下游

    // Port2: Link -> InputQueue
    port2 := NewPort()
    link.SetDownstreamPort(port2.AsInPort())         // Link作为上游
    node1Queue.SetUpstreamPort(port2.AsOutPort())    // InputQueue作为下游
}
```

### 5.2 简洁的Connect函数

```go
// Connect 连接两个组件
// upstream: 上游组件（必须有 SetDownstreamPort 方法）
// downstream: 下游组件（必须有 SetUpstreamPort 方法）
// 返回创建的Port实例
func Connect(upstream, downstream Component) *Port {
    port := NewPort()

    // 设置上游组件的下游Port（InPort视图）
    if setter, ok := upstream.(interface{ SetDownstreamPort(InPort) }); ok {
        setter.SetDownstreamPort(port.AsInPort())
    }

    // 设置下游组件的上游Port（OutPort视图）
    if setter, ok := downstream.(interface{ SetUpstreamPort(OutPort) }); ok {
        setter.SetUpstreamPort(port.AsOutPort())
    }

    return port
}

// 使用示例
func BuildNetwork_V2() {
    node0Queue := NewOutputQueue(32, 8)
    link := NewLink(0, 1, 5, 10)
    node1Queue := NewInputQueue(32, 8)

    // 一行代码创建连接
    Connect(node0Queue, link)
    Connect(link, node1Queue)
}
```

---

## 6. 组件实现示例

### 6.1 Link.Tick实现

```go
func (l *Link) Tick(cycle int) error {
    // 1. 从上游接收数据（使用OutPort接口）
    if l.fromUpstream != nil {
        waitCycle := cycle - l.latency
        if waitCycle >= 0 {
            l.fromUpstream.WaitUpstreamDone(waitCycle)
        }
        packets := l.fromUpstream.Receive(waitCycle)

        // 2. Link业务逻辑：延迟、流控
        for _, pkt := range packets {
            targetCycle := cycle + l.latency

            // 检查下游是否ready
            if l.toDownstream != nil {
                ready, _ := l.toDownstream.IsDownstreamReady(targetCycle)
                if ready {
                    // 发送到下游（使用InPort接口）
                    l.toDownstream.Send(targetCycle, PacketWithCycle{
                        Cycle:  targetCycle,
                        Packet: pkt,
                    })
                } else {
                    // 下游未ready，缓存等待重试
                    l.pendingPackets = append(l.pendingPackets, ...)
                }
            }
        }

        // 3. 更新自己的ready状态（告诉上游）
        if l.toDownstream != nil {
            downstreamReady, _ := l.toDownstream.IsDownstreamReady(cycle+1)
            l.fromUpstream.UpdateReady(cycle+1, downstreamReady)
        }
    }

    // 4. 标记完成（告诉下游）
    if l.toDownstream != nil {
        l.toDownstream.MarkDone(cycle)
    }

    return nil
}
```

### 6.2 InputQueue.Tick实现

```go
func (iq *InputQueue) Tick(cycle int) error {
    if iq.fromUpstream == nil {
        return nil
    }

    // 1. 等待上游完成
    iq.fromUpstream.WaitUpstreamDone(cycle - 1)

    // 2. 接收数据（使用OutPort接口）
    packets := iq.fromUpstream.Receive(cycle)

    // 3. Queue业务逻辑：存储packets
    for _, pkt := range packets {
        slot := iq.findFreeSlot()
        if slot >= 0 {
            iq.slots[slot] = PacketWithCycle{Cycle: cycle, Packet: pkt}
            iq.freeBitmap[slot] = false
        }
    }

    // 4. 告诉上游自己的ready状态
    hasCapacity := iq.Length() < iq.Capacity()
    iq.fromUpstream.UpdateReady(cycle+1, hasCapacity)

    return nil
}
```

### 6.3 OutputQueue.Tick实现

```go
func (oq *OutputQueue) Tick(cycle int) error {
    if oq.toDownstream == nil {
        return nil
    }

    sent := 0
    newSlots := make([]PacketWithCycle, 0)

    // 尝试发送slots中的packets
    for _, pkt := range oq.slots {
        if sent >= oq.outBandwidth {
            newSlots = append(newSlots, pkt)
            continue
        }

        // 发送到下游（使用InPort接口）
        if oq.toDownstream.Send(pkt.Cycle, pkt) {
            sent++
        } else {
            // 下游未ready，保留packet
            newSlots = append(newSlots, pkt)
        }
    }

    oq.slots = newSlots

    // 标记完成
    oq.toDownstream.MarkDone(cycle)

    return nil
}
```

---

## 7. 类型安全保证

### 7.1 编译时检查

```go
// ✅ 正确用法
queue := NewInputQueue(32, 8)
queue.fromUpstream.Receive(0)        // ✅ OutPort有这个方法
queue.fromUpstream.UpdateReady(1, true)  // ✅ OutPort有这个方法

// ❌ 编译错误
queue.fromUpstream.Send(0, pkt)      // ❌ 编译失败：OutPort没有Send方法
queue.fromUpstream.MarkDone(0)       // ❌ 编译失败：OutPort没有MarkDone方法
```

### 7.2 接口视图对照表

| 组件角色 | 持有的接口类型 | 可以调用的方法 | 不能调用的方法 |
|---------|--------------|--------------|---------------|
| **上游组件**（发送方） | `InPort` | `Send()`, `IsDownstreamReady()`, `MarkDone()` | `Receive()`, `WaitUpstreamDone()`, `UpdateReady()` |
| **下游组件**（接收方） | `OutPort` | `Receive()`, `WaitUpstreamDone()`, `GetUpstreamDone()`, `UpdateReady()` | `Send()`, `IsDownstreamReady()`, `MarkDone()` |
| **Link**（双重角色） | `OutPort` (from上游)<br>`InPort` (to下游) | 两端分别有不同的方法 | - |

---

## 8. 设计优势

### 8.1 Port作为独立实体

**之前**：
- Port属于组件（Link有inPort/outPort成员）
- 一个连接需要两个Port实例
- Plug模式复杂

**之后**：
- Port是独立的连接对象
- 一个连接只需一个Port实例
- 直接设置引用，简单清晰

### 8.2 同步逻辑集中

**之前**：
- 每个组件都要处理ComponentSync
- Ready/Done逻辑分散
- 代码重复

**之后**：
- 所有同步逻辑在Port内部
- 组件只调用Port的API
- Link/Queue只关注业务逻辑

### 8.3 类型安全

**通过接口视图防止误用**：
- InputQueue只能获得OutPort视图（不能发送）
- OutputQueue只能获得InPort视图（不能接收）
- Link两端有不同的视图

### 8.4 代码简化

| 方面 | 之前 | 之后 |
|-----|------|------|
| **Port类型数量** | BaseInPort + BaseOutPort | 统一的Port |
| **组件port字段** | linkInPort + linkOutPort | fromUpstream + toDownstream |
| **同步逻辑** | 组件内部处理 | Port内部处理 |
| **连接代码** | Plug(upstream, downstream) | Connect(upstream, downstream) |

---

## 9. 实现计划

### Phase 1: Port核心实现
- [ ] 实现Port结构体
- [ ] 实现InPort接口方法（Send、IsDownstreamReady、MarkDone）
- [ ] 实现OutPort接口方法（Receive、WaitUpstreamDone、GetUpstreamDone、UpdateReady）
- [ ] 实现AsInPort/AsOutPort视图转换
- [ ] 编写Port单元测试

### Phase 2: Link重构
- [ ] 修改Link结构体（使用接口类型引用Port）
- [ ] 重构Link.Tick方法
- [ ] 移除linkInPort/linkOutPort类型
- [ ] 更新Link测试

### Phase 3: Queue重构
- [ ] 修改InputQueue结构体
- [ ] 修改OutputQueue结构体
- [ ] 重构Tick方法
- [ ] 移除inputQueueInPort等类型
- [ ] 更新Queue测试

### Phase 4: Network集成
- [ ] 实现Connect函数
- [ ] 更新Network.AddLink等方法
- [ ] 更新Network测试
- [ ] 集成测试

### Phase 5: 清理
- [ ] 移除BaseInPort/BaseOutPort
- [ ] 移除Plug相关代码
- [ ] 移除ComponentSync从组件中（移到Port）
- [ ] 更新文档

---

## 10. 兼容性考虑

### 10.1 接口定义变化

**InPort接口**：
- 移除：`Plug()`
- 新增：`Send()`, `IsDownstreamReady()`, `MarkDone()`

**OutPort接口**：
- 移除：`Plug()`
- 保留：`GetPackets()` → 改为 `Receive()`
- 新增：`WaitUpstreamDone()`, `GetUpstreamDone()`, `UpdateReady()`

### 10.2 迁移策略

**不兼容改动**：
- 这是一次完全重构，不保证向后兼容
- 所有使用Port的代码都需要更新

**渐进式迁移不可行**：
- Port是核心接口，必须一次性切换
- 建议在单独的分支进行重构

---

## 11. 总结

### 11.1 核心思想

1. **Port是独立实体**：连接两个组件的桥梁
2. **一Port双视图**：通过接口保证类型安全
3. **同步逻辑集中**：Port管理Ready/Done，组件简化
4. **组件只引用**：不创建Port，只持有接口引用

### 11.2 关键改进

| 方面 | 改进 |
|-----|------|
| **架构清晰度** | Port作为独立实体，责任明确 |
| **代码复用** | 统一Port实现，消除重复 |
| **类型安全** | 接口视图防止误用API |
| **维护性** | 同步逻辑集中，易于调试 |
| **简洁性** | 一个连接一个Port，代码更少 |

### 11.3 下一步

1. 审查本设计文档
2. 开始Phase 1实现Port核心
3. 逐步重构Link/Queue
4. 运行所有测试
5. 性能验证

---

## 附录：完整示例代码

### A.1 Port完整定义

```go
package ahead_port

import (
    "sync"
    "github.com/Readm/flow_sim/internal/dataflow/packet"
)

type Port struct {
    // 通信通道
    channel chan PacketWithCycle

    // 上游Done状态
    upstreamDone   int
    upstreamDoneCh chan int
    upstreamDoneMu sync.Mutex

    // 下游Ready状态
    downstreamReady   map[int]bool
    downstreamReadyCh map[int]chan bool
    downstreamReadyMu sync.Mutex

    // 数据缓存
    pendingPackets map[int][]packet.Packet
    pendingMu      sync.Mutex
}

func NewPort() *Port {
    return &Port{
        channel:           make(chan PacketWithCycle, 8),
        upstreamDone:      -1,
        upstreamDoneCh:    make(chan int, 1),
        downstreamReady:   make(map[int]bool),
        downstreamReadyCh: make(map[int]chan bool),
        pendingPackets:    make(map[int][]packet.Packet),
    }
}

// ===== InPort接口实现 =====

func (p *Port) Send(cycle int, pkt PacketWithCycle) bool {
    // 检查下游ready
    ready, decided := p.IsDownstreamReady(cycle)
    if !decided || !ready {
        return false
    }
    // 发送到channel
    p.channel <- pkt
    return true
}

func (p *Port) IsDownstreamReady(cycle int) (bool, bool) {
    p.downstreamReadyMu.Lock()
    ready, decided := p.downstreamReady[cycle]
    p.downstreamReadyMu.Unlock()
    return ready, decided
}

func (p *Port) MarkDone(cycle int) {
    p.upstreamDoneMu.Lock()
    if cycle > p.upstreamDone {
        p.upstreamDone = cycle
        select {
        case p.upstreamDoneCh <- cycle:
        default:
        }
    }
    p.upstreamDoneMu.Unlock()
}

// ===== OutPort接口实现 =====

func (p *Port) Receive(cycle int) []packet.Packet {
    // 1. 检查缓存
    p.pendingMu.Lock()
    if cached, ok := p.pendingPackets[cycle]; ok {
        delete(p.pendingPackets, cycle)
        p.pendingMu.Unlock()
        return cached
    }
    p.pendingMu.Unlock()

    // 2. 从channel读取
    var result []packet.Packet
    for {
        select {
        case pwc := <-p.channel:
            if pwc.Cycle == cycle {
                result = append(result, pwc.Packet)
            } else if pwc.Cycle > cycle {
                // 缓存未来的packet
                p.pendingMu.Lock()
                p.pendingPackets[pwc.Cycle] = append(p.pendingPackets[pwc.Cycle], pwc.Packet)
                p.pendingMu.Unlock()
            }
        default:
            return result
        }
    }
}

func (p *Port) WaitUpstreamDone(cycle int) {
    for {
        p.upstreamDoneMu.Lock()
        if p.upstreamDone >= cycle {
            p.upstreamDoneMu.Unlock()
            return
        }
        p.upstreamDoneMu.Unlock()
        <-p.upstreamDoneCh
    }
}

func (p *Port) GetUpstreamDone() int {
    p.upstreamDoneMu.Lock()
    defer p.upstreamDoneMu.Unlock()
    return p.upstreamDone
}

func (p *Port) UpdateReady(cycle int, ready bool) {
    p.downstreamReadyMu.Lock()
    p.downstreamReady[cycle] = ready
    if ch, ok := p.downstreamReadyCh[cycle]; ok {
        select {
        case ch <- ready:
        default:
        }
        delete(p.downstreamReadyCh, cycle)
    }
    p.downstreamReadyMu.Unlock()
}

// ===== 视图转换 =====

func (p *Port) AsInPort() InPort {
    return p
}

func (p *Port) AsOutPort() OutPort {
    return p
}
```

### A.2 Connect函数实现

```go
func Connect(upstream, downstream interface{}) *Port {
    port := NewPort()

    // 设置上游
    if setter, ok := upstream.(interface{ SetDownstreamPort(InPort) }); ok {
        setter.SetDownstreamPort(port.AsInPort())
    } else {
        panic("upstream does not have SetDownstreamPort method")
    }

    // 设置下游
    if setter, ok := downstream.(interface{ SetUpstreamPort(OutPort) }); ok {
        setter.SetUpstreamPort(port.AsOutPort())
    } else {
        panic("downstream does not have SetUpstreamPort method")
    }

    return port
}
```

---

## 12. 周期依赖与死锁分析 (Cycle Dependency & Deadlock Analysis)

在 `ahead_port` 架构中，**OutputQueue**, **Link**, **InputQueue** 三者在 Cycle `N` 的依赖关系如下：

### 12.1 数据流依赖 (Data Flow: Forward)
数据从上游流向下游。下游组件处理 Cycle `N` 的数据时，依赖上游组件在 Cycle `N` (或更早) 的输出。

*   **OutputQueue (Cycle N)**
    *   **依赖**: `Node` 在 Cycle `N` 注入的数据。
    *   **行为**: 尝试将数据 `Push` 给 Link。

*   **Link (Cycle N)**
    *   **依赖**: `OutputQueue` 在 Cycle `N-Latency` 产生的数据。
    *   **行为**: 将数据 `Push` 给 InputQueue。
    *   **关键点**: Link 内部充当了时间的 "传送带"。

*   **InputQueue (Cycle N)**
    *   **依赖**: `Link` 在 Cycle `N` 的输出。
    *   **行为**: 等待 Link 完成 Cycle `N` (`WaitUpstreamDone(N)`)，然后读取所有属于 Cycle `N` 的包。
    *   **时序**: **必须等待 Link 执行完 Cycle N 的逻辑**。

### 12.2 反压信号依赖 (Backpressure: Backward)
Ready 信号从下游流向上游。这是打破死锁的关键，也是跨周期依赖的核心。

*   **InputQueue @ Cycle N (结尾)**
    *   **行为**: 在 Cycle `N` 结束时，计算自己 **Cycle `N+1`** 是否有空位。
    *   **动作**: 调用 `UpdateReady(Cycle=N+1)`。
    *   **意义**: 提前通知上游 "下一周期我不堵"。

*   **Link @ Cycle N+1 (开始)**
    *   **依赖**: `InputQueue` 在 **Cycle `N`** 产生的 Ready 信号。
    *   **行为**: 调用 `PeekReady(Cycle=N+1)`。
    *   **无死锁原因**: 因为这个信号由 **上一个周期 (N)** 产生，所以 Link 在 Cycle `N+1` 读取时 **不需要等待** InputQueue 在 Cycle `N+1` 运行。

### 12.3 执行顺序与死锁避免

**结论：** 模拟器必须先执行 **Link.Tick**，再执行 **Node.Tick (InputQueue.Tick)**。

**死锁场景 (错误的执行顺序)**：
1. 先执行 **Node.Tick(T)** -> 调用 `WaitUpstreamDone(T)` -> 阻塞等待 Link。
2. 此时 Link 还没运行，无法发出 Done 信号。
3. 主线程阻塞在 Node，Link 永远无法运行 -> **死锁**。

**正确场景 (先 Link 后 Node)**：
1. **Link.Tick(T)** 率先执行:
    * 读取 `Ready(T)` 信号 (由 `Node(T-1)` 产生，已就绪)。
    * 推送数据。
    * 发出 `MarkDone(T)` 信号。
2. **Node.Tick(T)** 随后执行:
    * 调用 `WaitUpstreamDone(T)`。
    * 收到 Link 刚发出的 Done 信号，**立即返回**，不阻塞。
    * 读取数据。

此模型解耦了当前的执行依赖：
*   **数据** 是当前周期的 (Strict Sync)。
*   **Ready信号** 是跨周期的 (Lookahead)。
