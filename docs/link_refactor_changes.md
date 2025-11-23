# Link 重构后需要修改的其他代码清单

## 已修复

### ✅ 1. NewLink 函数 - 初始化 totalBackpressure & 共享 Channel 优化

**文件**：`internal/core/link/link.go`

**修复内容**：
1. 添加 `totalBackpressure: 0` 初始化
2. **新增**：使用共享 Channel 优化多上游端口聚合，消除竞态条件和额外 goroutine 开销。

### ✅ 2. 测试代码 - 适应新的处理逻辑

**文件**：`internal/core/link/link_test.go`

**已修复的测试**：
1. **TestLinkBandwidthLimit**：验证带宽限制正确，无 panic。
2. **TestLinkRingBufferMechanism**：
   - 更新断言：检查包接收情况而不是 occupancy（因为下游 ready 时 slot 会被立即清空）。
   - 验证了 ring buffer 的延迟发送逻辑。
3. **TestLinkMultipleUpstream**：
   - 验证多上游端口聚合（使用新的共享 Channel 机制）。
   - 验证包正确传输。

### ✅ 3. 错误处理 - 设计约束

**设计约束**：
- `cycle < targetCycle` panic：这是设计保证，不应该出现。
- `Slot is full` panic：这是设计约束，调用方必须确保不超过带宽限制。

### ✅ 4. 核心逻辑修正

**LinkPacketProcessor**：
- 修正了回绕（wrap-around）判断条件：`if targetCycle-cycle >= int(l.link.latency)`。
- 之前使用的是 `latency-1`，导致 latency=1 时所有包都被 pending。

## 待办事项

### ⚠️ 1. 网络层测试适配

**文件**：`internal/core/network/network_test.go`

**需要检查**：
- 网络级别的测试可能需要适应新的 backpressure 机制。
- 验证 `totalBackpressure` 在网络中的传播。

### ⚠️ 2. 节点层测试适配

**文件**：`internal/core/node/node_test.go`

**需要检查**：
- 节点级别的测试可能需要适应新的处理逻辑。
- 验证周期顺序和 DoneUntil 管理。

### ⚠️ 3. 性能监控

**需要监控**：
1. `checkReady(cycle+1)` 的执行时间
2. `SyncAggregator` 的开销（极低，主要是原子操作和锁）

### ⚠️ 4. 文档更新

**已更新**：
- ✅ `docs/link_refactor.md` - 主要重构文档
- ✅ `docs/link_refactor_changes.md` - 本文档

**可能需要更新**：
- 其他引用 Link 实现的文档
- API 文档
- 架构设计文档（更新共享 Channel 架构）

## 关键设计要点更新

1. **共享 Channel 优化**：
   - 对于多上游 Link，`NewLink` 会创建一个共享 Channel。
   - 上游端口（`CyclePortImpl`）被重定向写入此共享 Channel。
   - `Link` 直接读取此共享 Channel，消除了 `MultiUpstreamPort` 的中间转发 goroutine。
   - 同步逻辑（DoneUntil）由 `SyncAggregator` 处理，确保所有上游完成后 Link 才处理。

2. **Link 处理逻辑**：
   - 只处理 `cycle >= targetCycle` 的包。
   - 修正了回绕逻辑：超出 ring buffer 窗口的包会被 pending。
   - Ring buffer 窗口：`[cycle, cycle + latency - 1]`。

3. **设计保证**：
   - `totalBackpressure` 不会超过 `cycle` 或 `targetCycle`。
