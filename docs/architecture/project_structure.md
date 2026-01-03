# 项目架构

本文档以树状结构提供了 FlowSim `flow_sim` 项目架构、目录结构和关键组件的全面概述。

## 目录结构树

```text
flow_sim/
├── cmd/                            # 应用程序入口点
│   └── server/                     # 主服务器应用程序
├── docs/                           # 文档文件
├── web/                            # 前端资源和配置 (openapi.yaml)
├── scripts/                        # 实用脚本 (trace 验证等)
└── internal/                       # 私有应用程序代码
    ├── core/                       # 核心仿真引擎
    │   ├── network/                # 网络管理
    │   │   ├── Network             # 节点/链路容器
    │   │   └── stats.go            # 聚合统计
    │   ├── node/                   # 节点基础设施
    │   │   ├── Node                # 核心接口 (Tick, AdvanceTo)
    │   │   └── BaseNode            # 通用基础实现
    │   ├── link/                   # 连接管理
    │   │   ├── Link                # 链路实体 (延迟/带宽)
    │   │   ├── LinkMonitor         # 链路监控/Trace
    │   │   └── TracedInPort        # 阻塞事件追踪
    │   ├── monitor/                # 可观测性封装
    │   │   ├── Monitor             # 监控接口
    │   │   └── low_level.go        # 高精度计时 (GetCPUCycles)
    │   ├── trace/                  # Tracing 系统 (Chrome Trace)
    │   ├── queue/                  # 缓冲队列 (GenericQueue)
    │   └── ahead_port/             # 高级端口管理
    │
    ├── champsim/                   # ChampSim 集成层
    │   ├── flowsim/                # FlowSim-ChampSim 桥接
    │   │   ├── CPUNode             # CPU 模型适配
    │   │   └── DRAMNode            # 内存模型适配
    │   └── [cache, dram, trace]    # ChampSim 子模块
    │
    ├── components/                 # 高级硬件组件库
    │   ├── cache/                  # 缓存模型 (L1, L2, LLC)
    │   ├── directory/              # 一致性目录
    │   ├── coherence/              # 一致性协议 (MESI, MOESI)
    │   └── decoder/                # 地址解码器
    │
    └── dataflow/                   # 数据定义
        ├── packet/                 # 基础数据包
        ├── message/                # 协议消息
        ├── transaction/            # 事务
        └── chi/                    # CHI 协议定义
```

## 关键接口与函数

### Node 接口 (`internal/core/node`)
系统中任何活动组件的主要契约。
```go
type Node interface {
    ID() int
    Tick(cycle uint64, duration time.Duration) error
    AddInputQueue(q InputQueue) error
    AddOutputQueue(q OutputQueue) error
    // ...
}
```

### Link 逻辑 (`internal/core/link`)
处理数据的移动。
- **`Tick(cycle int, targetCycle int) error`**: 主要生命周期方法。
  1. **Receive**: 从上游拉取数据包。
  2. **Process**: 执行传输逻辑（延迟/带宽）。
  3. **Send**: 向下游推送执行单元。

### 监控
可观测性与逻辑解耦。
- **`monitor.GetCPUCycles()`**: 低开销时间戳。
- **`TraceRecorder.RegisterSource(src)`**: 启用组件 Tracing 的使用模式。
