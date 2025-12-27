# Flow_Sim 同步机制问题分析报告

## 问题现象

在尝试使用flow_sim框架集成ChampSim的CPU+Cache+DRAM时，测试在`Link.Tick(1)` → `Receive(0)` → `WaitDone(0)`处死锁。

**堆栈信息**：
```
goroutine 8 [chan receive]:
github.com/Readm/flow_sim/internal/core/ahead_port.(*ComponentSync).WaitDone(0xc0000c22c0, 0x0?)
    /home/readm/flow_sim/internal/core/ahead_port/sync.go:104 +0x99
github.com/Readm/flow_sim/internal/core/link.(*Link).Tick(0xc0000145a0, 0x1, 0x1)
    /home/readm/flow_sim/internal/core/link/link.go:144 +0x57
```

## 根本原因

### 1. ComponentSync 初始状态

`ComponentSync`的初始`done`值为`-1`（sync.go:46）：

```go
func NewComponentSync() *ComponentSync {
    cs := &ComponentSync{
        done: -1,  // ← 初始值
        // ...
    }
    return cs
}
```

这意味着：
- Port创建时，`upstreamSync.done = -1`
- Link在cycle 1调用`Receive(0)`时，需要等待`upstreamSync.done >= 0`
- 如果OutputQueue从未调用`MarkDone(0)`，Link将永远等待

### 2. 错误的AdvanceTo调用模式

**❌ 错误的用法** (simple_test.go):

```go
for cycle := 0; cycle < 10; cycle++ {
    net.AdvanceTo(cycle)  // 错误：重复推进到同一个周期
}
```

**✅ 正确的用法** (network_test.go):

```go
// 方式1：在注入包后一次性推进
mustInject(t, outputs0[0], 0, packet.Packet{Payload: "A->B"})
if err := net.AdvanceTo(net.CurrentCycle() + 6 - 1); err != nil {
    t.Fatalf("Advance: %v", err)
}

// 方式2：连续推进（如果需要）
for i := 0; i < 10; i++ {
    // 每次推进1个周期
    target := net.CurrentCycle()
    if err := net.AdvanceTo(target); err != nil {
        t.Fatalf("Advance to %d: %v", target, err)
    }
}
```

### 3. 包注入时机的关键差异

**Flow_sim的设计假设**：
- 包应该在`AdvanceTo`调用**之前**通过`OutputQueue.InjectPackets()`注入
- 或者在Process hook中注入后，由同一个Tick的`tickOutputQueues`发送

**我的错误实现**：
- 在Process hook中注入包
- 但立即在循环中调用下一个AdvanceTo
- 导致Link在接收时，OutputQueue可能还没有机会MarkDone

### 4. AdvanceTo的执行流程

```
Network.AdvanceTo(targetCycle):
  1. 广播targetCycle到所有worker goroutines
  2. 所有Node并发执行：
     for cycle := currentCycle; cycle <= targetCycle; cycle++:
         Tick(cycle):
             a. tickInputQueues(cycle)
             b. Process(cycle, inputs)  ← 在这里注入包
             c. tickOutputQueues(cycle) ← MarkDone(cycle)
         currentCycle++
  3. 所有Link并发执行：
     for cycle := currentCycle; cycle <= targetCycle; cycle++:
         Tick(cycle):
             a. waitCycle = cycle - latency
             b. if waitCycle >= 0: Receive(waitCycle) ← WaitDone(waitCycle)
             c. Process(...)
             d. MarkDone(cycle)
         currentCycle++
  4. WaitGroup.Wait() 等待所有完成
  5. network.currentCycle = targetCycle + 1
```

### 5. 死锁场景重现

**Cycle 0时**：
```
Node.Tick(0):
  - Process(0): 注入包到OutputQueue (cycle=0)
  - tickOutputQueues(0):
      - OutputQueue.Tick(0): TrySend(0, packet)
      - OutputQueue.Tick(0): MarkDone(0)  ✓

Link.Tick(0):
  - waitCycle = 0 - 1 = -1
  - waitCycle < 0，不接收数据
  - MarkDone(0)  ✓
```

**Cycle 1时** (第二次调用AdvanceTo):
```
Node.currentCycle = 1, AdvanceTo(1):
  - for cycle in [1, 1]:
      - Tick(1)
      - currentCycle = 2

Link.currentCycle = 1, AdvanceTo(1):
  - for cycle in [1, 1]:
      - Tick(1):
          - waitCycle = 1 - 1 = 0
          - Receive(0):
              - WaitDone(0)  ← 等待 upstreamSync.done >= 0
          ❌ 死锁：如果MarkDone(0)的信号已经丢失
      - currentCycle = 2
```

## 问题的关键

**并发执行导致的时序问题**：
- Node和Link是在**独立的goroutine**中并发执行
- Link.Tick(1)可能在Node.Tick(0)的MarkDone(0)之前就开始等待
- 虽然理论上AdvanceTo(0)应该完成后才会调用AdvanceTo(1)，但ComponentSync的done状态可能没有正确传播

## 正确的使用模式（基于network_test.go）

### 模式1：预注入 + 单次推进

```go
// 1. 创建网络和节点
net := network.New()
node0, _, outputs0 := newTestNodeHandle(t, 0, 0, 1)
node1, inputs1, _ := newTestNodeHandle(t, 1, 1, 0)
net.AddNode(node0)
net.AddNode(node1)
net.Connect(0, 0, 1, 0, 1, 1)  // latency=1

// 2. 在AdvanceTo之前注入包
outputs0[0].InjectPackets(0, []packet.Packet{{Payload: "test"}})

// 3. 一次性推进足够的周期
// latency=1意味着包在cycle 1到达
// 所以推进2-3个周期确保接收
if err := net.AdvanceTo(net.CurrentCycle() + 3 - 1); err != nil {
    t.Fatal(err)
}

// 4. 验证接收
received := inputs1[0].GetReceivedPackets()
```

### 模式2：Process hook中转发

```go
// Sender在Process中注入包
senderNode.SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
    if cycle == 0 {
        return senderOutput.InjectPackets(int(cycle), []packet.Packet{{Payload: "msg"}})
    }
    return nil
})

// Receiver转发所有输入到输出
forwardNode.SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
    var packets []packet.Packet
    for _, q := range inputs {
        for _, ref := range q {
            packets = append(packets, ref.Packet)
            ref.Queue.Free(ref.Slot)  // ← 必须Free
        }
    }
    if len(packets) > 0 {
        return forwardOutput.InjectPackets(int(cycle), packets)
    }
    return nil
})

// 一次性推进多个周期（不要循环调用AdvanceTo）
if err := net.AdvanceTo(10); err != nil {
    t.Fatal(err)
}
```

## 对ChampSim集成的影响

### 当前架构的问题

我们的ChampSim集成尝试在Process hook中：
1. 执行CPU.Tick() - 产生新的内存请求
2. 从MemoryAdapter获取pending requests
3. 立即InjectPackets到OutputQueue

这个模式在flow_sim中是支持的，但需要确保：
- **不要循环调用AdvanceTo(0), AdvanceTo(1), ...**
- **应该一次性推进：AdvanceTo(maxCycles - 1)**

### 修复方案

**方案A：修改测试循环** ✅ 推荐

```go
// 修改前
for cycle := 0; cycle < maxCycles; cycle++ {
    net.AdvanceTo(cycle)
}

// 修改后
net.AdvanceTo(maxCycles - 1)
```

**方案B：使用预注入模式**

不在Process中注入，而是在AdvanceTo前收集所有初始请求并注入。

**方案C：回退到直接集成**

放弃flow_sim，继续使用当前已经工作的CPU+Cache+DRAM直接集成方式。

## 测试验证

### 验证步骤

1. 修改simple_test.go，使用正确的AdvanceTo模式
2. 验证简单ping测试通过
3. 修改flowsim_integration_test.go
4. 运行完整的CPU+Cache+DRAM测试

### 预期结果

- AdvanceTo应该不再死锁
- 包应该正确从Sender传递到Receiver
- CPU应该能够正常执行指令

## 总结

**核心问题**：误用了`AdvanceTo`的调用模式

**关键教训**：
1. Flow_sim的AdvanceTo(N)会执行从currentCycle到N的所有周期
2. 不应该在循环中调用AdvanceTo(0), AdvanceTo(1), ...
3. 应该一次性推进：AdvanceTo(targetCycle)
4. 包可以在Process中注入，但要理解tick的执行顺序

**下一步行动**：
1. 修复测试代码使用正确的AdvanceTo模式
2. 如果修复后仍有问题，考虑回退到直接集成方案
3. Flow_sim框架本身没有问题，问题在于我们的使用方式

## 代码示例

### 正确的简单测试

```go
func Test_Simple_FlowSim_Ping_Fixed(t *testing.T) {
    net := network.New()

    // ... 创建节点和连接 ...

    // 预注入包
    senderOutputQueue.InjectPackets(0, []packet.Packet{{
        SourceID: senderID,
        TargetID: receiverID,
        Payload:  "Hello",
    }})

    // 一次性推进10个周期
    if err := net.AdvanceTo(9); err != nil {  // 0-9 = 10个周期
        t.Fatalf("AdvanceTo failed: %v", err)
    }

    // 验证
    if receivedCount != 1 {
        t.Errorf("Expected 1 packet, got %d", receivedCount)
    }
}
```

### 正确的ChampSim集成测试

```go
func Test_FlowSim_CPU_DRAM_Integration_Fixed(t *testing.T) {
    // ... 创建CPU, Cache, DRAM nodes ...

    net := network.New()
    net.AddNode(cpuNodeHandle)
    net.AddNode(dramNodeHandle)
    net.Connect(cpuNodeID, 0, dramNodeID, 0, 1, 1)
    net.Connect(dramNodeID, 0, cpuNodeID, 0, 1, 1)

    // 一次性推进1000个周期（不要循环）
    maxCycles := 1000
    if err := net.AdvanceTo(maxCycles - 1); err != nil {
        t.Fatalf("Advance failed: %v", err)
    }

    // 验证统计
    cpuStats := o3cpu.GetStats()
    dramStats := dramChannel.GetStats()
    // ... 断言 ...
}
```
