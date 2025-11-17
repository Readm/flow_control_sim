# JSON-based Simulation Configs

- `configs/json/*.json` 通过 `go:embed` 自动装载，`predefined.go` 在 `init()` 阶段解析 JSON → `app.Config` 并注册。
- JSON Schema 见 `doc/config_schema.md`：`meta/defaults/nodes/links/schedules`，强调“节点 + 能力组合 + 任意拓扑”。
- `registry.go` 仍提供 `Provider()` / `Register()` / `RegisterJSON(path string)`，CLI 与 Web 入口依赖该 Provider 罗列配置。

## 新增内置配置
1. 在 `configs/json/` 中创建 `xxx.json`：
   - `meta`：`name`（唯一）、`description`。
   - `defaults`：`total_cycles`、latency/bandwidth、`slave_weights`、`visual_mode`、`headless`、插件等。
   - `nodes`：仅描述 `id/label/capabilities`，例如 `["requester","cache:L1"]`，附加参数写入 `params`。
   - `links`：任意图描述，可为每条边指定 `latency/bandwidth/bidirectional/metadata`。
   - `schedules`（可选）：按 `tick` + `source` + `transactions` 注入 deterministic traffic。
2. 运行 `go test ./configs`，确保 loader 成功解析并通过 `app.ValidateConfig`。

## 外部/动态配置
- 使用 `configs.RegisterJSON("/abs/path/config.json")` 载入磁盘 JSON；Web UI 可在生成 JSON 后直接调用。
- 高级用法：`loader.Load(io.Reader)` → `doc.ToAppConfig()` → `Register(...)`，适用于数据库 / API 来源。

## Graph 输出
- JSON `nodes/links` 会构建 `app.GraphConfig`，在 `cfg.Graph` 中可读取完整拓扑、能力、位置与 per-edge latency。
- 旧字段（`NumMasters`, `MasterRelayLatency` 等）仍会根据 JSON 自动推导，以便现有模拟器逐步过渡到真正的通用节点模型。

