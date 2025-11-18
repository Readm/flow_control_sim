 # JSON 配置 Schema（草案）

 该 Schema 旨在替代 `configs/*.go` 的硬编码方式，让 CLI/Web 统一消费 JSON，最终仍转换为 `app.Config` 或更泛化的拓扑对象。

 ## 顶层结构

 ```json
 {
   "meta": { "name": "ring_demo", "description": "..." },
   "defaults": {
     "total_cycles": 400,
     "bandwidth_limit": 1,
     "dispatch_queue_capacity": 1024,
     "visual_mode": "web",
     "headless": false,
     "plugins": { "incentives": [] }
   },
   "nodes": [ ... ],
   "links": [ ... ],
   "schedules": [ ... ],
   "initial_states": { ... }
 }
 ```

 - `meta`：注册所需的唯一名称、描述、标签等。
 - `defaults`：全局模拟参数，缺省时可回退到 `app` 内置默认值。
 - `nodes`：节点集合，仅记录通用字段与能力组合。
 - `links`：任意拓扑的有向边（可通过 `bidirectional` 标记生成对向边）。
 - `schedules`：可选，描述事务注入（替代 `ScheduleConfig`）。
 - `initial_states`：可选，描述缓存初始状态。
- **强制要求**：`nodes` 与 `links` 共同描述完整图结构，JSON 不再支持 `RingEnabled` / `RingInterleaveStride` 等旧式开关，也不会在 Loader 中补全虚拟 router。

 ## 节点定义

 ```json
 {
   "id": "rn0",
   "label": "Request 0",
   "position": { "x": 0, "y": 0 },
   "capabilities": ["requester", "cache:L1"],
   "params": {
     "request_rate": 0.6,
     "cache": { "capacity": 64 },
     "hooks": ["route.default", "process.default"]
   }
 }
 ```

 - `capabilities` 是字符串数组，仅表达“具备什么能力”，不绑定具体策略实现。
 - 推荐内置模板：
   - `requester`：代表 CHI RN 行为，对应 RequestNode 逻辑。
   - `home_directory`：代表 HN + 目录能力。
   - `slave_target`：代表 SN/Memory，支持 `process_rate`。
   - `relay`：纯转发节点，可附加缓存/统计插件。
   - `cache:<tier>`：缓存能力（如 `cache:L1`, `cache:L2`）。
 - `params`：能力特定参数，例如缓存容量、请求速率、Hook 选择。

 ## 链路定义

 ```json
 {
   "from": "rn0",
   "to": "relay0",
   "latency": 2,
   "bandwidth": 1,
   "metadata": { "channel": "req" }
 }
 ```

 - 允许任意 `from`/`to` 组合，latency/bandwidth 必须显式提供。
 - 可选 `metadata` 用于扩展（例如链路类型、颜色、统计标签）。
 - 对称链路由 JSON 写两次或设置 `bidirectional: true`。

 ## 调度 & 初始状态

 ```json
 {
   "tick": 0,
   "source": "rn0",
   "transactions": [
     { "type": "ReadOnce", "address": "0x1000", "target": "slave0" }
   ]
 }
 ```

 - `source`/`target` 均使用节点 ID，与节点角色解耦。
 - `initial_states` 结构：`{ "rn0": { "0x1000": "S" } }`，支持 cache/template。

 ## 转换与校验

 - JSON Loader 负责：
   1. 校验 `id` 唯一性、链接引用有效性、图连通性。
   2. 用能力模板映射到 Hook/Capability 组合，生成 `TopologyConfig`。
   3. 根据 `links` 填充 per-edge latency/bandwidth 数据结构。
   4. 将 `defaults` 注入 `app.Config` 的通用字段，未覆盖字段按旧默认值处理。
  5. 产出 `cfg.Graph`（`app.GraphConfig`），后续流程仅依赖该图；不再推导固定的 `Master↔Home↔Slave` 延迟，未提供的边会被视为无连接。

 - 能力模板建议使用 `configs/capabilities/*.go` 维护，避免在 loader 中直接写策略逻辑。

 ## 后续步骤

 1. 先实现 JSON -> `TopologyConfig` -> `app.Config` 的转换，保持与旧代码兼容。
 2. 后续逐步改造 `app` 层，使其直接消费 `TopologyConfig`（支持任意图）。
 3. Web 端可直接生成 JSON，通过 REST 传入 `configs.RegisterJSON` 完成注册。

