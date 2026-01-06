# Performance and CPU Profiling Guide

## Overview
FlowSim provides built-in metrics and tooling to analyze CPU usage, synchronization overhead, and threading latency. This guide explains how to interpret the `CPU Stats` and how to perform deeper profiling.

## Standard CPU Stats
The benchmark output includes `CPU Stats` collected from `runtime/metrics`:
```
CPU Stats: Idle=3.1% User=80.9% Sys=16.0% | Wait: P50=0.0us P99=0.5us STW=0.00ms
```

### Metrics Breakdown
1.  **Idle**: CPU time spent idle.
2.  **Sys (System)**: Time spent in GC (Garbage Collection) and Scavenge (returning memory to OS).
    *   *High Sys* usually indicates intense memory allocation/deallocation pressure.
3.  **User**: Time spent executing Go code (Simulation Logic + Runtime Synchronization).
4.  **Wait P50/P99**: Goroutine Scheduling Latency.
    *   Time a goroutine waits in the `Runnable` queue before executing.
    *   *High Latency (>50µs)* indicates CPU saturation or thread contention.
5.  **STW**: Stop-The-World Pause duration.

## Advanced Profiling (Sync vs Work)
To split **User** time into "Actual Work" vs "Synchronization Overhead", use the profiling tools.

### 1. Integrated Runtime Profiling
The benchmark suite now has integrated profiling support. Simply set `PROFILE=true` environment variable.

```bash
PROFILE=true go test -bench=Benchmark_ChampSim_Baseline_1CPU -run=^$ -v ./internal/benchmarks
```

**Output Example**:
```
CPU Stats: Idle=2.4% User=83.7% Sys=13.9% | Wait: P50=0.0us P99=0.8us STW=0.00ms
----- Runtime Profile Breakdown (CPU Active Time Only) -----
App Logic:     71.6%  <-- Actual work
Runtime Sync:   9.6%  <-- Sched/Chan/Lock overhead
GC & Memory:    7.7%  <-- Runtime memory ops
System & I/O:   3.3%  <-- Syscalls
Data Copy:      7.8%  <-- memmove/memclr
----------------------------------------------------------
```
This automatically breaks down the "User" time into meaningful categories. No extra scripts required.

### 3. Theoretical "Total Breakdown"
To get a full 100% breakdown, combine `CPU Stats` (for Idle) with Profile data (for User split):
*   **Idle**: From `CPU Stats`.
*   **GC/System**: From `CPU Stats` (Sys) or Profile (GC+System).
*   **Sync**: `User%` * `(Runtime Sync / Total User in Profile)`
*   **Work**: `User%` * `(App Logic / Total User in Profile)`
