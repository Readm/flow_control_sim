# TODO List

## 1. Config Serialization Support

- **Priority**: Medium  
- **现状**: 配置仍硬编码在 `soc_configs.go`，`Simulator` 初始化时构造请求生成器。  
- **目标**:
  1. 提供 `Config` 结构体的 JSON 序列化/反序列化
  2. 将 `RequestGenerator`、`ScheduleGenerator` 的配置化信息写入 JSON
  3. 支持从文件加载/保存配置，保持对现有硬编码配置的兼容
- **关键点**:
  - 需要引入可序列化的中间结构，并在加载时转换为具体 Generator
  - `ScheduleConfig` 可直接序列化，但需校验 JSON 标签
- **相关文件**: `models.go`、`request_generator.go`、`soc_configs.go`、`web_server.go`
- **预计工作量**: 2-3 天

## 2. Policy / FlowControl 策略扩展

- **Priority**: High  
- **目标**:
  1. 在 `policy.Manager` 基础上实现一个示范性 FlowControl 插件（基于 credit 或 token bucket）
  2. 支持通过 `PluginBroker` 注册/卸载策略插件（示例：动态调整路由、限速）
  3. 将 DomainMapping 与策略关联，允许插件基于 `DomainOf` 返回值做差异化决策
- **验证**:
  - 为新策略编写单元测试：并发发送下的 credit 消耗、拒绝发送日志
  - 扩展现有模拟器集成测试，验证启用/禁用策略的行为差异
- **相关文件**: `policy/manager.go`、`simulator.go`、`rn.go`、`hn.go`

## 3. 文档与示例完善

- **Priority**: High  
- **任务**:
  1. 在 `doc/design.md` 基础上新增实战示例：如何注册自定义 Hook、如何组合 `policy.Manager`
  2. 编写 `doc/testing.md` 或在 `TEST_RESULTS.md` 增补 Hook/Policy 测试说明及常见断言
  3. 更新 Web 端或 CLI 使用说明，让配置与策略可视化（如输出当前路由、credit 状态）
- **交付**:
  - Markdown 文档，包含示例代码与时序示意
  - 若涉及可视化，提供截图或操作步骤

## 4. 长时间运行与性能测试

- **Priority**: Medium  
- **内容**:
  1. 在 `simulator_test.go` 添加长周期（>10k）运行示例，观测 Hook 对性能影响
  2. 针对多 Master / 多 Slave 场景编写基准测试，分析吞吐与延迟
  3. 输出测试报告（CSV/Markdown），便于对比优化前后数据
- **相关文件**: `benchmark.go`、`simulator_test.go`

---

> 如需新增任务，请保持同样的分节格式（Priority / 目标 / 验证 / 相关文件），方便团队追踪。

