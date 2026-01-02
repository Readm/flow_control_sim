# Network Performance Optimization Plan

Based on the performance analysis, this document outlines potential optimization strategies.

## Current Performance Characteristics

### Baseline (from analysis)
- **4-8 nodes**: Good performance (1-1.5x baseline)
- **16 nodes**: Acceptable (2-3x baseline)
- **32+ nodes**: Poor (5-9x baseline)

### Root Causes
1. **Runtime scheduler lock contention** (76% of mutex contention)
2. **Excessive goroutine creation** (~2.5x node count per cycle)
3. **Fine-grained synchronization** (192+ sync ops per cycle for 8 nodes)

## Optimization Strategies

### Level 1: Quick Wins (Low Risk, High Impact)

#### 1.1 Reuse Goroutines Instead of Creating Per-Cycle

**Current**: Each cycle creates new goroutines for queues
```go
// Current: Node.tickQueuesConcurrently
for cycle := 0; cycle < N; cycle++ {
    for _, queue := range queues {
        go queue.Tick(cycle)  // ← New goroutine every cycle
    }
    wg.Wait()
}
```

**Optimized**: Reuse goroutines across cycles
```go
// Optimized: Goroutines live for all N cycles
for _, queue := range queues {
    go func(q Queue) {
        for cycle := 0; cycle < N; cycle++ {
            barrier.Wait()  // Sync point
            q.Tick(cycle)
        }
    }(queue)
}
```

**Expected Impact**:
- Reduce goroutine creation by 1000x (for 1000 cycles)
- Lower GC pressure
- Estimated: 20-30% speedup

#### 1.2 Batch Synchronization

**Current**: Synchronize every cycle
```go
for cycle := 0; cycle < N; cycle++ {
    tickAll(cycle)
    wg.Wait()  // ← Every cycle
}
```

**Optimized**: Synchronize every K cycles
```go
const batchSize = 10
for batch := 0; batch < N/batchSize; batch++ {
    for i := 0; i < batchSize; i++ {
        tickAll(batch*batchSize + i)
    }
    wg.Wait()  // ← Every 10 cycles
}
```

**Expected Impact**:
- Reduce sync operations by 10x
- Estimated: 15-25% speedup

#### 1.3 Object Pooling for Packets

**Current**: Allocate new packet slices
```go
buffer := make([]packet.Packet, len(packets))
copy(buffer, packets)
```

**Optimized**: Use sync.Pool
```go
var packetPool = sync.Pool{
    New: func() interface{} {
        return make([]packet.Packet, 0, 64)
    },
}

buffer := packetPool.Get().([]packet.Packet)
defer func() {
    buffer = buffer[:0]
    packetPool.Put(buffer)
}()
```

**Expected Impact**:
- Reduce allocations by 50-70%
- Lower GC pressure
- Estimated: 5-10% speedup

### Level 2: Architectural Changes (Medium Risk, High Impact)

#### 2.1 Worker Pool Pattern

**Concept**: Fixed number of workers = CPU cores

```go
type Task struct {
    Component ComponentTicker
    Cycle     int
}

func WorkerPool(numWorkers int) {
    tasks := make(chan Task, 1000)

    // Start fixed workers
    for i := 0; i < numWorkers; i++ {
        go func() {
            for task := range tasks {
                task.Component.Tick(task.Cycle)
            }
        }()
    }

    // Submit tasks
    for cycle := 0; cycle < N; cycle++ {
        for _, component := range allComponents {
            tasks <- Task{component, cycle}
        }
        // Wait for cycle completion
    }
}
```

**Expected Impact**:
- Goroutine count = CPU cores (constant)
- Eliminate scheduler contention
- Estimated: 2-3x speedup for 32+ nodes

#### 2.2 Replace Cond with Atomic Operations

**Current**: Mutex + Cond for Ready state
```go
type Queue struct {
    readyMu   sync.Mutex
    readyCond *sync.Cond
    ready     map[int]bool
}

func (q *Queue) Ready(cycle int) bool {
    q.readyMu.Lock()
    defer q.readyMu.Unlock()
    for !q.ready[cycle] {
        q.readyCond.Wait()
    }
    return true
}
```

**Optimized**: Atomic counter
```go
type Queue struct {
    readyUntil atomic.Int64
}

func (q *Queue) Ready(cycle int) bool {
    for {
        ready := q.readyUntil.Load()
        if ready >= int64(cycle) {
            return true
        }
        runtime.Gosched()  // Yield to avoid busy-wait
    }
}
```

**Expected Impact**:
- Eliminate mutex contention on Ready checks
- Estimated: 10-20% speedup

#### 2.3 Partitioned Execution

**Concept**: Divide network into partitions, each on dedicated thread

```go
type Partition struct {
    Nodes []*Node
    Links []*Link
}

func (p *Partition) Run(cycles int) {
    runtime.LockOSThread()
    defer runtime.UnlockOSThread()

    // Run partition independently
    for cycle := 0; cycle < cycles; cycle++ {
        for _, node := range p.Nodes {
            node.Tick(cycle)
        }
        for _, link := range p.Links {
            link.Tick(cycle)
        }
    }
}
```

**Expected Impact**:
- Reduce global synchronization
- Better cache locality
- Estimated: 1.5-2x speedup

### Level 3: Advanced Optimizations (High Risk, Moderate Impact)

#### 3.1 Lock-Free Ring Buffers

Replace channels with lock-free ring buffers for packet transmission.

**Expected Impact**: 10-15% speedup

#### 3.2 Asynchronous Execution Model

Remove barrier synchronization, allow components to advance at different rates.

**Expected Impact**: 2-3x speedup, but requires careful dependency tracking

#### 3.3 SIMD Packet Processing

Batch process multiple packets using SIMD instructions.

**Expected Impact**: 15-25% speedup for packet-heavy workloads

## Recommended Implementation Order

### Phase 1: Quick Wins (1-2 weeks)
1. Goroutine reuse (1.1)
2. Object pooling (1.3)
3. Measure and validate

### Phase 2: Worker Pool (2-3 weeks)
1. Implement worker pool (2.1)
2. Atomic Ready state (2.2)
3. Measure and validate

### Phase 3: Advanced (if needed)
1. Partitioned execution (2.3)
2. Lock-free structures (3.1)

## Success Metrics

### Target Performance
- **8 nodes**: <2x baseline (currently 1.4x)  Already good
- **16 nodes**: <2x baseline (currently 2.5x)  Need improvement
- **32 nodes**: <3x baseline (currently 5x)  Need improvement
- **64 nodes**: <4x baseline (currently 9x)  Need improvement

### Validation
After each optimization:
1. Run `./run_analysis.sh`
2. Compare with baseline
3. Check for regressions
4. Update this document

## Risk Assessment

### Low Risk
- Object pooling: Easy to revert, isolated change
- Goroutine reuse: Well-understood pattern

### Medium Risk
- Worker pool: Changes execution model, needs careful testing
- Atomic operations: Need to ensure correctness

### High Risk
- Asynchronous execution: Fundamentally changes semantics
- Lock-free structures: Complex to implement correctly

## Next Steps

1. **Establish baseline**: Run current analysis, save results
2. **Implement Phase 1**: Start with goroutine reuse
3. **Measure impact**: Use automated analysis
4. **Iterate**: Continue with Phase 2 if needed

## Notes

- All optimizations should preserve correctness
- Test suite must pass after each change
- Performance tests should show consistent improvement
- Consider adding micro-benchmarks for specific optimizations
