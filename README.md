# Master-Slave 通信流控模拟系统

一个基于 Go 语言和前后端分离架构的通信流控模拟系统，用于研究 Master-Slave 架构中的通信延迟、队列管理和系统性能。

## 项目状态

🚧 **开发中** - 本项目正在重新开发，采用 Go 语言后端和前端可视化技术栈。

详细的开发计划请参考 [DEVELOPMENT_PLAN.md](./DEVELOPMENT_PLAN.md)

## 系统特性

- **多Master多Slave架构**: 支持多个 Master 节点通过 Relay 向多个 Slave 节点发送请求
- **Relay队列缓冲**: Relay 节点支持缓冲数据包（bufferable），提供队列管理能力
- **可配置延迟**: Master-Relay、Relay-Slave 等各段通信延迟可独立配置
- **队列管理**: 
  - Slave 维护 FIFO 请求队列，按固定速率处理
  - Master 跟踪待响应请求队列（pending requests）
  - Relay 维护转发队列，支持缓冲转发
- **事件驱动模拟**: 基于 cycle 的离散事件模拟
- **节点可视化**: 
  - 统一的 Node 基类架构，Master/Slave/Relay 继承自 Node
  - 实时 GUI 可视化（使用 Fyne 框架）
  - 显示节点 ID 和队列长度信息
  - 每个 cycle 实时刷新显示
  - 支持 headless 模式（无可视化运行）
- **可视化控制**: GUI 窗口提供暂停/继续/重置控制按钮

## 技术栈

### 后端
- **语言**: Go 1.21+
- **GUI 框架**: Fyne（用于节点可视化）
- **Web 框架**: Gin（计划）
- **WebSocket**: Gorilla WebSocket（计划）
- **API 文档**: Swagger/OpenAPI（计划）

### 前端
- **GUI 可视化**: Fyne（跨平台桌面 GUI）
- **Web 前端**: React 18+ 或 Vue 3+（计划）
- **Web 可视化**: Chart.js/Recharts + D3.js（计划）
- **构建工具**: Vite（计划）

### 部署
- **容器化**: Docker + Docker Compose

## 项目结构

```
flow_control_sim/
├── node.go           # Node 基类定义（统一节点架构）
├── visualizer.go     # Fyne 可视化器实现
├── master.go         # Master 节点（嵌入 Node）
├── slave.go          # Slave 节点（嵌入 Node）
├── relay.go          # Relay 节点（嵌入 Node，支持队列缓冲）
├── simulator.go      # 模拟器主循环
├── channel.go        # 通信信道
├── models.go         # 数据模型定义
├── stats.go          # 统计功能
├── go.mod            # Go 模块定义
└── README.md         # 项目说明

详细的项目结构请参考开发计划文档。

## 开发计划概览

### 第一阶段：后端核心开发
- 实现 Master/Slave/Channel 核心逻辑
- 实现主模拟器事件循环
- 单元测试

### 第二阶段：后端 API 开发
- 实现 REST API 端点
- 实现 WebSocket 支持
- API 文档生成

### 第三阶段：前端基础开发
- 搭建 React/Vue 项目
- 实现配置面板
- 实现 API 服务层

### 第四阶段：前端可视化开发
- 实现统计图表
- 实现拓扑图可视化
- 实现实时数据更新

### 第五阶段：功能增强与优化
- 性能优化
- 用户体验优化
- 高级功能开发

### 第六阶段：测试与文档
- 全面测试
- 文档编写
- 部署准备

**预计开发时间：6-7 周**

## 功能特性（计划）

### 核心功能
- [x] 多 Master 节点支持
- [x] 多 Slave 节点支持
- [x] Relay 节点支持（含队列缓冲功能）
- [x] Slave 队列管理
- [x] Master 待响应请求跟踪
- [x] 延迟模拟
- [ ] 节点可视化 GUI（开发中）
- [ ] REST API 接口（计划）
- [ ] WebSocket 实时通信（计划）

### 可视化功能
- [ ] 节点可视化 GUI（Fyne）
  - [ ] 统一的 Node 基类架构
  - [ ] 网格布局显示 Master/Relay/Slave 节点
  - [ ] 实时显示节点 ID 和队列长度
  - [ ] 每个 cycle 实时刷新
  - [ ] 窗口控制按钮（暂停/继续/重置）
  - [ ] Headless 模式支持
- [ ] Web 可视化界面（计划）
  - [ ] 延迟对比图表
  - [ ] 吞吐量统计图表
  - [ ] 队列长度趋势图
  - [ ] 延迟分布直方图
  - [ ] Master-Slave-Relay 拓扑图（实时动画）
  - [ ] 系统状态仪表板

### 配置功能
- [ ] 动态配置 Master 数量
- [ ] 配置各 Master 延迟
- [ ] 配置 Slave 处理速率
- [ ] 配置请求生成模式
- [ ] 配置预设管理

## 开发环境要求

### 后端（必需）
- Go 1.21+
- Go 开发工具（GoLand/VS Code）

### GUI 可视化（必需）
- Fyne 框架（通过 `go get fyne.io/fyne/v2` 安装）
- 支持图形界面的操作系统（Linux/Windows/macOS）

### Web 前端（计划）
- Node.js 18+
- npm/yarn
- 现代浏览器（Chrome/Firefox/Edge）

## 快速开始

### 开发环境设置

**安装依赖**:
```bash
go mod download
```

**运行模拟器（Headless 模式）**:
```bash
go run *.go
# 或使用测试
go test -v
```

**运行可视化模式**:
```bash
# 可视化模式将在实现后提供
go run *.go -visual
```

### Docker 部署（计划）
```bash
docker-compose up -d
```

## API 文档

API 文档将在开发完成后提供。计划使用 Swagger/OpenAPI 格式。

## 贡献指南

开发计划完成前，暂不接受外部贡献。完成后的贡献指南将包含：
- 代码规范
- Git 工作流
- 测试要求
- 提交格式

## 许可证

待定

## 联系方式

如有问题或建议，请通过 Issue 提交。

---

**注意**: 本项目正在重新开发中，当前版本为规划阶段。实际功能将在开发完成后提供。
