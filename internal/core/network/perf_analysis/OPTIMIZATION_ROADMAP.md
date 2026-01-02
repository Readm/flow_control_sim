# Network Performance Optimization Roadmap

**Last Updated**: 2025-12-05
**Current Status**: 64 nodes @ 15.17ms (7.28x baseline)
**Target**: 64 nodes @ <8.5ms (<4x baseline)
**Gap**: Need ~80% improvement

## 已完成优化

###  Queue同步化 (2025-12-05)
- **改动**: Node queue processing从并发改为串行
- **实现**: Build tags (`node_queues.go` vs `node_queues_async.go`)
- **提升**: 29.93ms → 15.17ms (**49% improvement**)
- **原因**: Queue数量少(2-4个/node)，goroutine开销大于并行收益

---

## 优化方案树

```
优化方案
│
├───  Level 1: 低风险快速优化 (预计总提升30-50%)
│    │
│    ├─── 1.1 减少Link并发粒度 ⭐⭐⭐
│    │    ├─ 问题: Link.ProcessPackets中每个Link创建goroutine检查Ready
│    │    ├─ 当前: 64节点 → ~128个Link → 每个Link一个goroutine
│    │    ├─ 方案: Link也改为同步串行处理
│    │    ├─ 代码: 修改Link.ProcessPackets，移除goroutine
│    │    ├─ 风险: 低
│    │    ├─ 工作量: 1-2天
│    │    └─ 预期: 20-30%提升
│    │
│    ├─── 1.2 对象池化 (sync.Pool) ⭐⭐
│    │    ├─ 问题: 频繁分配packet切片、错误channel
│    │    ├─ 方案A: Packet切片池
│    │    │   └─ var packetPool = sync.Pool{New: func() { return make([]Packet, 0, 64) }}
│    │    ├─ 方案B: Error channel池
│    │    │   └─ var errChPool = sync.Pool{New: func() { return make(chan error, 16) }}
│    │    ├─ 风险: 低
│    │    ├─ 工作量: 2-3天
│    │    └─ 预期: 5-10%提升
│    │
│    ├─── 1.3 批量同步 (Batch Synchronization) ⭐
│    │    ├─ 问题: 每个cycle都WaitGroup.Wait()
│    │    ├─ 方案: 每K个cycle才同步一次
│    │    ├─ 实现: for batch := 0; batch < N/K; batch++ { ... }
│    │    ├─ 风险: 中等（需要验证正确性）
│    │    ├─ 工作量: 3-5天
│    │    └─ 预期: 10-15%提升
│    │
│    └─── 1.4 优化Cond使用 ⭐
│         ├─ 问题: Link/Queue中sync.Cond可能有开销
│         ├─ 方案: 替换为atomic + 轮询（带Gosched）
│         ├─ 风险: 低
│         ├─ 工作量: 2-3天
│         └─ 预期: 5-10%提升
│
├───  Level 2: 中等风险架构优化 (预计总提升50-100%)
│    │
│    ├─── 2.1 Worker Pool模式 ⭐⭐⭐⭐
│    │    ├─ 问题: Goroutine数量 >> CPU核心数
│    │    │   └─ 64节点: ~320 goroutines vs 16 cores
│    │    ├─ 方案: 固定worker数 = CPU核心数
│    │    │   ├─ type Task struct { Component Ticker; Cycle int }
│    │    │   ├─ workers := make([]Worker, runtime.NumCPU())
│    │    │   └─ taskQueue := make(chan Task, 1000)
│    │    ├─ 优势:
│    │    │   ├─ 消除scheduler lock contention (185ms → ~0)
│    │    │   ├─ Goroutine数量恒定
│    │    │   └─ 更好的CPU亲和性
│    │    ├─ 劣势:
│    │    │   ├─ 需要重构Network.Advance
│    │    │   ├─ 需要任务调度器
│    │    │   └─ 改变执行语义
│    │    ├─ 风险: 中等
│    │    ├─ 工作量: 2-3周
│    │    └─ 预期: 2-3x提升 (可能达到目标！)
│    │
│    ├─── 2.2 分区执行 (Partitioned Execution) ⭐⭐⭐
│    │    ├─ 问题: 全局同步开销大
│    │    ├─ 方案: 将网络划分为P个分区，每个分区独立运行
│    │    │   ├─ type Partition struct { Nodes, Links []Component }
│    │    │   ├─ 分区内: 无需同步，串行执行
│    │    │   ├─ 分区间: 仅在packet跨分区时同步
│    │    │   └─ 每分区绑定一个OS线程 (runtime.LockOSThread)
│    │    ├─ 优势:
│    │    │   ├─ 减少全局同步频率
│    │    │   ├─ 提升缓存局部性
│    │    │   └─ 更好的NUMA性能
│    │    ├─ 劣势:
│    │    │   ├─ 需要拓扑分析算法
│    │    │   ├─ 跨分区通信复杂
│    │    │   └─ 不平衡分区影响性能
│    │    ├─ 风险: 中高
│    │    ├─ 工作量: 3-4周
│    │    └─ 预期: 1.5-2x提升
│    │
│    ├─── 2.3 流水线执行 (Pipeline Execution) ⭐⭐
│    │    ├─ 问题: Barrier同步导致所有组件等待最慢的
│    │    ├─ 方案: 不同组件处于不同cycle
│    │    │   ├─ Node[i] 在 cycle=100
│    │    │   ├─ Link[j] 在 cycle=99
│    │    │   └─ 只要满足依赖即可继续
│    │    ├─ 优势:
│    │    │   └─ 消除全局barrier
│    │    ├─ 劣势:
│    │    │   ├─ 复杂的依赖跟踪
│    │    │   ├─ 难以调试
│    │    │   └─ 语义变化大
│    │    ├─ 风险: 高
│    │    ├─ 工作量: 4-6周
│    │    └─ 预期: 1.5-2x提升
│    │
│    └─── 2.4 事件驱动模型 ⭐⭐
│         ├─ 问题: 即使没有packet，所有组件每cycle都Tick
│         ├─ 方案: 只在有事件时才执行
│         │   ├─ type Event struct { Time int; Component Ticker }
│         │   ├─ eventQueue := PriorityQueue{}
│         │   └─ 只处理active组件
│         ├─ 优势:
│         │   └─ 稀疏流量下性能好
│         ├─ 劣势:
│         │   ├─ 密集流量下开销大
│         │   └─ 改变执行模型
│         ├─ 风险: 高
│         ├─ 工作量: 4-5周
│         └─ 预期: 取决于流量密度
│
└───  Level 3: 高风险激进优化 (预计提升100-200%)
     │
     ├─── 3.1 Lock-Free数据结构 ⭐⭐⭐
     │    ├─ 问题: Mutex开销
     │    ├─ 方案A: Lock-free ring buffer
     │    │   └─ 替换Queue内部slice为ring buffer
     │    ├─ 方案B: Lock-free packet pool
     │    │   └─ 使用atomic.Pointer实现无锁分配
     │    ├─ 风险: 高（正确性难保证）
     │    ├─ 工作量: 3-4周
     │    └─ 预期: 10-20%提升
     │
     ├─── 3.2 SIMD Packet处理 ⭐⭐
     │    ├─ 问题: 逐个处理packet
     │    ├─ 方案: 批量SIMD处理packet字段
     │    │   ├─ 使用 golang.org/x/sys/cpu
     │    │   └─ AVX2指令批量更新
     │    ├─ 风险: 高
     │    ├─ 工作量: 4-6周
     │    └─ 预期: 15-25%提升（packet密集型）
     │
     ├─── 3.3 零拷贝Packet传输 ⭐⭐
     │    ├─ 问题: Packet在组件间拷贝
     │    ├─ 方案: 使用指针或索引代替拷贝
     │    │   ├─ type PacketRef = uint32
     │    │   └─ 全局packet pool + 引用计数
     │    ├─ 风险: 中高
     │    ├─ 工作量: 2-3周
     │    └─ 预期: 5-15%提升
     │
     ├─── 3.4 JIT编译拓扑 ⭐
     │    ├─ 问题: 动态拓扑的间接调用开销
     │    ├─ 方案: 为固定拓扑生成特化代码
     │    │   └─ 编译时展开所有Tick调用
     │    ├─ 风险: 极高
     │    ├─ 工作量: 6-8周
     │    └─ 预期: 20-30%提升
     │
     └─── 3.5 GPU加速 ⭐
          ├─ 问题: CPU并行度有限
          ├─ 方案: 将packet处理offload到GPU
          │   └─ 使用CUDA/OpenCL
          ├─ 风险: 极高
          ├─ 工作量: 8-12周
          └─ 预期: 取决于工作负载
```

---

## 推荐实施路径

###  Phase 1: 快速见效 (1-2周)

**目标**: 达到10ms左右 (~50%提升)

```
Step 1: Link同步化 (1.1)
  - 类似Queue同步化，风险低
  - 预期: 15.17ms → ~11ms
  - 优先级: P0

Step 2: 对象池化 (1.2)
  - 标准优化，风险低
  - 预期: 11ms → ~10ms
  - 优先级: P0
```

**验证**: 运行 `./run_analysis.sh` 确认提升

---

###  Phase 2: 架构优化 (2-4周) - 二选一

**目标**: 达到<4x基线 (<8.5ms)

#### 方案A: Worker Pool (2.1) - 激进方案 ⭐⭐⭐⭐⭐
```
优势:
  - 预期提升最大 (2-3x)
  - 10ms → ~4ms  直接达到目标
  - 解决根本问题（scheduler contention）

劣势:
  - 需要重构Network.Advance
  - 改变执行模型
  - 测试工作量大

推荐: 如果追求性能极致
```

#### 方案B: 分区执行 (2.2) - 保守方案 ⭐⭐⭐
```
优势:
  - 10ms → ~6ms
  - 保留现有架构
  - 针对特定拓扑优化

劣势:
  - 提升可能不够达标
  - 需要拓扑分析

推荐: 如果需要保持架构稳定
```

---

###  Phase 3: 精细优化 (按需)

**仅当Phase 2未达标时执行**

```
备选方案:
  - 批量同步 (1.3) - 10-15%提升
  - 零拷贝 (3.3) - 5-15%提升
  - 优化Cond (1.4) - 5-10%提升
```

---

## 决策矩阵

| 方案 | 预期提升 | 风险 | 工作量 | 推荐度 | 优先级 |
|------|---------|------|--------|--------|--------|
| **1.1 Link同步化** | 20-30% | 低 | 1-2天 | ⭐⭐⭐⭐⭐ | P0 |
| **1.2 对象池化** | 5-10% | 低 | 2-3天 | ⭐⭐⭐⭐ | P0 |
| **2.1 Worker Pool** | 2-3x | 中 | 2-3周 | ⭐⭐⭐⭐⭐ | P1 |
| 1.3 批量同步 | 10-15% | 中 | 3-5天 | ⭐⭐⭐ | P2 |
| 2.2 分区执行 | 1.5-2x | 中高 | 3-4周 | ⭐⭐⭐ | P1 |
| 3.3 零拷贝 | 5-15% | 中高 | 2-3周 | ⭐⭐ | P3 |
| 1.4 优化Cond | 5-10% | 低 | 2-3天 | ⭐⭐ | P2 |
| 3.1 Lock-free | 10-20% | 高 | 3-4周 | ⭐⭐ | P3 |
| 2.3 流水线 | 1.5-2x | 高 | 4-6周 | ⭐ | P4 |
| 3.2 SIMD | 15-25% | 高 | 4-6周 | ⭐ | P4 |

---

## 性能目标

### 当前基线 (Queue同步化后)

| Nodes | Time (ms) | Slowdown | Status |
|-------|-----------|----------|--------|
| 4     | 2.11      | 1.00x    |  |
| 8     | 2.68      | 1.27x    |  |
| 16    | 4.27      | 2.03x    |  |
| 32    | 8.75      | 4.15x    |  |
| 64    | 15.17     | 7.28x    |  |

### Phase 1目标 (Link同步化 + 对象池化)

| Nodes | Target (ms) | Slowdown | Status |
|-------|-------------|----------|--------|
| 4     | ~2.0        | 1.00x    |  |
| 8     | ~2.5        | 1.25x    |  |
| 16    | ~4.0        | 2.00x    |  |
| 32    | ~7.0        | 3.50x    |  |
| 64    | ~10.0       | 5.00x    |  |

### Phase 2目标 (Worker Pool)

| Nodes | Target (ms) | Slowdown | Status |
|-------|-------------|----------|--------|
| 4     | ~2.0        | 1.00x    |  |
| 8     | ~2.5        | 1.25x    |  |
| 16    | ~3.5        | 1.75x    |  |
| 32    | ~5.0        | 2.50x    |  |
| 64    | ~7.0        | 3.50x    |   达标！ |

---

## 实施检查清单

### 开始优化前
- [ ] 运行 `./run_analysis.sh` 建立当前基线
- [ ] 保存 `output/report.md` 为 `report_baseline.md`
- [ ] 确保所有测试通过: `go test -timeout=3s ./...`
- [ ] 提交当前代码: `git commit -am "Baseline before optimization"`

### 每个优化后
- [ ] 运行 `./run_analysis.sh` 生成新报告
- [ ] 对比前后性能数据
- [ ] 确认没有功能回归: `go test -timeout=3s ./...`
- [ ] 更新本文档的"已完成优化"部分
- [ ] 提交代码: `git commit -am "Optimization: <name>"`

### Phase完成后
- [ ] 运行完整测试套件
- [ ] 验证是否达到Phase目标
- [ ] 决定是否继续下一Phase
- [ ] 更新README.md中的性能数据

---

## 下一步行动

**立即执行**:
1. 实现 Link同步化 (1.1)
   - 文件: `internal/core/link/*.go`
   - 预计: 2天
   - 验证: `./run_analysis.sh`

2. 如果提升符合预期 (>20%)，继续对象池化 (1.2)
   - 文件: `internal/core/network/network.go`, `internal/core/queue/*.go`
   - 预计: 3天
   - 验证: `./run_analysis.sh`

3. 评估是否需要 Phase 2
   - 如果 64节点 < 10ms → 考虑Worker Pool
   - 如果 64节点 > 10ms → 必须Worker Pool

**需要讨论**:
- [ ] 是否接受Worker Pool的架构改动？
- [ ] 性能目标是否需要调整？
- [ ] 是否有特定拓扑可以针对性优化？
- [ ] 是否需要更激进的优化（Level 3）？

---

## 参考文档

- [OPTIMIZATION_PLAN.md](./OPTIMIZATION_PLAN.md) - 详细优化技术方案
- [README.md](./README.md) - 性能分析工具使用说明
- [output/report.md](./output/report.md) - 最新性能分析报告

## 变更历史

- 2025-12-05: 初始版本，记录Queue同步化优化
- 2025-12-05: 添加完整优化方案树和实施路径
