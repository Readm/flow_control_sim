# Master-Slave 通信流控模拟系统 - 开发计划

## 项目概述

基于 Go 语言和前后端分离架构，重新开发 Master-Slave 通信流控模拟系统。系统将模拟多个 Master 节点向单个 Slave 节点发送请求的场景，研究通信延迟、队列管理和系统性能。

## 技术栈

### 后端（Go）
- **语言版本**: Go 1.21+
- **Web 框架**: Gin (轻量级 HTTP 框架)
- **WebSocket**: Gorilla WebSocket (实时数据传输)
- **JSON**: 标准库 encoding/json
- **日志**: logrus 或标准库 log
- **配置管理**: viper 或自定义配置结构
- **并发模型**: Goroutine + Channel

### 前端
- **框架**: React 18+ 或 Vue 3+ (推荐 React)
- **可视化库**: 
  - Chart.js 或 Recharts (统计图表)
  - D3.js (高级可视化，如拓扑图动画)
- **WebSocket 客户端**: Socket.io-client 或原生 WebSocket
- **构建工具**: Vite 或 Create React App
- **UI 组件库**: Ant Design 或 Material-UI

### 部署与开发
- **API 文档**: Swagger/OpenAPI (go-swagger)
- **容器化**: Docker + Docker Compose
- **包管理**: Go Modules + npm/yarn

## 项目架构

```
flow_control_sim/
├── backend/                    # Go 后端服务
│   ├── cmd/
│   │   └── server/            # 主程序入口
│   ├── internal/              # 内部包（不对外暴露）
│   │   ├── simulator/         # 模拟器核心逻辑
│   │   │   ├── master.go      # Master 节点实现
│   │   │   ├── slave.go       # Slave 节点实现
│   │   │   ├── channel.go     # 通信信道实现
│   │   │   └── simulator.go   # 主模拟器
│   │   ├── api/               # API 路由和处理器
│   │   │   ├── handlers.go    # HTTP 处理器
│   │   │   ├── websocket.go   # WebSocket 处理器
│   │   │   └── routes.go      # 路由定义
│   │   ├── models/            # 数据模型
│   │   │   ├── request.go     # 请求模型
│   │   │   ├── stats.go       # 统计数据模型
│   │   │   └── config.go      # 配置模型
│   │   └── service/           # 业务逻辑层
│   │       └── sim_service.go # 模拟服务封装
│   ├── pkg/                   # 可复用的公共包
│   │   └── utils/             # 工具函数
│   ├── configs/               # 配置文件
│   │   └── config.yaml        # 默认配置
│   ├── go.mod
│   ├── go.sum
│   └── main.go
├── frontend/                   # 前端应用
│   ├── public/
│   ├── src/
│   │   ├── components/        # React 组件
│   │   │   ├── Dashboard/     # 主仪表板
│   │   │   ├── Charts/        # 图表组件
│   │   │   │   ├── DelayChart.jsx
│   │   │   │   ├── ThroughputChart.jsx
│   │   │   │   ├── QueueChart.jsx
│   │   │   │   └── DistributionChart.jsx
│   │   │   ├── Topology/      # 拓扑图组件
│   │   │   │   └── TopologyView.jsx
│   │   │   ├── Controls/      # 控制面板
│   │   │   │   ├── ConfigPanel.jsx
│   │   │   │   └── ControlPanel.jsx
│   │   │   └── Statistics/    # 统计展示
│   │   │       └── StatsCard.jsx
│   │   ├── services/          # API 服务
│   │   │   ├── api.js         # REST API 客户端
│   │   │   └── websocket.js   # WebSocket 客户端
│   │   ├── store/             # 状态管理（Redux/Zustand）
│   │   │   ├── slices/
│   │   │   │   ├── simulation.js
│   │   │   │   └── config.js
│   │   │   └── store.js
│   │   ├── utils/             # 工具函数
│   │   ├── App.jsx
│   │   └── index.js
│   ├── package.json
│   └── vite.config.js (或 webpack.config.js)
├── docs/                       # 文档
│   ├── api.md                 # API 文档
│   └── architecture.md        # 架构文档
├── docker/                     # Docker 配置
│   ├── Dockerfile.backend
│   ├── Dockerfile.frontend
│   └── docker-compose.yml
├── scripts/                    # 脚本文件
│   ├── build.sh               # 构建脚本
│   └── run.sh                 # 运行脚本
├── .gitignore
└── README.md                   # 项目说明

```

## 核心模块设计

### 1. 后端模拟器核心（internal/simulator/）

#### 1.1 Master 节点 (master.go)
**职责**:
- 生成请求（支持多种模式：随机、固定间隔、泊松分布）
- 跟踪待发送请求队列
- 维护请求状态（pending, in-transit, completed）
- 计算端到端延迟统计

**关键结构**:
```go
type Master struct {
    ID              int
    PendingRequests []*Request
    CompletedCount  int
    TotalDelay      int64
    RequestRate     float64  // 请求生成速率
    Stats           *MasterStats
}

type MasterStats struct {
    TotalRequests      int
    CompletedRequests  int
    AvgDelay           float64
    MaxDelay           int
    MinDelay           int
    Throughput         float64
}
```

**主要方法**:
- `GenerateRequest(cycle int) *Request` - 根据配置生成请求
- `SendRequest(req *Request) error` - 发送请求到信道
- `ReceiveResponse(resp *Response)` - 处理响应并更新统计
- `GetStats() *MasterStats` - 获取统计信息

#### 1.2 Slave 节点 (slave.go)
**职责**:
- 维护 FIFO 请求队列
- 按固定速率处理队列中的请求
- 记录队列长度变化和处理统计
- 生成响应并发送回 Master

**关键结构**:
```go
type Slave struct {
    ID              int
    RequestQueue    []*Request
    ProcessRate     int  // 每 cycle 处理的请求数
    ProcessedCount  int
    Stats           *SlaveStats
    MaxQueueLength  int
}

type SlaveStats struct {
    TotalProcessed   int
    MaxQueueLength   int
    AvgQueueLength   float64
    QueueHistory     []int  // 历史队列长度
    Utilization      float64
}
```

**主要方法**:
- `EnqueueRequest(req *Request)` - 将请求加入队列
- `ProcessQueue(cycle int) []*Response` - 处理队列并生成响应
- `GetQueueLength() int` - 获取当前队列长度
- `GetStats() *SlaveStats` - 获取统计信息

#### 1.3 通信信道 (channel.go)
**职责**:
- 模拟固定延迟传输
- 管理在途的请求和响应
- 支持不同 Master 的独立延迟配置

**关键结构**:
```go
type Channel struct {
    MasterDelays map[int]int  // Master ID -> 延迟(cycles)
    InFlight     []*InFlightMessage
}

type InFlightMessage struct {
    Type      string  // "request" 或 "response"
    Message   interface{}
    FromID    int
    ToID      int
    ArrivalCycle int
}

type Request struct {
    ID          int64
    MasterID    int
    GeneratedAt int  // 生成 cycle
    SentAt      int  // 发送 cycle
}

type Response struct {
    RequestID   int64
    MasterID    int
    CompletedAt int
}
```

**主要方法**:
- `SendRequest(req *Request, cycle int)` - 发送请求（设置到达时间）
- `SendResponse(resp *Response, cycle int)` - 发送响应
- `GetArrivals(cycle int) []*InFlightMessage` - 获取当前 cycle 到达的消息
- `GetInFlightCount() int` - 获取在途消息数量

#### 1.4 模拟器 (simulator.go)
**职责**:
- 事件驱动的主循环（cycle-based）
- 协调所有组件交互
- 收集模拟数据和统计信息
- 支持暂停、恢复、重置操作

**关键结构**:
```go
type Simulator struct {
    Masters      []*Master
    Slave        *Slave
    Channel      *Channel
    CurrentCycle int
    TotalCycles  int
    IsRunning    bool
    IsPaused     bool
    Stats        *SimulationStats
    EventBus     chan *SimulationEvent
}

type SimulationStats struct {
    GlobalStats    *GlobalStats
    MasterStats    []*MasterStats
    SlaveStats     *SlaveStats
    CycleHistory   []*CycleSnapshot
}

type CycleSnapshot struct {
    Cycle           int
    MasterStates    []*MasterState
    SlaveQueueLen   int
    InFlightCount   int
    AvgDelay        float64
}

type SimulationEvent struct {
    Type    string  // "cycle", "stat_update", "complete"
    Data    interface{}
    Cycle   int
}
```

**主要方法**:
- `Run() error` - 运行模拟（阻塞）
- `RunAsync() chan *SimulationEvent` - 异步运行并返回事件流
- `Pause()` - 暂停模拟
- `Resume()` - 恢复模拟
- `Reset()` - 重置模拟状态
- `GetStats() *SimulationStats` - 获取当前统计
- `GetSnapshot(cycle int) *CycleSnapshot` - 获取特定 cycle 的快照

### 2. API 设计 (internal/api/)

#### 2.1 REST API 端点

**配置管理**:
- `GET /api/config` - 获取当前配置
- `POST /api/config` - 更新配置
- `GET /api/config/presets` - 获取预设配置

**模拟控制**:
- `POST /api/simulation/start` - 开始模拟
  - Request Body: `{ "config": {...} }`
  - Response: `{ "status": "started", "simulation_id": "..." }`
- `POST /api/simulation/pause` - 暂停模拟
- `POST /api/simulation/resume` - 恢复模拟
- `POST /api/simulation/stop` - 停止模拟
- `POST /api/simulation/reset` - 重置模拟
- `GET /api/simulation/status` - 获取模拟状态
  - Response: `{ "is_running": true, "is_paused": false, "current_cycle": 1000, "total_cycles": 100000 }`

**统计数据**:
- `GET /api/stats` - 获取完整统计数据
  - Response: `SimulationStats` JSON
- `GET /api/stats/global` - 获取全局统计
- `GET /api/stats/masters` - 获取所有 Master 统计
- `GET /api/stats/slave` - 获取 Slave 统计
- `GET /api/stats/history?start=0&end=1000` - 获取历史快照

#### 2.2 WebSocket API

**连接端点**: `WS /ws/simulation`

**消息格式**:
```json
// 客户端 -> 服务端
{
  "type": "subscribe",
  "events": ["cycle", "stats", "complete"]
}

{
  "type": "unsubscribe",
  "events": ["cycle"]
}

{
  "type": "control",
  "action": "pause" | "resume" | "stop" | "reset"
}

// 服务端 -> 客户端
{
  "type": "cycle",
  "data": {
    "cycle": 1000,
    "snapshot": {...}
  }
}

{
  "type": "stats",
  "data": {
    "stats": {...}
  }
}

{
  "type": "complete",
  "data": {
    "final_stats": {...}
  }
}

{
  "type": "error",
  "message": "..."
}
```

### 3. 前端组件设计

#### 3.1 主仪表板 (Dashboard)
**功能**:
- 显示整体系统状态
- 实时统计卡片（总请求数、完成率、平均延迟等）
- 系统控制按钮（开始/暂停/停止/重置）

#### 3.2 图表组件

**延迟对比图** (DelayChart):
- 显示各 Master 的平均延迟、最大延迟、最小延迟
- 支持柱状图和折线图切换

**吞吐量对比图** (ThroughputChart):
- 显示各 Master 的请求完成速率
- 实时更新

**队列趋势图** (QueueChart):
- 显示 Slave 队列长度随时间（cycle）的变化
- 面积图或折线图

**延迟分布图** (DistributionChart):
- 端到端延迟的分布直方图
- 显示 P50, P90, P99 等分位数

#### 3.3 拓扑图组件 (TopologyView)
**功能**:
- 可视化 Master-Slave 网络拓扑
- 实时显示节点状态（颜色编码：队列深度）
- 显示在途请求数量（连线粗细/动画）
- 支持拖拽调整布局

**视觉效果**:
- Master 节点：圆角矩形，颜色根据待发送请求数变化
- Slave 节点：圆形，颜色根据队列长度变化
- 连线：粗细表示在途消息数量，动画表示传输中
- 实时数字标签显示关键指标

#### 3.4 配置面板 (ConfigPanel)
**功能**:
- 设置 Master 数量
- 设置各 Master 的延迟配置
- 设置 Slave 处理速率
- 设置总模拟 cycles 数
- 选择请求生成模式（随机/固定/泊松）
- 保存/加载配置预设

#### 3.5 统计卡片 (StatsCard)
**功能**:
- 显示关键指标的大数字
- 支持多卡片布局
- 实时更新数值和趋势

## 开发阶段规划

### 第一阶段：后端核心开发（Week 1-2）

#### 1.1 项目初始化
- [ ] 创建 Go 项目结构
- [ ] 配置 go.mod 和依赖管理
- [ ] 设置 Git 仓库和 .gitignore
- [ ] 创建基础包结构

#### 1.2 核心模拟逻辑
- [ ] 实现 Request/Response 数据模型
- [ ] 实现 Master 节点
  - [ ] 请求生成逻辑（随机模式）
  - [ ] 请求状态管理
  - [ ] 统计收集
- [ ] 实现 Slave 节点
  - [ ] FIFO 队列管理
  - [ ] 固定速率处理逻辑
  - [ ] 统计收集
- [ ] 实现通信信道
  - [ ] 延迟模拟逻辑
  - [ ] 在途消息管理
- [ ] 实现主模拟器
  - [ ] Cycle-based 事件循环
  - [ ] 组件协调逻辑
  - [ ] 快照生成
  - [ ] 状态管理（运行/暂停/停止）

#### 1.3 单元测试
- [ ] Master 节点测试
- [ ] Slave 节点测试
- [ ] Channel 测试
- [ ] Simulator 集成测试

### 第二阶段：后端 API 开发（Week 2-3）

#### 2.1 HTTP API
- [ ] 设置 Gin 路由框架
- [ ] 实现配置管理端点
- [ ] 实现模拟控制端点
- [ ] 实现统计数据端点
- [ ] 添加 CORS 中间件
- [ ] 添加错误处理中间件
- [ ] 添加日志中间件

#### 2.2 WebSocket 支持
- [ ] 集成 Gorilla WebSocket
- [ ] 实现 WebSocket 处理器
- [ ] 实现事件广播机制
- [ ] 连接管理和心跳检测
- [ ] 客户端订阅/取消订阅逻辑

#### 2.3 配置管理
- [ ] 定义配置结构
- [ ] 实现配置文件加载（YAML/JSON）
- [ ] 实现配置验证
- [ ] 支持命令行参数覆盖

#### 2.4 API 文档
- [ ] 集成 Swagger/OpenAPI
- [ ] 编写 API 文档注释
- [ ] 生成 API 文档

### 第三阶段：前端基础开发（Week 3-4）

#### 3.1 项目初始化
- [ ] 创建 React/Vue 项目
- [ ] 配置构建工具（Vite/webpack）
- [ ] 配置路由（React Router）
- [ ] 集成 UI 组件库
- [ ] 配置状态管理（Redux/Zustand）

#### 3.2 API 服务层
- [ ] 实现 REST API 客户端
- [ ] 实现 WebSocket 客户端封装
- [ ] 添加错误处理和重试逻辑
- [ ] 实现请求拦截器（认证/日志）

#### 3.3 基础组件
- [ ] 创建布局组件
- [ ] 创建导航组件
- [ ] 创建加载组件
- [ ] 创建错误提示组件

#### 3.4 配置面板
- [ ] 实现配置表单组件
- [ ] 添加表单验证
- [ ] 实现配置预设管理
- [ ] 实现配置持久化（localStorage）

### 第四阶段：前端可视化开发（Week 4-5）

#### 4.1 统计图表
- [ ] 集成图表库（Chart.js/Recharts）
- [ ] 实现延迟对比图
- [ ] 实现吞吐量对比图
- [ ] 实现队列趋势图
- [ ] 实现延迟分布图
- [ ] 实现实时数据更新逻辑

#### 4.2 拓扑图可视化
- [ ] 集成 D3.js（或替代方案如 vis.js）
- [ ] 实现基础拓扑布局
- [ ] 实现节点渲染（Master/Slave）
- [ ] 实现连线渲染和动画
- [ ] 实现节点状态颜色编码
- [ ] 实现拖拽交互
- [ ] 实现实时数据绑定

#### 4.3 仪表板整合
- [ ] 实现主仪表板布局
- [ ] 集成统计卡片
- [ ] 集成控制面板
- [ ] 实现响应式布局

#### 4.4 实时更新
- [ ] 实现 WebSocket 事件处理
- [ ] 实现状态同步
- [ ] 实现图表实时刷新
- [ ] 优化性能（防抖/节流）

### 第五阶段：功能增强与优化（Week 5-6）

#### 5.1 高级功能
- [ ] 实现多种请求生成模式（固定间隔、泊松分布）
- [ ] 实现模拟速度控制（加速/减速）
- [ ] 实现数据导出功能（CSV/JSON）
- [ ] 实现历史回放功能
- [ ] 添加更多统计指标（P95, P99 等）

#### 5.2 性能优化
- [ ] 后端：优化模拟器性能（减少内存分配）
- [ ] 后端：优化 WebSocket 消息发送频率
- [ ] 前端：优化图表渲染性能（虚拟滚动）
- [ ] 前端：优化拓扑图动画性能
- [ ] 前后端：实现数据采样和聚合

#### 5.3 用户体验优化
- [ ] 添加加载状态指示
- [ ] 添加错误提示和恢复
- [ ] 优化移动端响应式布局
- [ ] 添加键盘快捷键
- [ ] 实现主题切换（明暗模式）

### 第六阶段：测试与文档（Week 6）

#### 6.1 测试
- [ ] 后端单元测试覆盖率达到 80%+
- [ ] 后端集成测试
- [ ] 前端单元测试（Jest + React Testing Library）
- [ ] 前后端集成测试
- [ ] 端到端测试（Cypress/Playwright）

#### 6.2 文档
- [ ] 编写用户手册
- [ ] 编写开发者文档
- [ ] 完善 API 文档
- [ ] 编写部署文档
- [ ] 更新 README

#### 6.3 部署准备
- [ ] 编写 Dockerfile
- [ ] 编写 docker-compose.yml
- [ ] 配置生产环境构建
- [ ] 编写部署脚本

## 技术细节

### 数据流设计

```
前端配置 → REST API → 后端配置更新
                ↓
前端启动指令 → REST API → Simulator.RunAsync()
                ↓
        生成事件流 → WebSocket → 前端实时更新
                ↓
        Simulator 内部循环
                ↓
        Master/Slave/Channel 交互
                ↓
        统计收集和快照生成
```

### 并发模型

**后端并发策略**:
- 主模拟器运行在独立的 Goroutine
- 每个 Master 和 Slave 的操作在模拟器中同步执行（保持确定性）
- WebSocket 连接管理和事件广播使用 Goroutine
- 使用 Channel 进行组件间通信

**前端并发策略**:
- WebSocket 连接在主线程
- 图表更新使用 React 状态管理
- 大数据处理使用 Web Worker（如需要）

### 数据模型

**配置模型**:
```go
type Config struct {
    NumMasters      int     `json:"num_masters" yaml:"num_masters"`
    NumSlaves       int     `json:"num_slaves" yaml:"num_slaves"`
    TotalCycles     int     `json:"total_cycles" yaml:"total_cycles"`
    MasterLatencies []int   `json:"master_latencies" yaml:"master_latencies"`
    SlaveProcessRate int    `json:"slave_process_rate" yaml:"slave_process_rate"`
    RequestRate     float64 `json:"request_rate" yaml:"request_rate"`
    RequestMode     string  `json:"request_mode" yaml:"request_mode"` // "random", "fixed", "poisson"
}
```

**统计模型**:
```go
type GlobalStats struct {
    TotalRequests      int     `json:"total_requests"`
    CompletedRequests  int     `json:"completed_requests"`
    CompletionRate     float64 `json:"completion_rate"`
    AvgEndToEndDelay   float64 `json:"avg_end_to_end_delay"`
    MaxDelay           int     `json:"max_delay"`
    MinDelay           int     `json:"min_delay"`
    SystemUtilization  float64 `json:"system_utilization"`
}
```

## 开发环境要求

### 后端
- Go 1.21 或更高版本
- 推荐 IDE: GoLand 或 VS Code + Go 插件

### 前端
- Node.js 18+ 和 npm/yarn
- 推荐 IDE: VS Code 或 WebStorm

### 工具
- Git
- Docker 和 Docker Compose（可选）
- Postman 或类似工具（API 测试）

## 开发规范

### 代码规范
- 后端：遵循 Go 官方代码规范，使用 gofmt，使用 golint 或 golangci-lint
- 前端：遵循 ESLint 配置，使用 Prettier 格式化

### Git 工作流
- 主分支：`main`
- 开发分支：`develop`
- 功能分支：`feature/功能名`
- 提交信息使用中文或英文，清晰描述变更

### 测试要求
- 所有核心逻辑必须有单元测试
- 新增功能必须包含测试用例
- 保持测试覆盖率在 80% 以上

## 风险评估与应对

### 技术风险
1. **WebSocket 连接稳定性**
   - 风险：大规模数据传输可能导致连接断开
   - 应对：实现自动重连、心跳检测、消息队列缓存

2. **前端性能问题**
   - 风险：大量实时数据更新可能导致页面卡顿
   - 应对：实现数据采样、防抖节流、虚拟渲染

3. **模拟器性能**
   - 风险：大规模模拟（10万+ cycles）可能耗时过长
   - 应对：优化算法、支持多线程（如可能）、实现进度报告

### 进度风险
- 风险：开发时间估算不准确
- 应对：每个阶段设置里程碑，及时调整计划

## 后续扩展方向

1. **功能扩展**:
   - 支持多 Slave 节点和负载均衡策略
   - 支持网络拥塞和丢包模拟
   - 支持动态配置调整（运行时修改参数）
   - 支持分布式模拟（多服务器协作）

2. **可视化扩展**:
   - 3D 拓扑图可视化
   - 热力图展示
   - 交互式时间轴回放

3. **分析能力**:
   - 性能瓶颈分析
   - 自动参数调优建议
   - 对比实验功能

## 参考资料

- Go 官方文档: https://go.dev/doc/
- Gin 框架文档: https://gin-gonic.com/docs/
- React 官方文档: https://react.dev/
- D3.js 文档: https://d3js.org/
- WebSocket 协议: RFC 6455

## 开发计划总结

本开发计划采用模块化、渐进式开发方式，分为 6 个主要阶段：
1. **后端核心**：实现模拟器核心逻辑（2 周）
2. **后端 API**：实现 REST API 和 WebSocket（1 周）
3. **前端基础**：搭建前端框架和基础功能（1 周）
4. **前端可视化**：实现图表和拓扑图（1 周）
5. **功能增强**：优化和完善功能（1 周）
6. **测试与文档**：全面测试和文档编写（1 周）

**预计总开发时间：6-7 周**

每个阶段都有明确的交付物和检查点，便于跟踪进度和及时调整。开发过程中应注重代码质量、测试覆盖率和用户体验。

