# CHI 设计对照差距矩阵

## 1. 概览

为对齐《设计文档》中提出的 TxFactory、Node、PolicyMgr、PluginBroker 四大核心模块，对当前 `flow_control_sim` 库（特别是 `simulator.go`、`rn.go`、`hn.go`、`sn.go`、`channel.go`、`transaction_manager.go`、`request_generator.go` 等文件）进行职责梳理，输出差距矩阵，作为后续重构的依据。

## 2. 核心职责对照表

| 设计模块 | 目标职责 | 现有实现 | 差距评估 |
| --- | --- | --- | --- |
| TxFactory | 创建 Transaction、分配源 Node、触发 `OnTxCreated` | 分散在 `Simulator` 初始化和 `RequestNode.Tick` 中，`TransactionManager.CreateTransaction` 直接被节点调用 | 缺少独立工厂与 Hook；事务 ID、生成策略与节点紧耦合 |
| Node（含 Router 默认 Hook） | 提供路由、消息发送、处理生命周期 Hook | `RequestNode`/`HomeNode`/`SlaveNode` 内嵌逻辑，使用队列钩子但无统一 Hook 体系 | 生命周期入口分散；默认路由硬编码；缺少 `OnBeforeRoute` 等 Hook |
| PolicyMgr | 提供路由、一致性、FlowControl、DomainMapping 决策 | 由 `Simulator`、`Link`、节点内部直接处理；无独立策略管理器 | 需要抽象出策略接口，并集中统一调用时机 |
| PluginBroker | 统一 Hook 注册与触发，支持 FlowControl、QoS、激励插件 | 当前仅有队列级 `QueueHooks` 和 Web 可视化事件 | 缺少全局 Broker、Hook 定义、优先级与错误处理 |

## 3. 子系统拆解差距

### 3.1 Transaction 生命周期
- **设计要求**：TxFactory 统一创建事务，节点通过 Hook 通知插件；PolicyMgr 参与路由与一致性决策。
- **现状**：`RequestNode.Tick` 直接访问 `TransactionManager`，`HomeNode`/`SlaveNode` 在运行时生成响应，无第三方观察点。
- **差距**：需要在事务创建、路由前/后、发送前、处理前后补齐 Hook，并由 Broker 调度。

### 3.2 Hook 系统
- **设计要求**：OnTxCreated、OnBeforeRoute、OnAfterRoute、OnBeforeSend、OnAfterSend、OnBeforeProcess、OnAfterProcess 等 Hook。
- **现状**：仅 `queue.TrackedQueue` 提供 OnEnqueue/OnDequeue；`Link` 在发送/到达时记录事件。
- **差距**：需要重新定义 Hook 接口、调用顺序、错误策略，并将默认路由/FlowControl 逻辑迁移为默认插件。

### 3.3 PolicyMgr / DomainMapping
- **设计要求**：统一管理一致性、FlowControl credit、DomainMapping 查询。
- **现状**：`Config` 提供静态参数；`Link` 根据固定 latency 与 bandwidth 执行；无 DomainMapping 接口。
- **差距**：需要设计 `policy.Manager`，包含默认实现与查询接口；DomainMapping 需抽象为独立组件。

### 3.4 配置与生成器
- **设计要求**：TxFactory 通过配置生成请求，插件可扩展。
- **现状**：`ConfigGeneratorFactory` 与 `RequestGenerator` 均内嵌在 `Simulator` 初始化。
- **差距**：需将配置拆分给 TxFactory，减少节点对生成器的直接依赖。

### 3.5 可视化与统计
- **设计要求**：Hook 触发点清晰，便于时间线与拓扑展示。
- **现状**：`TransactionManager` 记录事件，但缺少 Hook 与策略层事件。
- **差距**：Broker 需要将 Hook 事件同步到 `TransactionManager` 或可视化层。

## 4. 可复用组件与改造建议

- **可复用**：
  - `queue.TrackedQueue` 及包装的可视化更新逻辑，可作为节点内部队列基础。
  - `TransactionManager` 结构与事件记录能力，后续配合 Hook 只需补充事件来源。
  - `Link` 管道模型与 backpressure 信号，可与 FlowControl 插件结合。
- **需替换/重构**：
  - 事务创建与请求生成流程：抽出 TxFactory，节点只消费事务。
  - 节点路由/生命周期逻辑：拆分默认路由 Hook、发送 Hook、处理 Hook。
  - 策略决策流程：引入 PolicyMgr，统一入口。
  - Hook 调度体系：实现 PluginBroker，并制定错误处理、优先级链路。

## 5. 风险与关注点

1. **循环依赖风险**：TxFactory、PolicyMgr、PluginBroker 需要清晰的接口分层，避免互相直接依赖。
2. **性能影响**：引入 Hook 后需确保链路调用开销可控，必要时提供同步/异步选项。
3. **测试覆盖**：需要在重构过程中保持 `simulator_test.go`、`backpressure_test.go` 可用，并补充 Hook 行为单测。
4. **可视化兼容**：`SimulationFrame` 输出结构需保持接口稳定，防止 Web 端断裂。

以上差距矩阵将作为后续模块化重构的输入，针对性推进包结构调整与新组件实现。


## 6. 近期进展

- `core/` 包已承载消息、事务、事件等基础类型，主包通过别名复用，消除了重复定义。
- 新增 `hooks.PluginBroker` 与 `TxFactory`，`OnTxCreated`、`OnBeforeSend`、`OnAfterSend`、`OnBeforeProcess`、`OnAfterProcess` 已接入 `RequestNode` 与 `SlaveNode` 的生命周期。
- 扩展路由阶段 Hook 与策略接口：`RequestNode`、`HomeNode` 已在 `OnBeforeRoute`/`OnAfterRoute` 中调用 `policy.Manager` 默认实现，便于后续路由/FlowControl 策略插件化。
- `policy.Manager` 现统一封装路由、流控、域查询接口，并提供默认实现；`RequestNode`/`HomeNode` 已在发送前调用 `CheckFlowControl`，为真实 FlowControl 策略留出接入点。
- `Simulator` 在初始化与重置阶段装配 `PluginBroker`、`TxFactory`，并为节点注入 Hook 能力，保持测试通过。

