# 架构概览 (Architecture Overview)

## 核心目标
- **高性能周期精确仿真**: 提供 NoC 或多核系统的周期精确建模。
- **配置驱动**: 通过 JSON 配置文件灵活构建网络拓扑。
- **可扩展性**: 支持自定义 Node（Router, Processor）和 Link 策略。

## 分层设计

### 1. Interface 层 (API/Frontend)
- **Web Interface**: 提供实时可视化，展示包流转和队列状态。
- **CLI**: 支持批处理仿真和基准测试。

### 2. Core 调度层 (`internal/core`)
- **Network**: 核心管理器，负责根据配置初始化拓扑（Node & Link），并驱动时钟周期（`AdvanceTo`）。
- **AheadPort**: 统一的异步同步机制（位于 `internal/core/ahead_port`），处理组件间的反压和数据同步。

### 3. Entity 实体层
- **Node**: 仿真中的主动实体（如 Router, BufferlessRingRouter）。
- **Link**: 仿真中的被动实体，处理传输延迟和带宽限制。
- **Queue**: 灵活的输入/输出缓冲实现。

### 4. DataFlow 数据层 (`internal/dataflow`)
- **Packet**: 最小传输单元，携带路由、类型和 Cycle 信息。
- **Transaction**: 逻辑事务抽象，用于追踪复杂的一致性协议状态。
- **Coherence/Directory**: 位于 `internal/components`，提供缓存一致性逻辑支持。

## 执行模型

`flow_sim` 采用 **基于 Port 的分布式并行同步模型**：
1. 每个组件（Node/Link）在逻辑上是并行的（可跑在不同 Goroutine）。
2. 组件间的同步通过 `AheadPort` 进行。
3. `Network.AdvanceTo(T)` 驱动时钟推进。
4. 组件接收输入（`Receive`），处理逻辑，产生输出（`TrySend`），并更新自身状态。

## 项目文件结构

```
flow_sim/
├── internal/
│   ├── core/
│   │   ├── network/        # 拓扑构建与调度
│   │   ├── node/           # Router, Processor 实体
│   │   ├── link/           # Link 实体逻辑
│   │   ├── ahead_port/     # 核心同步端口
│   │   ├── queue/          # 缓存队列
│   │   └── capability/     # Cache/Directory 插件功能
│   └── dataflow/           # 数据格式定义 (Packet, Message, Transaction)
├── configs/                # 示例拓扑配置
├── web/                    # 可视化前端
├── tests/                  # 端到端与集成测试
└── docs/                   # 项目文档
```

## 测试策略
- **单元测试**: 针对每个核心组件（Port, Queue, Node）进行严格的时序校验。
- **集成测试**: 模拟实际拓扑（如 Ring）验证全流程数据包投递准确率。
- **性能测试**: 使用 `go test -bench` 监控仿真周期的执行吞吐量。


