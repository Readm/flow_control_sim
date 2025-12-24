# GEM5 & Hierarchical Ring NoC Roadmap (16 Cores)

目标：构建基于GEM5和FlowSim的混合仿真系统。拓扑为两级互连：顶层为双向Ring (连接L3和DDR)，底层为通过L3扇出的L2集群。共16个CPU Cores。L2/L3/DDR模型也计划使用GEM5提供的仿真模型。

## P0: GEM5 Integration (Phased)

### Phase 1: Connectivity (Ping Test)
- [ ] **Go Infrastructure** (`internal/bridge/gem5`)
  - Define `Gem5Request` struct
  - Export `FlowSim_Init`, `FlowSim_Tick`, `FlowSim_RecvRequest`
  - Build `libflowsim.so`
- [ ] **C++ Shim** (`docs/guides/gem5`)
  - `flow_sim_port.cc`: Basic `recvTimingReq`
  - `gem5_api.cc`: `Gem5_Init`, `Gem5_Simulate`
- [ ] **Validation**
  - Simple TrafficGen -> FlowSim -> Print connection test

### Phase 2: Minimum Viable Bridge (MVB)
- [ ] **Synchronization**
  - C++ `TickEvent` drives Go `FlowSim_Tick`
- [ ] **Request/Response Loop**
  - Support `ReadReq`, `WriteReq` -> `ReadResp`, `WriteResp`
  - Go -> C++ `Gem5_SendResponse` callback
- [ ] **Validation**
  - Loopback test: Request -> FlowSim -> Response

### Phase 3: Full Event Support
- [ ] **Flow Control**
  - Implement `RetryEvent` for backpressure
- [ ] **Advanced Protocol**
  - Support `ReadEx`, `Upgrade`, `WritebackDirty`
- [ ] **Validation**
  - Full CPU/Cache/DDR5 system simulation

## P1: Hierarchical Topology Implementation (16 Cores)
构建 16-Core 分层拓扑。

- [ ] **Top-Level: Bidirectional Ring**
  - **Ring Stations**: 5 个节点 (4x L3 Nodes + 1x DDR Node)
  - 协议: 双向环，最短路径路由

- [ ] **Sub-Level: L3 Clusters**
  - **Structure**: 每个 L3 Node 作为 Cluster Root，下挂 4 个 L2 Nodes
  - **Interconnect**: Crossbar 或 Bus (L3 <-> 4x L2)
  - **Total Cores**: 4 Clusters * 4 L2/CPU = 16 Cores

- [ ] **Leaf-Level: CPU Injection**
  - 每个 L2 Node 连接一个 `GEM5CpuAgent`

## P2: Flow & Coherence Handling
处理跨多级互连的流量和部分一致性逻辑（如需）。

- [ ] **Routing Logic**
  - **Upstream (Request)**: CPU -> L2 (Hit?) -> L3 (Hit?) -> Ring -> DDR
  - **Downstream (Response)**: DDR -> Ring -> L3 -> L2 -> CPU
  - **Peer-to-Peer**: L2 <-> L2 (Snoop/Coherence, if modeled)

- [ ] **GEM5 Model Wrapping**
  - `WrapperL2`: 接收来自Ring/CPU的包 -> 调用 GEM5 L2 -> 输出结果
  - `WrapperL3`: 接收来自Ring/L2的包 -> 调用 GEM5 L3 -> 输出结果
  - `WrapperDDR`: 接收来自Ring的包 -> 调用 GEM5 Memory -> 输出结果

## P3: Analysis & Tuning
- [ ] **Latency Profile**: 分析跨层级访问延迟 (L2 Hit, L3 Local Hit, L3 Remote Hit, DDR Access)
- [ ] **Backpressure Verification**: 验证当 Ring 或 L3 拥塞时，对 L2/CPU 的反压机制
