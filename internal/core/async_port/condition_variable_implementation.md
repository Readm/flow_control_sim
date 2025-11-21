# Condition Variable 实现说明

## 问题：Busy Wait 的缺陷

### 原始实现（第69行）

```go
// Wait for upstream DoneUntil >= cycle
for cp.upstreamPort.GetDoneUntil() < cycle {
    // Busy wait or yield - in real implementation, might use condition variable
}
```

### Busy Wait 的问题

1. **CPU 资源浪费**
   - 空循环持续占用 CPU 核心
   - 即使 `DoneUntil` 没有变化，也会不断检查
   - 多个 goroutine 同时 busy wait 会消耗大量 CPU

2. **响应延迟**
   - 需要轮询检查，不能立即响应变化
   - 即使 `SetDoneUntil` 被调用，也需要等待下一次循环检查

3. **无法真正休眠**
   - goroutine 无法进入等待状态
   - 调度器无法有效管理资源

4. **可扩展性差**
   - 随着等待的 goroutine 数量增加，CPU 消耗线性增长

## 解决方案：Condition Variable

### Go 语言中的 Condition Variable

Go 语言使用 `sync.Cond` 实现条件变量：

```go
cond := sync.NewCond(&mutex)
cond.Wait()    // 等待条件满足（会解锁 mutex，阻塞 goroutine）
cond.Signal()  // 唤醒一个等待者
cond.Broadcast() // 唤醒所有等待者
```

### 实现细节

#### 1. 在 Port 中添加 Condition Variable

```go
type Port struct {
    // ... 其他字段
    doneUntilMu  sync.Mutex      // 保护 doneUntilCond
    doneUntilCond *sync.Cond      // 等待 DoneUntil 变化的条件变量
}
```

#### 2. 实现 WaitForDoneUntil 方法

```go
func (p *Port) WaitForDoneUntil(targetCycle int) {
    // Fast path: 如果条件已满足，直接返回
    if p.GetDoneUntil() >= targetCycle {
        return
    }

    p.doneUntilMu.Lock()
    defer p.doneUntilMu.Unlock()

    // 初始化 condition variable
    if p.doneUntilCond == nil {
        p.doneUntilCond = sync.NewCond(&p.doneUntilMu)
    }

    // 等待直到条件满足
    for p.GetDoneUntil() < targetCycle {
        p.doneUntilCond.Wait() // 解锁、阻塞、等待唤醒
    }
}
```

**关键点**：
- `Wait()` 会自动解锁 mutex，阻塞 goroutine
- 当 `Broadcast()` 被调用时，`Wait()` 会重新加锁并返回
- 使用 `for` 循环检查条件（防止虚假唤醒）

#### 3. 在 SetDoneUntil 中唤醒等待者

```go
func (p *Port) SetDoneUntil(cycle int) {
    atomic.StoreInt64(&p.doneUntil, int64(cycle))

    // 唤醒所有等待 DoneUntil 变化的 goroutine
    p.doneUntilMu.Lock()
    if p.doneUntilCond != nil {
        p.doneUntilCond.Broadcast() // 唤醒所有等待者
    }
    p.doneUntilMu.Unlock()
}
```

**关键点**：
- `Broadcast()` 唤醒所有等待的 goroutine
- 每个被唤醒的 goroutine 会重新检查条件
- 只有条件满足的 goroutine 会继续执行

#### 4. 在 ProcessCycle 中使用

```go
// 改用前：Busy Wait
for cp.upstreamPort.GetDoneUntil() < cycle {
    // 空循环，持续占用 CPU
}

// 改用后：Condition Variable
cp.upstreamPort.WaitForDoneUntil(cycle)
// goroutine 真正阻塞，不占用 CPU
```

## 改用前后的区别

### 1. CPU 使用率

**改用前（Busy Wait）**：
```
Goroutine 状态：运行中（Running）
CPU 使用：100% 占用一个核心
即使没有工作，也在持续消耗 CPU
```

**改用后（Condition Variable）**：
```
Goroutine 状态：阻塞（Blocked）
CPU 使用：接近 0%（休眠状态）
只有在条件满足时才会被唤醒
```

### 2. 响应时间

**改用前（Busy Wait）**：
```
SetDoneUntil 被调用
    ↓
等待下一次循环检查（轮询间隔）
    ↓
检查条件，继续执行
响应延迟 = 轮询间隔
```

**改用后（Condition Variable）**：
```
SetDoneUntil 被调用
    ↓
立即调用 Broadcast()
    ↓
立即唤醒等待的 goroutine
    ↓
检查条件，继续执行
响应延迟 ≈ 0（即时唤醒）
```

### 3. 资源管理

**改用前（Busy Wait）**：
- Goroutine 无法进入等待状态
- 调度器无法有效管理
- 多个等待者 = 多个 CPU 核心被占用

**改用后（Condition Variable）**：
- Goroutine 真正阻塞
- 调度器可以将其移出运行队列
- 多个等待者共享同一个条件变量，高效管理

### 4. 可扩展性

**改用前（Busy Wait）**：
```
1 个等待者：占用 1 个 CPU 核心
10 个等待者：占用 10 个 CPU 核心
100 个等待者：占用 100 个 CPU 核心（如果可用）
```

**改用后（Condition Variable）**：
```
1 个等待者：0% CPU（阻塞）
10 个等待者：0% CPU（全部阻塞）
100 个等待者：0% CPU（全部阻塞）
唤醒时：所有等待者共享唤醒机制
```

## 性能对比示例

### 场景：10 个 goroutine 等待 DoneUntil

**Busy Wait 方式**：
```
CPU 使用率：~1000% (10 个核心 × 100%)
响应时间：~1-10ms (取决于轮询间隔)
资源消耗：高
```

**Condition Variable 方式**：
```
CPU 使用率：~0% (全部阻塞)
响应时间：~0.1ms (即时唤醒)
资源消耗：低
```

## 实现要点

### 1. 为什么需要 for 循环？

```go
for p.GetDoneUntil() < targetCycle {
    p.doneUntilCond.Wait()
}
```

**原因**：防止虚假唤醒（spurious wakeup）
- `Wait()` 可能在没有 `Broadcast()` 的情况下返回
- 使用 `for` 循环确保条件真正满足才退出
- 这是使用 condition variable 的标准模式

### 2. 为什么需要 Fast Path？

```go
if p.GetDoneUntil() >= targetCycle {
    return
}
```

**原因**：避免不必要的锁竞争
- 如果条件已满足，直接返回
- 不需要获取锁和初始化 condition variable
- 提高常见情况下的性能

### 3. 为什么使用 Broadcast 而不是 Signal？

```go
p.doneUntilCond.Broadcast() // 唤醒所有等待者
```

**原因**：
- 可能有多个 goroutine 等待不同的 cycle
- `Broadcast()` 确保所有等待者都被唤醒
- 每个等待者会检查自己的条件，只有满足条件的继续执行

## 总结

### Busy Wait 的适用场景
- 等待时间极短（< 1 微秒）
- 单线程环境
- 对延迟要求极高的场景（但通常有更好的方案）

### Condition Variable 的适用场景
- 等待时间不确定
- 多 goroutine 并发
- 需要高效资源管理
- **我们的场景：等待 DoneUntil 变化**

### 改进效果
- ✅ CPU 使用率：从 100% 降到接近 0%
- ✅ 响应时间：从轮询延迟降到即时唤醒
- ✅ 可扩展性：从线性增长到常数级别
- ✅ 资源管理：从持续占用到真正休眠

