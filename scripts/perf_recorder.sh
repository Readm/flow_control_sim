#!/bin/bash
# 运行 ChampSim 扩展性基准测试并输出结果

set -e

# 进入基准测试目录
cd internal/champsim/flowsim

echo "[FlowSim] Starting Benchmark_ChampSim_64CPU..."

# 运行基准测试并记录结果到 stdout
# 注意：我们使用 -tags profile 来获取实际周期和效率数据
# BENCH_CORES=1,2 适配 GitHub CI 的 2 核环境
BENCH_CORES=1,2 go test -bench=Benchmark_ChampSim_64CPU -benchmem -tags profile -timeout 20m .

echo "[FlowSim] Benchmark completed."
