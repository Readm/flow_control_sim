# Network Performance Analysis

Automated performance analysis for the network simulation package.

## Quick Start

```bash
cd internal/core/network/perf_analysis
./run_analysis.sh
```

This will:
1. Run benchmarks across different node counts (4, 8, 16, 32, 64)
2. Generate CPU and mutex profiles
3. Analyze performance bottlenecks
4. Create visualization charts
5. Generate a comprehensive report

## Output

All results are saved to `output/`:

- `report.md` - Comprehensive analysis report
- `performance.png` - Performance charts
- `cpu.prof` - CPU profile for interactive analysis
- `mutex.prof` - Mutex contention profile
- `benchmark.txt` - Raw benchmark results

## Viewing Results

### Quick View
```bash
cat output/report.md
```

### Interactive Analysis
```bash
# Analyze CPU hotspots
go tool pprof output/cpu.prof
# Then type: top, list <function>, web

# Analyze mutex contention
go tool pprof output/mutex.prof
```

### View Charts
```bash
# Linux
xdg-open output/performance.png

# macOS
open output/performance.png
```

## Requirements

- Go 1.22+
- Python 3.6+ (for report generation)
- matplotlib (optional, for charts): `pip3 install matplotlib`

## Understanding the Results

### Performance Metrics

- **Execution Time**: Total time to run 1000 cycles
- **Slowdown**: How much slower compared to baseline (4 nodes)
- **Parallel Efficiency**: How well the system utilizes parallelism

### Good vs Bad Performance

-  **Good**: Efficiency > 70%, Slowdown < 2x
-  **Warning**: Efficiency 50-70%, Slowdown 2-5x
-  **Poor**: Efficiency < 50%, Slowdown > 5x

### Typical Bottlenecks

1. **runtime.schedule** - Too many goroutines
2. **runtime.mallocgc** - Excessive memory allocation
3. **sync.Mutex** - Lock contention
4. **sync.Cond** - Condition variable overhead

## Customizing the Analysis

### Change Node Counts

Edit `run_analysis.sh` and modify the benchmark command:

```bash
# Test specific node counts
go test -bench='BenchmarkNetworkScaling/(Nodes_8|Nodes_16)' ...
```

### Adjust Benchmark Duration

```bash
# Run more iterations for stable results
go test -bench=... -benchtime=10x ...
```

### Focus on Specific Profile

```bash
# Generate memory profile
go test -bench=... -memprofile=mem.prof

# Generate block profile
go test -bench=... -blockprofile=block.prof
```

## Interpreting Profiles

### CPU Profile
Shows where the program spends most CPU time. Look for:
- Functions with high **flat%** (direct time)
- Functions with high **cum%** (total time including calls)

### Mutex Profile
Shows lock contention. Look for:
- High contention in `runtime._LostContendedRuntimeLock` (scheduler)
- High contention in application locks

## Example Session

```bash
$ ./run_analysis.sh
[1/5] Running benchmarks...
 Benchmarks complete
[2/5] Generating CPU profile...
 CPU profile generated
[3/5] Generating mutex profile...
 Mutex profile generated
[4/5] Analyzing profiles...
 Profile analysis complete
[5/5] Generating report...
 Report generated

Generated files:
  - output/benchmark.txt      : Benchmark results
  - output/report.md          : Analysis report
  - output/performance.png    : Performance charts
  ...

$ cat output/report.md
# Network Performance Analysis Report
...

$ go tool pprof output/cpu.prof
(pprof) top
(pprof) list Network.Advance
```

## Troubleshooting

### Charts not generated
Install matplotlib:
```bash
pip3 install matplotlib
```

### Permission denied
Make script executable:
```bash
chmod +x run_analysis.sh
```

### Benchmark timeout
Increase timeout in test:
```bash
go test -timeout=5m -bench=...
```

## Notes

- Node simulation includes 2-20us random delay (simulates GEM5 O3CPU)
- Ring topology is used for testing
- Each test runs with bandwidth=1 to stress synchronization
- Default test runs 1000 cycles per benchmark
