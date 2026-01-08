#!/bin/bash
# 生成 Go 类型定义从 OpenAPI schema

set -e
export PATH=$PATH:$HOME/go/bin

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

OPENAPI_FILE="$PROJECT_ROOT/web/openapi.yaml"
OUTPUT_DIR="$PROJECT_ROOT/internal/core/visualization/protocol"
OUTPUT_FILE="$OUTPUT_DIR/types.gen.go"

echo "==> 生成 Go 类型定义"
echo "    输入: $OPENAPI_FILE"
echo "    输出: $OUTPUT_FILE"

# 确保输出目录存在
mkdir -p "$OUTPUT_DIR"

# 检查 oapi-codegen 是否安装
if ! command -v oapi-codegen &> /dev/null; then
    echo "错误: oapi-codegen 未安装"
    echo "请运行: go install github.com/deepmap/oapi-codegen/cmd/oapi-codegen@latest"
    exit 1
fi

# 生成类型定义
oapi-codegen \
    -package protocol \
    -generate types \
    -o "$OUTPUT_FILE" \
    "$OPENAPI_FILE"

echo "==> 生成完成: $OUTPUT_FILE"

# 格式化代码
if command -v gofmt &> /dev/null; then
    echo "==> 格式化代码"
    gofmt -w "$OUTPUT_FILE"
fi

echo "✓ Go 类型生成成功"
