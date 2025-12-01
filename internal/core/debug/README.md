# Debug 工具使用说明

## 概述

这个 debug 工具用于在测试超时或死锁时辅助定位问题。它会记录关键组件的执行状态、等待条件和同步操作。

## 启用方式

使用 build tag `debug` 编译时包含 debug 代码，然后通过环境变量控制是否输出日志：

```bash
# 编译带 debug 代码的版本
go test -tags debug ./... -timeout=30s

# 启用 debug 日志输出
FLOW_SIM_DEBUG=1 go test -tags debug ./... -timeout=30s
```

**优势**：
- 默认编译时，debug 代码完全被移除，零开销
- 使用 `-tags debug` 编译时，才包含 debug 代码
- 即使包含 debug 代码，也可以通过环境变量控制是否输出

## 实现原理

使用 Go 的 build tags 实现编译期条件编译：

- **`debug.go`**：默认版本（`//go:build !debug`），提供空实现，编译器会完全优化掉
- **`debug_debug.go`**：debug 版本（`//go:build debug`），包含完整实现

在默认编译时，只有 `debug.go` 被编译，`Logf()` 是空函数，编译器会将其优化掉，调用处不会有任何开销。

## 日志内容

### 1. WaitForDone 日志
记录等待操作：
- `port`: 等待的端口地址
- `targetCycle`: 目标 cycle（需要等待到的值）
- `currentDone`: 当前的 Done 值
- `waitCount`: 等待次数（如果多次等待）

示例：
```
[15:04:05.123456] [goroutine 123] single_port.go:293: WaitForDone: port=0xc000123456, targetCycle=0, currentDone=-1, blocking...
```

### 2. SetDone 日志
记录设置 Done 操作：
- `port`: 设置的端口地址
- `cycle`: 设置的 cycle 值
- `oldDone`: 之前的 Done 值

示例：
```
[15:04:05.123789] [goroutine 124] single_port.go:80: SetDone: port=0xc000123456, cycle=0, oldDone=-1
```

### 3. Network.Advance 日志
记录网络级别的执行：
- 启动时记录节点和链路数量
- 每个组件启动和完成时记录

示例：
```
[15:04:05.100000] [goroutine 1] network.go:293: Network.Advance: starting, cycles=6
[15:04:05.100100] [goroutine 1] network.go:300: Network.Advance: nodes=3, links=3
[15:04:05.100200] [goroutine 18] network.go:308: Network.Advance: node 0 starting Advance(6)
```

### 4. Node.Advance 和 Link.Advance 日志
记录每个组件的 cycle 执行进度：
- 每个 cycle 开始和结束时记录
- 如果出错，记录错误信息

示例：
```
[15:04:05.100300] [goroutine 18] node.go:256: Node.Advance: node=0, executing cycle=0 (1/6)
[15:04:05.100400] [goroutine 18] node.go:262: Node.Advance: node=0, cycle=0 completed
```

## 分析超时问题

当测试超时时，查看日志：

1. **查找阻塞的 WaitForDone**：
   - 搜索 "blocking..." 或 "still waiting"
   - 记录 `targetCycle` 和 `currentDone`
   - 检查是否有对应的 `SetDone` 调用

2. **检查依赖链**：
   - 从阻塞的 `WaitForDone` 向上查找
   - 找到应该调用 `SetDone` 的组件
   - 检查该组件是否正常执行

3. **检查执行进度**：
   - 查看各个组件的 cycle 执行进度
   - 确认是否有组件卡在某个 cycle
   - 检查是否有组件提前退出

## 注意事项

- Debug 日志会增加性能开销，仅在调试时启用
- 日志输出可能很多，建议重定向到文件：
  ```bash
  FLOW_SIM_DEBUG=1 go test -tags debug ./... -timeout=30s 2>&1 | tee debug.log
  ```
- 日志包含 goroutine ID，可以用于追踪特定 goroutine 的执行路径

