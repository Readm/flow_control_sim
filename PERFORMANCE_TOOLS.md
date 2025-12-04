# Go Performance Analysis Tools Guide

This guide demonstrates how to use Go's built-in profiling tools to analyze performance bottlenecks.

## Quick Start: Common Commands

### 1. Benchmark Testing
```bash
# Run basic benchmark
go test -bench=. -benchtime=10x ./package/path/

# Run specific benchmark
go test -bench=BenchmarkName -benchtime=5x ./package/path/

# Show memory allocations
go test -bench=. -benchmem ./package/path/
```

### 2. CPU Profiling
```bash
# Generate CPU profile
go test -bench=BenchmarkName -cpuprofile=cpu.prof ./package/path/

# Analyze CPU profile
go tool pprof cpu.prof

# Common pprof commands:
#   top          - Show top functions by time
#   top -cum     - Show by cumulative time
#   list FuncName - Show source code with timing
#   web          - Generate visual graph (needs graphviz)
#   exit         - Quit pprof
```

### 3. Memory Profiling
```bash
# Generate memory profile
go test -bench=BenchmarkName -memprofile=mem.prof ./package/path/

# Analyze memory profile
go tool pprof mem.prof

# Focus on allocations
go tool pprof -alloc_space mem.prof

# Focus on in-use memory
go tool pprof -inuse_space mem.prof
```

### 4. Mutex Contention Profiling
```bash
# Generate mutex profile
go test -bench=BenchmarkName -mutexprofile=mutex.prof ./package/path/

# Analyze mutex contention
go tool pprof mutex.prof
```

### 5. Execution Tracing
```bash
# Generate execution trace
go test -bench=BenchmarkName -trace=trace.out ./package/path/

# View trace in browser
go tool trace trace.out
```

### 6. Block Profiling
```bash
# Generate block profile (blocking operations)
go test -bench=BenchmarkName -blockprofile=block.prof ./package/path/

# Analyze blocking
go tool pprof block.prof
```

## Real Example: Network Performance Analysis

### Step 1: Create Benchmarks
```go
// File: network_perf_test.go
func BenchmarkNetworkScaling(b *testing.B) {
    for nodes := 4; nodes <= 64; nodes *= 2 {
        b.Run(fmt.Sprintf("Nodes_%d", nodes), func(b *testing.B) {
            // Setup network with 'nodes' nodes
            net := setupNetwork(nodes)

            b.ResetTimer()
            for i := 0; i < b.N; i++ {
                net.Advance(1000)
            }
        })
    }
}
```

### Step 2: Run Benchmarks
```bash
# Quick performance comparison
go test -bench=BenchmarkNetworkScaling -benchtime=3x ./internal/core/network/

# Output:
# BenchmarkNetworkScaling/Nodes_4-16      3   3952345 ns/op
# BenchmarkNetworkScaling/Nodes_8-16      3   5171181 ns/op
# BenchmarkNetworkScaling/Nodes_16-16     3   9453042 ns/op
# BenchmarkNetworkScaling/Nodes_32-16     3  21235888 ns/op
# BenchmarkNetworkScaling/Nodes_64-16     3  34518009 ns/op
```

### Step 3: Profile CPU Hotspots
```bash
# Generate profile for 32 nodes
go test -bench=BenchmarkNetworkScaling/Nodes_32 -cpuprofile=cpu.prof ./internal/core/network/

# Analyze top functions
go tool pprof -top -cum cpu.prof | head -20
```

**Example Output:**
```
      flat  flat%   sum%        cum   cum%
         0     0%     0%      0.42s 30.00%  Node.Advance
         0     0%     0%      0.31s 22.14%  Node.tickQueuesConcurrently
     0.04s  2.86%  3.57%      0.27s 19.29%  runtime.mallocgc
     0.06s  4.29% 10.71%      0.22s 15.71%  runtime.schedule
```

**Interpretation:**
- `runtime.mallocgc` (19.29%): Memory allocation overhead
- `runtime.schedule` (15.71%): Goroutine scheduling overhead
- High scheduler cost indicates too many goroutines

### Step 4: Profile Mutex Contention
```bash
# Generate mutex profile
go test -bench=BenchmarkNetworkScaling/Nodes_64 -mutexprofile=mutex.prof ./internal/core/network/

# Analyze contention
go tool pprof -top mutex.prof
```

**Example Output:**
```
      flat  flat%   sum%        cum   cum%
  414.43ms 73.93% 73.93%   414.43ms 73.93%  runtime._LostContendedRuntimeLock
  146.15ms 26.07%   100%   146.15ms 26.07%  sync.(*Mutex).Unlock
         0     0%   100%    14.31ms  2.55%  Link.updateReady
         0     0%   100%   110.69ms 19.75%  Queue.Tick
```

**Interpretation:**
- 73.93% contention on runtime scheduler lock
- Indicates goroutine count >> CPU cores
- Need to reduce goroutine count

### Step 5: Visualize Call Graph
```bash
# Generate visual graph (requires graphviz)
go tool pprof -web cpu.prof

# Or generate SVG file
go tool pprof -svg cpu.prof > cpu_graph.svg
```

### Step 6: Analyze Specific Functions
```bash
# Show source code with timing annotations
go tool pprof cpu.prof
(pprof) list Network.Advance
```

**Example Output:**
```
     .      .  320:   for cycle := 0; cycle < cycles; cycle++ {
     .      .  321:       var wg sync.WaitGroup
     .      .  322:
     .   50ms  323:       // Start all nodes
     .  200ms  324:       for _, node := range n.nodes {
     .      .  325:           wg.Add(1)
     .      .  326:           go func(n *Node) {
     .      .  327:               defer wg.Done()
  10ms  100ms  328:               n.Tick(cycle)
     .      .  329:           }(node)
     .      .  330:       }
     .      .  331:
     .  150ms  332:       wg.Wait()
     .      .  333:   }
```

## Common Performance Issues

### Issue 1: Too Many Goroutines
**Symptoms:**
- High `runtime.schedule` time
- High `runtime._LostContendedRuntimeLock`
- Performance degrades when goroutines >> CPU cores

**Detection:**
```bash
go tool pprof cpu.prof
(pprof) top -cum | grep schedule
```

**Solutions:**
- Use worker pool pattern
- Batch processing instead of one goroutine per item
- Reduce synchronization frequency

### Issue 2: Memory Allocation Overhead
**Symptoms:**
- High `runtime.mallocgc` time
- High `runtime.newobject` time
- Frequent GC pauses

**Detection:**
```bash
go test -bench=. -benchmem
# Look for high allocs/op
```

**Solutions:**
- Use object pools (`sync.Pool`)
- Reuse buffers instead of allocating
- Pre-allocate slices with known capacity
- Avoid unnecessary copying

### Issue 3: Lock Contention
**Symptoms:**
- High mutex contention in `-mutexprofile`
- Many goroutines waiting on `sync.Mutex.Lock`
- High `sync.Cond.Wait` time

**Detection:**
```bash
go test -bench=. -mutexprofile=mutex.prof
go tool pprof mutex.prof
```

**Solutions:**
- Use atomic operations instead of mutexes
- Reduce critical section size
- Use lock-free data structures
- Shard locks (multiple locks for different data)

### Issue 4: Channel Bottlenecks
**Symptoms:**
- Goroutines blocked on channel operations
- High time in channel send/receive

**Detection:**
```bash
go test -bench=. -blockprofile=block.prof
go tool pprof block.prof
```

**Solutions:**
- Use buffered channels
- Increase buffer size
- Batch channel operations
- Consider lock-free alternatives

## Advanced Techniques

### Continuous Profiling
```bash
# Enable CPU profiling in production
import _ "net/http/pprof"

http.ListenAndServe("localhost:6060", nil)

# Then profile remotely
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
```

### Comparing Profiles
```bash
# Generate baseline
go test -bench=. -cpuprofile=baseline.prof

# After optimization
go test -bench=. -cpuprofile=optimized.prof

# Compare
go tool pprof -base=baseline.prof optimized.prof
```

### Flamegraphs
```bash
# Generate flamegraph (more visual than pprof web)
go test -bench=. -cpuprofile=cpu.prof
go tool pprof -raw cpu.prof > cpu.raw
stackcollapse-go.pl cpu.raw | flamegraph.pl > cpu_flame.svg
```

## Benchmarking Best Practices

### 1. Reset Timer
```go
func BenchmarkExample(b *testing.B) {
    // Expensive setup
    data := setupLargeDataset()

    b.ResetTimer() // Don't count setup time
    for i := 0; i < b.N; i++ {
        processData(data)
    }
}
```

### 2. Avoid Compiler Optimizations
```go
var result int

func BenchmarkExample(b *testing.B) {
    var r int
    for i := 0; i < b.N; i++ {
        r = expensiveComputation()
    }
    result = r // Prevent optimization
}
```

### 3. Parallel Benchmarks
```go
func BenchmarkParallel(b *testing.B) {
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            // Each goroutine runs this
            doWork()
        }
    })
}
```

### 4. Sub-benchmarks
```go
func BenchmarkSizes(b *testing.B) {
    for _, size := range []int{10, 100, 1000, 10000} {
        b.Run(fmt.Sprintf("Size_%d", size), func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                processSize(size)
            }
        })
    }
}
```

## Resources

- Official Go Blog: [Profiling Go Programs](https://go.dev/blog/pprof)
- Go pprof documentation: `go doc runtime/pprof`
- Execution tracer guide: `go doc cmd/trace`
- Benchmark guide: `go help testflag`

## Summary

**Essential Tools:**
1. `go test -bench` - Measure performance
2. `go tool pprof` - Analyze CPU/memory/mutex profiles
3. `go tool trace` - Visualize execution timeline

**Common Workflow:**
1. Write benchmarks
2. Profile with `-cpuprofile`
3. Find hotspots with `pprof -top`
4. Analyze with `pprof -list FuncName`
5. Optimize
6. Repeat

**Red Flags:**
- `runtime.schedule` > 10%: Too many goroutines
- `runtime.mallocgc` > 15%: Too many allocations
- High mutex contention: Lock competition
- Goroutines >> CPU cores: Over-parallelization
