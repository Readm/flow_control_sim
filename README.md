# Flow Simulation

高性能网络流量模拟器，用于模拟和分析片上网络（NoC）的数据包传输行为。

## 特性

- **周期精确模拟**: 精确模拟每个时钟周期的网络行为。
- **灵活拓扑**: 支持 Ring、Mesh 等多种网络拓扑。
- **同步模型**: 采用 `AheadPort` 进行高效的异步同步调度，保证确定的执行顺序。
- **高性能**: 优化的 Go 语言并发模型，支持并行执行节点更新。
- **可视化**: 提供 Web 界面实时查看网络状态和数据流变化。

## 快速开始

### 安装

```bash
go get github.com/Readm/flow_sim
```

### 基本使用

目前的 API 采用 `Network` 管理所有节点和链路。以下是一个简单的示例：

```go
import (
    "github.com/Readm/flow_sim/internal/core/network"
    "github.com/Readm/flow_sim/internal/core/node"
)

// 创建网络
net := network.New()

// 添加节点 (使用 NodeHandle 注册)
handle0 := &network.NodeHandle{
    Node: node.NewWorkerNode(0, ...),
}
net.AddNode(handle0)

// 连接节点 (自动创建 AheadPort 链路)
// 连接 node0 的 out_port 0 到 node1 的 in_port 0
net.Connect(0, 0, 1, 0) // srcNode, srcPort, dstNode, dstPort

// 运行模拟
net.AdvanceTo(1000) // 运行至 1000 个周期
```

### 运行测试

```bash
# 运行所有测试
go test ./...

# 运行性能测试
go test -bench=. ./internal/benchmarks/...

# 运行核心扩展性基准测试（自定义节点数）
# -bench: 指定测试函数
# -run=^$: 跳过普通单元测试
# -args -bench_nodes=N: 指定网络节点数量（默认50）
go test -bench=BenchmarkRingCoreScaling -run=^$ -v ./internal/benchmarks -args -bench_nodes=100

# 运行 ChampSim 64-CPU 并行扩展性基准测试
# 测试不同物理核心数下的并行效率（1/2/4/8/16核）
go test -bench=Benchmark_ChampSim_64CPU -run=^$ -v ./internal/benchmarks -benchtime=1x -timeout=300s
```

性能追踪网址（仅2核）：

https://readm.github.io/flow_sim/dev/bench/

## 项目结构

```
flow_sim/
├── internal/
│   ├── benchmarks/     # 统一性能基准测试
│   ├── core/
│   │   ├── network/        # 网络拓扑管理与调度中心
│   │   ├── node/           # 节点（Router, Processor）实现
│   │   ├── link/           # 链路逻辑处理
│   │   ├── ahead_port/     # 核心异步同步端口实现 (InPort, OutPort)
│   │   ├── queue/          # 输入/输出队列实现
│   │   └── capability/     # Cache 一致性、目录等扩展功能
│   └── dataflow/           # 数据包 (Packet)、事务 (Transaction) 定义
├── configs/                # JSON 拓扑配置文件
├── web/                    # 基于 React 的 Web 可视化前端
├── scripts/                # 自动化测试与分析脚本
└── docs/                   # 结构化文档目录
```

## 性能基准测试

### ChampSim 64-CPU 并行扩展性测试

该测试模拟一个包含 64 个 CPU 核心的完整 ChampSim 系统，评估框架在不同物理核心数下的并行效率。

#### 系统配置

- **64 个 CPU 节点**：每个运行 O3 流水线模拟（~400K CPU cycles/cycle）
- **32 个 L2 Cache**：每 2 个 CPU 共享 1 个 L2
- **8 个 L3 Cache**：每 4 个 L2 共享 1 个 L3
- **16 个 Ring Router**：连接 L3 和 Memory Controller 的 bufferless 双向环形网络
- **8 个 Memory Controller + 8 个 DRAM**
- **总计 136 个节点**，模拟 2000 个 cycles

#### 运行基准测试

```bash
# 运行完整的并行扩展性测试（1/2/4/8/16核）
go test -bench=Benchmark_ChampSim_64CPU -run=^$ -v ./internal/benchmarks -benchtime=1x -timeout=300s

# 运行特定核心数的测试
go test -bench=Benchmark_ChampSim_64CPU/Cores_8 -run=^$ -v ./internal/benchmarks -benchtime=1x -timeout=300s
```

#### 性能结果（参考）

测试平台：AMD Ryzen 7 8745HS (16 物理核心)

| 核心数 | 实际 CPU 周期 | 加速比 | 并行效率 |
|--------|---------------|--------|----------|
| 1 核   | 7.9B          | 1.00x  | 100.0%   |
| 2 核   | 3.8B          | 2.08x  | 104.0%   |
| 4 核   | 2.2B          | 3.64x  | 91.0%    |
| 8 核   | 1.5B          | 5.34x  | 66.8%    |
| 16 核  | 0.98B         | 8.08x  | 50.5%    |

#### Profiling 分析

测试自动输出详细的性能 profiling 数据：

1. **同步阻塞 Profile**：显示哪些节点之间存在 WaitDone/Ready 阻塞
2. **节点执行时间 Profile**：显示每个节点的平均处理时间
3. **三阶段时间 Profile**：分析 Receive/Process/Send 三阶段的时间分布

关键发现：
- **Receive 阶段**占 92%+ 时间（同步等待上游）
- **Ring Router 的 Send 阶段**在单核时是主要瓶颈
- 多核并行执行显著减少同步等待时间

#### 优化建议

如需进一步提升并行效率：
1. 减少同步依赖深度（缩短关键路径）
2. 使用 bufferless link 减少 Ready 等待
3. 增加 pipeline 深度允许更多 tick-ahead 优化

## 开发工作流 (Developer Workflows)

为了兼顾使用的便捷性和开发的灵活性，本项目分为两种开发模式。

### 1. 普通开发者 (后端/算法)
你的关注点在 Go 语言核心逻辑、算法或后端架构。
- **不需要** 安装 Node.js / npm。
- **不需要** 初始化 `web_dev` 子模块。
- **前端界面**: 仓库自带编译好的静态资源 (`web/static`)，开箱即用。
- **运行**: 直接 `go build` 和 `./server` 即可。

### 2. 前端开发者 (Vue/Web)
你需要修改 Web 界面或可视化逻辑。
1. **初始化子模块**:
   ```bash
   git submodule update --init --recursive
   ```
2. **进入开发目录**:
   ```bash
   cd web_dev
   npm install
   ```
3. **开发与调试**:
   - 修改 `web_dev` 下的源码。
   - 运行开发服务器进行调试 (支持热重载):
     ```bash
     npm run serve
     ```
4. **构建与同步**:
   开发完成后，运行构建脚本将资源同步到主仓库：
   ```bash
   ./scripts/build_frontend.sh
   ```
5. **提交变更**:
   - 在 `web_dev` 中提交源码变更。
   - 在主仓库中提交 updated `web/static` 资源。

### 3. API 契约与代码生成 (API Synchronization)

本项目采用 Schema-First 策略，后端 (`internal/`) 和前端 (`web_dev/src/`) 的类型定义均由 `web/openapi.yaml` 自动生成。

| 角色 | 关注脚本 | 调用时机 | 作用 |
|------|----------|----------|------|
| **后端开发** | `generate_go_types.sh` | 修改 `openapi.yaml` 后 | 更新 Go 结构体 (`types.gen.go`)，确保 impl 与 spec 一致。Git Hook 会自动触发。 |
| **前端开发** | `generate_ts_types.sh` | 后端更新 API 后 | 更新 TS 类型 (`api.ts`)，获得最新的字段提示和类型检查。需手动运行。 |

> **提示**: 配置 `git config core.hooksPath .githooks` 后，提交时会自动检查并生成代码。



- [架构概览](docs/architecture/arch.md)
- [AheadPort 设计详解](docs/design/ahead_port.md)
- [Port API 重构说明](docs/design/port_api_redesign.md)
- [性能分析报告](docs/dev/performance_analysis.md)

## 共享指南 (Shared Guidelines)

为了保持团队在 OpenAPI 变更上的高效协作，我们采用了 **"Schema-First" + "CI 强制一致性"** 的策略。

### 1. 核心原则
- **Golden Source**: `web/openapi.yaml` 是 API 定义的唯一真理源。
- **自动同步**: Go 类型 (`types.gen.go`) 和 TS 类型 (`api.ts`) 必须与 YAML 保持一致。
- **CI 守门**: 如果生成的代码与 YAML 不一致，CI 会直接失败。

### 2. 推荐工作流 (Git Hooks)
为了避免提交后被 CI 打回，建议配置本地 Git Hooks：

```bash
# 只需运行一次
git config core.hooksPath .githooks
```

配置后，每次 `git commit` 时会自动检查并重新生成代码（如果 `openapi.yaml` 有变动）。

### 3. 如何手动生成
如果不使用 Git Hooks，请在修改 `openapi.yaml` 后手动运行：

```bash
./scripts/generate_go_types.sh
./scripts/generate_ts_types.sh
```

## License

Copyright (c) 2025
