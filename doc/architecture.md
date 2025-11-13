# CHI 协议模拟框架架构说明

本文档用于正式记录框架的核心边界、Hook 扩展体系和目录职责，确保团队在后续实现中始终遵循“最小核心 + 插件解耦”的设计原则。

## 架构概览

- **核心层（Core）**：仅定义协议最小实体（`Packet`、`Transaction`、`Node` 基础信息、事件类型等），并提供 Metadata API。核心层不包含任何策略或决策逻辑。
- **节点层（Nodes）**：负责节点生命周期调度、队列管理以及触发 Hook。节点层不得直接实现路由、流控、一致性等策略功能。
- **Hook 系统（Hooks）**：`PluginBroker` 串行触发所有 Hook，并通过 `RouteContext` / `MessageContext` / `ProcessContext` 等上下文将必要数据暴露给外部。Hook 是核心与外部扩展之间唯一合法的交互层。
- **能力层（Capabilities）**：封装框架内置的默认策略能力（如 PolicyMgr 包装、流控、统计等），并以 Hook 形式注册。能力层允许访问 `policy` 包中的接口，但仍通过 Hook 影响核心行为。
- **插件层（Plugins）**：面向框架使用者的定制代码。插件只能通过 Hook Context 或 Metadata 扩展行为，禁止直接操作核心结构或节点内部状态。
- **策略接口层（Policy）**：提供路由、流控、域映射等接口及默认实现，服务于能力层或插件层，核心层不直接依赖具体策略实现。
- **可视化与前端（Visualization / Web）**：读取核心和 Hook 输出的结构化数据进行展示，不反向影响模拟流程。

## 目录职责

| 目录                    | 职责说明                                                                 |
| ----------------------- | ------------------------------------------------------------------------ |
| `core/`                 | 定义最小协议实体、Metadata API、事件类型等纯数据对象                      |
| `hooks/`                | Hook 类型与上下文、`PluginBroker`、注册中心；强调“核心与扩展隔离层”            |
| `nodes/`（未来拆分）    | 各类节点的最小实现：队列管理、生命周期调度、Hook 触发                      |
| `capabilities/`         | 内置能力封装（默认 PolicyMgr Hook、基础流控等），以 Hook 注册的方式影响行为    |
| `policy/`               | 策略接口定义与默认实现，为能力层/插件层提供统一策略 API                   |
| `plugins/`              | 使用者自定义插件，遵循 Hook API 与 Metadata 扩展规范                       |
| `config/`               | 拓扑、Hook 挂载顺序、插件参数等运行时配置                                 |
| `doc/`                  | 设计文档、架构约束、进度记录                                               |

> **强调**：`hooks/` 是隔离边界。核心层和节点层只与 Hook 系统交互，使用者定制代码必须通过能力层或插件层注册到 Hook 中，禁止越过该边界直接操作核心数据结构。

## Hook 调用顺序

1. **事务创建阶段**：`OnTxCreated`（TxFactory -> Hook）
2. **路由阶段**：`OnBeforeRoute` -> 默认策略/Policy -> `OnAfterRoute`
3. **链路发送阶段**：`OnBeforeSend` -> 默认流控/Policy -> Send -> `OnAfterSend`
4. **节点处理阶段**：`OnBeforeProcess` -> 节点处理逻辑 -> `OnAfterProcess`

- Hook 按注册顺序串行执行，任意 Hook 返回错误都会终止后续 Hook 并阻止该阶段继续。
- 所有 Hook 仅能通过上下文对象读写数据；若需要传递额外信息，应使用 Packet/Transaction 的 Metadata。

## 扩展机制

- **Metadata Map**：`Packet`、`Transaction` 等核心对象提供 `map[string]string` 的 Metadata 字段，以及 `Set/Get/Delete` 封装方法。插件或能力可在不修改核心结构的情况下附加自定义信息。
- **Context 扩展**：当扩展场景需要更多信息时，应优先在 `RouteContext`、`MessageContext`、`ProcessContext` 中补充字段，而非修改核心结构体。
- **能力注入**：默认策略以 Capability 的形式注册为 Hook，形成“核心 -> Hook -> 能力”链路，方便替换或禁用。
- **原子能力**：缓存（`NewCacheCapability` 及其包装 `NewMESICacheCapability` / `NewHomeCacheCapability`）、目录（`NewDirectoryCapability`）与事务（`NewTransactionCapability`）均通过能力封装，节点仅保存接口引用，禁止直接维护内部 map。
- **外部桥接**：若需要对接外部模拟器或可视化系统，应通过 `OnAfterProcess`、`OnAfterSend` 等 Hook 输出事件，而不是直接耦合在核心代码中。

## 约束摘要

- 核心代码只管理生命周期与数据结构，**绝不实现策略逻辑**。
- 节点代码只负责调度与触发 Hook，**不得直接访问 PolicyMgr**。
- 所有策略扩展（流控、路由、激励、QoS 等）都必须通过 Hook 系统实现。
- 插件卸载后，所有策略字段或行为必须自动消失，不影响核心结构。
- 文档与规则（`.cursorrules`）需要与本说明保持一致，确保团队协作遵守架构边界。

## 后续工作指引

1. 根据本文档更新 `.cursorrules`，使开发工具自动提醒违规使用。
2. 将现有节点实现迁移到 `nodes/`（若尚未拆分），并剥离其中的策略逻辑。
3. 以 Capability 形式实现默认 PolicyMgr Hook，验证 Hook 隔离机制。

通过上述架构约束，我们可以保证核心框架保持精简并易于维护，同时为使用者提供强大的可插拔扩展能力。

