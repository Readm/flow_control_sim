# Busy Wait vs Condition Variable 说明

## 当前实现（第69行）的问题

### Busy Wait 的问题

```go
// Wait for upstream DoneUntil >= cycle
for cp.upstreamPort.GetDoneUntil() < cycle {
    // Busy wait or yield - in real implementation, might use condition variable
}
```

**问题**：
1. **CPU 浪费**：循环不断检查 `GetDoneUntil()`，即使值没有变化也会持续占用 CPU
2. **延迟响应**：需要轮询检查，不能立即响应 `SetDoneUntil` 的变化
3. **可扩展性差**：多个 goroutine 同时 busy wait 会消耗大量 CPU 资源
4. **无法阻塞**：当前实现是空循环，goroutine 无法真正"休眠"

### 为什么需要 Condition Variable

**Condition Variable（条件变量）** 的作用：
1. **阻塞等待**：goroutine 可以真正休眠，不占用 CPU
2. **即时唤醒**：当条件满足时（`SetDoneUntil` 被调用），立即唤醒等待的 goroutine
3. **高效同步**：避免轮询，减少 CPU 使用
4. **可扩展**：多个 goroutine 可以高效地等待同一个条件

## 改用 Condition Variable 的实现

### 1. 在 Port 中添加 Condition Variable

需要在 `Port` 结构体中添加一个 condition variable 用于等待 `DoneUntil` 的变化。

### 2. 在 SetDoneUntil 中唤醒等待者

当 `SetDoneUntil` 被调用时，唤醒所有等待的 goroutine。

### 3. 在 ProcessCycle 中使用 Condition Variable 等待

使用 `cond.Wait()` 阻塞等待，而不是 busy wait。

## 改用前后的区别

### 改用前（Busy Wait）

```go
// 问题：持续占用 CPU
for cp.upstreamPort.GetDoneUntil() < cycle {
    // 空循环，持续检查
}
```

**特点**：
- CPU 使用率高（100% 占用一个核心）
- 响应延迟（需要轮询间隔）
- 无法真正休眠
- 简单但低效

### 改用后（Condition Variable）

```go
// 高效：真正阻塞，不占用 CPU
cp.upstreamPort.WaitForDoneUntil(cycle)
```

**特点**：
- CPU 使用率低（goroutine 休眠）
- 即时响应（条件满足立即唤醒）
- 真正阻塞（goroutine 进入等待状态）
- 高效且可扩展

## 性能对比

| 特性 | Busy Wait | Condition Variable |
|------|-----------|-------------------|
| CPU 使用 | 高（持续占用） | 低（休眠时接近0） |
| 响应时间 | 延迟（轮询间隔） | 即时（条件满足立即唤醒） |
| 可扩展性 | 差（多个等待者消耗大量 CPU） | 好（多个等待者共享条件变量） |
| 实现复杂度 | 简单 | 稍复杂 |
| 适用场景 | 短暂等待 | 长时间等待 |

