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
- **插件化骨架**:
  - 核心仿真由 `Node`/`Link` 骨架驱动，策略/可视化/激励通过插件装配
  - `hooks.PluginBroker` + `hooks.Registry` 统一管理插件生命周期
  - `capabilities/`, `plugins/visualization`, `plugins/incentives` 提供默认能力，可按需扩展
- **节点可视化**: 
  - 统一的 Node 基类架构，Master/Slave/Relay 继承自 Node
  - Web 可视化界面（使用 Cytoscape.js）
  - 实时显示节点拓扑、队列长度、延迟信息
  - 每个 cycle 实时刷新显示
  - 支持 headless 模式（无可视化运行）
- **可视化控制**: Web 界面提供暂停/继续/重置控制，支持参数调整
- **配置预设管理**: 支持多个预定义的网络配置，可通过命令行或 Web 界面选择
- **请求生成模式**: 支持概率生成（ProbabilityGenerator）和调度生成（ScheduleGenerator）两种模式

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
     - `GET /api/frame` - 返回最新帧数据（兼容性保留）
     - `GET /api/stats` - 返回统计信息
     - `GET /api/configs` - 返回预定义配置列表
    - `POST /api/control` - 接收控制命令（pause/run/reset）
     - `WS /ws` - WebSocket 连接端点（实时推送）
   - 通过 channel 队列传递控制命令
   - 支持 WebSocket 实时推送帧数据

4. **前端页面** (`web/static/index.html`)
   - 优先使用 WebSocket 连接获取实时数据推送（延迟 < 50ms）
   - WebSocket 连接失败时自动回退到 HTTP 轮询（500ms 间隔）
   - 首次加载时创建拓扑图（Master 左，Relay 中，Slave 右）
   - 后续更新只更新节点数据，不重新布局
   - 通过 WebSocket 或 `POST /api/control` 发送控制命令

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
- **WebSocket**: gorilla/websocket（实时数据传输）
- **并发模型**: Goroutine + Channel
- **数据同步**: sync.RWMutex（保护共享状态）

#### 前端
- **可视化库**: Cytoscape.js（通过 CDN 加载）
- **技术**: 原生 JavaScript（无需构建工具）
- **通信方式**: 
  - WebSocket 实时推送（优先，延迟 < 50ms）
  - HTTP 轮询（500ms 间隔，WebSocket 失败时自动回退）
- **布局算法**: 自定义固定布局（Master 左、Relay 中、Slave 右）

#### 部署
- **容器化**: Docker + Docker Compose（计划中）

## 项目结构

```
flow_control_sim/
├── capabilities/             # 节点能力实现（路由、流控、统计等）
├── hooks/                    # Hook Broker 与插件登记中心
├── plugins/
│   ├── incentives/           # 激励插件注册器与示例实现
│   └── visualization/        # 可视化插件注册器
├── visual/                   # Visualizer 接口与 Null 实现
├── node.go                   # Node 基类定义（统一节点架构）
├── visualization.go          # 可视化数据模型（帧、节点快照等）
├── web_visualizer.go         # Web 可视化器实现
├── web_server.go             # Web 服务器和 REST API
├── rn.go                     # RequestNode (Master) 节点实现
├── sn.go                     # SlaveNode 节点实现
├── hn.go                     # HomeNode (Relay) 节点实现
├── simulator.go              # 模拟器主循环，负责骨架与插件装配
├── channel.go                # 通信信道 (Link)
├── models.go                 # 数据模型与 Config 定义
├── stats.go                  # 统计功能
├── request_generator.go      # 请求生成器接口和实现
├── soc_configs.go            # 预定义网络配置
├── benchmark.go              # 性能基准测试
├── main.go                   # 程序入口
├── web/
│   └── static/
│       └── index.html        # Cytoscape.js 前端页面
├── go.mod                    # Go 模块定义
├── README.md                 # 项目说明
├── DEVELOPMENT_PLAN.md       # 开发计划文档
├── TEST_RESULTS.md           # 测试结果文档
└── TODO.md                   # 待办事项
```
骨架组件和插件目录遵循“骨架最小化、能力可插拔”原则，详细设计请参阅 `doc/design.md`。

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
  - [x] WebSocket 实时推送（延迟 < 50ms），失败时自动回退到 HTTP 轮询（500ms 间隔）
  - [x] 控制按钮（暂停/继续/单步执行/重置）
  - [x] 配置预设选择（从下拉菜单选择预定义网络配置）
  - [x] 参数配置表单（调整总 cycles 数等参数，重置后生效）
  - [x] 统计信息面板（全局、Master、Slave 统计）
  - [x] Headless 模式支持

### 配置功能
- [x] 配置预设管理（支持多个预定义配置，可通过命令行或 Web 界面选择）
- [x] 配置请求生成模式（支持概率生成和调度生成）
- [x] 通过配置预设动态配置 Master/Slave 数量、延迟、处理速率等参数
- [x] 插件装配配置（如 `Config.Plugins.Incentives` 声明激励插件列表）
- [x] 命令行参数支持（`-config` 指定预设配置，`-headless` 无头模式，`-benchmark` 性能测试）

## 开发环境要求

### 后端（必需）
- Go 1.21+
- Go 开发工具（GoLand/VS Code）

### Web 可视化（可选）
- 现代浏览器（Chrome/Firefox/Edge/Safari）
- 无需额外安装，前端页面通过 CDN 加载 Cytoscape.js

## 快速开始

### 开发环境设置

**首次配置或克隆项目后，需要同步依赖**:
```bash
# 推荐：同步依赖并更新 go.sum（推荐方式）
go mod tidy

# 或者仅下载依赖（不更新 go.sum）
go mod download
```

**说明**:
- `go mod tidy`: 下载依赖、更新 `go.sum` 校验和文件、清理无用依赖（推荐）
- `go mod download`: 仅下载依赖到本地缓存
- 新环境配置时，建议使用 `go mod tidy` 确保依赖完整性

**运行模拟器（Headless 模式）**:
```bash
# 使用默认配置运行
go run *.go -headless

# 使用指定预设配置运行
go run *.go -headless -config backpressure_test

# 或使用测试
go test -v
```

**运行 Web 可视化模式**:
```bash
# 启动模拟器（默认使用 Web 可视化）
go run *.go

# 使用指定预设配置启动
go run *.go -config multi_master_multi_slave

# 模拟器启动后，Web 服务器会在 http://127.0.0.1:8080 启动
# 在浏览器中打开 http://127.0.0.1:8080 即可查看可视化界面
```

**可用的预设配置**:
- `multi_master_multi_slave`: 多 Master 多 Slave 网络（3 Masters, 2 Slaves, 1 Home Node）
- `simple_single_master_slave`: 简单单 Master-Slave 网络（1 Master, 1 Slave, 1 Home Node）
- `backpressure_test`: 背压测试（高负载、慢处理，触发背压）
- `single_request_10cycle_latency`: 单请求测试（10-cycle 延迟，确定性调度）

**查看所有可用配置**:
```bash
# 通过 API 查看（需要先启动服务器）
curl http://127.0.0.1:8080/api/configs
```

**启用示例激励插件**:
- 在自定义配置初始化阶段设置 `cfg.Plugins.Incentives = []string{"random"}`
- 或在 Web 控制命令中选择 Reset 并提交包含上述字段的配置
```

**Web 界面功能**:
- 实时拓扑图：显示 Master、Slave、Relay 节点及其连接关系
- 队列信息：鼠标悬停节点查看队列详情
- 控制按钮：暂停、运行（指定 cycles）、重置模拟
- 配置选择：从下拉菜单选择预定义网络配置
- 配置信息：配置面板根据预设展示总 cycles 数，重置时自动生效
- 统计信息：实时显示全局、Master、Slave 统计信息

**运行性能测试（Benchmark）**:
```bash
# 运行性能基准测试，测试 headless 模式下的仿真性能
go run *.go -benchmark
# 或使用编译后的二进制文件
./flow_control_sim -benchmark
```

性能测试会运行多个不同规模的测试（10,000、50,000、100,000 和 1,000,000 cycles），并输出：
- 每秒可仿真的 cycle 数（cycles/sec）
- 总耗时
- 每个 cycle 的平均耗时

典型的性能指标：在 headless 模式下，仿真器每秒可运行约 **300,000-400,000 cycles**。

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
- 前端优先使用 WebSocket 实时接收数据（推荐）
- WebSocket 不可用时，前端会回退到每 500ms 轮询此端点
- 首次调用时创建拓扑图，后续调用只更新节点数据

#### GET /api/stats

获取当前统计信息（与 frame 中的 stats 字段相同）。

**请求**: `GET http://127.0.0.1:8080/api/stats`

**响应**: 与 `/api/frame` 中的 `stats` 字段相同

**使用说明**:
- 用于单独获取统计信息，无需完整帧数据

#### GET /api/configs

获取所有可用的预定义网络配置列表。

**请求**: `GET http://127.0.0.1:8080/api/configs`

**响应** (200 OK):
```json
[
  {
    "name": "multi_master_multi_slave",
    "description": "Multi-Master Multi-Slave Network (3 Masters, 2 Slaves, 1 Home Node)"
  },
  {
    "name": "simple_single_master_slave",
    "description": "Simple Single Master-Slave Network (1 Master, 1 Slave, 1 Home Node)"
  },
  {
    "name": "backpressure_test",
    "description": "Backpressure Test: High load, slow processing (3 Masters, 1 Slave, triggers backpressure)"
  },
  {
    "name": "single_request_10cycle_latency",
    "description": "Single Request Test: 1 RN, 1 HN, 1 SN, 10-cycle latency, single ReadNoSnp request"
  }
]
```

**使用说明**:
- 返回所有预定义配置的名称和描述
- 前端界面使用此端点加载配置下拉菜单
- 配置的完整参数在重置时通过 `configName` 字段指定

#### POST /api/control

发送控制指令（暂停、执行指定 cycle、重置）。

**请求**: `POST http://127.0.0.1:8080/api/control`

**请求头**:
```
Content-Type: application/json
```

**请求体**:
**请求体示例（执行指定 cycles）**:
```json
{
  "type": "run",
  "cycles": 500
}
```

**请求体示例（重置并传入配置）**:
```json
{
  "type": "reset",
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
- `type` (必需): 控制指令类型
  - `"pause"`: 暂停模拟
  - `"run"`: 执行指定数量的 cycles（支持累加请求）
  - `"reset"`: 重置模拟（可带新配置）
- `cycles` (可选): 当 `type` 为 `"run"` 时，指定需要执行的 cycles 数量（> 0）
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

### WebSocket API

#### WS /ws

建立 WebSocket 连接，用于实时接收模拟帧数据。

**连接**: `ws://127.0.0.1:8080/ws` 或 `wss://127.0.0.1:8080/ws`（HTTPS 环境）

**消息格式**:

**客户端 → 服务端**（控制命令）:
```json
{
  "type": "pause" | "run" | "reset",
  "cycles": 250,
  "configName": "backpressure_test",
  "totalCycles": 1000
}
```

**字段说明**:
- `type` (必需): 控制指令类型
  - `"pause"`: 暂停模拟
  - `"run"`: 执行指定数量的 cycles（可叠加）
  - `"reset"`: 重置模拟（可带新配置）
- `cycles` (可选): 当 `type` 为 `"run"` 时，指定 cycles 数量
- `configName` (可选): 预设配置名称，仅当 `type` 为 `"reset"` 时有效
- `totalCycles` (可选): 总模拟 cycles 数，仅当 `type` 为 `"reset"` 时有效

**服务端 → 客户端**（帧数据推送）:
```json
{
  "cycle": 100,
  "nodes": [...],
  "edges": [...],
  "stats": {...}
}
```

**特性**:
- 连接建立后立即发送最新帧数据
- 每次模拟器更新帧时自动推送给所有连接的客户端
- 支持通过 WebSocket 发送控制命令（pause/run/reset）
- 自动重连机制（连接断开后 2 秒自动重试）

**使用说明**:
- 前端优先使用 WebSocket 连接，提供更低的延迟（< 50ms）
- WebSocket 连接失败时自动回退到 HTTP 轮询
- 支持多客户端同时连接

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

**项目状态**: 核心功能已完成，Web 可视化界面已实现并可用。支持配置预设管理、多种请求生成模式、性能基准测试等功能。
