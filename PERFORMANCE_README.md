# Performance Analysis Resources

This directory contains comprehensive performance analysis for the flow_sim network simulation.

## 📊 Performance Analysis Results

### Quick Summary

**Problem**: Why does performance degrade when node count exceeds CPU count?

**Answer**: Synchronization overhead dominates when `goroutines >> CPU cores`

| Nodes | Time (ms) | Slowdown | Main Bottleneck |
|-------|-----------|----------|-----------------|
| 4     | 3.95      | 1.0x     | baseline        |
| 8     | 5.17      | 1.3x     | ~acceptable     |
| 16    | 9.45      | 2.4x     | CPU count       |
| 32    | 21.24     | 5.4x     | ⚠️ 2x CPU but 5x slower! |
| 64    | 34.52     | 8.7x     | ⚠️ 4x CPU but 9x slower! |

**Root Cause**: 
- 73.93% contention on runtime scheduler lock
- 19.29% memory allocation overhead
- 15.71% goroutine scheduling overhead

## 📁 Files in This Analysis

### 1. `PERFORMANCE_ANALYSIS.md`
**Comprehensive performance analysis report**

Contains:
- Detailed benchmark results
- CPU profiling analysis (pprof)
- Mutex contention analysis
- Memory allocation analysis
- Root cause identification
- Optimization recommendations (short/medium/long term)
- Visual performance graphs

**Start here for**: Understanding the performance bottleneck

### 2. `PERFORMANCE_TOOLS.md`
**Complete guide to Go performance profiling tools**

Contains:
- Quick reference for all Go profiling commands
- Real-world examples from this project
- Common performance issues and solutions
- Best practices for benchmarking
- Advanced profiling techniques

**Start here for**: Learning how to profile Go programs

### 3. `network_performance.png`
**Visual performance analysis chart**

Shows:
- Left: Actual vs Ideal linear scaling
- Right: Per-node overhead (sync cost)
- Green line: CPU count marker (16 cores)

### 4. `network_perf_profile_test.go`
**Benchmark test suite for profiling**

Contains:
- `BenchmarkNetworkScaling`: Tests 4, 8, 16, 32, 64 nodes
- `BenchmarkNetworkScalingMultiCore`: Multi-core comparison

**Usage**:
```bash
# Run benchmarks
go test -bench=BenchmarkNetworkScaling -benchtime=5x ./internal/core/network/

# Generate CPU profile
go test -bench=BenchmarkNetworkScaling/Nodes_32 -cpuprofile=cpu.prof ./internal/core/network/

# Analyze profile
go tool pprof cpu.prof
```

## 🚀 Quick Start: Reproduce the Analysis

### Step 1: Run Benchmarks
```bash
cd /home/readm/flow_sim
go test -bench=BenchmarkNetworkScaling -run=^$ ./internal/core/network/
```

Expected output:
```
BenchmarkNetworkScaling/Nodes_4-16      3   3952345 ns/op
BenchmarkNetworkScaling/Nodes_8-16      3   5171181 ns/op
BenchmarkNetworkScaling/Nodes_16-16     3   9453042 ns/op
BenchmarkNetworkScaling/Nodes_32-16     3  21235888 ns/op
BenchmarkNetworkScaling/Nodes_64-16     3  34518009 ns/op
```

### Step 2: Generate CPU Profile
```bash
go test -bench=BenchmarkNetworkScaling/Nodes_32 -cpuprofile=cpu.prof ./internal/core/network/
```

### Step 3: Analyze Hotspots
```bash
go tool pprof -top -cum cpu.prof | head -20
```

Look for:
- High `runtime.schedule` → too many goroutines
- High `runtime.mallocgc` → memory allocation overhead
- High `sync.*` functions → lock contention

### Step 4: Generate Mutex Profile
```bash
go test -bench=BenchmarkNetworkScaling/Nodes_64 -mutexprofile=mutex.prof ./internal/core/network/
go tool pprof -top mutex.prof
```

Look for:
- `runtime._LostContendedRuntimeLock` → scheduler contention
- High contention in your code → need optimization

## 🎯 Key Findings

### 1. Over-Parallelization Problem

When nodes > CPU cores:
- 64 nodes = ~128 goroutines (nodes + links)
- 16 CPU cores → each core handles 8 goroutines
- Constant context switching
- Scheduler lock becomes serial bottleneck

### 2. Synchronization Overhead

Each cycle requires:
- ~64 `WaitGroup.Done` calls
- ~1 `WaitGroup.Wait` call
- ~128 `Cond.Wait/Broadcast` operations
- ~256 `Mutex Lock/Unlock` operations

Total: ~500 sync operations per cycle × 1000 cycles = 500,000 sync ops!

### 3. The Tipping Point

Performance is acceptable when `nodes ≤ CPU_count`:
- ✅ 4 nodes on 16 cores: Good
- ✅ 8 nodes on 16 cores: Acceptable
- ⚠️ 16 nodes on 16 cores: Noticeable overhead
- ❌ 32+ nodes on 16 cores: Severe degradation

## 💡 Optimization Recommendations

### Quick Wins (Low-hanging fruit)

1. **Reduce goroutine count**
   - Use worker pool pattern
   - Fixed # of workers = CPU cores
   - Dynamic task assignment

2. **Batch processing**
   - Process multiple cycles before sync
   - Reduce sync frequency

3. **Object pooling**
   - Use `sync.Pool` for temporary objects
   - Reuse buffers

### Architectural Changes

1. **Replace sync.Cond with atomic operations**
   - For simple state checks
   - Much faster than mutex + cond

2. **Partitioned execution**
   - Divide network into partitions
   - Each partition runs on dedicated thread
   - Only sync at partition boundaries

3. **Lock-free data structures**
   - Replace channels with ring buffers
   - Use atomic operations where possible

## 📚 Learn More

### Go Profiling Resources
- [Official Go Blog: Profiling Go Programs](https://go.dev/blog/pprof)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
- [Runtime Package Documentation](https://pkg.go.dev/runtime)

### Performance Patterns
- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide)
- [Advanced Go Concurrency Patterns](https://go.dev/talks/2013/advconc.slide)

### Tools
- `go test -bench` - Benchmarking
- `go tool pprof` - CPU/Memory/Mutex profiling
- `go tool trace` - Execution visualization
- `go tool compile -m` - Escape analysis

## 🔍 Interactive Analysis

### View CPU Profile Interactively
```bash
go tool pprof cpu.prof

# Then use these commands:
(pprof) top          # Top functions
(pprof) top -cum     # By cumulative time
(pprof) list Network.Advance  # Source code view
(pprof) web          # Visual graph (needs graphviz)
```

### Generate Visual Graph
```bash
# Requires: sudo apt install graphviz
go tool pprof -web cpu.prof
```

### Compare Before/After
```bash
# Baseline
go test -bench=. -cpuprofile=before.prof ./internal/core/network/

# After optimization
go test -bench=. -cpuprofile=after.prof ./internal/core/network/

# Compare
go tool pprof -base=before.prof after.prof
```

## ⚡ Performance Checklist

Before optimizing, always:
- [ ] Write benchmarks first
- [ ] Profile to find actual bottlenecks
- [ ] Optimize the hottest paths first
- [ ] Measure improvement
- [ ] Don't guess - profile!

Common anti-patterns to avoid:
- ❌ Creating goroutines in tight loops
- ❌ Using channels when simple variables work
- ❌ Excessive synchronization
- ❌ Not reusing buffers/objects
- ❌ Premature optimization without profiling

## 📊 Profiling Command Reference

```bash
# CPU profiling
go test -bench=. -cpuprofile=cpu.prof
go tool pprof cpu.prof

# Memory profiling
go test -bench=. -memprofile=mem.prof
go tool pprof mem.prof

# Mutex contention
go test -bench=. -mutexprofile=mutex.prof
go tool pprof mutex.prof

# Block profiling
go test -bench=. -blockprofile=block.prof
go tool pprof block.prof

# Execution trace
go test -bench=. -trace=trace.out
go tool trace trace.out

# All profiles at once
go test -bench=. -cpuprofile=cpu.prof -memprofile=mem.prof -mutexprofile=mutex.prof
```

---

**Questions?** Check `PERFORMANCE_TOOLS.md` for detailed tool usage guide.

**Need optimization help?** See recommendations in `PERFORMANCE_ANALYSIS.md`.
