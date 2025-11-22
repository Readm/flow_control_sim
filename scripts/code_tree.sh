#!/bin/bash

# 代码树形统计脚本
# 递归显示所有层级的代码行数统计

set -e

PROJECT_ROOT="${1:-.}"
cd "$PROJECT_ROOT"

echo "=== 项目代码树形统计（排除测试文件和测试目录）==="
echo ""

# 统计函数：统计指定目录直接包含的 Go 文件行数（不包括子目录）
count_go_lines() {
    local dir="$1"
    find "$dir" -maxdepth 1 -name "*.go" ! -name "*_test.go" -type f 2>/dev/null | \
        xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}' || echo "0"
}

# 递归显示目录树和代码统计
show_dir_tree() {
    local dir="$1"
    local prefix="$2"
    local is_last="$3"
    
    # 跳过排除的目录
    if [[ "$dir" =~ (vendor|\.git|tests) ]]; then
        return
    fi
    
    local dirname=$(basename "$dir")
    if [ "$dirname" = "." ]; then
        dirname="flow_sim"
    fi
    
    # 统计当前目录直接包含的 Go 文件（不包括子目录）
    local count=$(count_go_lines "$dir")
    
    # 获取子目录（只包含有 Go 文件的目录）
    local subdirs=()
    while IFS= read -r subdir; do
        # 检查子目录是否有 Go 文件（递归检查）
        local has_go=$(find "$subdir" -name "*.go" ! -name "*_test.go" -type f 2>/dev/null | head -1)
        if [ -n "$has_go" ]; then
            subdirs+=("$subdir")
        fi
    done < <(find "$dir" -maxdepth 1 -type d ! -path "$dir" 2>/dev/null | \
        grep -vE "(vendor|\.git|tests)" | sort)
    
    # 如果有代码或子目录，显示
    if [ "$count" != "0" ] || [ ${#subdirs[@]} -gt 0 ]; then
        local connector=""
        # 根目录不显示连接符，其他目录根据是否是最后一个子节点显示不同连接符
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
        
        # 计算子目录的前缀（用于缩进）
        local new_prefix=""
        if [ "$dir" = "." ]; then
            # 根目录的子目录，不需要额外前缀（它们会自己添加连接符）
            new_prefix=""
        else
            # 非根目录的子目录，需要添加缩进
            if [ "$is_last" = "true" ]; then
                # 最后一个子节点，使用空格缩进
                new_prefix="${prefix}    "
            else
                # 非最后一个子节点，使用竖线保持连接
                new_prefix="${prefix}│   "
            fi
        fi
        
        # 递归处理子目录
        local total=${#subdirs[@]}
        local idx=0
        for subdir in "${subdirs[@]}"; do
            idx=$((idx + 1))
            if [ $idx -eq $total ]; then
                show_dir_tree "$subdir" "$new_prefix" "true"
            else
                show_dir_tree "$subdir" "$new_prefix" "false"
            fi
        done
    fi
}

# 显示完整的目录树
show_dir_tree "." ""

echo ""

# 显示汇总
echo "=== 模块汇总 ==="
total=0

# 统计主要模块
for module in internal pkg framework web configs; do
    if [ -d "$module" ]; then
        count=$(find "$module" -name "*.go" ! -name "*_test.go" -type f 2>/dev/null | \
            xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}' || echo "0")
        if [ "$count" != "0" ]; then
            printf "  %-20s %6d 行\n" "$module/:" "$count"
            total=$((total + count))
        fi
    fi
done

# 统计根目录文件
if [ -f "main.go" ]; then
    main_count=$(wc -l < "main.go" 2>/dev/null || echo "0")
    if [ "$main_count" != "0" ]; then
        printf "  %-20s %6d 行\n" "main.go:" "$main_count"
        total=$((total + main_count))
    fi
fi

echo "  $(printf '%*s' 20 '总计:') $total 行"
echo ""

# 使用 cloc 显示详细统计
if command -v cloc &> /dev/null; then
    echo "=== 详细统计（cloc）==="
    cloc --exclude-dir=tests,vendor --exclude-ext=test.go,example.go . 2>/dev/null | grep -E "^Go|^SUM" | head -2
fi
