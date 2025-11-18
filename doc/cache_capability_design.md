# 缓存能力设计说明

## 背景

随着系统中节点角色与一致性策略不断扩展，原有的 `NewMESICacheCapability` 与 `NewHomeCacheCapability` 采用完全独立的存储实现，导致代码重复且难以复用于新的组合场景（例如 Home Node 同时作为其他网络的 Request Node）。为降低耦合、便于后续扩展，引入统一的缓存能力设计。

## 设计目标

1. **去除节点角色耦合**：能力层不再区分 Request/Home 角色，通过配置决定启用的功能。
2. **复用存储逻辑**：共享底层 `cacheStore`，消除双份 map/锁管理。
3. **保持接口稳定**：继续暴露 `RequestCache`、`HomeCache` 接口，避免节点代码修改。
4. **扩展友好**：新增能力组合仅需调整配置或包装函数，无需复制实现。

## 核心改动

- 引入 `CacheConfig`，包含 `EnableRequest`、`EnableLine`、`DefaultState`、`Description` 等开关，驱动统一构造函数 `NewCacheCapability`。
- `RequestCache` 与 `HomeCache` 通过适配器访问共享存储，内部完成默认状态填充、元数据拷贝等细节处理。
- `NewMESICacheCapability` / `NewHomeCacheCapability` 变为基于配置的薄包装，向下兼容既有调用。
- 增加类型断言保证能力实现同时满足 `CacheCapability`、`CacheWithRequestStore`、`CacheWithHomeStore`、`RequestCacheHandler` 的接口契约。

## CacheConfig 详解

- `EnableRequest`：启用 MESI 状态表，提供 `RequestCache()`，适合需要 Snoop 响应与状态迁移的节点。
- `EnableLine`：启用缓存行管理，提供 `HomeCache()`，可维持 MI/MESI 等缓存行元数据。
- `DefaultState`：当更新缓存行但未指定状态时使用的默认值，便于构建 MI-only 场景。
- `Description`：注册到 `PluginDescriptor` 的文案，可自定义能力描述；若为空则按功能组合自动生成。

## 使用范式

| 场景 | 推荐配置 |
| ---- | -------- |
| 传统 Request Node | `NewMESICacheCapability`（仅 `EnableRequest`） |
| 传统 Home Node（MI 缓存） | `NewHomeCacheCapability`（`EnableLine` + 默认状态） |
| 混合角色（既是 RN 又是 HN） | `NewCacheCapability` + `{EnableRequest:true, EnableLine:true}` |
| 自定义策略/调试 | 传入自定义 `CacheConfig`，或在 Metadata 中扩展额外属性 |

## 扩展点

- 可以在 `HomeCacheLine.Metadata` 中挂载写策略、统计信息等扩展字段。
- 若后续需要支持 write-through / write-back，可在 hooks 或能力包装层读取状态并实现数据回写策略。
- 统一存储为后续引入替换实现（如 LRU/LFU、分层缓存）提供了更清晰的扩展边界。
- 2025-12 新增 `capabilities.NewLRUEvictionCapability`，RequestNode/HomeNode 默认挂载 LRU 能力：
  - `LRUEvictionConfig.Capacity` 控制缓存行上限，超限时通过能力自动调用 `Invalidate`。
  - `RequestCacheCapacity` / `HomeCacheCapacity` 支持按配置覆写容量，方便模拟不同缓存规模。

## 验证

- `go test ./... -timeout 60s`：确保能力行为与现有节点逻辑兼容。
- 单元测试新增统一能力组合场景，验证默认状态与接口兼容性。


