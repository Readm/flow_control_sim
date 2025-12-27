# Flow_Sim API 改进建议

## 问题分析

### 当前API存在的用户体验问题

#### 1. **命名歧义**

```go
net.AdvanceTo(5)  // "推进到cycle 5" - 但实际上执行了cycle 0,1,2,3,4,5
```

**问题**：
- `AdvanceTo(N)` 容易被理解为"设置cycle为N"
- 实际上是"执行从currentCycle到N的所有周期"
- 用户自然会想："既然是推进到N，那我就循环调用AdvanceTo(0), AdvanceTo(1), ..."

**对比其他框架**：
- SystemC: `sc_start(10, SC_NS)` - 明确是"运行10纳秒"
- Verilator: `sim.step()` - 单步执行
- gem5: `simulate(ticks)` - 执行指定数量的ticks

#### 2. **缺少常用模式的便捷API**

当前只有`AdvanceTo(targetCycle int)`，用户想要：
- 单步执行 → 需要手动计算：`AdvanceTo(net.CurrentCycle())`
- 执行N个周期 → 需要计算：`AdvanceTo(net.CurrentCycle() + N - 1)`
- 循环逐周期执行 → 容易写出错误的循环

#### 3. **缺少运行时保护**

```go
net.AdvanceTo(5)  // currentCycle = 6
net.AdvanceTo(3)  // ??? 应该报错但没有明确的行为
net.AdvanceTo(5)  // ??? 重复调用，应该no-op但不明显
```

**当前行为**（network.go:360）：
```go
if targetCycle < n.currentCycle {
    return nil  // 静默返回，用户不知道发生了什么
}
```

#### 4. **错误信息不够友好**

死锁时只显示goroutine堆栈，没有：
- 当前网络状态（哪些节点在等待什么）
- 周期信息（各组件的currentCycle）
- 同步状态（哪些Port在等待MarkDone）

---

## 改进方案

### 方案A：改进现有API（最小侵入）

#### 1. 添加更清晰的便捷方法

```go
// network.go

// Step 单步执行一个周期
// 等价于 AdvanceTo(CurrentCycle())
func (n *Network) Step() error {
    return n.AdvanceTo(n.currentCycle)
}

// AdvanceBySteps 执行指定数量的周期
// 等价于 AdvanceTo(CurrentCycle() + steps - 1)
func (n *Network) AdvanceBySteps(steps int) error {
    if steps <= 0 {
        return fmt.Errorf("steps must be positive, got %d", steps)
    }
    target := n.currentCycle + steps - 1
    return n.AdvanceTo(target)
}

// RunUntil 运行到指定周期（包含）
// 这是AdvanceTo的别名，但名称更清晰
func (n *Network) RunUntil(targetCycle int) error {
    return n.AdvanceTo(targetCycle)
}
```

**用户代码改进**：
```go
// 改进前（容易出错）
for cycle := 0; cycle < 10; cycle++ {
    net.AdvanceTo(cycle)  // 错误！
}

// 改进后（清晰明确）
net.AdvanceBySteps(10)  // 执行10个周期

// 或者单步执行
for i := 0; i < 10; i++ {
    net.Step()
}
```

#### 2. 添加运行时检查和警告

```go
// network.go

func (n *Network) AdvanceTo(targetCycle int) error {
    // 检查：targetCycle < currentCycle
    if targetCycle < n.currentCycle {
        return fmt.Errorf(
            "AdvanceTo: targetCycle=%d < currentCycle=%d (backward time travel not supported)\n"+
            "Hint: Did you mean to call Step() or AdvanceBySteps()?",
            targetCycle, n.currentCycle,
        )
    }

    // 检查：targetCycle == currentCycle - 1（常见错误）
    if targetCycle == n.currentCycle - 1 {
        return fmt.Errorf(
            "AdvanceTo: targetCycle=%d is already completed (currentCycle=%d)\n"+
            "Hint: AdvanceTo executes cycles from currentCycle to targetCycle (inclusive)\n"+
            "      To execute 1 cycle, use Step() instead",
            targetCycle, n.currentCycle,
        )
    }

    // 警告：重复调用（targetCycle == currentCycle）
    if targetCycle < n.currentCycle {
        debug.Logf("WARNING: AdvanceTo(%d) called but currentCycle already at %d, no-op",
                   targetCycle, n.currentCycle)
        return nil
    }

    // ... 原有逻辑 ...
}
```

#### 3. 改进文档注释

```go
// AdvanceTo executes the network from the current cycle up to and including the target cycle.
//
// Example:
//   net := New()                    // currentCycle = 0
//   net.AdvanceTo(5)                // executes cycles 0,1,2,3,4,5; currentCycle becomes 6
//   net.AdvanceTo(10)               // executes cycles 6,7,8,9,10; currentCycle becomes 11
//
// Common mistakes:
//   ❌ for cycle := 0; cycle < 10; cycle++ {
//          net.AdvanceTo(cycle)     // WRONG: repeatedly calls with stale cycle numbers
//      }
//   ✅ net.AdvanceBySteps(10)       // CORRECT: execute 10 cycles
//   ✅ net.AdvanceTo(9)              // CORRECT: execute cycles 0-9
//
// Parameters:
//   - targetCycle: The last cycle to execute (inclusive)
//
// Returns:
//   - error if targetCycle < currentCycle (backward time travel)
//
// See also: Step(), AdvanceBySteps(), RunUntil()
func (n *Network) AdvanceTo(targetCycle int) error {
    // ...
}
```

#### 4. 添加调试支持

```go
// network.go

// PrintState 打印网络当前状态（用于调试）
func (n *Network) PrintState() string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("Network State (currentCycle=%d):\n", n.currentCycle))

    sb.WriteString("  Nodes:\n")
    for _, handle := range n.nodeList {
        node := handle.Node
        sb.WriteString(fmt.Sprintf("    - Node %d: currentCycle=%d\n",
                                   node.ID(), node.CurrentCycle()))
    }

    sb.WriteString("  Links:\n")
    for i, link := range n.links {
        sb.WriteString(fmt.Sprintf("    - Link %d: %d->%d, currentCycle=%d\n",
                                   i, link.SourceID(), link.TargetID(),
                                   link.CurrentCycle()))
    }

    return sb.String()
}

// ValidateState 验证网络状态一致性
func (n *Network) ValidateState() error {
    // 检查所有组件的currentCycle是否一致
    expectedCycle := n.currentCycle

    for _, handle := range n.nodeList {
        if handle.Node.CurrentCycle() != expectedCycle {
            return fmt.Errorf("Node %d currentCycle=%d, expected %d",
                             handle.Node.ID(), handle.Node.CurrentCycle(),
                             expectedCycle)
        }
    }

    for _, link := range n.links {
        if link.CurrentCycle() != expectedCycle {
            return fmt.Errorf("Link %d->%d currentCycle=%d, expected %d",
                             link.SourceID(), link.TargetID(),
                             link.CurrentCycle(), expectedCycle)
        }
    }

    return nil
}
```

---

### 方案B：重新设计API（更激进）

#### 1. 引入显式的Simulator对象

```go
// simulator.go (新文件)

// Simulator 封装网络和执行控制
type Simulator struct {
    network      *Network
    currentCycle int
    maxCycle     int  // 可选的最大周期限制
}

// NewSimulator 创建仿真器
func NewSimulator(net *Network) *Simulator {
    return &Simulator{
        network:      net,
        currentCycle: 0,
        maxCycle:     -1,  // 无限制
    }
}

// Run 运行指定数量的周期
func (s *Simulator) Run(cycles int) error {
    if cycles <= 0 {
        return fmt.Errorf("cycles must be positive")
    }

    target := s.currentCycle + cycles - 1
    if s.maxCycle >= 0 && target > s.maxCycle {
        return fmt.Errorf("would exceed maxCycle=%d", s.maxCycle)
    }

    if err := s.network.AdvanceTo(target); err != nil {
        return err
    }

    s.currentCycle = target + 1
    return nil
}

// Step 单步执行
func (s *Simulator) Step() error {
    return s.Run(1)
}

// RunUntil 运行到指定周期
func (s *Simulator) RunUntil(cycle int) error {
    if cycle < s.currentCycle {
        return fmt.Errorf("cannot go backward: %d < %d", cycle, s.currentCycle)
    }

    steps := cycle - s.currentCycle + 1
    return s.Run(steps)
}

// CurrentCycle 返回当前周期
func (s *Simulator) CurrentCycle() int {
    return s.currentCycle
}
```

**用户代码**：
```go
// 创建仿真器
sim := NewSimulator(net)

// 方式1：运行N个周期
sim.Run(1000)

// 方式2：单步执行
for i := 0; i < 10; i++ {
    sim.Step()
    // 检查状态...
}

// 方式3：运行到指定周期
sim.RunUntil(100)
```

#### 2. 提供事件驱动的API（可选）

```go
// event_simulator.go

// EventSimulator 事件驱动的仿真器
type EventSimulator struct {
    *Simulator
    events      []Event
    eventQueue  *EventQueue  // 优先队列
}

type Event struct {
    Time     int
    Callback func() error
}

// ScheduleEvent 在指定周期调度事件
func (es *EventSimulator) ScheduleEvent(cycle int, callback func() error) {
    es.eventQueue.Push(Event{Time: cycle, Callback: callback})
}

// RunUntilNoEvents 运行直到没有事件
func (es *EventSimulator) RunUntilNoEvents() error {
    for !es.eventQueue.Empty() {
        event := es.eventQueue.Pop()

        // 运行到事件时间
        if err := es.RunUntil(event.Time); err != nil {
            return err
        }

        // 执行事件
        if err := event.Callback(); err != nil {
            return err
        }
    }
    return nil
}
```

---

### 方案C：类型安全的API（最激进）

#### 1. 使用类型系统防止错误

```go
// cycle.go (新文件)

// Cycle 周期类型（防止直接使用int）
type Cycle int

// CycleDelta 周期增量（用于相对推进）
type CycleDelta int

// Network API 修改
func (n *Network) CurrentCycle() Cycle {
    return Cycle(n.currentCycle)
}

// AdvanceTo 推进到指定周期（绝对）
func (n *Network) AdvanceTo(target Cycle) error {
    return n.advanceToInt(int(target))
}

// AdvanceBy 推进指定数量的周期（相对）
func (n *Network) AdvanceBy(delta CycleDelta) error {
    if delta <= 0 {
        return fmt.Errorf("delta must be positive")
    }
    target := int(n.currentCycle) + int(delta) - 1
    return n.advanceToInt(target)
}

// 用户代码
net.AdvanceBy(CycleDelta(10))  // 明确是相对推进
net.AdvanceTo(Cycle(100))      // 明确是绝对周期
```

---

## 推荐的改进优先级

### 短期（立即可做）：

1. ✅ **添加便捷方法**：`Step()`, `AdvanceBySteps(n)`
   - 工作量：低
   - 收益：高（大幅降低错误率）

2. ✅ **改进文档注释**
   - 工作量：低
   - 收益：中（帮助新用户理解）

3. ✅ **添加运行时检查**
   - 工作量：低
   - 收益：高（及时发现错误）

### 中期（下个版本）：

4. ✅ **添加Simulator封装**
   - 工作量：中
   - 收益：中（更清晰的API）

5. ✅ **添加调试工具**：`PrintState()`, `ValidateState()`
   - 工作量：中
   - 收益：中（调试时非常有用）

### 长期（考虑中）：

6. ⚠️ **类型安全API**
   - 工作量：高（破坏性修改）
   - 收益：中（编译时防错）

7. ⚠️ **事件驱动API**
   - 工作量：高
   - 收益：低（特定场景有用）

---

## 具体实现示例

### Step 1: 添加便捷方法（最小改动）

```go
// internal/core/network/network.go

// Step executes one cycle.
// Equivalent to AdvanceTo(CurrentCycle()).
func (n *Network) Step() error {
    return n.AdvanceTo(n.currentCycle)
}

// AdvanceBySteps executes the specified number of cycles.
// Equivalent to AdvanceTo(CurrentCycle() + steps - 1).
//
// Example:
//   net.AdvanceBySteps(10)  // Execute 10 cycles
func (n *Network) AdvanceBySteps(steps int) error {
    if steps <= 0 {
        return fmt.Errorf("AdvanceBySteps: steps must be positive, got %d", steps)
    }

    target := n.currentCycle + steps - 1
    return n.AdvanceTo(target)
}
```

### Step 2: 改进AdvanceTo的检查

```go
// AdvanceTo executes the network from the current cycle up to and including the target cycle.
//
// IMPORTANT: AdvanceTo executes cycles INCLUSIVELY from CurrentCycle() to targetCycle.
//
// Example:
//   net := New()              // currentCycle = 0
//   net.AdvanceTo(5)          // executes 0,1,2,3,4,5 -> currentCycle = 6
//   net.AdvanceTo(10)         // executes 6,7,8,9,10 -> currentCycle = 11
//
// Common mistakes:
//   ❌ for c := 0; c < 10; c++ { net.AdvanceTo(c) }  // WRONG
//   ✅ net.AdvanceBySteps(10)                        // CORRECT
//   ✅ net.AdvanceTo(9)                              // CORRECT (cycles 0-9)
func (n *Network) AdvanceTo(targetCycle int) error {
    // Validate targetCycle
    if targetCycle < n.currentCycle {
        return fmt.Errorf(
            "AdvanceTo: cannot go backward in time\n"+
            "  targetCycle:  %d\n"+
            "  currentCycle: %d\n"+
            "Hint: Did you mean Step() or AdvanceBySteps()?",
            targetCycle, n.currentCycle,
        )
    }

    // No-op if already at target (but log warning in debug mode)
    if targetCycle < n.currentCycle {
        debug.Logf(
            "WARNING: AdvanceTo(%d) called but already at cycle %d (no-op)",
            targetCycle, n.currentCycle,
        )
        return nil
    }

    // ... 原有逻辑 ...
}
```

### Step 3: 添加测试示例

```go
// internal/core/network/network_convenience_test.go (新文件)

func TestNetwork_Step(t *testing.T) {
    net := New()

    if net.CurrentCycle() != 0 {
        t.Fatalf("initial cycle should be 0, got %d", net.CurrentCycle())
    }

    // 单步执行
    if err := net.Step(); err != nil {
        t.Fatalf("Step failed: %v", err)
    }

    if net.CurrentCycle() != 1 {
        t.Fatalf("after Step, cycle should be 1, got %d", net.CurrentCycle())
    }
}

func TestNetwork_AdvanceBySteps(t *testing.T) {
    net := New()

    // 执行10个周期
    if err := net.AdvanceBySteps(10); err != nil {
        t.Fatalf("AdvanceBySteps failed: %v", err)
    }

    if net.CurrentCycle() != 10 {
        t.Fatalf("after 10 steps, cycle should be 10, got %d", net.CurrentCycle())
    }

    // 继续执行5个周期
    if err := net.AdvanceBySteps(5); err != nil {
        t.Fatalf("AdvanceBySteps failed: %v", err)
    }

    if net.CurrentCycle() != 15 {
        t.Fatalf("after 5 more steps, cycle should be 15, got %d", net.CurrentCycle())
    }
}

func TestNetwork_AdvanceTo_ErrorOnBackward(t *testing.T) {
    net := New()

    net.AdvanceTo(10)  // cycle = 11

    // 尝试后退应该报错
    err := net.AdvanceTo(5)
    if err == nil {
        t.Fatal("AdvanceTo should error on backward time travel")
    }

    if !strings.Contains(err.Error(), "cannot go backward") {
        t.Fatalf("unexpected error message: %v", err)
    }
}

func TestNetwork_CommonMistake_LoopWithAdvanceTo(t *testing.T) {
    // 演示常见错误及其修复

    // ❌ 错误的循环方式
    // for cycle := 0; cycle < 10; cycle++ {
    //     net.AdvanceTo(cycle)  // 会报错！
    // }

    // ✅ 正确方式1
    net1 := New()
    if err := net1.AdvanceBySteps(10); err != nil {
        t.Fatalf("Method 1 failed: %v", err)
    }

    // ✅ 正确方式2
    net2 := New()
    if err := net2.AdvanceTo(9); err != nil {  // 0-9 = 10个周期
        t.Fatalf("Method 2 failed: %v", err)
    }

    // ✅ 正确方式3（如果确实需要循环）
    net3 := New()
    for i := 0; i < 10; i++ {
        if err := net3.Step(); err != nil {
            t.Fatalf("Method 3 failed at step %d: %v", i, err)
        }
    }
}
```

---

## 示例：改进后的ChampSim集成测试

```go
func Test_FlowSim_CPU_DRAM_Integration_Improved(t *testing.T) {
    // ... 创建网络和节点 ...

    net := network.New()
    net.AddNode(cpuNodeHandle)
    net.AddNode(dramNodeHandle)
    net.Connect(cpuNodeID, 0, dramNodeID, 0, 1, 1)
    net.Connect(dramNodeID, 0, cpuNodeID, 0, 1, 1)

    // 方式1：一次性运行（推荐）
    if err := net.AdvanceBySteps(1000); err != nil {
        t.Fatalf("Simulation failed: %v", err)
    }

    // 方式2：逐周期执行（如果需要每周期检查状态）
    // for i := 0; i < 1000; i++ {
    //     if err := net.Step(); err != nil {
    //         t.Fatalf("Cycle %d failed: %v", i, err)
    //     }
    //
    //     // 每100周期打印一次进度
    //     if (i+1) % 100 == 0 {
    //         t.Logf("Progress: %d/1000 cycles", i+1)
    //     }
    // }

    // 验证结果
    cpuStats := o3cpu.GetStats()
    dramStats := dramChannel.GetStats()
    // ...
}
```

---

## 总结

### 推荐立即采取的措施

1. **添加便捷方法**：
   - `Step()` - 单步执行
   - `AdvanceBySteps(n)` - 执行N个周期

2. **改进错误处理**：
   - 检测backward time travel
   - 友好的错误消息，包含修复建议

3. **改进文档**：
   - 在`AdvanceTo`的文档中明确说明行为
   - 添加常见错误示例
   - 提供推荐的使用模式

4. **添加测试用例**：
   - 演示正确和错误的用法
   - 作为用户学习的参考

### 长期改进方向

- 考虑引入`Simulator`封装，提供更高层次的API
- 添加状态验证和调试工具
- 考虑类型安全的API（破坏性改动，需要谨慎）

### 对现有代码的影响

- ✅ 向后兼容：新方法是附加的，不影响现有代码
- ✅ 渐进式改进：可以逐步添加，不需要一次性完成
- ✅ 用户友好：降低学习曲线，减少错误

这些改进将使flow_sim更加易用和健壮！
