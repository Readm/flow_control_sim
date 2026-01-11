# 性能基准测试结果

本目录记录重构过程中每个阶段的性能基准测试结果。

## 文件命名规范

- `baseline_phaseN.txt`: Phase N 的性能基准
- `YYYYMMDD_HHMMSS.txt`: 临时测试结果（带时间戳）

## 运行 Benchmark

```bash
# 运行并保存到指定文件
./scripts/run_benchmarks.sh --output docs/refactoring/benchmarks/baseline_phase1.txt

# 对比两次结果
./scripts/run_benchmarks.sh --baseline docs/refactoring/benchmarks/baseline_phase1.txt --compare
```

## 关键指标说明

### BenchmarkRingCoreScaling
- **测试内容**: Ring 网络拓扑的性能，测试不同 CPU 核心数下的扩展性
- **关键指标**:
  - ns/op: 每次操作的纳秒数（越小越好）
  - B/op: 每次操作的内存分配字节数（越小越好）
  - allocs/op: 每次操作的内存分配次数（越小越好）

### Benchmark_ChampSim_64CPU
- **测试内容**: 64-CPU ChampSim 系统的性能
- **关键指标**:
  - ns/op: 仿真 1000 个周期的耗时
  - B/op: 内存占用
  - allocs/op: 内存分配次数

## 性能变化阈值

根据设计文档，允许的性能变化范围：

| 指标 | 允许变化 | 说明 |
|------|---------|------|
| CPU Tick 延迟 | ±5% | 主要逻辑未改变，应保持 |
| Cache Access 延迟 | ±5% | 只是移动了目录，不应变化 |
| Packet 处理吞吐量 | ±3% | 通信机制未改变 |
| 内存分配次数 | 0% | 不应增加额外分配 |

## 分析工具

### benchstat（推荐）

安装：
```bash
go install golang.org/x/perf/cmd/benchstat@latest
```

使用：
```bash
benchstat docs/refactoring/benchmarks/baseline_phase1.txt \
          docs/refactoring/benchmarks/baseline_phase2.txt
```

输出示例：
```
name                    old time/op    new time/op    delta
RingCoreScaling/1-8       1.23ms ± 2%    1.25ms ± 3%   +1.63%
RingCoreScaling/2-8       1.45ms ± 1%    1.44ms ± 2%     ~

name                    old alloc/op   new alloc/op   delta
RingCoreScaling/1-8       234kB ± 0%     234kB ± 0%     ~
```

### pprof（CPU profiling）

生成 profile：
```bash
go test -bench=. -cpuprofile=cpu.prof ./internal/benchmarks/...
```

分析：
```bash
go tool pprof cpu.prof
# 输入 top 查看热点函数
# 输入 web 生成调用图（需要 graphviz）
```

## 历史记录

| 日期 | Phase | 文件 | 备注 |
|------|-------|------|------|
| 2026-01-09 | 0 (初始) | baseline_initial.txt | 重构前的基准 |
| | 1 | baseline_phase1.txt | OpenAPI Schema 扩展 |
| | 2 | baseline_phase2.txt | 目录复制 |
| | 3 | baseline_phase3.txt | Import 路径更新 |
| | 4 | baseline_phase4.txt | NodeHandler 实现 |
| | 5 | baseline_phase5.txt | State/Adapter 扩展 |
| | 6 | baseline_phase6.txt | Builder 扩展 |
| | 7 | baseline_phase7.txt | 集成测试 |
| | 8 | baseline_phase8.txt | 迁移现有代码 |
| | 9 | baseline_phase9.txt | 删除旧代码 |

## 性能回归处理流程

如果发现性能下降超过阈值：

1. **确认问题**
   ```bash
   benchstat baseline_prev.txt baseline_current.txt
   ```

2. **生成 CPU profile**
   ```bash
   go test -bench=<failing_bench> -cpuprofile=cpu.prof ./internal/benchmarks/...
   go tool pprof -http=:8080 cpu.prof
   ```

3. **定位热点**
   - 查看火焰图
   - 找到新增的热点函数
   - 分析是否为重构引入

4. **优化或回滚**
   - 如果可以快速优化，修复后重新测试
   - 如果问题复杂，考虑回滚该 Phase
