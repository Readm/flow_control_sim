 # Config 现状梳理

 > 基于 `framework/app/models.go`、`framework/app/config_validator.go` 与 `configs/predefined.go` 的代码阅读整理。

 ## 核心结构

 - `app.Config` 以 `NumMasters/NumSlaves/NumRelays` 和固定延迟字段（`MasterRelayLatency` 等）描述拓扑，默认映射为 RN/HN/SN/Relay 角色。
 - 校验逻辑强制上述数量 >0 并补齐 `SlaveWeights`、`BandwidthLimit`、`RequestCacheCapacity` 等默认值。
 - 配置注册由 `configs.Register` 完成，调用方直接构造 `&app.Config{...}`，无法外部化为 JSON。

 ## 耦合问题

 1. **节点类型写死**：`NumMasters` 等字段分别对应 RN/HN/SN，无法描述“任意节点 + 能力组合”，后续新增节点类型必须改动 `app.Config`。
 2. **延迟模型单一**：仅支持 Master↔Relay↔Slave 这三段固定延迟，无法为任意两个节点指定独立 latency/bandwidth。
3. **环形拓扑特化（已移除）**：旧版依赖 `RingEnabled`、`RingInterleaveStride` 自动挂接 router，现在通过 Graph 显式描述节点与边，不再保留该类开关。
 4. **调度/逻辑混杂**：`ScheduleConfig`、`InitialCacheState` 等字段与节点角色强关联（例如 `SlaveIndex`），缺乏基于节点 ID 的统一标识。
 5. **Go 代码即配置**：`configs/predefined.go` 中每个 demo 通过 Go 代码硬编码数量、延迟与调度，Web 端无法动态组合。

 ## 迁移方向

 - 以 JSON Schema 抽象 `nodes/capabilities` 与 `links`，在加载阶段转换为旧 `app.Config` 或新拓扑对象，逐步替换硬编码字段。
 - 引入节点能力模板（cache、directory、protocol role 等），由 Hook/Capability 层装配，不再在 config 中出现 HN/SN/RN 缩写。
 - 构建图描述与边属性，解决任意拓扑与非对称 latency 需求。

