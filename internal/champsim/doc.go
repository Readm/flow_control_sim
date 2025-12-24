// Package champsim 提供 ChampSim CPU 模拟器的纯 Go 实现
//
// ChampSim 是一个 trace-based 的微架构模拟器，主要用于研究：
// - CPU 缓存层次结构
// - 分支预测
// - 预取算法
// - 内存系统性能
//
// # 架构概览
//
// 本包实现了完整的乱序执行 (Out-of-Order) CPU 模型：
//
//	+-------------+
//	| Trace File  | ──> InputInstr / CloudSuiteInstr
//	+-------------+
//	       │
//	       ▼
//	+-------------+
//	| TraceReader | ──> OOOModelInstr (带 InstrID)
//	+-------------+
//	       │
//	       ▼
//	+-------------+
//	|   O3_CPU    |
//	|             |
//	| ┌─────────┐ |
//	| │ Fetch   │ |
//	| ├─────────┤ |
//	| │ Decode  │ |
//	| ├─────────┤ |
//	| │Dispatch │ |
//	| ├─────────┤ |
//	| │Schedule │ |
//	| ├─────────┤ |
//	| │ Execute │ |
//	| ├─────────┤ |
//	| │   LSQ   │ | ──> 内存请求 ──> Framework Transaction
//	| ├─────────┤ |
//	| │ Retire  │ |
//	| └─────────┘ |
//	+-------------+
//
// # 包结构
//
//   - trace: Trace 文件读取和格式定义
//   - instruction: 指令数据结构 (OOOModelInstr, LSQEntry)
//   - cpu: CPU 核心逻辑 (O3_CPU, 流水线, LSQ, ROB, DIB)
//   - branch: 分支预测器
//   - btb: Branch Target Buffer
//   - integration: 与 flow_sim 框架的集成
//
// # 使用示例
//
//	import (
//	    "github.com/Readm/flow_sim/internal/champsim/integration"
//	    "github.com/Readm/flow_sim/internal/dataflow/transaction"
//	)
//
//	// 创建 CPU 激励源
//	incentive := integration.NewChampSimIncentive(
//	    "traces/600.perlbench_s.champsimtrace.xz",
//	    0, // CPU ID
//	    txnManager,
//	)
//
//	// 在仿真循环中调用
//	for cycle := uint64(0); cycle < maxCycles; cycle++ {
//	    if incentive.ShouldCreateTransaction(nodeID, cycle) {
//	        txn, _ := incentive.CreateTransaction(nodeID, cycle)
//	        network.Submit(txn)
//	    }
//	}
//
// # 与原版 ChampSim 的对应关系
//
//   - trace/format.go ↔ trace_instruction.h
//   - instruction/instruction.go ↔ instruction.h (ooo_model_instr)
//   - instruction/lsq_entry.go ↔ ooo_cpu.h (LSQ_ENTRY)
//   - cpu/o3_cpu.go ↔ ooo_cpu.h (O3_CPU)
//   - cpu/pipeline.go ↔ ooo_cpu.cc (各流水线阶段方法)
//
// # 参考文献
//
// ChampSim 论文:
// Gober, N., et al. (2022). The Championship Simulator: Architectural
// Simulation for Education and Competition. arXiv:2210.14324
//
// 官方仓库:
// https://github.com/ChampSim/ChampSim
package champsim
