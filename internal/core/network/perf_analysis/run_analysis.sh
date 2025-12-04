#!/bin/bash
# Network Performance Analysis Script
# Automatically runs benchmarks, generates profiles, and creates analysis reports
# Usage: ./run_analysis.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NETWORK_DIR="$(dirname "$SCRIPT_DIR")"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../../.." && pwd)"
OUTPUT_DIR="$SCRIPT_DIR/output"

echo "================================================================================"
echo "              Network Performance Analysis"
echo "================================================================================"
echo "Project Root: $PROJECT_ROOT"
echo "Output Dir:   $OUTPUT_DIR"
echo ""

# Clean and create output directory
rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

# Step 1: Run benchmarks
echo "[1/5] Running benchmarks..."
cd "$NETWORK_DIR"
go test -bench=BenchmarkNetworkScaling -run=^$ -benchtime=3x \
    > "$OUTPUT_DIR/benchmark.txt" 2>&1
echo "✓ Benchmarks complete"

# Step 2: Generate CPU profile
echo "[2/5] Generating CPU profile..."
go test -bench=BenchmarkNetworkScaling/Nodes_32 -run=^$ -benchtime=5x \
    -cpuprofile="$OUTPUT_DIR/cpu.prof" > /dev/null 2>&1
echo "✓ CPU profile generated"

# Step 3: Generate mutex profile
echo "[3/5] Generating mutex profile..."
go test -bench=BenchmarkNetworkScaling/Nodes_64 -run=^$ -benchtime=10x \
    -mutexprofile="$OUTPUT_DIR/mutex.prof" > /dev/null 2>&1 || true
echo "✓ Mutex profile generated"

# Step 4: Analyze profiles
echo "[4/5] Analyzing profiles..."

# CPU profile top functions
go tool pprof -top -cum "$OUTPUT_DIR/cpu.prof" 2>/dev/null | head -20 \
    > "$OUTPUT_DIR/cpu_top.txt" || echo "No CPU profile data" > "$OUTPUT_DIR/cpu_top.txt"

# Mutex profile top functions
if [ -f "$OUTPUT_DIR/mutex.prof" ]; then
    go tool pprof -top "$OUTPUT_DIR/mutex.prof" 2>/dev/null | head -20 \
        > "$OUTPUT_DIR/mutex_top.txt" || echo "No mutex contention detected" > "$OUTPUT_DIR/mutex_top.txt"
else
    echo "No mutex profile generated" > "$OUTPUT_DIR/mutex_top.txt"
fi

echo "✓ Profile analysis complete"

# Step 5: Generate report and charts
echo "[5/5] Generating report..."
python3 "$SCRIPT_DIR/generate_report.py" "$OUTPUT_DIR"
echo "✓ Report generated"

echo ""
echo "================================================================================"
echo "                         Analysis Complete!"
echo "================================================================================"
echo ""
echo "Generated files:"
echo "  - $OUTPUT_DIR/benchmark.txt      : Benchmark results"
echo "  - $OUTPUT_DIR/cpu.prof           : CPU profile (use: go tool pprof)"
echo "  - $OUTPUT_DIR/mutex.prof         : Mutex profile (use: go tool pprof)"
echo "  - $OUTPUT_DIR/cpu_top.txt        : CPU hotspots"
echo "  - $OUTPUT_DIR/mutex_top.txt      : Mutex contention"
echo "  - $OUTPUT_DIR/report.md          : Analysis report"
echo "  - $OUTPUT_DIR/performance.png    : Performance charts"
echo ""
echo "View report: cat $OUTPUT_DIR/report.md"
echo "View charts: open $OUTPUT_DIR/performance.png"
echo ""
echo "Interactive analysis:"
echo "  CPU:   go tool pprof $OUTPUT_DIR/cpu.prof"
echo "  Mutex: go tool pprof $OUTPUT_DIR/mutex.prof"
echo "================================================================================"
