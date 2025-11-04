# Master-Slave 通信流控模拟系统

一个基于 Go 语言和前后端分离架构的通信流控模拟系统，用于研究 Master-Slave 架构中的通信延迟、队列管理和系统性能。

## 项目状态

✅ **核心功能完成** - 项目已完成核心模拟功能，并实现了基于 Cytoscape.js 的 Web 可视化界面。

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
  - Web 可视化界面（使用 Cytoscape.js）
  - 实时显示节点拓扑、队列长度、延迟信息
  - 每个 cycle 实时刷新显示
  - 支持 headless 模式（无可视化运行）
- **可视化控制**: Web 界面提供暂停/继续/重置控制，支持参数调整

## 系统架构

### 架构概览

系统采用前后端分离架构，通过 REST API 进行通信：

```
┌─────────────────┐         HTTP REST API        ┌──────────────────┐
│                 │ ◄──────────────────────────► │                  │
│  Web 前端       │    GET /api/frame            │  Go 后端服务     │
│  (Cytoscape.js) │    POST /api/control         │  (模拟器 + HTTP) │
│                 │                               │                  │
│  - 拓扑可视化    │                               │  - 模拟器核心    │
│  - 实时数据展示  │                               │  - Web 服务器    │
│  - 控制面板     │                               │  - API 端点      │
└─────────────────┘                               └──────────────────┘
```

### 数据流

1. **模拟器运行** (`simulator.go`)
   - 在每个 cycle 中执行模拟逻辑
   - 调用 `visualizer.PublishFrame()` 发布帧数据
   - 调用 `visualizer.NextCommand()` 检查控制命令

2. **可视化器桥接** (`web_visualizer.go`)
   - 实现 `Visualizer` 接口
   - 将帧数据转发到 `WebServer`
   - 从 `WebServer` 获取控制命令

3. **Web 服务器** (`web_server.go`)
   - 维护最新的帧数据和统计信息（线程安全）
   - 处理 HTTP 请求：
     - `GET /api/frame` - 返回最新帧数据
     - `GET /api/stats` - 返回统计信息
     - `POST /api/control` - 接收控制命令（pause/resume/reset）
   - 通过 channel 队列传递控制命令

4. **前端页面** (`web/static/index.html`)
   - 每 500ms 轮询 `GET /api/frame` 获取最新数据
   - 首次加载时创建拓扑图（Master 左，Relay 中，Slave 右）
   - 后续更新只更新节点数据，不重新布局
   - 通过 `POST /api/control` 发送控制命令

### 控制流程

```
前端点击暂停/继续/重置
    ↓
POST /api/control
    ↓
WebServer.handleControl() 验证并放入命令队列
    ↓
WebVisualizer.NextCommand() 从队列取出
    ↓
Simulator.Run() 检查命令并执行
    ↓
暂停: 设置 isPaused = true，循环等待
继续: 设置 isPaused = false，继续执行
重置: 调用 reset() 重新初始化模拟器
```

### 技术栈

#### 后端
- **语言**: Go 1.21+
- **Web 框架**: net/http（标准库）
- **并发模型**: Goroutine + Channel
- **数据同步**: sync.RWMutex（保护共享状态）

#### 前端
- **可视化库**: Cytoscape.js（通过 CDN 加载）
- **技术**: 原生 JavaScript（无需构建工具）
- **通信方式**: REST API 轮询（500ms 间隔）
- **布局算法**: 自定义固定布局（Master 左、Relay 中、Slave 右）

#### 部署
- **容器化**: Docker + Docker Compose（计划中）

## 项目结构

```
flow_control_sim/
├── node.go           # Node 基类定义（统一节点架构）
├── visualization.go  # 可视化接口和数据模型
├── web_visualizer.go # Web 可视化器实现
├── web_server.go     # Web 服务器和 REST API
├── master.go         # Master 节点（嵌入 Node）
├── slave.go          # Slave 节点（嵌入 Node）
├── relay.go          # Relay 节点（嵌入 Node，支持队列缓冲）
├── simulator.go      # 模拟器主循环
├── channel.go        # 通信信道
├── models.go         # 数据模型定义
├── stats.go          # 统计功能
├── main.go           # 程序入口
├── web/
│   └── static/
│       └── index.html # Cytoscape.js 前端页面
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
- [x] REST API 接口
- [x] Web 可视化界面

### 可视化功能
- [x] Web 可视化界面（Cytoscape.js）
  - [x] 统一的 Node 基类架构
  - [x] 拓扑图显示 Master/Relay/Slave 节点
  - [x] 实时显示节点 ID、类型、队列长度
  - [x] 每个 cycle 实时刷新（500ms 轮询）
  - [x] 控制按钮（暂停/继续/重置）
  - [x] 参数配置表单（重置时生效）
  - [x] 统计信息面板（全局、Master、Slave 统计）
  - [x] Headless 模式支持

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

### Web 可视化（可选）
- 现代浏览器（Chrome/Firefox/Edge/Safari）
- 无需额外安装，前端页面通过 CDN 加载 Cytoscape.js

## 快速开始

### 开发环境设置

**安装依赖**:
```bash
go mod download
```

**运行模拟器（Headless 模式）**:
```bash
go run *.go -headless
# 或使用测试
go test -v
```

**运行 Web 可视化模式**:
```bash
# 启动模拟器（默认使用 Web 可视化）
go run *.go

# 模拟器启动后，Web 服务器会在 http://127.0.0.1:8080 启动
# 在浏览器中打开 http://127.0.0.1:8080 即可查看可视化界面
```

**Web 界面功能**:
- 实时拓扑图：显示 Master、Slave、Relay 节点及其连接关系
- 队列信息：鼠标悬停节点查看队列详情
- 控制按钮：暂停、继续、重置模拟
- 参数配置：在配置面板调整参数，重置后生效
- 统计信息：实时显示全局、Master、Slave 统计信息

### Docker 部署（计划）
```bash
docker-compose up -d
```

## API 文档

### REST API 端点

所有 API 端点都返回 JSON 格式数据，错误时返回相应的 HTTP 状态码。

#### GET /api/frame

获取当前模拟帧数据，包含拓扑信息和统计信息。

**请求**: `GET http://127.0.0.1:8080/api/frame`

**响应** (200 OK):
```json
{
  "cycle": 100,
  "nodes": [
    {
      "id": 0,
      "type": "master",
      "label": "Master 0",
      "queues": [
        {"name": "pending", "length": 5, "capacity": -1}
      ],
      "payload": {
        "totalRequests": 100,
        "completedRequests": 95,
        "avgDelay": 12.5,
        "maxDelay": 25,
        "minDelay": 5
      }
    },
    {
      "id": 3,
      "type": "relay",
      "label": "Relay 0",
      "queues": [
        {"name": "queue", "length": 2, "capacity": -1}
      ]
    },
    {
      "id": 4,
      "type": "slave",
      "label": "Slave 0",
      "queues": [
        {"name": "request", "length": 3, "capacity": -1}
      ],
      "payload": {
        "totalProcessed": 50,
        "maxQueue": 10,
        "avgQueue": 5.2
      }
    }
  ],
  "edges": [
    {
      "source": 0,
      "target": 3,
      "label": "request",
      "latency": 2
    },
    {
      "source": 3,
      "target": 0,
      "label": "response",
      "latency": 2
    }
  ],
  "inFlightCount": 10,
  "stats": {
    "Global": {
      "TotalRequests": 300,
      "Completed": 285,
      "CompletionRate": 95.0,
      "AvgEndToEndDelay": 12.3,
      "MaxDelay": 25,
      "MinDelay": 5
    },
    "PerMaster": [
      {
        "TotalRequests": 100,
        "CompletedRequests": 95,
        "AvgDelay": 12.5,
        "MaxDelay": 25,
        "MinDelay": 5
      }
    ],
    "PerSlave": [
      {
        "TotalProcessed": 50,
        "MaxQueueLength": 10,
        "AvgQueueLength": 5.2
      }
    ]
  }
}
```

**错误响应**:
- `404 Not Found`: 模拟尚未开始或没有可用数据

**使用说明**:
- 前端每 500ms 轮询此端点获取最新数据
- 首次调用时创建拓扑图，后续调用只更新节点数据

#### GET /api/stats

获取当前统计信息（与 frame 中的 stats 字段相同）。

**请求**: `GET http://127.0.0.1:8080/api/stats`

**响应**: 与 `/api/frame` 中的 `stats` 字段相同

**使用说明**:
- 用于单独获取统计信息，无需完整帧数据

#### POST /api/control

发送控制指令（暂停、继续、重置）。

**请求**: `POST http://127.0.0.1:8080/api/control`

**请求头**:
```
Content-Type: application/json
```

**请求体**:
```json
{
  "type": "pause" | "resume" | "reset",
  "config": {
    "NumMasters": 3,
    "NumSlaves": 2,
    "NumRelays": 1,
    "TotalCycles": 1000,
    "MasterRelayLatency": 2,
    "RelayMasterLatency": 2,
    "RelaySlaveLatency": 1,
    "SlaveRelayLatency": 1,
    "SlaveProcessRate": 2,
    "RequestRate": 0.3,
    "SlaveWeights": [1, 1]
  }
}
```

**字段说明**:
- `type` (必需): 控制指令类型
  - `"pause"`: 暂停模拟
  - `"resume"`: 继续模拟
  - `"reset"`: 重置模拟（可带新配置）
- `config` (可选): 仅当 `type` 为 `"reset"` 时有效，用于更新模拟配置

**响应**:
- `202 Accepted`: 命令已接受
- `400 Bad Request`: 请求格式错误或配置验证失败
- `405 Method Not Allowed`: 请求方法不正确
- `503 Service Unavailable`: 命令队列已满

**配置验证规则**:
- `NumMasters` 和 `NumSlaves` 必须为正整数
- `TotalCycles` 必须为正整数
- `RequestRate` 必须在 0 到 1 之间
- `SlaveWeights` 数组长度必须等于 `NumSlaves`

**使用说明**:
- 命令通过 channel 队列传递，模拟器在每个 cycle 检查命令
- 重置操作会重新初始化模拟器，使用新配置（如果提供）

### 静态文件服务

**GET /** - 返回 `web/static/index.html` 前端页面

所有对 `/` 路径的请求都会返回前端页面，前端通过相对路径调用 API 端点。

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

**项目状态**: 核心功能已完成，Web 可视化界面已实现并可用。
