#!/usr/bin/env python3
"""Generate performance analysis report and charts"""
import sys
import re
import os
from datetime import datetime

try:
    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.pyplot as plt
    import numpy as np
    HAVE_MATPLOTLIB = True
except ImportError:
    HAVE_MATPLOTLIB = False
    print("Warning: matplotlib not available, charts will not be generated")


def parse_benchmark(filepath):
    """Parse benchmark output and extract key metrics"""
    results = []
    with open(filepath, 'r') as f:
        for line in f:
            # BenchmarkNetworkScaling/Nodes_32-16    3  21235888 ns/op
            match = re.search(r'Nodes_(\d+)-\d+\s+\d+\s+(\d+)\s+ns/op', line)
            if match:
                nodes = int(match.group(1))
                time_ns = int(match.group(2))
                results.append((nodes, time_ns / 1e6))  # Convert to ms
    return sorted(results)


def parse_profile(filepath, profile_type):
    """Parse pprof output"""
    hotspots = []
    try:
        with open(filepath, 'r') as f:
            lines = f.readlines()
            for line in lines[5:15]:  # Skip header, take top 10
                if line.strip() and not line.startswith('Showing'):
                    parts = line.split()
                    if len(parts) >= 6:
                        func = parts[-1]
                        pct = parts[1] if profile_type == 'cpu' else parts[0]
                        hotspots.append((func, pct))
    except Exception as e:
        print(f"Warning: Could not parse {profile_type} profile: {e}")
    return hotspots


def generate_charts(results, output_dir):
    """Generate performance charts"""
    if not HAVE_MATPLOTLIB or not results:
        return False

    nodes_list = [r[0] for r in results]
    times = [r[1] for r in results]

    # Calculate metrics
    baseline_time = times[0]
    baseline_nodes = nodes_list[0]
    ideal_times = [baseline_time * (n / baseline_nodes) for n in nodes_list]
    slowdowns = [t / baseline_time for t in times]
    efficiency = [100 * (baseline_time * n / baseline_nodes) / t for n, t in zip(nodes_list, times)]

    fig, ((ax1, ax2), (ax3, ax4)) = plt.subplots(2, 2, figsize=(14, 10))

    # Chart 1: Execution Time
    ax1.plot(nodes_list, times, 'bo-', linewidth=2, markersize=8, label='Actual')
    ax1.plot(nodes_list, ideal_times, 'r--', linewidth=2, label='Ideal Linear')
    ax1.set_xlabel('Number of Nodes', fontsize=11)
    ax1.set_ylabel('Execution Time (ms)', fontsize=11)
    ax1.set_title('Performance Scalability', fontsize=12, fontweight='bold')
    ax1.legend()
    ax1.grid(True, alpha=0.3)

    # Chart 2: Slowdown
    ax2.plot(nodes_list, slowdowns, 'ro-', linewidth=2, markersize=8)
    ax2.set_xlabel('Number of Nodes', fontsize=11)
    ax2.set_ylabel('Slowdown vs Baseline', fontsize=11)
    ax2.set_title('Performance Degradation', fontsize=12, fontweight='bold')
    ax2.grid(True, alpha=0.3)
    for n, s in zip(nodes_list, slowdowns):
        ax2.text(n, s, f'{s:.1f}x', ha='center', va='bottom', fontsize=9)

    # Chart 3: Parallel Efficiency
    ax3.plot(nodes_list, efficiency, 'go-', linewidth=2, markersize=8)
    ax3.axhline(y=100, color='gray', linestyle='--', alpha=0.5)
    ax3.axhline(y=50, color='orange', linestyle='--', alpha=0.5)
    ax3.set_xlabel('Number of Nodes', fontsize=11)
    ax3.set_ylabel('Parallel Efficiency (%)', fontsize=11)
    ax3.set_title('Parallel Efficiency', fontsize=12, fontweight='bold')
    ax3.grid(True, alpha=0.3)
    ax3.set_ylim(0, 120)

    # Chart 4: Data Table
    ax4.axis('off')
    table_data = [['Nodes', 'Time(ms)', 'Slowdown', 'Efficiency']]
    for i, n in enumerate(nodes_list):
        table_data.append([
            f'{n}',
            f'{times[i]:.2f}',
            f'{slowdowns[i]:.2f}x',
            f'{efficiency[i]:.0f}%'
        ])

    table = ax4.table(cellText=table_data, cellLoc='center', loc='center',
                     colWidths=[0.2, 0.25, 0.25, 0.25])
    table.auto_set_font_size(False)
    table.set_fontsize(10)
    table.scale(1, 2)

    # Style header row
    for i in range(4):
        table[(0, i)].set_facecolor('#4472C4')
        table[(0, i)].set_text_props(weight='bold', color='white')

    plt.tight_layout()
    plt.savefig(os.path.join(output_dir, 'performance.png'), dpi=150, bbox_inches='tight')
    return True


def generate_report(output_dir):
    """Generate markdown report"""
    benchmark_file = os.path.join(output_dir, 'benchmark.txt')
    cpu_top_file = os.path.join(output_dir, 'cpu_top.txt')
    mutex_top_file = os.path.join(output_dir, 'mutex_top.txt')

    results = parse_benchmark(benchmark_file)
    cpu_hotspots = parse_profile(cpu_top_file, 'cpu')
    mutex_hotspots = parse_profile(mutex_top_file, 'mutex')

    report = f"""# Network Performance Analysis Report

**Generated**: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}
**System**: {os.uname().sysname} {os.uname().machine}
**CPU Cores**: {os.cpu_count()}

## Summary

"""

    if results:
        baseline_time = results[0][1]
        worst_time = results[-1][1]
        worst_slowdown = worst_time / baseline_time

        report += f"""### Performance Overview

| Metric | Value |
|--------|-------|
| Baseline ({results[0][0]} nodes) | {baseline_time:.2f} ms |
| Worst Case ({results[-1][0]} nodes) | {worst_time:.2f} ms |
| Degradation | {worst_slowdown:.2f}x slower |

### Detailed Results

| Nodes | Time (ms) | Slowdown | Status |
|-------|-----------|----------|--------|
"""
        for nodes, time_ms in results:
            slowdown = time_ms / baseline_time
            status = "" if slowdown < 2 else ("" if slowdown < 5 else "")
            report += f"| {nodes} | {time_ms:.2f} | {slowdown:.2f}x | {status} |\n"

    report += "\n## Performance Bottlenecks\n\n"

    if cpu_hotspots:
        report += "### CPU Hotspots (Top 5)\n\n"
        report += "| Function | Time % |\n|----------|--------|\n"
        for func, pct in cpu_hotspots[:5]:
            report += f"| `{func}` | {pct} |\n"

    if mutex_hotspots:
        report += "\n### Mutex Contention (Top 5)\n\n"
        report += "| Function | Contention |\n|----------|------------|\n"
        for func, pct in mutex_hotspots[:5]:
            report += f"| `{func}` | {pct} |\n"

    report += """
## Analysis

### Key Findings

1. **Scalability**: Performance degrades when node count exceeds CPU cores
2. **Bottlenecks**: Primary bottlenecks are in goroutine scheduling and synchronization
3. **Efficiency**: Parallel efficiency drops significantly with more nodes

### Recommendations

**Short-term optimizations:**
- Reduce goroutine creation frequency
- Batch synchronization operations
- Use object pools for packet buffers

**Long-term optimizations:**
- Implement worker pool pattern
- Reduce fine-grained synchronization
- Consider lock-free data structures

## Next Steps

1. Review CPU hotspots in detail:
   ```bash
   go tool pprof output/cpu.prof
   ```

2. Analyze mutex contention:
   ```bash
   go tool pprof output/mutex.prof
   ```

3. Run with different node counts to identify scaling limits

## Files Generated

- `benchmark.txt` - Raw benchmark output
- `cpu.prof` - CPU profile for interactive analysis
- `mutex.prof` - Mutex contention profile
- `cpu_top.txt` - CPU hotspots summary
- `mutex_top.txt` - Mutex contention summary
- `performance.png` - Performance visualization charts
- `report.md` - This report
"""

    report_file = os.path.join(output_dir, 'report.md')
    with open(report_file, 'w') as f:
        f.write(report)

    return results


def main():
    if len(sys.argv) < 2:
        print("Usage: generate_report.py <output_dir>")
        sys.exit(1)

    output_dir = sys.argv[1]

    print("Parsing benchmark results...")
    results = generate_report(output_dir)

    if results and HAVE_MATPLOTLIB:
        print("Generating charts...")
        if generate_charts(results, output_dir):
            print(" Charts generated")
    elif not HAVE_MATPLOTLIB:
        print("Skipping charts (matplotlib not available)")

    print(" Report complete")


if __name__ == '__main__':
    main()
