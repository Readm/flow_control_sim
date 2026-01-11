#!/bin/bash
# 运行性能基准测试并保存结果

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 默认参数
BASELINE_FILE=""
OUTPUT_FILE=""
BENCHTIME="3s"
COMPARE=false

# 解析参数
while [[ $# -gt 0 ]]; do
    case $1 in
        --baseline)
            BASELINE_FILE="$2"
            shift 2
            ;;
        --output)
            OUTPUT_FILE="$2"
            shift 2
            ;;
        --benchtime)
            BENCHTIME="$2"
            shift 2
            ;;
        --compare)
            COMPARE=true
            shift
            ;;
        --help)
            echo "用法: $0 [选项]"
            echo ""
            echo "选项:"
            echo "  --baseline FILE    与基准文件对比"
            echo "  --output FILE      输出结果到文件（默认：自动生成）"
            echo "  --benchtime TIME   每个 benchmark 运行时长（默认：3s）"
            echo "  --compare          对比结果并显示差异"
            echo "  --help             显示此帮助信息"
            echo ""
            echo "示例:"
            echo "  $0 --output baseline.txt"
            echo "  $0 --baseline baseline.txt --compare"
            exit 0
            ;;
        *)
            echo "未知选项: $1"
            echo "使用 --help 查看帮助"
            exit 1
            ;;
    esac
done

# 如果没有指定输出文件，使用时间戳
if [ -z "$OUTPUT_FILE" ]; then
    TIMESTAMP=$(date +%Y%m%d_%H%M%S)
    OUTPUT_FILE="docs/refactoring/benchmark_${TIMESTAMP}.txt"
fi

echo -e "${GREEN}=== Flow Simulation Benchmark ===${NC}"
echo "输出文件: $OUTPUT_FILE"
echo "Benchtime: $BENCHTIME"
echo ""

# 确保输出目录存在
mkdir -p "$(dirname "$OUTPUT_FILE")"

# 运行 benchmark
echo -e "${YELLOW}运行 Benchmark...${NC}"
echo ""

# 添加系统信息到输出文件
{
    echo "=== System Info ==="
    echo "Date: $(date)"
    echo "Go Version: $(go version)"
    echo "OS: $(uname -s)"
    echo "Arch: $(uname -m)"
    echo "CPU: $(grep "model name" /proc/cpuinfo 2>/dev/null | head -1 || echo "N/A")"
    echo ""
    echo "=== Git Info ==="
    echo "Commit: $(git rev-parse --short HEAD 2>/dev/null || echo 'N/A')"
    echo "Branch: $(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo 'N/A')"
    echo "Dirty: $(git diff --quiet 2>/dev/null && echo 'No' || echo 'Yes')"
    echo ""
    echo "=== Benchmark Results ==="
    echo ""
} > "$OUTPUT_FILE"

# 运行所有 benchmark
go test -bench=. -benchmem -benchtime="$BENCHTIME" \
    ./internal/benchmarks/... \
    2>&1 | tee -a "$OUTPUT_FILE"

# 如果测试失败，退出
if [ ${PIPESTATUS[0]} -ne 0 ]; then
    echo -e "${RED}Benchmark 运行失败${NC}"
    exit 1
fi

echo ""
echo -e "${GREEN}✓ Benchmark 完成${NC}"
echo "结果已保存到: $OUTPUT_FILE"

# 如果需要对比
if [ "$COMPARE" = true ] && [ -n "$BASELINE_FILE" ]; then
    echo ""
    echo -e "${YELLOW}=== 性能对比 ===${NC}"

    if [ ! -f "$BASELINE_FILE" ]; then
        echo -e "${RED}错误: 基准文件不存在: $BASELINE_FILE${NC}"
        exit 1
    fi

    # 使用 benchcmp 对比（如果已安装）
    if command -v benchcmp &> /dev/null; then
        echo ""
        benchcmp "$BASELINE_FILE" "$OUTPUT_FILE"
    elif command -v benchstat &> /dev/null; then
        echo ""
        benchstat "$BASELINE_FILE" "$OUTPUT_FILE"
    else
        echo -e "${YELLOW}提示: 安装 benchstat 可以查看详细对比:${NC}"
        echo "  go install golang.org/x/perf/cmd/benchstat@latest"
        echo ""
        echo -e "${YELLOW}手动对比关键指标:${NC}"
        echo ""

        # 简单对比（提取关键数据）
        echo "=== Baseline ==="
        grep "^Benchmark" "$BASELINE_FILE" | head -10
        echo ""
        echo "=== Current ==="
        grep "^Benchmark" "$OUTPUT_FILE" | head -10
    fi
fi

echo ""
echo -e "${GREEN}完成！${NC}"
