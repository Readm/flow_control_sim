#!/bin/bash
# 运行 ChampSim 扩展性基准测试并输出结果

set -e

# 运行基准测试并记录结果到 stdout
# 注意：我们使用 -tags profile 来获取实际周期和效率数据
# BENCH_CORES 环境变量已被 runner.go 自动处理 (默认 1..NumCPU)
go test -bench=. -benchmem -tags profile -timeout 20m -v ./internal/benchmarks/...

echo "[FlowSim] Benchmark completed."
