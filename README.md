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
go test -bench=. ./internal/core/network/...
```

## 项目结构

```
flow_sim/
├── internal/
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

## 文档指引

- [架构概览](docs/architecture/arch.md)
- [AheadPort 设计详解](docs/design/ahead_port.md)
- [Port API 重构说明](docs/design/port_api_redesign.md)
- [性能分析报告](docs/dev/performance_analysis.md)

## License

Copyright (c) 2025
