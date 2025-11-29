# FIFO Node 说明

## 设计目标

- 基于 `internal/core/node/Node` 构建最简 FIFO 节点，遵循 KISS。
- 仅包含一个 `InputQueue` 与一个 `OutputQueue`，保证链路直通。
- 每个 cycle 只转发一条报文，避免复杂调度。

## 工作流程

1. `Node.Tick` 先从唯一的 `InputQueue` 收集报文。
2. `ProcessHook` 拦截收集结果，只取第一条。
3. 通过 `OutputQueue.InjectPackets` 把该条报文送入下游。
4. Tick 结束时并行推进输入、输出队列，等待下一 cycle。

## 单元测试

- `TestFIFONodeForwardsOnePacketPerCycle`：构造输入包含两条消息，断言只转发第一条。
- `TestFIFONodeNoPackets`：输入为空时不触发发送，`ProcessBuffer` 保持空。

运行：`go test -timeout 5s ./internal/core/node -run TestFIFONode`.

