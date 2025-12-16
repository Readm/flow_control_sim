# Bufferless Ring Performance Analysis

Comprehensive performance analysis for the Bufferless Ring Network-on-Chip implementation.

## Quick Start

```bash
cd internal/core/node/perf_analysis
./run_bufferless_analysis.sh
```

This will:
1. Run all performance benchmarks (4-32 nodes)
2. Generate CPU, mutex, and memory profiles
3. Analyze performance characteristics
4. Create a comprehensive report

## Benchmark Suites

### 1. Scaling Performance
Tests network scaling from 4 to 32 nodes with both single-core and multi-core execution.

```bash
go test -bench=BenchmarkBufferlessRing_Scaling -benchtime=5x ../
```

**Metrics**:
- `cycles/sec`: Simulation throughput
- `ns/op`: Time per operation
- `injected/received`: Packet statistics

### 2. Throughput Tests
Measures maximum throughput with different packet injection rates.

```bash
go test -bench=BenchmarkBufferlessRing_Throughput -benchtime=5x ../
```

**Tests**:
- Inject every 1/2/3/5 cycles
- Measures delivery rate and dropped packets

### 3. Backpressure Performance
Evaluates performance under backpressure conditions (blocked worker).

```bash
go test -bench=BenchmarkBufferlessRing_Backpressure -benchtime=5x ../
```

**Scenarios**:
- Normal operation
- With backpressure (Worker1 blocked using PickHook)

### 4. Buffer Size Impact
Tests effect of different router injection buffer sizes.

```bash
go test -bench=BenchmarkBufferlessRing_BufferSize -benchtime=5x ../
```

**Buffer sizes**: 1, 2, 4, 8, 16 packets

## Output Files

All results are saved to `output/`:

- `REPORT.md` - Comprehensive analysis report
- `benchmark.txt` - Raw benchmark results
- `cpu.prof` - CPU profile for interactive analysis
- `mutex.prof` - Mutex contention profile
- `mem.prof` - Memory allocation profile
- `*_analysis.txt` - Pre-generated profile summaries

## Viewing Results

### Quick View
```bash
cat output/REPORT.md
```

### Interactive CPU Analysis
```bash
go tool pprof output/cpu.prof

# Common commands:
(pprof) top            # Show top functions by CPU time
(pprof) top -cum       # Show by cumulative time
(pprof) list Tick      # Show source for Tick function
(pprof) web            # Generate call graph (requires graphviz)
```

### Interactive Mutex Analysis
```bash
go tool pprof output/mutex.prof

# Look for:
(pprof) top            # High contention functions
```

### Memory Analysis
```bash
go tool pprof -alloc_space output/mem.prof

# Look for:
(pprof) top            # Allocation hotspots
```

## Performance Targets

### Good Performance ✅
- **Single-core**: > 50k cycles/sec
- **Multi-core**: > 20k cycles/sec (varies with node count)
- **Delivery rate**: > 95%
- **Parallel efficiency**: > 70%

### Warning Signs ⚠️
- **Single-core**: < 30k cycles/sec
- **Multi-core**: < 10k cycles/sec
- **Delivery rate**: < 90%
- **High mutex contention**: > 10% of total time

### Performance Issues ❌
- **Throughput**: < 10k cycles/sec
- **Delivery rate**: < 80%
- **Excessive allocations**: High GC overhead
- **Lock contention**: Frequent waiting on mutexes

## Common Bottlenecks

### CPU Hotspots
1. **BufferlessRingRouter.Tick** - Main processing loop
2. **Queue.Pick/Inject** - Packet movement
3. **Link.Tick** - Inter-router communication
4. **runtime.schedule** - Too many goroutines (tune GOMAXPROCS)

### Mutex Contention
1. **Queue locks** - High-frequency packet transfers
2. **sync.Cond** - Ready signal coordination
3. **Node processHook** - Custom processing logic

### Memory Allocation
1. **Packet cloning** - Use object pools
2. **Slice growth** - Pre-allocate buffers
3. **Interface conversions** - Reduce indirection

## Optimization Tips

### 1. Reduce Lock Contention
```go
// Use EnableAlwaysReady() to skip ready checks
queue.EnableAlwaysReady()
```

### 2. Tune GOMAXPROCS
```bash
# Test different values
GOMAXPROCS=1 go test -bench=...
GOMAXPROCS=4 go test -bench=...
```

### 3. Profile-Guided Optimization
```bash
# 1. Generate profile
go test -bench=... -cpuprofile=cpu.prof

# 2. Identify hotspot
go tool pprof -top cpu.prof

# 3. Optimize the top function
# 4. Re-benchmark to verify improvement
```

## Customizing Tests

### Run Specific Benchmark
```bash
# Only scaling tests
go test -bench=Scaling -benchtime=10x ../

# Only throughput tests
go test -bench=Throughput ../
```

### Change Benchmark Duration
```bash
# Run 10 iterations
go test -bench=... -benchtime=10x ../

# Run for 5 seconds
go test -bench=... -benchtime=5s ../
```

### Focus on Specific Node Count
```bash
# Only 4-node tests
go test -bench='Scaling/Nodes_4' ../
```

## Example Analysis Session

```bash
$ ./run_bufferless_analysis.sh
[1/5] Running benchmarks...
✓ Benchmarks complete
[2/5] Analyzing CPU profile...
✓ CPU profile analyzed
...

$ cat output/REPORT.md
# Bufferless Ring Performance Analysis Report
...

$ go tool pprof output/cpu.prof
(pprof) top
Showing nodes accounting for 450ms, 75% of 600ms total
      flat  flat%   sum%        cum   cum%
     150ms 25.00% 25.00%      300ms 50.00%  BufferlessRingRouter.Tick
     100ms 16.67% 41.67%      150ms 25.00%  Queue.Pick
      80ms 13.33% 55.00%      120ms 20.00%  Link.Tick
...

(pprof) list BufferlessRingRouter.Tick
# Shows source code with time annotations
```

## Troubleshooting

### Benchmarks Hang
- Increase timeout: `go test -timeout=10m -bench=...`
- Reduce node count in script

### Out of Memory
- Reduce `-benchtime` iterations
- Run specific benchmarks only

### No Profile Data
- Ensure benchmarks complete successfully
- Check file permissions in `output/`

## Requirements

- Go 1.22+
- Optional: graphviz (for `pprof -web`)

```bash
# Install graphviz (for call graphs)
sudo apt-get install graphviz  # Ubuntu/Debian
brew install graphviz          # macOS
```

## Continuous Performance Monitoring

### Baseline Establishment
```bash
# Run benchmarks and save baseline
./run_bufferless_analysis.sh
cp output/benchmark.txt baseline_$(date +%Y%m%d).txt
```

### Regression Detection
```bash
# Compare with baseline
go test -bench=... -benchtime=5x ../ > current.txt
benchcmp baseline.txt current.txt
```

## Performance Analysis Workflow

1. **Run baseline** - Establish current performance
2. **Identify bottleneck** - Use CPU/mutex profiles
3. **Implement optimization** - Focus on top hotspot
4. **Re-benchmark** - Verify improvement
5. **Iterate** - Repeat until targets met

## Notes

- Benchmarks use realistic network configurations (4-32 nodes)
- Ring latency: 5 cycles (configurable)
- Queue bandwidth: 2 packets/cycle
- Tests include both normal and stress conditions (backpressure)
- All tests use bufferless flow control (no ring buffering)
