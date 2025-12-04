# Network.Advance 并行执行流程详解

## 整体架构概览

```
Network.Advance(N cycles)
    |
    +-- 启动所有组件的goroutine（并行）
    |   |
    |   +-- Node1.Advance(N) ----+
    |   +-- Node2.Advance(N)     |
    |   +-- Node3.Advance(N)     |  所有这些goroutine
    |   +-- ...                  |  同时运行
    |   +-- Link1.Advance(N)     |  (并行层级1)
    |   +-- Link2.Advance(N)     |
    |   +-- Link3.Advance(N) ----+
    |
    +-- WaitGroup.Wait() (等待所有组件完成N个cycles)
```

## 详细的并行层级

### 第1层并行：Network级别（粗粒度）

```go
// network.go: Network.Advance(cycles)
func (n *Network) Advance(cycles int) error {
    var wg sync.WaitGroup

    // 为每个Node创建一个goroutine
    for _, handle := range n.nodeList {
        wg.Add(1)
        go func(h *NodeHandle) {
            defer wg.Done()
            h.Node.Advance(cycles)  // ← 这个goroutine运行所有N个cycles
        }(handle)
    }

    // 为每个Link创建一个goroutine
    for _, lk := range n.links {
        wg.Add(1)
        go func(l *link.Link) {
            defer wg.Done()
            l.Advance(cycles)  // ← 这个goroutine运行所有N个cycles
        }(lk)
    }

    wg.Wait()  // 等待所有组件完成
    return nil
}
```

**关键点**：
- **并行度 = nodeCount + linkCount**
  - 例如8个节点的ring = 8个Node + 8个Link = **16个goroutine**
- **每个goroutine独立运行完整的N个cycles**
- **只有一个同步点**：最后的WaitGroup.Wait()

### 第2层并行：Node级别（每个cycle内部）

```go
// node.go: Node.Advance(cycles)
func (n *Node) Advance(cycles int) error {
    // 注意：这是串行循环！
    for i := 0; i < cycles; i++ {
        cycle := n.currentCycle
        n.Tick(ctx, cycle, 0)  // ← 执行一个cycle
        n.currentCycle++
    }
    return nil
}

// node.go: Node.Tick(cycle)
func (n *Node) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
    buffer := n.collectPackets()              // 1. 收集packets

    if hook := n.getProcessHook(); hook != nil {
        processed, err := hook(ctx, cycle, buffer)  // 2. 处理packets（用户逻辑）
    }

    n.storeProcessBuffer(buffer)
    n.tickQueuesConcurrently(int(cycle))      // 3. ← 并行Tick所有队列
    n.invokeTickHook(cycle)
    return nil
}

// node.go: tickQueuesConcurrently
func (n *Node) tickQueuesConcurrently(cycle int) error {
    var wg sync.WaitGroup

    // 为每个InputQueue创建goroutine
    for _, input := range n.inputs {
        wg.Add(1)
        go func(q InputQueue) {
            defer wg.Done()
            q.Tick(cycle)  // ← InputQueue.Tick
        }(input)
    }

    // 为每个OutputQueue创建goroutine
    for _, output := range n.outputs {
        wg.Add(1)
        go func(q OutputQueue) {
            defer wg.Done()
            q.Tick(cycle)  // ← OutputQueue.Tick
        }(output)
    }

    wg.Wait()  // 等待所有Queue完成当前cycle
    return nil
}
```

**关键点**：
- Node.Advance是**串行循环**执行N个cycles
- 但**每个cycle内部**，所有Queue并行Tick
- **并行度 = inputCount + outputCount（每个Node）**
  - 通常每个Node有1个input + 1个output = **2个goroutine/cycle**

### 第3层并行：Link级别（内部使用goroutine）

```go
// link.go: Link.Advance(cycles)
func (l *Link) Advance(cycles int) error {
    // 注意：这也是串行循环！
    for i := 0; i < cycles; i++ {
        cycle := l.currentCycle
        l.Tick(cycle)  // ← 执行一个cycle
        l.currentCycle++
    }
    return nil
}

// link.go: Link.Tick -> LinkCycleProcessor.Tick -> ProcessPackets
func (l *LinkPacketProcessor) ProcessPackets(...) {
    var wg sync.WaitGroup
    wg.Add(1)

    // 创建goroutine检查downstream ready状态
    go func() {
        defer wg.Done()
        updateUpstreamReady(cycle+1, checkReady(cycle+1))
    }()

    // ... 处理packets的逻辑 ...

    wg.Wait()  // 等待goroutine完成
}
```

**关键点**：
- Link.Advance也是**串行循环**执行N个cycles
- 每个cycle内部创建1个goroutine检查Ready状态
- **并行度 = 1个goroutine/cycle**

## 完整的并行调用链示例

假设有8个节点的ring拓扑，运行1000个cycles：

```
Network.Advance(1000)
├─ 启动 16 个 goroutine（8 Nodes + 8 Links）─────────┐
│                                                        │
│  Node[0].Advance(1000)                                │
│  ├─ for cycle 0..999: (串行)                         │
│  │   ├─ Node.Tick(cycle)                              │
│  │   │   ├─ collectPackets()                          │
│  │   │   ├─ processHook()                             │
│  │   │   └─ tickQueuesConcurrently()                  │
│  │   │       ├─ InputQueue.Tick(cycle) ──┐            │
│  │   │       └─ OutputQueue.Tick(cycle) ─┴─ 并行     │  第1层
│  │   │           (2 goroutines)                        │  并行
│  │   └─ currentCycle++                                │
│  └─ 完成                                               │
│                                                        │
│  Node[1].Advance(1000)                                │
│  ├─ (同样的结构) ...                                  │
│                                                        │
│  ... (Node[2] 到 Node[7])                             │
│                                                        │
│  Link[0→1].Advance(1000)                              │
│  ├─ for cycle 0..999: (串行)                         │
│  │   ├─ Link.Tick(cycle)                              │
│  │   │   └─ ProcessPackets()                          │
│  │   │       └─ 启动1个goroutine检查Ready ─ 并行     │  (16 goroutines)
│  │   └─ currentCycle++                                │
│  └─ 完成                                               │
│                                                        │
│  ... (Link[1→2] 到 Link[7→0])                         │
│                                                        │
└─ WaitGroup.Wait() ←─ 等待所有16个goroutine完成 ──────┘
```

## 每个Cycle的Goroutine数量

对于8节点ring拓扑，**在某个特定时刻**：

### Network层面
- 8个Node goroutine（每个运行Advance）
- 8个Link goroutine（每个运行Advance）
- **总计：16个主goroutine**

### Node层面（每个Node在Tick时）
- 每个Node创建2个goroutine（InputQueue + OutputQueue）
- 8个Node × 2 = **16个Queue goroutine**

### Link层面（每个Link在ProcessPackets时）
- 每个Link创建1个goroutine（检查Ready）
- 8个Link × 1 = **8个Ready检查goroutine**

### 总goroutine数（峰值）
```
16 (主) + 16 (Queue) + 8 (Ready) = 40个goroutine
```

但实际上由于生命周期不同：
- 主goroutine：持续存在整个Advance期间
- Queue goroutine：每个cycle创建和销毁
- Ready goroutine：每个cycle创建和销毁

## 关键的同步点

### 同步点1：Network.Advance结束
```
所有Node和Link必须完成全部N个cycles
↓
Network.WaitGroup.Wait()
```

### 同步点2：Node.Tick结束（每个cycle）
```
Node的所有Queue必须完成当前cycle
↓
Node.tickQueuesConcurrently() -> WaitGroup.Wait()
```

### 同步点3：Link.ProcessPackets结束（每个cycle）
```
Ready检查goroutine必须完成
↓
ProcessPackets() -> WaitGroup.Wait()
```

### 同步点4：组件间依赖（通过WaitDone）

```go
// 例如：InputQueue等待upstream OutputQueue完成
func (iq *InputQueue) Tick(cycle int) {
    // 等待上游OutputQueue完成cycle-1
    if iq.upstream != nil {
        iq.upstream.WaitDone(cycle - 1)  // ← 阻塞等待
    }
    // ... 处理当前cycle ...
}

// Link等待upstream OutputQueue完成
func (l *Link) Tick(cycle int) {
    // 等待上游完成 cycle - latency
    if link.inPort.UpstreamOut != nil {
        link.inPort.UpstreamOut.WaitDone(cycle - latency)  // ← 阻塞等待
    }
    // ... 处理当前cycle ...
}
```

## 执行时序图

```
时间 →

Cycle 0:
  Node[0-7]    [开始Tick] ─────→ [tickQueues并行] ──→ [完成]
  Link[0-7]    [等待upstream] ─→ [ProcessPackets] ──→ [完成]

Cycle 1:
  Node[0-7]    [开始Tick] ─────→ [tickQueues并行] ──→ [完成]
  Link[0-7]    [等待upstream] ─→ [ProcessPackets] ──→ [完成]

...

Cycle 999:
  Node[0-7]    [开始Tick] ─────→ [tickQueues并行] ──→ [完成]
  Link[0-7]    [等待upstream] ─→ [ProcessPackets] ──→ [完成]

Network.Advance完成 ←─────────────────────────────────┘
```

## 性能影响分析

### 为什么32个节点比8个节点慢5倍？

**8节点ring**：
- 主goroutine: 16个（8 Node + 8 Link）
- 峰值goroutine: ~40个

**32节点ring**：
- 主goroutine: 64个（32 Node + 32 Link）
- 峰值goroutine: ~160个

**在16核CPU上**：
- 8节点：40个goroutine / 16核 ≈ 2.5个goroutine/核（**可接受**）
- 32节点：160个goroutine / 16核 = 10个goroutine/核（**过载**）

### 同步开销

**每个cycle的同步操作数**：
- Network层：0次（只在最后同步）
- Node层：8个Node × 1次WaitGroup.Wait = 8次
- Link层：8个Link × 1次WaitGroup.Wait = 8次
- WaitDone调用：约32次（每个组件等待upstream）

**总计：每个cycle ~48次同步操作**

**1000 cycles = 48,000次同步操作！**

对于32节点：**1000 cycles = 192,000次同步操作！**

这就是为什么性能下降如此严重的原因！

## 总结

### 并行层级
1. **第1层（粗粒度）**：Network启动所有Node和Link的goroutine
   - 并行度 = nodeCount + linkCount
   - 每个goroutine独立运行全部N个cycles

2. **第2层（中粒度）**：每个Node在每个cycle内并行Tick所有Queue
   - 并行度 = inputCount + outputCount（每个Node）
   - 每个cycle创建和销毁goroutine

3. **第3层（细粒度）**：每个Link在每个cycle创建goroutine检查Ready
   - 并行度 = 1（每个Link）
   - 每个cycle创建和销毁goroutine

### 每次执行多少Cycle？
- **Network.Advance(N)**：启动所有goroutine，每个运行完整的N个cycles
- **Node.Advance(N)**：串行循环N次，每次执行一个cycle
- **Link.Advance(N)**：串行循环N次，每次执行一个cycle

### 关键特点
- ✅ **粗粒度并行**：所有组件同时运行多个cycles
- ⚠️ **细粒度同步**：每个cycle内部有大量同步点
- ❌ **过度并行**：当goroutine数 >> CPU核心数时，同步开销占主导

### 优化方向
1. **减少goroutine数量**：不要每个cycle都创建新的goroutine
2. **批量同步**：不要每个cycle都同步，可以每10个cycle同步一次
3. **减少WaitDone调用**：使用更高效的同步机制
