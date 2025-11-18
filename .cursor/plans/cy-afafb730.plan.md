<!-- afafb730-6519-45a8-987e-e9ee160ff301 2a7f6362-8797-462f-8213-af3d357c922a -->
# Cytoscape Web 可视化改造

1. 重构可视化接口 ✅（完成）

- 调整 `node.go` 中 `Visualizer` 接口以支持状态快照/控制指令 **已完成**
- 移除 `visualizer.go` Fyne 实现及旧依赖，新增 `web_visualizer.go` **已完成**
- 更新 `simulator.go`、`main.go` 在不同模式下选择可视化实现并保留 headless **已完成**

2. 实现 Web 服务与实时通道 ✅（完成）

- 新建 `web/server.go` 使用 `net/http` 与 `gorilla/websocket` 提供静态页面、REST 重置接口、WebSocket 推送
- 在模拟过程中按 cycle 广播拓扑状态、队列长度、统计信息
- 处理暂停/继续/重置指令，并在重置后允许更新 `Config`

3. 构建 Cytoscape 前端

- 添加 `web/static/index.html` 与基础 JS，使用 Cytoscape.js 渲染拓扑、显示节点/连线、队列和延迟信息
- 实现控制区：暂停、继续、重置、参数调节（重置后生效）
- 通过 WebSocket 接收实时数据、通过 REST/WS 发送指令

4. 更新依赖与文档

- 清理 `go.mod` 中 Fyne 相关依赖，加入 `gorilla/websocket`
- 在 `README.md`（及必要的中文文档）记录新的运行模式、前端访问方式、参数调整流程

### To-dos

- [x] 重构 Visualizer 接口并替换模拟器中对 Fyne 的依赖
- [ ] 实现 Web 服务、REST/WS 控制与实时广播
- [ ] 编写 Cytoscape 静态页面与控制逻辑
- [ ] 更新依赖配置与中文文档说明