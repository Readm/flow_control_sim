# Link 组件重构文档

## 概述

本文档详细描述了 `Link` 组件从旧接口重构为基于 `CyclePort` 的新接口的过程。重构后的 `Link` 支持多上游端口聚合、延迟和带宽限制，并完全集成到同步周期处理框架中。

## 重构前后对比

### 重构前（旧接口）

```go
// 旧接口方法
type Link struct {
    slots              [][]packet.Packet
    backpressured      bool
    currentCycle       uint64
    sendFinishedCycle  uint64
    noBackpressureUntil uint64
    // ...
}

// 旧方法
func NewLink(sourceID int, targetFlow Flow, sourceFlow Flow, dispatchQueueIndex int, latency, bandwidth, slotCount uint64) *Link
func (l *Link) Transmit(cycle uint64, pkt packet.Packet)
func (l *Link) Advance(cycle uint64)
func (l *Link) SetNoBackpressureUntil(cycle uint64)
func (l *Link) ReadFromFlow(cycle uint64)
func (l *Link) IsBackpressured() bool
func (l *Link) CurrentCycle() uint64
func (l *Link) SendFinishedCycle() uint64
```

**特点：**
- 直接操作 Flow 对象
- 手动管理 backpressure
- 需要手动调用 `Advance()` 推进周期
- 单上游、单下游设计
- 通过 `ReadFromFlow()` 从 Flow 的 dispatch_queue 读取包

### 重构后（新接口）

```go
// 新接口
type Link struct {
    sourceID          int
    targetID          int
    upstreamPorts     []cycle_port.CyclePort  // 支持多个上游端口
    downstreamPort    cycle_port.CyclePort    // 单个下游端口
    processor         *cycle_port.CycleProcessor
    packetProc        *LinkPacketProcessor
    latency           uint64
    bandwidth         uint64
    totalBackpressure uint64  // 累积的 backpressure 周期数，用于调整 slot 索引
}

// 新方法
func NewLink(sourceID, targetID int, upstreamPorts []cycle_port.CyclePort, downstreamPort cycle_port.CyclePort, latency, bandwidth uint64) *Link
func (l *Link) ProcessCycle(cycle int) error
func (l *Link) UpstreamPorts() []cycle_port.CyclePort
func (l *Link) DownstreamPort() cycle_port.CyclePort
func (l *Link) SnapshotOccupancy() []int
```

**特点：**
- 基于 `CyclePort` 的同步通信
- 自动管理 DoneUntil 和 Ready 状态
- 通过 `ProcessCycle()` 统一处理周期
- 支持多上游端口聚合（通过 `MultiUpstreamPort`）
- 延迟和带宽限制通过 `LinkPacketProcessor` 实现

## 核心组件

### 1. Link 结构体

```go
type Link struct {
    sourceID       int                        // 源节点 ID
    targetID       int                        // 目标节点 ID
    upstreamPorts  []cycle_port.CyclePort    // 上游端口列表（支持多个）
    downstreamPort cycle_port.CyclePort      // 下游端口（单个）
    processor      *cycle_port.CycleProcessor // 周期处理器
    packetProc     *LinkPacketProcessor      // 包处理器
    latency        uint64                     // 延迟（周期数）
    bandwidth      uint64                     // 带宽（每周期最大包数）
}
```

### 2. LinkPacketProcessor

`LinkPacketProcessor` 实现了 `PacketProcessor` 接口，负责处理延迟和带宽限制：

```go
type LinkPacketProcessor struct {
    link           *Link
    pendingPackets []cycle_port.PacketWithCycle  // 待发送的包
    slots          [][]cycle_port.PacketWithCycle // 延迟槽（环形缓冲区）
}
```

**关键设计：**
- `slots`: 环形缓冲区，大小为 `latency`，用于存储延迟发送的包
- `pendingPackets`: 存储因下游未就绪或带宽限制而无法立即发送的包

## 主要函数详解

### NewLink - 构造函数

**函数签名：**
```go
func NewLink(sourceID int, targetID int, upstreamPorts []cycle_port.CyclePort, 
             downstreamPort cycle_port.CyclePort, latency uint64, bandwidth uint64) *Link
```

**流程图：**

```
开始
  |
  v
检查参数有效性
  | (latency == 0) -> latency = 1
  | (bandwidth == 0) -> bandwidth = 1
  | (len(upstreamPorts) == 0) -> panic
  |
  v
创建 Link 结构体
  |
  v
创建 LinkPacketProcessor
  | -> 初始化 slots (大小为 latency)
  | -> 初始化 pendingPackets
  |
  v
处理上游端口
  | (len(upstreamPorts) == 1) -> 直接使用
  | (len(upstreamPorts) > 1) -> 创建 MultiUpstreamPort 聚合
  |
  v
创建 CycleProcessor
  | -> upstreamPort: 上游端口（单个或 MultiUpstreamPort）
  | -> downstreamPort: 下游端口
  | -> processor: LinkPacketProcessor
  |
  v
返回 Link
  |
结束
```

**代码实现：**

```143:185:internal/core/link/link.go
// NewLink creates a link with the specified upstream ports and downstream port.
// - sourceID: ID of the source node
// - targetID: ID of the target node
// - upstreamPorts: list of CyclePorts from source Flows (can be multiple)
// - downstreamPort: CyclePort to target Flow (single)
// - latency: number of cycles for packet delivery (defaults to 1 if 0)
// - bandwidth: maximum packets per cycle (defaults to 1 if 0)
func NewLink(sourceID int, targetID int, upstreamPorts []cycle_port.CyclePort, downstreamPort cycle_port.CyclePort, latency uint64, bandwidth uint64) *Link {
	if latency == 0 {
		latency = 1
	}
	if bandwidth == 0 {
		bandwidth = 1
	}
	if len(upstreamPorts) == 0 {
		panic("Link requires at least one upstream port")
	}

	link := &Link{
		sourceID:       sourceID,
		targetID:       targetID,
		upstreamPorts:  upstreamPorts,
		downstreamPort: downstreamPort,
		latency:        latency,
		bandwidth:      bandwidth,
	}

	// Create packet processor
	link.packetProc = NewLinkPacketProcessor(link)

	// Create multi-upstream port if multiple upstream ports
	var upstreamPort cycle_port.CyclePort
	if len(upstreamPorts) == 1 {
		upstreamPort = upstreamPorts[0]
	} else {
		upstreamPort = cycle_port.NewMultiUpstreamPort(upstreamPorts)
	}

	// Create cycle processor
	link.processor = cycle_port.NewCycleProcessor(upstreamPort, downstreamPort, link.packetProc)

	return link
}
```

### ProcessCycle - 周期处理入口

**函数签名：**
```go
func (l *Link) ProcessCycle(cycle int) error
```

**流程图：**

```
开始 ProcessCycle(cycle)
  |
  v
调用 processor.ProcessCycle(cycle)
  |
  v
[CycleProcessor 内部流程]
  |
  v
1. WaitForDoneUntil(cycle)
  | -> 等待上游 DoneUntil >= cycle
  | -> 使用条件变量，避免忙等待
  |
  v
2. 调用 LinkPacketProcessor.ProcessPackets()
  | -> 处理包接收、延迟、带宽限制
  |
  v
3. 设置下游 DoneUntil = cycle + 1
  |
  v
4. 断言上游 cycle+1 已配置 Ready
  |
  v
返回 nil
  |
结束
```

**代码实现：**

```207:210:internal/core/link/link.go
// ProcessCycle processes a single cycle.
func (l *Link) ProcessCycle(cycle int) error {
	return l.processor.ProcessCycle(cycle)
}
```

### LinkPacketProcessor.ProcessPackets - 核心包处理逻辑

**函数签名：**
```go
func (l *LinkPacketProcessor) ProcessPackets(
	receiveChan <-chan cycle_port.PacketWithCycle,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(cycle_port.PacketWithCycle),
	setDoneUntil func(int),
	updateUpstreamReady func(cycle int, ready bool),
)
```

**详细流程图：**

```
开始 ProcessPackets(cycle)
  |
  v
[阶段 0: 异步通知上游 Ready 状态]
  |
  v
0.1 启动 goroutine
  | -> updateUpstreamReady(cycle+1, checkReady(cycle+1))
  | -> 透明转发下游的 Ready 状态到上游
  |
  v
[阶段 1: 收集包]
  |
  v
1.1 将 pendingPackets 添加到 incomingPackets
  |
  v
1.2 非阻塞接收所有可用包
  | -> 循环从 receiveChan 接收
  | -> 直到 channel 为空
  |
  v
[阶段 2: 处理新收到的包]
  |
  v
2.1 对每个 incomingPacket：
  |
  v
  2.1.1 计算目标周期
  | -> targetCycle = sourceCycle + latency
  |
  v
  2.1.2 验证周期（防止过去周期）
  | -> if cycle < targetCycle: panic("Past cycle detected")
  |
  v
  2.1.3 处理回绕情况
  | -> if targetCycle-cycle >= latency:
  |    | -> 放入 pendingPackets（超过一个完整循环）
  |    | -> continue
  |
  v
  2.1.4 计算目标槽索引（考虑 backpressure）
  | -> targetSlotIndex = (targetCycle - totalBackpressure) % len(slots)
  | -> 使用 totalBackpressure 调整索引，处理 backpressure 延迟
  |
  v
  2.1.5 检查带宽限制并放入槽
  | -> if len(slots[targetSlotIndex]) >= bandwidth:
  |    | -> panic("Slot is full")  // 槽已满，直接报错
  | -> else:
  |    | -> 放入 slots[targetSlotIndex]
  |
  v
[阶段 3: 更新 pendingPackets]
  |
  v
3.1 更新 pendingPackets = newPendingPackets
  |
  v
[阶段 4: 发送延迟槽中的包]
  |
  v
4.1 检查下游是否 ready
  |
  |-- [checkReady(cycle) == true] -> 发送路径
  |   |
  |   v
  |   4.1.1 计算当前周期的槽索引（考虑 backpressure）
  |   | -> slotIndex = (cycle - totalBackpressure) % len(slots)
  |   |
  |   v
  |   4.1.2 从槽中取出所有包
  |   | -> 对每个包：
  |   |    | -> 设置 pkt.Cycle = cycle（更新为当前周期）
  |   |    | -> sendPacket(pkt)
  |   |
  |   v
  |   4.1.3 清空槽
  |   | -> slots[slotIndex] = nil
  |
  |-- [checkReady(cycle) == false] -> Backpressure 路径
  |   |
  |   v
  |   4.1.4 增加 totalBackpressure
  |   | -> totalBackpressure = totalBackpressure + 1
  |   | -> 延迟槽索引，等待下游就绪
  |
  v
[阶段 5: 更新状态]
  |
  v
5.1 设置下游 DoneUntil = cycle + 1
  | -> setDoneUntil(cycle + 1)
  |
  v
5.2 等待异步通知完成
  | -> wg.Wait()  // 等待 updateUpstreamReady goroutine 完成
  |
  v
结束
```

**代码实现：**

```47:133:internal/core/link/link.go
// ProcessPackets processes packets for Link: receive from upstream, apply latency, and send to downstream.
func (l *LinkPacketProcessor) ProcessPackets(
	receiveChan <-chan cycle_port.PacketWithCycle,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(cycle_port.PacketWithCycle),
	setDoneUntil func(int),
	updateUpstreamReady func(cycle int, ready bool),
) {
	// Link just trasparency forward the updateUpstreamReady call to the upstream ports.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		updateUpstreamReady(cycle+1, checkReady(cycle+1))
	}()
	// Collect all incoming packets
	var incomingPackets []cycle_port.PacketWithCycle

	// Process pending packets first
	incomingPackets = append(incomingPackets, l.pendingPackets...)

	// Receive all available packets from channel (non-blocking, drain all)
	for {
		select {
		case pkt := <-receiveChan:
			incomingPackets = append(incomingPackets, pkt)
		default:
			goto process
		}
	}

process:
	// Process incoming packets: apply latency and bandwidth constraints
	newPendingPackets := make([]cycle_port.PacketWithCycle, 0)

	// Process new incoming packets: add latency and put into slots
	for _, pkt := range incomingPackets {
		sourceCycle := int(pkt.Cycle)
		targetCycle := sourceCycle + int(l.link.latency)
		if cycle < targetCycle {
			panic("Past cycle detected in link processing")
		}

		// Create packet with target cycle
		delayedPkt := cycle_port.PacketWithCycle{
			Cycle:  uint64(targetCycle),
			Packet: pkt.Packet,
		}

		// Future cycle: put into slot
		// If the target cycle is more than or equal to one full loop ahead, treat as after wraparound and put into pendingPackets.
		if targetCycle-cycle >= int(l.link.latency) {
			newPendingPackets = append(newPendingPackets, delayedPkt)
			continue
		}
		targetSlotIndex := (targetCycle - int(l.link.totalBackpressure)) % len(l.slots)
		// Check bandwidth limit for the target slot
		if len(l.slots[targetSlotIndex]) >= int(l.link.bandwidth) {
			panic("Slot is full (bandwidth limit exceeded)")
		} else {
			l.slots[targetSlotIndex] = append(l.slots[targetSlotIndex], delayedPkt)
		}
	}

	// Update pending packets
	l.pendingPackets = newPendingPackets

	// If the downstream is ready, send the packets from the slots.
	if checkReady(cycle) {
		slotIndex := int(cycle-int(l.link.totalBackpressure)) % len(l.slots)
		for _, pkt := range l.slots[slotIndex] {
			pkt.Cycle = uint64(cycle)
			sendPacket(pkt)
		}
		l.slots[slotIndex] = nil // Clear the slot
	} else {
		l.link.totalBackpressure = l.link.totalBackpressure + 1
	}

	// Set DoneUntil
	setDoneUntil(cycle + 1)

	// Notify upstream that we are ready for next cycle using waitGroup to wait for completion

	wg.Wait()
}
```

## 关键设计决策

### 1. 延迟实现：环形缓冲区（Ring Buffer）与 Backpressure 调整

**设计：**
- 使用大小为 `latency` 的环形缓冲区 `slots`
- 每个槽存储将在特定周期发送的包
- 槽索引计算考虑 `totalBackpressure`：`targetSlotIndex = (targetCycle - totalBackpressure) % len(slots)`

**Backpressure 机制：**
- 当下游未就绪时，`totalBackpressure` 递增
- `totalBackpressure` 用于调整槽索引，延迟包的发送时机
- 当下游恢复就绪时，从调整后的槽索引中取出包并发送

**示例（latency = 3）：**
```
正常情况（totalBackpressure = 0）:
  Cycle 0: 包放入 slot[(0+3-0) % 3] = slot[0] (将在 cycle 3 发送)
  Cycle 1: 包放入 slot[(1+3-0) % 3] = slot[1] (将在 cycle 4 发送)
  Cycle 3: 从 slot[(3-0) % 3] = slot[0] 取出包并发送

Backpressure 情况（totalBackpressure = 1）:
  Cycle 0: 包放入 slot[(0+3-1) % 3] = slot[2]
  Cycle 3: 下游不 ready，totalBackpressure = 1
  Cycle 4: 下游 ready，从 slot[(4-1) % 3] = slot[0] 取出包并发送
```

**优势：**
- O(1) 时间复杂度的槽访问
- 内存占用固定（不随延迟增长）
- 自动处理周期回绕
- Backpressure 通过调整索引实现，无需移动包

### 2. 带宽限制实现

**单层限制：**

1. **每槽限制**：`len(slots[targetSlotIndex]) < bandwidth`
   - 控制每个延迟槽中存储的包数量
   - 防止未来周期槽溢出
   - **如果槽已满，直接 panic**（不再放入 pendingPackets）

**处理策略：**
- 槽容量 = `bandwidth`，确保每个周期最多发送 `bandwidth` 个包
- 如果 `targetCycle-cycle >= latency`（回绕情况），放入 `pendingPackets`
- 槽已满时 panic，要求调用方确保带宽配置合理

**注意：**
- 新实现移除了每周期发送时的带宽检查
- 带宽限制完全通过槽容量控制
- 每个槽最多存储 `bandwidth` 个包，确保每个周期最多发送 `bandwidth` 个包

### 3. 多上游端口聚合

**实现：**
- 如果 `len(upstreamPorts) == 1`：直接使用单个端口
- 如果 `len(upstreamPorts) > 1`：创建 `MultiUpstreamPort` 聚合所有上游端口

**MultiUpstreamPort 行为：**
- `WaitForDoneUntil(cycle)`：等待所有上游端口的 DoneUntil >= cycle
- `ReceiveChan()`：聚合所有上游端口的包
- `UpdateReady(cycle, ready)`：更新所有上游端口的 Ready 状态

### 4. 同步机制

**DoneUntil 管理：**
- Link 在 `ProcessPackets` 结束时设置 `downstreamPort.DoneUntil = cycle + 1`
- 异步通知上游 `cycle+1` 的 Ready 状态：在 goroutine 中调用 `updateUpstreamReady(cycle+1, checkReady(cycle+1))`
- 使用 `WaitGroup` 等待异步通知完成，确保状态一致性

**Ready 检查：**
- 在发送延迟槽中的包前检查 `checkReady(cycle)`
- 如果下游未就绪：
  - 不发送包
  - 增加 `totalBackpressure`，延迟槽索引
  - 包保留在槽中，等待后续周期发送
- 如果下游就绪：
  - 从当前周期的槽中取出所有包并发送
  - 清空槽

**透明转发机制：**
- Link 透明地将下游的 Ready 状态转发给上游
- `updateUpstreamReady(cycle+1, checkReady(cycle+1))` 确保上游知道 Link 在 cycle+1 的可用性

## 数据流示例

### 示例 1：单包传输（latency = 2, totalBackpressure = 0）

```
Cycle 0:
  Flow0 -> outPort -> Link.receiveChan
  Link.ProcessCycle(0):
    - 启动 goroutine: updateUpstreamReady(1, checkReady(1))
    - 接收包 (sourceCycle=0)
    - 计算 targetCycle = 0 + 2 = 2
    - 验证: cycle(0) >= targetCycle(2)? No -> 继续
    - 检查回绕: (2-0) >= 2? No -> 继续
    - 计算槽索引: (2-0) % 3 = 2
    - 放入 slots[2]

Cycle 1:
  Link.ProcessCycle(1):
    - 启动 goroutine: updateUpstreamReady(2, checkReady(2))
    - 无新包接收
    - 检查下游 ready(1): false
    - totalBackpressure = 1

Cycle 2:
  Link.ProcessCycle(2):
    - 启动 goroutine: updateUpstreamReady(3, checkReady(3))
    - 检查下游 ready(2): true
    - 计算槽索引: (2-1) % 3 = 1
    - 从 slots[1] 取出包（空，因为包在 slots[2]）
    - 注意：由于 totalBackpressure=1，实际应该从 slots[(2-1)%3]=slots[1] 读取
    - 但包在 slots[2]，需要等待 totalBackpressure 恢复

Cycle 3:
  Link.ProcessCycle(3):
    - 检查下游 ready(3): true
    - 计算槽索引: (3-1) % 3 = 2
    - 从 slots[2] 取出包
    - 发送包到 downstreamPort
    - Flow1 在 ProcessCycle(3) 时接收包
```

### 示例 2：带宽限制（bandwidth = 2）

```
Cycle 0:
  Link 收到 3 个包，都将在 cycle 2 发送
  Link.ProcessCycle(0):
    - 计算 targetCycle = 0 + 2 = 2
    - 计算槽索引: (2-0) % 3 = 2
    - 前 2 个包：放入 slots[2]（容量=2）
    - 第 3 个包：尝试放入 slots[2]，但已满 -> panic("Slot is full")
    
注意：新实现要求调用方确保不会超过带宽限制，否则直接 panic
```

### 示例 3：多上游端口

```
Flow0 -> outPort0 ─┐
Flow1 -> outPort1 ─┼─> MultiUpstreamPort -> Link -> Flow2
Flow2 -> outPort2 ─┘

Link.ProcessCycle(0):
  - MultiUpstreamPort.WaitForDoneUntil(0)
    -> 等待 outPort0, outPort1, outPort2 的 DoneUntil >= 0
  - MultiUpstreamPort.ReceiveChan()
    -> 聚合来自所有上游端口的包
  - 处理并转发到 Flow2
```

## 接口变更总结

### 删除的方法
- `Transmit(cycle, pkt)` - 由 Flow 通过 CyclePort 发送替代
- `Advance(cycle)` - 由 `ProcessCycle(cycle)` 替代
- `SetNoBackpressureUntil(cycle)` - backpressure 机制已移除
- `ReadFromFlow(cycle)` - 由 CyclePort 接收机制替代
- `IsBackpressured()` - backpressure 状态不再维护
- `CurrentCycle()` - 周期由调用方管理
- `SendFinishedCycle()` - DoneUntil 机制替代

### 新增的方法
- `ProcessCycle(cycle)` - 统一的周期处理入口
- `UpstreamPorts()` - 获取所有上游端口
- `DownstreamPort()` - 获取下游端口
- `SnapshotOccupancy()` - 快照延迟槽占用情况

### 修改的方法
- `NewLink()` - 参数从 Flow 对象改为 CyclePort，支持多上游端口

## 测试要点

重构后的测试需要：
1. 创建 `CyclePort` 实例
2. 使用 `Flow.AddOutPort()` 连接 Flow 和 Link
3. 初始化 DoneUntil 和 Ready 状态
4. 按顺序调用 `ProcessCycle()` 处理周期

## 优势总结

1. **同步性**：基于 CyclePort 的同步通信，避免竞态条件
2. **灵活性**：支持多上游端口聚合，更灵活的拓扑结构
3. **自动化**：DoneUntil 和 Ready 状态自动管理
4. **清晰性**：延迟和带宽限制逻辑集中在 `LinkPacketProcessor`
5. **可测试性**：接口更清晰，易于单元测试

## 注意事项

1. **初始化要求**：
   - 无上游的 Flow 需要初始化 `inPort.SetDoneUntil(0)`
   - 下游需要初始化 `SetReadyUntil()` 避免阻塞
   - `NewLink` 会自动初始化 `totalBackpressure = 0`

2. **周期顺序**：
   - 必须按顺序调用 `ProcessCycle(0)`, `ProcessCycle(1)`, ...
   - 上游的 DoneUntil 必须及时更新
   - **重要**：`cycle >= targetCycle` 必须成立，否则会 panic

3. **带宽限制**：
   - 每个槽最多存储 `bandwidth` 个包
   - 如果槽已满，会直接 panic，要求调用方确保不会超过带宽限制
   - 回绕情况（`targetCycle-cycle >= latency`）的包会放入 `pendingPackets`

4. **Backpressure 机制**：
   - 当下游未就绪时，`totalBackpressure` 递增
   - 槽索引计算使用 `(targetCycle - totalBackpressure) % len(slots)`
   - 包保留在槽中，等待下游恢复就绪后发送

5. **异步通知**：
   - `updateUpstreamReady` 在 goroutine 中异步执行
   - 使用 `WaitGroup` 确保在返回前完成
   - Link 透明转发下游的 Ready 状态到上游

## 需要修改的其他代码

### 1. NewLink 函数 - 初始化 totalBackpressure

**位置**：`internal/core/link/link.go:153-160`

**问题**：`NewLink` 创建 `Link` 结构体时未初始化 `totalBackpressure` 字段

**修复**：已在代码中添加 `totalBackpressure: 0` 初始化

```go
link := &Link{
    sourceID:          sourceID,
    targetID:          targetID,
    upstreamPorts:     upstreamPorts,
    downstreamPort:    downstreamPort,
    latency:           latency,
    bandwidth:         bandwidth,
    totalBackpressure: 0,  // 必须初始化
}
```

### 2. 测试代码 - 适应新的处理逻辑

**需要检查的测试文件**：
- `internal/core/link/link_test.go`
- `internal/core/network/network_test.go`
- `internal/core/node/node_test.go`

**可能需要的修改**：

1. **带宽限制测试**：
   - 旧实现：超过带宽的包会放入 `pendingPackets`
   - 新实现：槽已满会直接 panic
   - **需要确保测试不会触发 panic**，或者测试 panic 行为

2. **Backpressure 测试**：
   - 新实现使用 `totalBackpressure` 调整槽索引
   - 测试需要验证 backpressure 时的行为
   - 验证包在 backpressure 后能正确发送

3. **周期验证**：
   - 新实现会检查 `cycle >= targetCycle`，否则 panic
   - 测试需要确保不会出现过去周期的情况

### 3. 文档和注释

**需要更新的文档**：
- 本文档（已完成）
- 其他引用 Link 实现的文档
- 代码中的注释（特别是 `ProcessPackets` 的注释）

### 4. 可能的性能影响

**需要注意的点**：
- 异步 `updateUpstreamReady` 使用 goroutine，需要评估性能影响
- `WaitGroup.Wait()` 会阻塞直到 goroutine 完成
- 如果 `checkReady(cycle+1)` 调用较慢，可能影响性能

**建议**：
- 监控 `updateUpstreamReady` 的执行时间
- 如果发现性能问题，考虑优化 `checkReady` 的实现

### 5. 错误处理

**新增的 panic 点**：
1. `cycle < targetCycle` - 过去周期检测
2. `len(slots[targetSlotIndex]) >= bandwidth` - 槽已满

**建议**：
- 考虑将 panic 改为返回 error（如果框架支持）
- 或者确保调用方不会触发这些条件
- 添加更详细的错误信息，便于调试

