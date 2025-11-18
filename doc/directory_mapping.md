# 目录迁移映射（草案）

| 现有路径 | 目标路径 | 说明 |
| --- | --- | --- |
| `core/` | `framework/core/` | 保留 `package core`，提供最小协议实体与元数据 API |
| `queue/` | `framework/core/queue/` | 进程/流水线队列实现，属于核心性能基础 |
| `slicc/` | `framework/core/slicc/` | 状态机规格工具，与核心协议定义耦合 |
| 根目录 `*.go`（除 `main.go`） | `framework/app/` | 改为 `package app`，涵盖节点、链路、事务、模拟器、可视化桥接、Web API 等实际模拟逻辑 |
| `simulator/` | `framework/app/simulator/` | Runner/CommandLoop/VisualBridge 属于应用辅助模块 |
| `visual/` | `framework/app/visual/` | Visualizer 接口及 Null 实现 |
| `web/` (静态资源) | `framework/app/web/` | 前端资源与 README 一并迁移，更新静态目录引用 |
| `hooks/` | `framework/hook/` | PluginBroker、Registry 等 Hook 管理系统 |
| `capabilities/`, `plugins/`, `policy/`, `router/`, `protocols/` | `framework/plugins/{capabilities,...}` | 统一收敛到插件目录下，维持原包名（如 `capabilities`、`router`）|
| `capability_utils.go` | `framework/app/capability_utils.go` | 辅助函数，仅供应用内部节点使用 |
| `config/*.md`, `docs/`, `DEVELOPMENT_PLAN.md`, `TEST_RESULTS.md`, `TODO.md` | `doc/` | 集中所有文档，保持中文说明 |
| `config/`（若需保留结构说明） | `doc/config/` | 作为配置模块设计文档 |
| `config_generator_factory.go`, `config_validator.go` | `framework/app/` | 运行期校验、默认值填充 |
| `configs/*.go` | `configs/` | 以 Go 代码注册所有拓扑、节点与能力组合 |
| `arm_chi_rag/`, `vector_db/`, `examples/`, `tools/`, `internal/` | `ref/{arm_chi_rag,...}` | 参考资料、外部脚本或空目录统一收纳到 `ref/`，与主逻辑隔离 |
| `README.md`, `LICENSE`, `go.mod`, `go.sum`, `main.go` | 根目录保留 | 顶层入口与模块声明 |

> 待定：`backpressure_*`、`benchmark.go` 属于核心模拟测试，迁往 `framework/app/` 同一包；`internal/` 目前为空，如无需保留可直接删除或并入 `ref/`。

## 待确认事项

1. `configs/` 目前仅包含 README，后续需要补充 JSON/YAML 示例并提供加载入口。
2. `ref/internal/`、`ref/tools/` 仍为空目录，需确认是否保留占位或改为 README 说明。
3. `framework/plugins/router_core/` 命名保留为 “core” 以区分 `framework/plugins/router/` 插件目录，如需更贴合业务可在下一轮重命名。
4. `ref/examples/` 通过最小 `main()` 维持可构建性，但是否保留为 CLI 工具仍待讨论。
