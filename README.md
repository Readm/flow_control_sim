# Flow Simulation

高性能网络流量模拟器，用于模拟和分析片上网络（NoC）的数据包传输行为。

## 特性

- **周期精确模拟**: 精确模拟每个时钟周期的网络行为
- **灵活拓扑**: 支持Ring、Mesh等多种网络拓扑
- **缓存一致性**: 支持MESI等缓存一致性协议模拟
- **高性能**: 优化的同步并行执行模型
- **可视化**: Web界面实时查看网络状态

## 快速开始

### 安装

```bash
go get github.com/Readm/flow_sim
```

### 基本使用

```go
import "github.com/Readm/flow_sim/internal/core/network"

// 创建网络
net := network.NewNetwork()

// 添加节点和链路
node0 := net.AddNode(queue.NewInputQueue(...), queue.NewOutputQueue(...))
node1 := net.AddNode(queue.NewInputQueue(...), queue.NewOutputQueue(...))
link := net.AddLink(10, 1) // latency=10, bandwidth=1

// 连接节点
net.Connect(node0.Output(0), link.Input())
net.Connect(link.Output(), node1.Input(0))

// 运行模拟
net.Advance(1000) // 运行1000个周期
```

### 运行测试

```bash
# 运行所有测试
go test -timeout=3s ./...

# 运行性能测试
cd internal/core/network/perf_analysis
./run_analysis.sh
```

## 编译选项

### Queue处理模式

项目支持两种Queue处理模式，通过build tag控制：

```bash
# 默认: 同步串行处理（推荐，性能最优）
go build ./...
go test ./...

# 异步并发处理（仅用于对比测试）
go build -tags async ./...
go test -tags async ./...
```

**性能对比**: 默认同步模式比异步模式快50-100%。

### Debug模式

```bash
# 启用调试日志
go build -tags debug ./...
```

## 项目结构

```
flow_sim/
├── internal/core/
│   ├── network/        # 网络模拟核心
│   ├── node/           # 节点实现
│   ├── link/           # 链路实现
│   ├── queue/          # 队列实现
│   ├── packet/         # 数据包定义
│   └── cache/          # 缓存一致性
├── configs/            # 配置文件
├── web/                # Web可视化
└── tools/              # 工具脚本
```

## 性能分析

项目内置自动化性能分析工具：

```bash
cd internal/core/network/perf_analysis
./run_analysis.sh
```

生成报告包括：
- 基准测试结果
- CPU profiling
- Mutex contention分析
- 性能可视化图表

详见 [性能分析文档](internal/core/network/perf_analysis/README.md)

## 配置

测试配置遵循以下约定：
- 所有 `go test` 使用 `-timeout=3s` 避免挂起
- 节点延迟参考GEM5 O3CPU速度（2-20μs）
- Link bandwidth默认为1（用于压力测试）

## 开发

### 添加新的网络拓扑

参考 `internal/core/network/network_test.go` 中的示例。

### 添加新的缓存一致性协议

实现 `internal/core/cache/coherence.go` 中定义的接口。

## License

Copyright (c) 2025
