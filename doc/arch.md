# Architecture Overview

## 背景与目标
- 面向流数据的仿真器，遵循 KISS 原则，优先保持实现路径清晰。
- 通过配置驱动，支持 Web/CLI 双界面触发仿真，并基于可插拔 Hook 系统扩展一致性、协议、路由等策略。
- 文档与实现均以英文命名，方便跨团队协作；对外说明使用中文。

## Interface 层
- Web Interface：提供可视化配置与运行监控，负责将表单/请求映射为 `config` 文件，并触发 `run cycle`。
- CLI Interface：面向批处理或 CI 场景，通过 `pkg/controller` 暴露的 `SimulationController` 启动/停止仿真，命令解析层只依赖接口方便注入 Mock。
- Interface 层统一通过配置服务写入 `config`，并在运行周期中接受 Network 的状态回执。

## Core 设计

### Core/Entity 扩展说明
- 详细接口与并行调度方案参考 `doc/core_entity.md`。
- 并行性回归测试命令：`go test ./internal/core/network -run TestNetworkRunsNodesInParallel -timeout 5s`。

### Entity 子层
- `config`：解析 YAML/JSON 配置，负责校验、生成内部结构化参数，并向 Network/Plugin 暴露。
- `Network`：读取配置建立拓扑，负责调度 Node、Link 生命周期，支撑并行运行。
- `Node`：处理局部状态与逻辑，依托 Link 获取跨 cycle 数据，维护本地缓冲、统计。
- `Link`：表示拓扑连接，负责节点间同步、延迟模拟及数据完整性。

### DataFlow 子层
- `Transaction`：由若干 Packet 组成的完整事务，记录一致性、目录、协议状态。
- `Packet`：承载 payload 及路由信息，是 Transaction 的最小传输单元。
- `Message`：高层语义对象，Packet 解码后供 Plugin 或 Interface 消费。

### 执行流程
1. Interface 写入/更新 `config`。
2. Network 解析配置，实例化 Node 与 Link，建立 Hook。
3. `run cycle` 推进，Node/Link 并行更新，Transaction/Packet/Message 在 DataFlow 中流转。
4. Hook System 在拓扑生成、事务生命周期、链路调度等阶段触发插件。

## Plugin System
- Hook 类型：配置加载、拓扑生成、cycle 前/后、Transaction 建立、Packet 路由、统计汇总。
- 插件族说明：
  - Coherence：实现 MESI/MOESI。
  - Directory：精确/模糊/混合目录。
  - Protocol：CHI/CXL 协议栈。
  - FlowControl：cbusy、RTT 调度策略。
  - Routing：Transaction Log、译码、Packet Spray。
  - Incentive：激励与优先级策略。
- 配置入口：`config.plugins` 中声明启用列表及参数，Network 初始化时注入并注册 Hook。

## 参考文件结构
```
flow_sim/
├── cmd/
│   ├── web/                # Web Interface 启动与路由
│   └── cli/                # CLI Interface 命令解析
├── internal/
│   ├── config/             # 配置解析与校验
│   ├── core/
│   │   ├── network/        # Network 调度、拓扑构建
│   │   ├── node/           # Node 实现
│   │   └── link/           # Link 实现
│   └── dataflow/
│       ├── transaction/
│       ├── packet/
│       └── message/
├── plugin/
│   ├── coherence/
│   ├── directory/
│   ├── protocol/
│   ├── flowcontrol/
│   ├── routing/
│   └── incentive/
├── pkg/hook/               # Hook 定义、注册表、事件分发
├── doc/                    # 文档（含本文件、序列图等）
└── test/
    ├── fixtures/           # 配置样例、Mock
    └── integration/        # 组合测试场景脚本
```

## 单元测试策略
- `config`：使用 table-driven 测试覆盖合法/非法配置解析、插件开关、默认值。
- `Network`：通过 fake Node/Link 验证拓扑生成、并行调度、Hook 注册顺序。
- `Node`/`Link`：对状态转移、延迟模型、错误场景进行独立测试，必要时注入 Mock DataFlow。
- `DataFlow`：为 Transaction/Packet/Message 建立纯逻辑测试，断言序列化、关联关系。
- `Plugin`：为每个插件实现提供 Hook stub，验证生命周期触发、配置注入、互斥策略。
- `Hook` 框架：测试注册/注销、事件顺序、异常隔离，确保插件崩溃不影响核心循环。
- `cmd`：CLI 使用 subcommand 测试，Web Handler 使用 httptest 验证 HTTP 流程。
- 所有测试均应设置超时时间，避免仿真循环阻塞；关键路径使用 fake clock 或 deterministic scheduler。


