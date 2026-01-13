+ [x] Profiling的API对原始API有入侵，可以优化 commit after 9eb551abbcfc435789636c69d028c01cfcae8838
+ [ ] transaction重新审视

### 性能优化 (Performance Optimization)
- [ ] **内存分配**: 为 `Packet` 复用实现 `sync.Pool` (需要重构为 `*Packet` 指针传递)
- [ ] **类型安全**: 将频繁使用的 `Metadata` 键提升为 `Packet` 原生字段 (减少 interface{} 开销)

### 化简与重构 (Simplification & Refactoring)
- [ ] **System Builder**: 重构 `BuildChampSimSystem` 以使用 Graph Builder DSL 或拓扑配置
- [ ] **Node Handler**: 将通用的 `Input/Output` 队列管理抽象到 `BaseNode`，特定的 Handler 只关注业务逻辑
- [ ] **配置管理**: 将仿真参数集中到 `SimulationConfig` 结构体中

### 架构可拓展性 (Architecture Extensibility)
- [ ] **插件化协议**: 将 Node 逻辑解耦为可插拔的 `Controller` (分离协议层与传输层)
- [ ] **事件驱动监控**: 实现全局 Event Bus 用于统计和监控
