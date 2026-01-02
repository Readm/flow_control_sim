#!/bin/bash

# 代码树形统计脚本
# 递归显示所有层级的代码行数统计

set -e

PROJECT_ROOT="${1:-.}"
cd "$PROJECT_ROOT"

# 定义排除目录和文件的模式 (按需调整)
# 排除项：.git, vendor, tests, node_modules, ThirdParty, dist, bin, 以及任何以 . 开头的目录（如 .cursor, .gemini）
# 注意：使用锚点和斜杠确保只匹配目录名全称，不误杀子目录（如 internal/testing）
EXCLUDE_DIRS_REGEX="(^|/)\.git(/|$)|(^|/)vendor(/|$)|(^|/)tests(/|$)|(^|/)node_modules(/|$)|(^|/)ThirdParty(/|$)|(^|/)dist(/|$)|(^|/)bin(/|$)|(^|/)\.[^/]+(/|$)"

echo "=== 项目代码树形统计（已排除测试文件及第三方/构建目录）==="
echo "排除范围: $EXCLUDE_DIRS_REGEX"
echo ""

# 统计函数：统计指定目录直接包含的 Go 文件行数（不包括子目录）
count_go_lines() {
    local dir="$1"
    # 使用 find 查找直接属于该目录的 .go 文件
    local files=$(find "$dir" -maxdepth 1 -name "*.go" ! -name "*_test.go" -type f 2>/dev/null)
    if [ -n "$files" ]; then
        echo "$files" | xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}'
    else
        echo "0"
    fi
}

# 递归显示目录树和代码统计
show_dir_tree() {
    local dir="$1"
    local prefix="$2"
    local is_last="$3"
    
    # 检查当前目录是否应该被排除（基于完整路径名）
    if [[ "$dir" =~ $EXCLUDE_DIRS_REGEX ]]; then
        return
    fi
    
    local dirname=$(basename "$dir")
    if [ "$dirname" = "." ] || [ "$dirname" = "$(basename $(pwd))" ]; then
        dirname="flow_sim"
    fi
    
    # 统计当前目录直接包含的 Go 文件（不包括子目录）
    local count=$(count_go_lines "$dir")
    
    # 获取子目录（递归检查是否包含 Go 文件）
    local subdirs=()
    while IFS= read -r subdir; do
        [ -z "$subdir" ] && continue
        # 检查子目录内部是否真的有非测试 Go 文件（排除掉被忽略的路径）
        local has_go=$(find "$subdir" -name "*.go" ! -name "*_test.go" -type f 2>/dev/null | grep -vE "$EXCLUDE_DIRS_REGEX" | head -1)
        if [ -n "$has_go" ]; then
            subdirs+=("$subdir")
        fi
    done < <(find "$dir" -maxdepth 1 -type d ! -path "$dir" 2>/dev/null | sort)
    
    # 如果有代码或子目录，显示
    if [ "$count" != "0" ] || [ ${#subdirs[@]} -gt 0 ]; then
        local connector=""
        if [ "$dir" != "." ]; then
            if [ "$is_last" = "true" ]; then
                connector="└── "
            else
                connector="├── "
            fi
        fi
        
        # 显示目录和代码行数
        if [ "$count" != "0" ]; then
            echo "${prefix}${connector}${dirname}/ [${count} 行]"
        else
            echo "${prefix}${connector}${dirname}/"
        fi
        
        # 计算子目录的前缀
        local new_prefix=""
        if [ "$dir" = "." ]; then
            new_prefix=""
        else
            if [ "$is_last" = "true" ]; then
                new_prefix="${prefix}    "
            else
                new_prefix="${prefix}│   "
            fi
        fi
        
        # 递归处理子目录
        local total=${#subdirs[@]}
        local idx=0
        for subdir in "${subdirs[@]}"; do
            idx=$((idx + 1))
            show_dir_tree "$subdir" "$new_prefix" "$([ $idx -eq $total ] && echo "true" || echo "false")"
        done
    fi
}

# 1. 运行树形展示
show_dir_tree "." "" "true"

echo ""

# 2. 显示模块汇总
echo "=== 模块汇总（仅统计非测试 Go 代码） ==="
total_all=0

# 获取所有顶级目录（排除被忽略的）
top_dirs=$(find . -maxdepth 1 -type d ! -path "." | grep -vE "$EXCLUDE_DIRS_REGEX" | sort)

for dir in $top_dirs; do
    module=$(basename "$dir")
    # 统计该目录下所有（递归）Go 文件，排除测试文件和内部可能存在的排除目录
    count=$(find "$dir" -name "*.go" ! -name "*_test.go" -type f 2>/dev/null | grep -vE "$EXCLUDE_DIRS_REGEX" | xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}' || echo "0")
    if [ -n "$count" ] && [ "$count" -gt 0 ]; then
        printf "  %-20s %6d 行\n" "$module/:" "$count"
        total_all=$((total_all + count))
    fi
done

# 统计根目录下的 Go 文件
root_files=$(find . -maxdepth 1 -name "*.go" ! -name "*_test.go" -type f 2>/dev/null)
if [ -n "$root_files" ]; then
    root_count=$(echo "$root_files" | xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}')
    if [ "$root_count" -gt 0 ]; then
        printf "  %-20s %6d 行\n" "(root files):" "$root_count"
        total_all=$((total_all + root_count))
    fi
fi

echo "  ------------------------------------"
printf "  %-20s %6d 行\n" "总计:" "$total_all"
echo ""

# 3. 使用 cloc 显示详细统计（如果安装了）
if command -v cloc &> /dev/null; then
    echo "=== cloc 对照统计（排除第三方/测试）==="
    # 将正则表达式转换为 cloc 排除目录参数
    CLOC_EXCLUDE="vendor,tests,node_modules,ThirdParty,dist,bin"
    cloc --exclude-dir=$CLOC_EXCLUDE --exclude-ext=test.go,example.go . 2>/dev/null | grep -E "^Go|^SUM" | head -2
fi
