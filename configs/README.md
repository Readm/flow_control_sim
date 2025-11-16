# Go-based Simulation Configs

- 每个 `.go` 文件通过 `configs.Register(app.ConfigDescriptor{...})` 在 `init()` 阶段注册一个可选拓扑/能力组合。
- `registry.go` 会深拷贝并缓存模板，`Provider()` 实现 `app.ConfigProvider` 接口，供 `main.go` 与 Web API 枚举与按名加载。
- 新增配置步骤：
  1. 在此目录创建 `xxx_config.go`，构造 `&app.Config{...}` 并调用 `Register(...)`。
  2. 必须填写 `Name`（唯一）、`Description`、核心拓扑参数与 `ValidateConfig` 能通过的字段。
  3. 如需调度或缓存预热，可直接设置 `ScheduleConfig`、`InitialCacheState` 等结构体字段。
- 所有注册配置会在 `configs/configs_test.go` 中自动验证，保证启动前即可发现错误。

CLI 与 Web 入口在启动时执行 `app.SetConfigProvider(configs.Provider())`，从而完全依赖 `configs/` 目录下的 Go 定义。
