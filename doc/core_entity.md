# Core/Entity 设计说明

## 设计目标

- 聚焦 `config`、`Network`、`Node`、`Link` 四个实体，提供最小可运行骨架，保证后续扩展仍然遵守 KISS 原则。
- 通过接口抽象隔离外部依赖（例如 Hook、插件、数据流），当前阶段使用 Mock 即可完成验证。
- 为并行执行提供可观测手段：放大 `link delay` 可以更容易验证多节点同时运行。
- 保留一个稳定的单元测试，确保未来改动不会破坏并行特性。

## 组件职责

- `config`：`internal/config/entity.go` 维持节点（int ID）与链路基础配置，是 Core/Entity 层与外部配置系统的边界。
- `node`：`internal/core/node/node.go` 规范节点接口（`ID() int`、`Flow() flow.Flow`、`Tick(...)`）。每个节点内部实例化 Flow 并在 Tick 中驱动它。
- `link`：`internal/core/link/link.go` 中的 `Link` 结构使用固定 slot 的 ringbuffer 在逻辑 cycle 间缓存 Packet，并在 `Advance(cycle)` 时把 `(cycle, Packet)` 投递到目标 Flow 的 mailbox channel，无锁实现。
- `dataflow`：`internal/dataflow/packet`/`flow` 提供正式的 Packet（`SourceID`/`TargetID` 均为 int）与 Flow（带 `Tick`、`Emit`、`DrainOutgoing`、`Mailbox` channel）。
- `network`：持有 “节点为点、Link 为边” 的有向图。每个 cycle 先调用所有 Link `Advance` 让 Flow 收包，再并发执行节点 Tick，并在 Tick 结束后顺序路由 Flow 的 `DrainOutgoing()` 结果到对应的 Link。
- `controller`：`pkg/controller` 暴露 `SimulationController` 接口，Interface 层通过它启动/停止 Network，并可在测试中注入 Mock Builder。

## 接口约定

```
// internal/config/entity.go
type EntityConfig struct {
    Nodes []NodeConfig
    Link  LinkConfig
}

// internal/core/node/node.go
type Node interface {
    ID() int
    Flow() flow.Flow
    Tick(ctx context.Context, cycle uint64, linkDelay time.Duration) error
}

// internal/core/network/network.go
type Manager interface {
    Run(ctx context.Context, cycles uint64) error
}
```

说明：

- `Tick` 的 `linkDelay` 参数用于在 Mock 场景下注入真实时间延迟（通过 `network.EnableMockDelay` 控制），以便放大并行信号；正式运行时默认传 0。
- Network 在每个 cycle 中为每个 Node 启动一个 goroutine，并借助 `sync.WaitGroup` 与 `errCh` 收敛错误。

## 并行执行流程

1. `Network.Run` 校验 nodes 非空，并创建 `errCh` 收集执行结果。
2. 每个 cycle：
   - 先调用所有 Link 的 `Advance(cycle)`，把到期的 Packet 写入目标 Flow 的 mailbox channel。
   - 并行调用所有节点的 `Tick`，驱动 Flow 处理并生成新的 Packet。
   - 将节点 Flow 的 `DrainOutgoing()` 结果按图结构投递给对应 Link。
   - 若开启 `network.EnableMockDelay`，在进入下一 cycle 前休眠指定时间；正式运行则直接继续。
3. `Run` 结束时返回第一条错误（若有），确保调用方能感知问题。

## Mock 策略

- **Node Mock**：单元测试实现的 `node.Node` 会在内部创建 `flow.FIFO`，Tick 内驱动 `flow.Tick`、借由 `flow.Emit` 生成待发送的 Packet，并通过工作负载模拟计算耗时。
- **Link Mock**：直接使用生产代码中的 `link.Link`。若未来需要特殊行为，可在测试中扩展独立实现。
- **时间延迟 Mock**：通过 `network.EnableMockDelay(time.Duration)` 注入真实时间等待，只在测试/Mock 时启用，避免污染正式逻辑。
- **Config Mock**：测试可直接构造 `config.EntityConfig` 或者仅依赖 `NewManager` 所需的 graph 信息。
- **Controller Mock**：CLI/Web 层只依赖 `SimulationController`，测试场景可以传入 Fake `ManagerBuilder`，而正式环境使用 Network + Mock Node 组合验证启停。

## 并行性测试

- 位置：`internal/core/network/network_test.go`
- 测试方法：
  1. 构建两个节点（ID 0/1），各自内部持有 `flow.FIFO`，通过共享的 `link.Link` 互发 `packet.Packet`，每条 Link 的 latency 为 1 个 cycle。
  2. `Link.Advance` 在每个 cycle 将 slot 中的 `(cycle, Packet)` 写入目标 Flow 的 `Mailbox`（Go channel），Flow Tick 读取 channel 并记录 `ProcessedCount`。
  3. 节点 Tick 内部调用 `flow.Tick` 消费报文，再使用 `flow.Emit` 生成下一个 cycle 的 Packet（非末尾 cycle），同时执行 35ms workload 来放大并发观察窗口。
  4. Network 在所有节点 Tick 完成后，从 Flow `DrainOutgoing` 中拿到 Packet，并按 graph（Node->Link）顺序调用 `Link.Transmit`。断言：两端 Flow 均处理 `cycles - latency` 个 Packet，`maxActive == 2`，总耗时显著小于串行估计。
- 运行命令：

```
go test ./internal/core/network -run TestNetworkNodesExchangePacketsThroughLink -timeout 5s
```

## 控制器测试

- 位置：`pkg/controller/controller_test.go`
- 测试方法：
  1. 使用正式 `network.Manager` + Mock Node/Link 构建 `ManagerBuilder`，验证 `SimulationController` 的 `Start/Stop/State` 行为。
  2. 断言重复 `Start` 会返回 `ErrAlreadyRunning`，未启动就 `Stop` 返回 `ErrNotRunning`，Stop 过程中上下文超时会正确传播。
- 运行命令：

```
go test ./pkg/controller -timeout 5s
```

## 未来扩展

- 当 DataFlow 准备就绪，可在 Node Tick 内引入 Packet/Message 缓冲，但接口保持不变。
- `link.Link` 可以扩展出多种 latency/policy（例如带宽、批量发送），Network 只需替换图中的具体实例即可。
- 当 Hook 系统接入时，可在 `Network.Run` 的 cycle 边界发出事件，无需改变 Node/Link 接口。

