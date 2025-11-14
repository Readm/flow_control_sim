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

## 当前进展（阶段 1）

- [x] 新增 `NodeTypeRT`  
- [x] 实现 `RingRouterCapability`（环转发判定）  
- [x] 实现 `RingRouterNode`（三阶段管线、Hook 框架）  
- [ ] 基础单元测试

> 说明：完成每个阶段后会更新此文档，确保协同人员了解最新状态。


