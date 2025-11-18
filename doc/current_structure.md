# 当前目录扫描概览（2025-11 重构后）

- 根目录仅保留 `doc/`、`framework/`、`configs/`、`ref/`、`main.go` 以及 `go.mod/go.sum`，入口职责清晰。
- `framework/app/` 收敛了原先散落的 40+ 个 `package main` 文件，包含节点、链路、事务、模拟器、Web API、静态前端资源与配置校验逻辑。
- `framework/core/` 承载 CHI 协议最小实体与性能关键基础设施，并下沉 `queue/`、`slicc/` 等子模块。
- `framework/hook/` 单独维护 Hook Broker 与 Registry，成为核心与插件之间的唯一边界。
- `framework/plugins/` 聚合 `capabilities`、`incentives`、`visualization`、`policy_manager`、`router_core`、`protocols` 等能力与扩展。
- `configs/` 目录以 Go 代码形式注册所有可选拓扑/能力组合，通过 `configs.Provider()` 暴露给 CLI 与 Web。
- `ref/` 收入口径：`arm_chi_rag/`、`vector_db/`、`examples/`、`tools/`、`internal/` 等与模拟逻辑无关的资料。
- 所有设计、计划、状态文档集中在 `doc/`，包括 `DEVELOPMENT_PLAN.md`、`TEST_RESULTS.md`、`TODO.md`、`config/README.md` 等。

该扫描用于记录重构后的基线，新的目录边界将作为后续迭代的参考。
