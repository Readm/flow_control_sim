# 追踪与分析指南 (Trace Analysis Guide)

本指南介绍 Flow_sim 中两种不同的追踪系统，分别用于分析仿真逻辑和程序性能：

1.  **Flow_sim 内部追踪 (Internal Trace)**：用于分析**仿真逻辑**，如数据包流动、节点状态、队列积压等。生成的 Trace 文件可在 Chrome 浏览器中查看。
2.  **Go 运行时追踪 (Go Runtime Trace)**：用于分析**程序性能**，如 Goroutine 调度、并发瓶颈、GC 暂停等。使用 `go tool trace` 查看。

---

## 第一部分：Flow_sim 内部追踪 (逻辑分析)

Flow_sim 内置了一个兼容 Chrome DevTools 的追踪系统，可视化呈现 Packet 在 Link 和 Node 间的精确流动。

### 1. 核心特性 (v2 Upgrade)

*   **Exclusive Model (独占模型)**：`Process` 事件仅代表纯计算时间。如果遇到阻塞（如等待下游 Ready），Trace 会自动切断 `Process`，插入一个红色的 `WaitReady` 事件，然后再恢复 `Process`。
*   **Compact Layout (紧凑布局)**：所有阶段（Receive, Process, Send, Wait）合并在同一行显示，极大节省屏幕空间。
*   **Color Coding (语义填色)**：
    *   **绿色 (Good)**: `Process` (正常计算)
    *   **红色 (Bad)**: `WaitReady` (拥塞/反压)
    *   **黄色**: `Receive`
    *   **橄榄色**: `Send`
*   **Metadata**: `Process` 名称包含 Cycle 号（如 `Process 105`），`WaitReady` 参数包含 Cycle 详情。

### 2. 启用追踪

追踪代码默认禁用，需通过 build tag `-tags trace` 启用。

**编译/测试命令**：
```bash
go test -v -tags trace ./internal/core/network -run TestDemoTrace
```

### 3. 生成 Trace

#### 方法 A：命令行参数 (推荐)
任何使用了 `network.New()` 的程序（包括测试），只要在 `main` 中调用了 `flag.Parse()`，都可以通过命令行参数启用追踪：

```bash
# 运行任意测试/程序，并指定输出文件
go test -tags trace ./internal/core/network -run TestConcurrentTracePerformance -args -flow_trace=/tmp/my_trace.json
```

#### 方法 B：编写测试
参考 `internal/core/network/demo_trace_test.go`：

```go
func TestTrace(t *testing.T) {
    // 1. 设置输出路径 (或者通过命令行 -flow_trace 设置)
    flag.Set("flow_trace", "/tmp/trace.json")

    // 2. 初始化网络 (会自动注入 Tracer)
    net := network.New()
    
    // ... 添加节点和链接 ...
    
    // 3. 运行仿真
    net.AdvanceTo(100)
    
    // 4. 刷新缓冲区 (必须)
    trace.FlushGlobal()
}
```

### 4. 查看与分析

1.  打开 Chrome 浏览器，访问 `chrome://tracing` (或 `ui.perfetto.dev`)。
2.  加载生成的 JSON 文件。
3.  **如何解读**：
    *   **寻找红色块 (`WaitReady`)**：这是性能杀手。红色块越长，说明反压越严重。点击它可以看是哪个 Cycle 卡住了。
    *   **检查绿色块 (`Process`)**：这是有效工作。如果绿色块很短而红色块很长，说明系统受限于下游吞吐。
    *   **Cycle 定位**：直接看 `Process` 的名字（如 `Process 50`）或下方 Args，快速定位仿真时刻。

---

## 第二部分：Go 运行时追踪 (性能分析)

当你发现仿真速度慢（Simulated Cycles Per Second 低），需要优化代码性能时，使用 Go 自带的 Trace 工具。

### 1. 生成 Trace

在运行 Benchmark 时添加 `-trace` 参数：

```bash
go test -bench="BenchmarkRingCoreScaling/Cores_16$" \
        -run=^$ \
        -benchtime=100x \
        -trace=trace_16.out \
        ./internal/core/network \
        -args -bench_nodes=20
```

这将生成一个名为 `trace_16.out` 的二进制文件。

### 2. 打开分析工具

使用 `go tool trace` 命令启动可视化界面：

```bash
go tool trace trace_16.out
```

浏览器会自动打开（通常是 `http://127.0.0.1:XXXX`）。

### 3. 关键视图解读

#### A. View Trace (时间线视图)
点击 **"View trace"**。这是最核心的视图。

*   **PROCS (逻辑处理器)**：行数等于 `GOMAXPROCS`。
*   **GOROUTINES**：显示具体的 Goroutine 执行情况。
*   **颜色含义**：
    *   **绿色**：用户代码正在运行 (User Code)。
    *   **红色**：阻塞 (Network/Channel/Select)。
    *   **蓝色**：调度器开销 (Scheduler)。
*   **分析重点 - "锯齿模式" (Sawtooth)**：
    *   在 Flow_sim 中，你通常会看到密集的计算期（绿色）跟随一段空白期（全局同步等待）。
    *   **空白宽度**：代表**同步开销**。如果一个 Core 先跑完，它必须等最慢的那个 Core 跑完，这段等待时间就是性能损失。

#### B. Synchronization blocking profile (同步阻塞分析)
点击 **"Synchronization blocking profile"**。

*   这个图告诉你是**哪行代码**导致了阻塞。
*   在 Flow_sim 中，通常会看到巨大的块指向 `runtime.chanrecv1`，这通常对应 `ComponentSync.WaitDone`。
*   如果看到 lock 竞争严重，则需要优化锁粒度。

#### C. Goroutine analysis (Goroutine 分析)
点击 **"Goroutine analysis"**。

*   选择与 Worker Node 相关的 Goroutine 类型。
*   查看 **Scheduler Wait Time**：如果很高，说明 CPU 饱和或调度竞争激烈。
*   查看 **Sync Block Time**：如果很高，说明大部分时间都在等锁或等 Channel。

### 4. 案例分析：16核瓶颈

在 16 核 Trace 中，常见的问题模式：
1.  **负载不均衡**：某些 Goroutine 的绿色条很长（处理慢），其他的很短（处理快）。
2.  **长尾效应**：Global Barrier 必须等待最后一个人。最慢的那个 Core 决定了整体速度。
3.  **结论**：如果发现大量的红色空白区，说明并行效率受限于同步机制。

---

## 总结

*   **分析 "仿真为什么跑得结果不对" 或 "某个包去哪了"** -> 使用 **Part 1 (Internal Trace)**。
*   **分析 "仿真为什么跑得这么慢" 或 "CPU 利用率上不去"** -> 使用 **Part 2 (Go Runtime Trace)**。
