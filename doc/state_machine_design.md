# 状态机协议扩展架构

本文档说明如何在现有框架中，以 **Capability + Hook** 方式实现可插拔的一致性协议，并用纯 Go 结构描述状态机。

## 设计目标

1. **状态机解耦**：缓存行状态（MESI/MOESI/MI…）以 `StateMachineSpec` 描述，不依赖外部 DSL。
2. **协议插件化**：Node 在例化时可选择不同协议实现（CHI、CXL.mem…）。
3. **策略可拆**：写策略（WT/WB）、替换策略、缓存结构等通过独立能力组合。

## 关键结构

### StateMachineSpec

```go
type StateSpec struct {
    Name        string
    Description string
}

type EventSpec struct {
    Name        string
    Description string
}

type TransitionSpec struct {
    FromStates []string
    Events     []string
    ToState    string
    Actions    []string
}

type StateMachineSpec struct {
    Name         string
    Description  string
    DefaultState string
    States       []StateSpec
    Events       []EventSpec
    Transitions  []TransitionSpec
}
```

协议作者只需在 Go 代码中构造一个 `StateMachineSpec` 变量即可完成协议描述。

### ProtocolStateMachine Capability

`capabilities/state_machine.go` 将 `StateMachineSpec` 包装为 `ProtocolStateMachine`：

- `ApplyEvent(address, event)`：驱动状态机发生状态转换。
- `CurrentState(address)`：查询当前状态。
- 可选绑定 `RequestCache`，当状态改变时同步 MESI 状态。

节点在 `ensureDefaultCapabilities` 中，通过工厂方法注入对应的状态机能力。

## 可插拔能力矩阵

| 维度                     | 责任组件                                |
|--------------------------|-----------------------------------------|
| 状态机 (MESI/MOESI/MI)   | `StateMachineSpec` + ProtocolStateMachine |
| 协议动作 (CHI/CXL)       | 分角色的 CHI Capability（Request/Home/Slave） |
| 写策略 (WT/WB)           | 独立 `WritePolicyCapability`            |
| 替换策略 (LRU/FIFO/…)    | `ReplacementPolicyCapability`           |
| 缓存组织 (Set/Direct/…)  | `CacheStructureCapability`              |

各能力之间只通过公共接口交换信息，实现真正的“按需组合”。

## Node 例化流程

1. 解析配置，确定节点所需角色（RN/HN/SN）以及协议实现（例如 `chi.mesi.mid`）。
2. 安装基础能力：
   - `CacheCapability`：可同时暴露 `RequestCache()` 与 `HomeCache()`，供不同角色共享同一份缓存。
   - `DirectoryCapability`：仅在 HN 需要。
3. 构建共享状态机：`StateMachineSpec` → `ProtocolStateMachine`，并向下游能力传递。
4. 组合角色能力：
   - `CHIRequestCapability` = `RequestCache` + `ProtocolStateMachine` + `TransactionCreator`（+ `RequestCacheHandler`），处理 RN 请求、响应与 Snoop。
   - `CHIHomeCapability` = `HomeCache` + `DirectoryStore` + `ProtocolStateMachine`，处理 HN 的目录/Snoop/数据编排。
   - `CHISlaveCapability` = `ProtocolStateMachine`（可选）+本地存储，负责 SN 的 CompData/CompAck。
5. 节点只保留 Hook 与队列操作，所有协议行为在 Capability 内完成。

## 测试策略

- `StateMachineSpec.Validate()`：确保状态、事件、转换引用合法。
- `ProtocolStateMachine` 单元测试：验证事件驱动后状态正确、能够同步缓存状态。

通过以上设计，任何新的协议或策略都可以在不修改核心 Node 的前提下，以 Go 包形式添加并注册，实现真正意义上的可插拔一致性实现。 

