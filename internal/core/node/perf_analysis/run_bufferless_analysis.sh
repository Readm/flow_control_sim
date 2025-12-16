#!/bin/bash

# Bufferless Ring Performance Analysis Script
# Runs comprehensive benchmarks and generates performance reports

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_DIR="$SCRIPT_DIR/output"
NODE_DIR="$(dirname "$SCRIPT_DIR")"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "╔════════════════════════════════════════════════════════════════╗"
echo "║   Bufferless Ring Network Performance Analysis                ║"
echo "╚════════════════════════════════════════════════════════════════╝"
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Step 1: Run benchmarks
echo -e "${BLUE}[1/5] Running benchmarks...${NC}"
cd "$NODE_DIR"
go test -bench=BenchmarkBufferlessRing \
    -benchtime=5x \
    -timeout=5m \
    -run=^$ \
    -cpuprofile="$OUTPUT_DIR/cpu.prof" \
    -memprofile="$OUTPUT_DIR/mem.prof" \
    -mutexprofile="$OUTPUT_DIR/mutex.prof" \
    . 2>&1 | tee "$OUTPUT_DIR/benchmark.txt"

echo -e "${GREEN}✓ Benchmarks complete${NC}"
echo ""

# Step 2: Generate CPU profile analysis
echo -e "${BLUE}[2/5] Analyzing CPU profile...${NC}"
go tool pprof -text -nodecount=20 "$OUTPUT_DIR/cpu.prof" > "$OUTPUT_DIR/cpu_analysis.txt" 2>/dev/null || true
echo -e "${GREEN}✓ CPU profile analyzed${NC}"
echo ""

# Step 3: Generate mutex profile analysis
echo -e "${BLUE}[3/5] Analyzing mutex profile...${NC}"
go tool pprof -text -nodecount=20 "$OUTPUT_DIR/mutex.prof" > "$OUTPUT_DIR/mutex_analysis.txt" 2>/dev/null || true
echo -e "${GREEN}✓ Mutex profile analyzed${NC}"
echo ""

# Step 4: Generate memory profile analysis
echo -e "${BLUE}[4/5] Analyzing memory profile...${NC}"
go tool pprof -text -nodecount=20 -alloc_space "$OUTPUT_DIR/mem.prof" > "$OUTPUT_DIR/mem_analysis.txt" 2>/dev/null || true
echo -e "${GREEN}✓ Memory profile analyzed${NC}"
echo ""

# Step 5: Generate summary report
echo -e "${BLUE}[5/5] Generating summary report...${NC}"

cat > "$OUTPUT_DIR/REPORT.md" << 'EOF'
# Bufferless Ring Performance Analysis Report

## Summary

This report contains performance analysis for the Bufferless Ring Network implementation.

## Benchmark Results

EOF

# Extract benchmark results
echo "### Scaling Performance" >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
grep "BenchmarkBufferlessRing_Scaling" "$OUTPUT_DIR/benchmark.txt" | sed 's/BenchmarkBufferlessRing_Scaling\///' >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"

echo "### Throughput Tests" >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
grep "BenchmarkBufferlessRing_Throughput" "$OUTPUT_DIR/benchmark.txt" | sed 's/BenchmarkBufferlessRing_Throughput\///' >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"

echo "### Backpressure Performance" >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
grep "BenchmarkBufferlessRing_Backpressure" "$OUTPUT_DIR/benchmark.txt" | sed 's/BenchmarkBufferlessRing_Backpressure\///' >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"

echo "### Buffer Size Impact" >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
grep "BenchmarkBufferlessRing_BufferSize" "$OUTPUT_DIR/benchmark.txt" | sed 's/BenchmarkBufferlessRing_BufferSize\///' >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"

# Add CPU hotspots
echo "## CPU Hotspots (Top 10)" >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
head -15 "$OUTPUT_DIR/cpu_analysis.txt" >> "$OUTPUT_DIR/REPORT.md" 2>/dev/null || echo "No CPU profile data available" >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"

# Add mutex contention
echo "## Mutex Contention (Top 10)" >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
head -15 "$OUTPUT_DIR/mutex_analysis.txt" >> "$OUTPUT_DIR/REPORT.md" 2>/dev/null || echo "No mutex profile data available" >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"

# Add memory allocation
echo "## Memory Allocation (Top 10)" >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
head -15 "$OUTPUT_DIR/mem_analysis.txt" >> "$OUTPUT_DIR/REPORT.md" 2>/dev/null || echo "No memory profile data available" >> "$OUTPUT_DIR/REPORT.md"
echo '```' >> "$OUTPUT_DIR/REPORT.md"
echo "" >> "$OUTPUT_DIR/REPORT.md"

# Add analysis notes
cat >> "$OUTPUT_DIR/REPORT.md" << 'NOTES'
## Analysis Notes

### Performance Characteristics

- **Single-core vs Multi-core**: Compare scaling efficiency
- **Throughput**: Packets delivered vs injection rate
- **Backpressure**: Impact of blocked workers on system performance
- **Buffer Size**: Effect of router buffer capacity on performance

### Key Metrics

- **cycles/sec**: Simulation throughput
- **ns/op**: Time per benchmark iteration
- **delivery_%**: Packet delivery success rate
- **injected/received**: Packet flow statistics

### How to Interpret

1. **Good Performance**:
   - cycles/sec > 50k (single-core)
   - delivery_% > 95%
   - Low mutex contention

2. **Performance Issues**:
   - High time in runtime.schedule
   - Excessive memory allocation
   - Lock contention in critical paths

### Interactive Analysis

```bash
# View CPU profile interactively
go tool pprof output/cpu.prof

# View mutex profile
go tool pprof output/mutex.prof

# View memory profile
go tool pprof output/mem.prof
```

NOTES

echo -e "${GREEN}✓ Report generated${NC}"
echo ""

# Print summary
echo "╔════════════════════════════════════════════════════════════════╗"
echo "║                    Analysis Complete                           ║"
echo "╚════════════════════════════════════════════════════════════════╝"
echo ""
echo "Generated files:"
echo "  📊 $OUTPUT_DIR/benchmark.txt       - Raw benchmark results"
echo "  📈 $OUTPUT_DIR/REPORT.md           - Summary report"
echo "  🔥 $OUTPUT_DIR/cpu.prof            - CPU profile"
echo "  🔒 $OUTPUT_DIR/mutex.prof          - Mutex profile"
echo "  💾 $OUTPUT_DIR/mem.prof            - Memory profile"
echo ""
echo "Quick view:"
echo -e "  ${YELLOW}cat $OUTPUT_DIR/REPORT.md${NC}"
echo ""
echo "Interactive analysis:"
echo -e "  ${YELLOW}go tool pprof $OUTPUT_DIR/cpu.prof${NC}"
echo ""
