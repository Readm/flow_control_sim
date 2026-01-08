#!/bin/bash
# 生成 TypeScript 类型定义从 OpenAPI schema

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

OPENAPI_FILE="$PROJECT_ROOT/web/openapi.yaml"
OUTPUT_FILE="$PROJECT_ROOT/web/src/types/api.ts"

echo "==> 生成 TypeScript 类型定义"
echo "    输入: $OPENAPI_FILE"
echo "    输出: $OUTPUT_FILE"

# 确保输出目录存在
mkdir -p "$(dirname "$OUTPUT_FILE")"

# 进入 web 目录
cd "$PROJECT_ROOT/web"

# 检查 openapi-typescript 是否安装
if [ ! -f "node_modules/.bin/openapi-typescript" ]; then
    echo "错误: openapi-typescript 未安装"
    echo "请运行: cd web && npm install --save-dev openapi-typescript"
    exit 1
fi

# 生成类型定义
npx openapi-typescript "../web/openapi.yaml" --output "src/types/api.ts"

echo "==> 生成完成: $OUTPUT_FILE"
echo "✓ TypeScript 类型生成成功"
