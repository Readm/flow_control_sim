# Ring Router 进展记录

## 阶段规划

1. **基础能力搭建**  
   - 定义 RingRouter 节点类型  
   - 实现 RingRouter 专属 Capability（路由判定）  
   - 构建 Router 节点三阶段管线与 Hook 框架

2. **业务节点迁移**  
   - RN/HN/SN 仅与本地 Router 交互  
   - RingRouter 负责沿环转发与本地投递

3. **集成与可视化**  
   - `Simulator` 创建 Router 节点与环形链路  
   - Web 拓扑展示 Router+业务节点的环结构

4. **测试与文档**  
   - 补充单元/集成测试  
   - 更新设计文档、使用说明

## 当前进展

### 阶段 1 - 基础能力搭建（已完成）

- [x] 新增 `NodeTypeRT`  
- [x] 实现 `RingRouterCapability`（环转发判定）  
- [x] 实现 `RingRouterNode`（三阶段管线、Hook 框架）  
- [x] 补充 Router 能力相关单元测试

### 阶段 2 - 业务节点迁移（已完成）

- [x] RN/HN/SN 接入统一 `PacketPipeline`  
- [x] 三类节点通过 `RingFinalTargetMetadataKey` 暴露最终投递目标  
- [x] 非 Ring 拓扑保持原有直连路径  
- [x] Ring 模式下通过 Router ID 进行转发  
- [x] 新增全局兜底路由（`router.DefaultRouter`）确保异常时仍可送达

### 阶段 3 - 集成与可视化（进行中）

- [x] `Simulator` 根据配置创建 Router 节点与环形链路  
- [x] 统一注册边延迟信息供 `Channel` 使用  
- [x] Frame 数据包含 Router 能力说明  
- [ ] Web 端展示 Router 能力详情（待补充）

### 阶段 4 - 测试与文档（未完成）

- [x] `TestReadNoSnpTransaction` 等核心用例恢复通过  
- [ ] Ring 专属场景测试  
- [ ] 可视化与 README 更新

> 说明：完成每个阶段后会更新此文档，确保协同人员了解最新状态。


