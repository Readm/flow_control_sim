# Config 模块（Go 定义）

```
configs/*.go (Register descriptors)
          ↓
    configs.Provider()
          ↓
app.SetConfigProvider(...) → ValidateConfig → initializeSimulatorComponents
```

- **定义入口**：`configs/` 目录内的 Go 文件通过 `configs.Register(app.ConfigDescriptor{...})` 注册拓扑、节点能力与运行参数。每个描述包含唯一的 `Name`、`Description` 与 `*app.Config` 模板。
- **Provider 注入**：`main.go` 在启动最前调用 `app.SetConfigProvider(configs.Provider())`，其后 Web API、CLI、Hook 等场景均通过 `GetPredefinedConfigs` / `GetConfigByName` 访问统一来源。
- **校验机制**：`configs/configs_test.go` 会遍历 Provider 暴露的所有配置并调用 `app.ValidateConfig`，确保必填字段、缺省值与拓扑约束有效。
- **拓扑/能力声明**：`app.Config` 仍负责描述节点数、链路延迟、环形启用、调度脚本、插件装配等；如需更细粒度的节点/能力映射，可在 `Config` 扩展字段或配套 Hook Metadata。
- **插件字段**：`Config.Plugins` 可声明激励、可视化等 Hook，示例：

```go
cfg.Plugins.Incentives = []string{"random"}
cfg.VisualMode = "web"
```

新增配置请同步更新 `doc/architecture.md` 或对应设计文档，说明该拓扑的使用场景与限制。
