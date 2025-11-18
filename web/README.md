# Web 模块

```
WebServer
  ├─ Router (multiplexer)
  ├─ ApiLayer (REST handlers)
  └─ WsHub (广播/订阅)
```

- **Router**：负责注册 HTTP/WS 路由。
- **ApiLayer**：按功能拆分控制、统计、配置等 REST 处理逻辑。
- **WsHub**：集中管理 WebSocket 会话、广播最新帧数据。

## 前端视图划分

前端采用标签页布局，对模拟信息进行分层展示：

| 视图 | 对应模块 | 说明 |
|------|----------|------|
| Flow View | `web/static/js/views/flowView.js` | 使用 Cytoscape 即时渲染链路负载、节点队列和流水线状态，并监听 WebSocket 帧更新。 |
| Transaction View | `web/static/js/views/txnView.js` | 按需请求 `/api/transaction/{id}/timeline`，利用 Mermaid 绘制时序图。 |
| Topology View | `web/static/js/views/topologyView.js` | 支持在 Cytoscape 中交互式增删节点/链路，提供撤销/重做、草稿保存能力，通过 `/api/topology` 持久化草稿。 |
| Policy View | `web/static/js/views/policyView.js` | 提供激励插件的勾选与草稿保存，使用 `/api/policy` 读取与更新当前激励配置。 |

所有视图均通过 `web/static/js/main.js` 统一注册、切换，并在收到最新帧后执行局部刷新，以避免互相干扰。

## 新增 REST 接口

| 路径 | 方法 | 功能 | 请求体 | 响应示例 |
|------|------|------|--------|-----------|
| `/api/topology` | GET | 获取当前拓扑草稿或最新帧快照 | - | `{"nodes":[...],"edges":[...],"updatedAt":"..."}` |
| `/api/topology` | PUT | 保存拓扑草稿（含节点/链路定义与布局坐标） | `{ "nodes": [...], "edges": [...] }` | 同 GET |
| `/api/policy` | GET | 列出激励插件候选项与当前草稿选择 | - | `{ "incentives": { "available": [...], "selected": [...] } }` |
| `/api/policy` | PUT | 更新激励插件草稿 | `{ "incentives": ["random"] }` | 同 GET |

> **说明**：`/api/policy` 返回的 `available` 来自插件注册表（`hooks.Registry`），保存草稿后会在下一次 Reset（含配置覆盖）时注入到 `Config.Plugins.Incentives`。

## 文件结构速览

```
web/static/
  ├─ index.html              # 多视图布局入口
  └─ js/
       ├─ main.js            # 视图注册/控制
       └─ views/
            ├─ flowView.js
            ├─ txnView.js
            ├─ topologyView.js
            └─ policyView.js
```

## 端到端测试（Rod）

- **目标**：通过 Go 写的桥接服务与 MockController 驱动真实前端页面，验证 Flow View 在不同控制命令下的拓扑重建、流水线渲染和状态切换。
- **依赖**：`github.com/go-rod/rod` 与 `github.com/go-rod/rod/lib/launcher`，默认由 Rod 自动下载 Chromium。如需复用现有浏览器，可设置 `ROD_BROWSER_PATH` 指向本地 Chrome/Chromium。
- **执行步骤**：
  1. 启动测试桥接服务（位于 `tests/e2e/server`，`go test` 会自动拉起并回收）。
  2. Rod 通过 DevTools 协议访问 `web/static/index.html`，模拟点击 Run/Reset，并断言前端 DOM 与 Cytoscape 状态。
  3. 运行命令示例：`go test -tags=e2e ./tests/e2e -run FlowView`。
- **输出**：所有断言均在 Go 测试日志中显示，失败时 Rod 可导出截图（位于 `tests/e2e/artifacts`，稍后实现）。

