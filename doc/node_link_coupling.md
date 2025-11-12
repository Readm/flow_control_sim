# Node / Link 耦合点梳理

## 1. 背景概览

当前仿真主干由 `Simulator`, `RequestNode`, `HomeNode`, `SlaveNode`, `Link` 组成，节点内嵌 `Node` 结构提供可视化所需的队列信息。所有组件直接引用 `TransactionManager`, `Policy`, 队列与可视化逻辑，导致骨架层难以独立抽象。本文件记录各模块与策略、事务、可视化的具体耦合位置，为后续插件化与骨架收敛提供依据。

## 2. RequestNode (`/home/readm/flow_control_sim/rn.go`)

- **外部依赖**
  - `queue.TrackedQueue`：直接维护 Stimulus/Dispatch/Snoop 队列并在回调中写入 `Node` 的可视化队列。
  - `policy.Manager`：`Tick` 中调用 `ResolveRoute` `CheckFlowControl`，与策略实现强绑定。
  - `hooks.PluginBroker`：虽已存在，但默认逻辑仍内嵌在 `Tick` 前后，Hook 只能观察少量阶段。
  - `TransactionManager`：生成与记录事件（Enqueue/Dequeue），同时通过 `TxFactory` 建立事务。
  - `PacketIDAllocator`、`Link`：直接生成并发送数据包，未经过能力层封装。
  - 日志、统计：在 `Tick` 中直接写指标，对外暴露字段。
- **耦合现象**
  - 生成事务流程与发送流程合并在 `Tick`，无法通过插件替换。
  - Flow control 校验与路由调整散落在 `Tick` 内的多处分支。
  - 缓存状态、统计、激励逻辑混杂于 RequestNode 内部。
- **拆分启示**
  - 需要抽离“事务生成”“流控校验”“缓存维护”“统计记录”为插件或能力。
  - Node 层应只保留上下文封装和 Hook 触发，移除具体策略实现。

## 3. HomeNode (`/home/readm/flow_control_sim/hn.go`)

- **外部依赖**
  - `queue.TrackedQueue`：管理 Forward 队列并向 `Node` 报告队列状态。
  - `hooks.PluginBroker`：用于路由与处理阶段的 Hook，但逻辑仍以内嵌为主。
  - `policy.Manager`：`HandleSnoop` 等流程调用策略接口。
  - `TransactionManager`：大量记录事件（Enqueue/Deque, Generated, Processing）。
  - `PacketIDAllocator`, `Link`, `Config`：生成响应、发送消息时直接使用。
  - 缓存、目录、MESI 处理全部在 HomeNode 内部完成。
- **耦合现象**
  - Snoop 流程、缓存目录、响应生成都位于单一文件，缺少能力边界。
  - 可视化与统计通过直接调用 `txnMgr.RecordPacketEvent` 完成。
  - Tick 函数中包含路由、缓存、协议状态机全部逻辑。
- **拆分启示**
  - 应提炼 `RoutingCapability`, `ConsistencyCapability`, `CacheCapability`，让 HomeNode 只负责任务调度与 Hook。
  - Snoop 与目录管理可封装为单独能力或策略插件。

## 4. SlaveNode (`/home/readm/flow_control_sim/sn.go`)

- **外部依赖**
  - `queue.TrackedQueue`：维护请求队列并在 Hook 中写事务事件。
  - `hooks.PluginBroker`：处理阶段帽只限 Before/AfterProcess。
  - `TransactionManager`, `PacketIDAllocator`, `Link`：处理响应、统计、发送。
- **耦合现象**
  - 队列调度、响应生成、事件记录同处，缺乏可替换的能力层。
  - RunRuntime 直接持有 `Link` 引用，导致节点无法脱离具体链路实现。
- **拆分启示**
  - 将请求调度、响应生成拆为 `ResponderCapability`。
  - 统计、事务事件可改由 `InstrumentationCapability` 统一记录。

## 5. Link (`/home/readm/flow_control_sim/channel.go`)

- **外部依赖**
  - `TransactionManager`：发送与到达时记录 PacketEvent。
  - `CycleCoordinator`：直接管理并发循环。
  - `NodeReceiver`：节点必须实现具体接口，导致骨架与节点实现紧耦合。
- **耦合现象**
  - Link 既负责调度也负责事件记录，缺少抽象层。
  - NodeRegistry 保存具体节点指针，不支持能力注入或代理。
- **拆分启示**
  - 将事件记录与统计转交给插件，通过 Hook 或接口回调完成。
  - 将 `NodeReceiver` 抽象为骨架层接口，允许通过代理包装节点能力。

## 6. Simulator (`/home/readm/flow_control_sim/simulator.go`)

- **外部依赖与职责**
  - 初始化时直接构造节点、设置 `TxFactory`, `PluginBroker`, `PolicyManager`, `Visualizer`。
  - 负责在节点之间传递 `TransactionManager`, `PacketIDAllocator`, `CycleCoordinator`。
  - 直接处理 Web 可视化（`WebVisualizer`）绑定、命令循环。
- **耦合现象**
  - 可视化、策略、事务管理逻辑散布在构造阶段，无法替换。
  - 插件注册集中在构造函数内，缺乏配置化加载。
- **拆分启示**
  - 提供 `PluginRegistry` 负责加载可视化、策略、激励等插件。
  - Simulator 只负责骨架对象组合与调度器启动。

## 7. 小结

- Node 与 Link 通过多重共享结构体直接耦合策略、事务和可视化逻辑。
- 事务事件记录散落在节点与链路内部，需要统一抽象。
- Hook 体系虽存在，但实际逻辑仍在节点内部完成，缺少能力化封装。
- 骨架层需要重新界定：只负责上下文、信号、调度；外部行为通过能力与插件提供。

以上梳理为后续插件化拆分提供目标范围，后续阶段可据此迁移各能力实现。

