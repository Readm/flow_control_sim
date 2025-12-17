# Port API 双层接口设计方案

## 文档信息
- **创建日期**: 2025-12-18
- **状态**: 设计方案
- **相关问题**: OutputQueue/Link 无法通过 OutPort 接口输出数据，被迫访问内部实现

---

## 1. 问题背景

### 1.1 当前设计的困境

**外部接口（公共 API）**：
```go
type InPort interface {
    TrySendPacket(cycle int, pkt PacketWithCycle) bool
    IsReadyNonBlocking(cycle int) (ready bool, decided bool)
    Plug(out OutPort) chan PacketWithCycle
}

type OutPort interface {
    GetPackets(cycle int) []packet.Packet
    Plug(in InPort) chan PacketWithCycle
}
```

**问题**：
- ✅ **InPort** 完整：外部可以 `TrySendPacket()` 发送数据，模块内部可以从 channel 读取
- ❌ **OutPort** 不完整：外部可以 `GetPackets()` 读取数据，但**模块内部没有 API 输出数据**

### 1.2 当前的 Workaround（破坏封装）

**OutputQueue** (output_queue.go:123):
```go
// 被迫访问 BaseOutPort 内部字段
if oq.outPort.BaseOutPort.DownstreamIn.TrySendPacket(pkt.Cycle, pkt) {
              ^^^^^^^^^^^^  ^^^^^^^^^^^^
              直接访问内部实现，破坏封装！
    sent++
}
```

**Link** (link.go:338):
```go
// 同样被迫访问内部字段
if l.outPort.DownstreamIn.TrySendPacket(targetCycle, pwc) {
             ^^^^^^^^^^^^
    return true
}
```

### 1.3 根本原因

**外部视角 vs 内部视角的冲突**：

```
外部视角（接口使用者）：
[Module A]                    [Module B]
  OutPort  ---------------->   InPort
        GetPackets()      TrySendPacket()
  ✅ 单向接口合理（类型安全）

内部视角（模块实现者）：
[Module A 内部]
  生成数据 --?--> OutPort
              ❌ 缺少内部输出 API！

[Module B 内部]
  InPort --?--> 处理数据
         ✅ 可以从 channel 读取
```

---

## 2. 设计目标

1. **保持外部接口的类型安全**：InPort/OutPort 的方向性约束
2. **提供完整的内部 API**：模块可以通过接口完成所有操作
3. **各模块自行实现逻辑**：接口只定义契约，实现由模块决定
4. **不破坏封装**：不需要访问 `BaseOutPort.DownstreamIn` 等内部字段

---

## 3. 双层接口设计方案

### 3.1 架构概览

```
                    外部使用                          内部使用
                    ========                          ========

  外部模块  ---->  InPort/OutPort  <----  模块边界  ----> InPortInternal/OutPortInternal
                  (公共接口)                              (内部接口)

                  类型安全                                 API 完整
                  方向约束                                 自由实现
```

### 3.2 接口定义

#### 公共接口层（给外部模块使用）

```go
// InPort 是模块的输入端口（外部视角）
// 外部模块调用 TrySendPacket() 向此端口发送数据
type InPort interface {
    // TrySendPacket 尝试向此端口发送数据包
    // 由外部模块（upstream）调用
    TrySendPacket(cycle int, pkt PacketWithCycle) bool

    // IsReadyNonBlocking 检查此端口是否准备好接收数据
    // 由外部模块调用
    IsReadyNonBlocking(cycle int) (ready bool, decided bool)

    // Plug 连接到上游 OutPort
    Plug(out OutPort) chan PacketWithCycle
}

// OutPort 是模块的输出端口（外部视角）
// 外部模块调用 GetPackets() 从此端口读取数据
type OutPort interface {
    // GetPackets 从此端口获取数据包
    // 由外部模块（downstream）调用
    GetPackets(cycle int) []packet.Packet

    // Plug 连接到下游 InPort
    Plug(in InPort) chan PacketWithCycle
}
```

#### 内部接口层（给模块实现者使用）

```go
// InPortInternal 扩展了 InPort，提供模块内部使用的方法
// 模块实现者使用此接口在内部处理接收到的数据
type InPortInternal interface {
    InPort  // 继承公共接口

    // ReceiveFromUpstream 从上游读取数据（模块内部调用）
    // 这是模块内部从 InPort 获取数据的方法
    //
    // 实现说明：
    // - 从内部 channel 读取数据包
    // - 可能会等待上游完成必要的 cycle
    // - 返回属于指定 cycle 的所有数据包
    ReceiveFromUpstream(cycle int) []packet.Packet
}

// OutPortInternal 扩展了 OutPort，提供模块内部使用的方法
// 模块实现者使用此接口将数据输出到 OutPort
type OutPortInternal interface {
    OutPort  // 继承公共接口

    // SendToDownstream 向下游发送数据（模块内部调用）
    // 这是模块内部向 OutPort 输出数据的方法
    //
    // 参数：
    //   cycle: 数据包的目标 cycle
    //   pkt: 要发送的数据包
    //
    // 返回：
    //   true: 发送成功
    //   false: 下游未准备好，发送失败
    //
    // 实现说明：
    // - 检查下游的 ready 状态
    // - 如果 ready，调用下游 InPort.TrySendPacket()
    // - 如果不 ready，返回 false（由调用者决定如何处理）
    SendToDownstream(cycle int, pkt packet.Packet) bool
}
```

### 3.3 基础实现类

```go
// BaseInPort 提供 InPortInternal 的默认实现
// 模块可以嵌入此结构体并覆盖特定方法
type BaseInPort struct {
    InputChan   chan PacketWithCycle  // 接收数据的 channel
    UpstreamOut OutPort               // 上游 OutPort 引用
}

func (p *BaseInPort) ReceiveFromUpstream(cycle int) []packet.Packet {
    // 默认实现：从 InputChan 读取数据
    var result []packet.Packet
    for {
        select {
        case pwc := <-p.InputChan:
            if pwc.Cycle == cycle {
                result = append(result, pwc.Packet)
            }
        default:
            return result
        }
    }
}

// BaseOutPort 提供 OutPortInternal 的默认实现
type BaseOutPort struct {
    OutputChan   chan PacketWithCycle  // 发送数据的 channel（与 downstream 的 InputChan 相同）
    DownstreamIn InPort                // 下游 InPort 引用
}

func (p *BaseOutPort) SendToDownstream(cycle int, pkt packet.Packet) bool {
    if p.DownstreamIn == nil {
        return false  // 没有连接下游
    }

    pwc := PacketWithCycle{
        Cycle:  cycle,
        Packet: pkt,
    }

    // 调用下游的公共接口方法
    return p.DownstreamIn.TrySendPacket(cycle, pwc)
}
```

---

## 4. 使用示例

### 4.1 OutputQueue 的重构

**之前（破坏封装）**：
```go
type OutputQueue struct {
    outPort *outputQueueOutPort
}

func (oq *OutputQueue) Tick(cycle int) error {
    for _, pkt := range oq.slots {
        // ❌ 直接访问内部字段
        if oq.outPort.BaseOutPort.DownstreamIn.TrySendPacket(pkt.Cycle, pkt) {
            sent++
        }
    }
}
```

**之后（使用内部接口）**：
```go
type OutputQueue struct {
    outPort OutPortInternal  // 使用内部接口
}

func (oq *OutputQueue) Tick(cycle int) error {
    for _, pkt := range oq.slots {
        // ✅ 通过接口方法发送
        if oq.outPort.SendToDownstream(pkt.Cycle, pkt.Packet) {
            sent++
        } else {
            // 下游未准备好，保留数据包
            newSlots = append(newSlots, pkt)
        }
    }
}
```

### 4.2 Link 的重构

**之前（访问内部字段）**：
```go
type Link struct {
    inPort  *linkInPort
    outPort *linkOutPort
}

func (l *Link) Tick(cycle int) error {
    // ❌ 直接访问 upstream OutPort
    packets = l.inPort.UpstreamOut.GetPackets(waitCycle)

    // ❌ 直接访问 downstream InPort
    l.outPort.DownstreamIn.TrySendPacket(targetCycle, pwc)
}
```

**之后（使用内部接口）**：
```go
type Link struct {
    inPort  InPortInternal   // 使用内部接口
    outPort OutPortInternal  // 使用内部接口
}

func (l *Link) Tick(cycle int) error {
    // ✅ 通过内部接口读取
    packets := l.inPort.ReceiveFromUpstream(waitCycle)

    // ✅ 通过内部接口发送
    for _, pkt := range packets {
        if !l.outPort.SendToDownstream(targetCycle, pkt) {
            // 处理发送失败
        }
    }
}
```

### 4.3 InputQueue 的实现

```go
type InputQueue struct {
    inPort InPortInternal  // 使用内部接口
}

func (iq *InputQueue) Tick(cycle int) error {
    // ✅ 通过内部接口接收数据
    packets := iq.inPort.ReceiveFromUpstream(cycle)

    // 处理接收到的数据包
    for _, pkt := range packets {
        iq.storePacket(pkt)
    }

    return nil
}
```

---

## 5. 接口对比

### 5.1 外部使用对比

| 场景 | 使用的接口 | 调用的方法 | 说明 |
|------|-----------|-----------|------|
| OutputQueue → Link | `OutPort` / `InPort` | `TrySendPacket` | 外部连接，使用公共接口 |
| Link → InputQueue | `OutPort` / `InPort` | `GetPackets` / `TrySendPacket` | 外部连接，使用公共接口 |

**类型安全**：
```go
func connect(out OutPort, in InPort) {
    // ✅ 编译器保证方向正确
    // ❌ 无法错误地传入两个 InPort 或两个 OutPort
}
```

### 5.2 内部使用对比

| 组件 | 使用的接口 | 调用的方法 | 说明 |
|------|-----------|-----------|------|
| OutputQueue 内部 | `OutPortInternal` | `SendToDownstream` | 输出数据到自己的 OutPort |
| Link 内部（读取） | `InPortInternal` | `ReceiveFromUpstream` | 从自己的 InPort 读取数据 |
| Link 内部（发送） | `OutPortInternal` | `SendToDownstream` | 输出数据到自己的 OutPort |
| InputQueue 内部 | `InPortInternal` | `ReceiveFromUpstream` | 从自己的 InPort 读取数据 |

**API 完整性**：
```go
type OutputQueue struct {
    outPort OutPortInternal  // ✅ 可以调用 SendToDownstream
}

type InputQueue struct {
    inPort InPortInternal   // ✅ 可以调用 ReceiveFromUpstream
}
```

---

## 6. 设计优势

### 6.1 类型安全

**编译时检查方向正确性**：
```go
// ✅ 正确
func connect(out OutPort, in InPort) { ... }
network.Connect(queue.outPort, link.inPort)

// ❌ 编译错误：类型不匹配
network.Connect(queue.inPort, link.inPort)  // 两个 InPort
```

### 6.2 接口隔离

**外部使用者只看到需要的方法**：
```go
// downstream 使用 OutPort
var upstream OutPort = link.outPort
packets := upstream.GetPackets(cycle)  // ✅ 可以
upstream.SendToDownstream(...)         // ❌ 编译错误（方法不存在）
```

### 6.3 实现灵活性

**各模块可以自定义内部行为**：
```go
// Link 可以特殊实现 SendToDownstream
type linkOutPort struct {
    BaseOutPort
    link *Link
}

func (p *linkOutPort) SendToDownstream(cycle int, pkt packet.Packet) bool {
    // Link 的特殊逻辑：检查流控策略
    if !p.link.flowControl.CanSendPacket(cycle) {
        return false
    }

    // 调用默认实现
    return p.BaseOutPort.SendToDownstream(cycle, pkt)
}
```

### 6.4 封装完整性

**不再需要访问内部字段**：
```go
// ❌ 之前：破坏封装
oq.outPort.BaseOutPort.DownstreamIn.TrySendPacket(...)

// ✅ 之后：通过接口
oq.outPort.SendToDownstream(...)
```

---

## 7. 实现计划

### 7.1 重构范围

需要修改的文件：

1. **ahead_port/port.go**
   - 添加 `InPortInternal` 和 `OutPortInternal` 接口
   - 更新 `BaseInPort` 实现 `InPortInternal`
   - 更新 `BaseOutPort` 实现 `OutPortInternal`

2. **queue/input_queue.go**
   - 修改 `InputQueue` 使用 `InPortInternal`
   - 更新 `Tick` 方法使用 `ReceiveFromUpstream()`

3. **queue/output_queue.go**
   - 修改 `OutputQueue` 使用 `OutPortInternal`
   - 更新 `Tick` 方法使用 `SendToDownstream()`

4. **link/link.go**
   - 修改 `Link` 的 port 字段类型
   - 更新 `Tick` 和 `processPackets` 方法

5. **network/network.go**
   - 确保 `Connect` 方法的类型签名仍然使用公共接口
   - 验证外部连接逻辑

### 7.2 实现步骤

#### Phase 1: 定义新接口
- [ ] 在 `ahead_port/port.go` 中添加 `InPortInternal` 和 `OutPortInternal`
- [ ] 为 `BaseInPort` 添加 `ReceiveFromUpstream()` 实现
- [ ] 为 `BaseOutPort` 添加 `SendToDownstream()` 实现

#### Phase 2: 更新 Queue
- [ ] 修改 `InputQueue` 使用 `InPortInternal`
- [ ] 修改 `OutputQueue` 使用 `OutPortInternal`
- [ ] 运行 queue 包的测试

#### Phase 3: 更新 Link
- [ ] 修改 `Link` 的 port 类型
- [ ] 重构 `processPackets` 方法
- [ ] 运行 link 包的测试

#### Phase 4: 验证集成
- [ ] 运行 network 包的测试
- [ ] 运行 node 包的测试
- [ ] 修复 deadlock 问题
- [ ] 验证 benchmark 测试

#### Phase 5: 清理
- [ ] 移除所有对 `BaseOutPort.DownstreamIn` 的直接访问
- [ ] 更新文档和注释
- [ ] 代码审查

### 7.3 兼容性考虑

**向后兼容**：
- 公共接口（`InPort`/`OutPort`）保持不变
- 外部使用代码（如测试）无需修改
- 只有内部实现需要更新

**渐进式迁移**：
- 可以逐个组件迁移
- 新旧实现可以共存（通过类型断言）

---

## 8. 与 ComponentSync 的关系

### 8.1 职责分离

| 组件 | 职责 | 关注点 |
|------|------|--------|
| **Port 接口** | 数据流动 | Send/Receive 数据包 |
| **ComponentSync** | 状态同步 | Ready/Done 状态管理 |

**它们是正交的**：
- Port 不关心同步状态（由 ComponentSync 管理）
- ComponentSync 不关心数据流动（由 Port 管理）

### 8.2 协同工作

```go
// InPort 实现中使用 ComponentSync
type linkInPort struct {
    BaseInPort
    link *Link
}

func (p *linkInPort) TrySendPacket(cycle int, pkt PacketWithCycle) bool {
    // 1. 检查 ready 状态（使用 ComponentSync）
    if !p.link.componentSync.Ready(cycle) {
        return false
    }

    // 2. 发送数据（使用 Port channel）
    p.InputChan <- pkt
    return true
}
```

**分工明确**：
- `ComponentSync.Ready()` 负责同步决策
- `InputChan <- pkt` 负责数据传递

---

## 9. 常见问题（FAQ）

### Q1: 为什么不直接统一为一个 Port 接口？

**答**：统一接口会失去类型安全：
```go
// 统一接口的问题
type Port interface {
    Send(...) bool
    Receive(...) []Packet
}

// ❌ 这些调用都是合法的，但语义错误
inputQueue.port.Send(...)      // InputQueue 不应该输出
outputQueue.port.Receive(...)  // OutputQueue 不应该接收
```

双层设计保留了方向性约束，同时提供完整 API。

### Q2: Internal 接口是否应该导出（大写）？

**答**：应该导出。理由：
- 其他包的组件需要使用（如 queue、link 包）
- 这是公开的扩展接口，不是私有实现细节
- 通过命名（Internal）已经清晰表明了用途

### Q3: 是否所有组件都必须使用 Internal 接口？

**答**：不是。
- **外部连接**：使用公共接口（`InPort`/`OutPort`）
- **内部实现**：使用内部接口（`InPortInternal`/`OutPortInternal`）

示例：
```go
// Network.Connect 仍然使用公共接口
func (n *Network) Connect(out OutPort, in InPort) { ... }

// OutputQueue 内部使用内部接口
type OutputQueue struct {
    outPort OutPortInternal
}
```

### Q4: BaseOutPort.SendToDownstream 和直接访问 DownstreamIn 有什么区别？

**答**：封装和可维护性。

**之前（直接访问）**：
```go
oq.outPort.BaseOutPort.DownstreamIn.TrySendPacket(...)
// - 依赖内部实现细节
// - 如果 DownstreamIn 字段改名或移除，所有调用处都要改
// - 破坏封装
```

**之后（接口方法）**：
```go
oq.outPort.SendToDownstream(...)
// - 通过接口契约
// - 内部实现可以改变，不影响调用者
// - 遵循封装原则
```

### Q5: 这会影响性能吗？

**答**：几乎没有影响。
- 接口方法调用在 Go 中开销很小（虚方法表查找）
- 之前也是通过方法调用（`TrySendPacket`），现在只是换了个入口
- 编译器可能会内联简单的方法

---

## 10. 总结

### 10.1 设计原则

1. **外部简洁，内部完整**：公共接口保持简单，内部接口提供完整功能
2. **类型安全优先**：编译时检查，防止方向错误
3. **封装完整性**：不暴露内部实现细节
4. **实现灵活性**：各模块可以自定义行为

### 10.2 关键改进

| 方面 | 改进前 | 改进后 |
|------|--------|--------|
| **API 完整性** | ❌ OutPort 缺少输出方法 | ✅ OutPortInternal 提供 SendToDownstream |
| **封装** | ❌ 直接访问 BaseOutPort 字段 | ✅ 通过接口方法 |
| **类型安全** | ✅ 已有 InPort/OutPort 区分 | ✅ 保持 |
| **实现灵活性** | ⚠️  受限于基类实现 | ✅ 可以覆盖内部方法 |

### 10.3 下一步行动

1. Review 本设计文档，确认方案
2. 按照 Phase 1-5 实施重构
3. 验证所有测试通过
4. 解决 deadlock 和数据包传输问题
5. 运行性能分析测试

---

## 附录：完整代码示例

### A.1 完整的接口定义

```go
package ahead_port

import "github.com/Readm/flow_sim/internal/dataflow/packet"

// ===== 公共接口（外部使用）=====

type InPort interface {
    TrySendPacket(cycle int, pkt PacketWithCycle) bool
    IsReadyNonBlocking(cycle int) (ready bool, decided bool)
    Plug(out OutPort) chan PacketWithCycle
}

type OutPort interface {
    GetPackets(cycle int) []packet.Packet
    Plug(in InPort) chan PacketWithCycle
}

// ===== 内部接口（模块实现使用）=====

type InPortInternal interface {
    InPort
    ReceiveFromUpstream(cycle int) []packet.Packet
}

type OutPortInternal interface {
    OutPort
    SendToDownstream(cycle int, pkt packet.Packet) bool
}

// ===== 基础实现 =====

type BaseInPort struct {
    InputChan   chan PacketWithCycle
    UpstreamOut OutPort
    self        InPort
}

func (p *BaseInPort) ReceiveFromUpstream(cycle int) []packet.Packet {
    // 实现数据接收逻辑
    var result []packet.Packet
    for {
        select {
        case pwc := <-p.InputChan:
            if pwc.Cycle == cycle {
                result = append(result, pwc.Packet)
            }
        default:
            return result
        }
    }
}

type BaseOutPort struct {
    OutputChan   chan PacketWithCycle
    DownstreamIn InPort
    self         OutPort
}

func (p *BaseOutPort) SendToDownstream(cycle int, pkt packet.Packet) bool {
    if p.DownstreamIn == nil {
        return false
    }

    pwc := PacketWithCycle{
        Cycle:  cycle,
        Packet: pkt,
    }

    return p.DownstreamIn.TrySendPacket(cycle, pwc)
}
```

### A.2 OutputQueue 完整示例

```go
package queue

import "github.com/Readm/flow_sim/internal/core/ahead_port"

type OutputQueue struct {
    slots        []packet.PacketWithCycle
    capacity     int
    outBandwidth int
    outPort      ahead_port.OutPortInternal  // 使用内部接口
}

type outputQueueOutPort struct {
    ahead_port.BaseOutPort
    outputQueue *OutputQueue
}

func (p *outputQueueOutPort) WaitDone(targetCycle int) {
    p.outputQueue.waitDone(targetCycle)
}

func (p *outputQueueOutPort) Plug(in ahead_port.InPort) chan ahead_port.PacketWithCycle {
    return p.BaseOutPort.PlugWithSelf(p, in)
}

func NewOutputQueue(capacity int, inBandwidth int, outBandwidth int) *OutputQueue {
    oq := &OutputQueue{
        slots:        make([]packet.PacketWithCycle, 0, capacity),
        capacity:     capacity,
        outBandwidth: outBandwidth,
    }

    oq.outPort = &outputQueueOutPort{
        outputQueue: oq,
    }

    return oq
}

func (oq *OutputQueue) Tick(cycle int) error {
    sent := 0
    newSlots := make([]packet.PacketWithCycle, 0, len(oq.slots))

    for _, pkt := range oq.slots {
        if sent >= oq.outBandwidth {
            newSlots = append(newSlots, pkt)
            continue
        }

        // ✅ 通过内部接口发送
        if oq.outPort.SendToDownstream(pkt.Cycle, pkt.Packet) {
            sent++
        } else {
            // 下游未准备好，保留数据包
            newSlots = append(newSlots, pkt)
        }
    }

    oq.slots = newSlots
    oq.setDone(cycle)
    return nil
}
```
