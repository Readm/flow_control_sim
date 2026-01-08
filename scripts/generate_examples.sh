#!/bin/bash

# 生成所有示例网络的脚本

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
EXAMPLES_DIR="$PROJECT_ROOT/web/examples"
PUBLIC_DIR="$PROJECT_ROOT/web/public"

echo "=== 生成 FlowSim 示例网络 ==="
echo

cd "$PROJECT_ROOT"

# 生成环形网络
echo "[1/4] 生成 4 节点环形网络..."
go run cmd/export_examples/main.go "$EXAMPLES_DIR" ring 4

echo "[2/4] 生成 8 节点环形网络..."
go run cmd/export_examples/main.go "$EXAMPLES_DIR" ring 8

echo "[3/4] 生成 16 节点环形网络..."
go run cmd/export_examples/main.go "$EXAMPLES_DIR" ring 16

# 生成多边示例
echo "[4/4] 生成多边示例网络..."
go run cmd/export_examples/main.go "$EXAMPLES_DIR" multi_edge

echo
echo "=== 复制示例到 public 文件夹 ==="
echo

# 复制到 public 文件夹供前端访问
cp "$EXAMPLES_DIR"/*.json "$PUBLIC_DIR/"
echo "✓ 所有示例已复制到 $PUBLIC_DIR"

echo
echo "=== 生成完成 ==="
echo "生成的示例文件："
ls -lh "$EXAMPLES_DIR"/*.json

echo
echo "可在前端访问的文件："
ls -lh "$PUBLIC_DIR"/*.json

echo
echo "🎉 完成！现在可以在前端界面的「选择示例网络」下拉菜单中使用这些示例。"
