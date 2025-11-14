# 能力实现状态一览

本文概述当前框架已经实现的能力、节点默认挂载的 Capability、事务（Transaction）支持能力，以及已覆盖的协议特性，便于后续扩展与对齐。

## 1. 协议支持范围

- **CHI 协议骨架**：目前聚焦 CHI (Coherent Hub Interface) 流程，覆盖 Request Node (RN) → Home Node (HN) → Slave Node (SN) 的消息流。
- **消息类型**：
  - 事务类型：`ReadNoSnp`、`WriteNoSnp`、`ReadOnce`、`WriteUnique` (`core/packet.go` 定义)。
  - 消息类型：`Req`、`Resp`、`Data`、`Comp`、`Snp`、`SnpResp`。
  - 响应类型：`CompData`、`CompAck`、`SnpData`、`SnpNoData`、`SnpInvalid`。
- **一致性/缓存**：
  - RequestNode 持有基于 `core.MESIState` 的本地缓存状态表，用于处理 Snoop 请求。
  - HomeNode 维护目录（Directory）和简化缓存，能够发起/收集 Snoop 响应，内置 ReadOnce MESI 场景。
  - SlaveNode 在 `generateCHIResponse` 中生成 CompData/CompAck，用于完成读事务。
- **限制**：当前重点在读取与 snoop 场景，写入与多播等高级特性仍待扩展。

## 2. Node 默认 Capability

| Node 类型 | 能力名称 | 说明 |
|-----------|----------|------|
| RequestNode | `NewMESICacheCapability` | 提供本地 MESI 缓存映射，基于通用缓存能力按需开启。 |
|            | `NewLRUEvictionCapability` | 负责缓存容量管理（LRU），触发驱逐时调用缓存能力 `Invalidate`。 |
|            | `NewTransactionCapability` | 封装 TxFactory / TransactionManager，负责生成请求与事务。 |
|            | `NewRoutingCapability` | 在 `OnBeforeRoute` 阶段通过 `policy.Manager.ResolveRoute` 决策下一跳。 |
|            | `NewFlowControlCapability` | 在 `OnBeforeSend` 阶段执行 `policy.Manager.CheckFlowControl`。 |
| HomeNode   | `NewHomeCacheCapability` | 维护地址级缓存命中状态，可复用到其他组合场景。 |
|            | `NewLRUEvictionCapability` | 针对 Home 缓存行启用 LRU 驱逐，保持缓存容量可配置。 |
|            | `NewDirectoryCapability` | 维护地址 -> RN 集合的目录，支持动态增删。 |
|            | `NewRoutingCapability` | HN 内部路由决策。 |
|            | `NewFlowControlCapability` | 控制 HN→SN/HN→RN 出站流量。 |
| SlaveNode  | `NewHookCapability(slave-processing-*)` | 在 `OnBeforeProcess` / `OnAfterProcess` 记录处理事件，属于默认仪表能力。 |

- 节点在调用 `SetPluginBroker` 后会尝试挂载能力；路由/流控等策略能力需等待 `SetPolicyManager` 完成后再注册。
- Capability 通过接口 `capabilities.NodeCapability` 抽象，可选地暴露特定 Provider（如缓存、目录、事务创建器），节点仅存储接口引用而非实现细节。

## 3. Transaction 支撑能力

- **核心结构**：`core.Transaction` + `TransactionContext`
  - 字段：事务 ID、类型、地址、状态、状态历史、起止时间、同地址/全局依赖、子事务列表、`Metadata map[string]string`。
  - 仅保留最小核心字段，扩展信息通过 Metadata 管理。
- **TransactionManager** 功能：
  - 创建事务 (`CreateTransaction`) 并分配 ID。
  - 跟踪状态 (`MarkTransactionInFlight`/`Completed`/`Aborted`) 与状态变迁历史。
  - 维护依赖关系 (`AddDependency`)：同地址顺序、全局顺序、因果链等。
  - 记录 Packet 事件，支持构建 Transaction timeline/graph。
  - 提供 `AddMetadata` 等接口给 Capability/Hook 写入自定义信息。
- **Hook 上下文**：
  - `hooks.ProcessContext` 已传递 `Transaction` 指针与 `Node` 只读引用，插件可安全读取事务数据。
  - `RouteContext`、`MessageContext` 用于路由/发送阶段的扩展。

## 4. Capability 库现状

- **缓存类**：
  - `NewCacheCapability`：统一的缓存能力构造器，通过 `CacheConfig` 控制是否启用 MESI 状态或缓存行管理。
  - `NewMESICacheCapability`：基于 `NewCacheCapability` 的包装，仅启用请求侧 MESI 状态读写接口 `RequestCache`。
  - `NewHomeCacheCapability`：基于 `NewCacheCapability` 的包装，仅启用缓存行管理接口 `HomeCache`。
  - `NewLRUEvictionCapability`：面向缓存容量控制的能力，提供 `Touch` / `Fill` / `Invalidate` 接口并在超额时触发驱逐。
- **目录类**：
  - `NewDirectoryCapability`：维护地址到节点集合的映射，暴露 `DirectoryStore`。
- **事务类**：
  - `NewTransactionCapability`：包装事务/请求创建函数 `TransactionCreator`，默认整合 TxFactory + TransactionManager。
  - `NewDefaultTransactionCapability`：封装包 ID 分配与事务创建回调，节点无需保留额外退化实现。
- **策略类**：
  - `NewRoutingCapability`：封装 `policy.Manager.ResolveRoute`。
  - `NewFlowControlCapability`：封装 `policy.Manager.CheckFlowControl`。
  - `NewRingRoutingCapability`：提供环形拓扑下的逐跳转发，接入 `BeforeRoute` Hook。
- **通用 Hook 封装**：
  - `NewHookCapability`：可将任意 `hooks.HookBundle` 注册到 Broker（SlaveNode 默认使用）。
- 能力分类通过 `hooks.PluginDescriptor.Category` 管理：`policy`、`capability`、`visualization`、`instrumentation` 等。

## 5. 缓存与一致性实现

- **RequestNode 缓存**：
  - 由 `NewMESICacheCapability`（或自定义 `NewCacheCapability` 配置）提供的 `RequestCache` 管理 MESI 状态，节点只调用 `GetState/SetState`。
  - 能力内置 `HandleResponse` / `BuildSnoopResponse`，Snoop 响应与状态迁移完全由能力负责，可按需替换实现。
- **HomeNode 缓存与目录**：
  - `NewHomeCacheCapability` 管理缓存命中与失效，缓存行包含 `State` / `Metadata`，命中时直接返回 CompData。
  - `NewDirectoryCapability` 维护 sharer 集合，驱动 Snoop 路径。
- **SlaveNode 数据出口**：
  - 当前仍专注于 CompData/CompAck 响应，可通过新增 Capability 扩展写入延迟或 QoS。

## 6. 协议/能力缺口（后续可扩展）

- 写事务（WriteNoSnp/WriteUnique）及相关一致性处理仍未完善。
- QoS、可靠性、激励等策略尚无默认 Capability，可通过插件扩展。
- PolicyMgr 默认 Router/Flow/Domain 实现仍为最简逻辑，需要按实际策略替换。
- Hook 体系已就绪，需根据场景实现更多插件（如统计、背压控制、可视化增强）。

---

如需新增或调整 Capability，请同步更新本文档与 `.cursor/rules/architecture.mdc`，保持架构约束与实际实现一致。

