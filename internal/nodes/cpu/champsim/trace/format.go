// Package trace 提供 ChampSim trace 文件的读取功能
//
// ChampSim 使用二进制 trace 格式记录程序执行的指令序列。
// 每条指令包含 PC、分支信息、寄存器和内存操作数。
//
// Trace 格式支持两种变体：
// - input_instr: 标准格式 (2 个目标操作数)
// - cloudsuite_instr: CloudSuite 格式 (4 个目标操作数 + ASID)
package trace

import (
	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/instruction"
)

// InputInstr 定义标准 ChampSim trace 格式的指令
//
// 这是 ChampSim 最常用的 trace 格式，对应 trace_instruction.h 中的 input_instr。
// 每个字段都是固定大小，适合二进制读取。
//
// 布局 (64 字节总计):
//   - IP: 8 字节
//   - IsBranch, BranchTaken: 各 1 字节
//   - DestRegisters: 2 字节
//   - SrcRegisters: 4 字节
//   - DestMemory: 16 字节
//   - SrcMemory: 32 字节
type InputInstr struct {
	// IP 指令地址 (Program Counter)
	IP uint64

	// IsBranch 指示该指令是否为分支
	// 注意: ChampSim 会根据寄存器读写模式重新识别分支类型
	IsBranch uint8

	// BranchTaken 分支是否跳转 (仅对分支指令有效)
	BranchTaken uint8

	// DestRegisters 目标寄存器编号 (最多 2 个)
	// 0 表示无寄存器
	DestRegisters [instruction.NumInstrDestinations]uint8

	// SrcRegisters 源寄存器编号 (最多 4 个)
	// 0 表示无寄存器
	SrcRegisters [instruction.NumInstrSources]uint8

	// DestMemory 写内存地址 (最多 2 个)
	// 0 表示无内存操作
	DestMemory [instruction.NumInstrDestinations]uint64

	// SrcMemory 读内存地址 (最多 4 个)
	// 0 表示无内存操作
	SrcMemory [instruction.NumInstrSources]uint64
}

// CloudSuiteInstr 定义 CloudSuite trace 格式的指令
//
// CloudSuite 是一组云计算基准测试，其 trace 格式扩展了标准格式：
// - 支持 SPARC 架构的 4 个目标操作数
// - 包含 ASID (Address Space ID) 用于多地址空间模拟
//
// 布局 (74 字节总计):
//   - IP: 8 字节
//   - IsBranch, BranchTaken: 各 1 字节
//   - DestRegisters: 4 字节
//   - SrcRegisters: 4 字节
//   - DestMemory: 32 字节
//   - SrcMemory: 32 字节
//   - ASID: 2 字节
type CloudSuiteInstr struct {
	// IP 指令地址
	IP uint64

	// IsBranch 指示该指令是否为分支
	IsBranch uint8

	// BranchTaken 分支是否跳转
	BranchTaken uint8

	// DestRegisters 目标寄存器编号 (最多 4 个, SPARC)
	DestRegisters [instruction.NumInstrDestinationsSparc]uint8

	// SrcRegisters 源寄存器编号 (最多 4 个)
	SrcRegisters [instruction.NumInstrSources]uint8

	// DestMemory 写内存地址 (最多 4 个)
	DestMemory [instruction.NumInstrDestinationsSparc]uint64

	// SrcMemory 读内存地址 (最多 4 个)
	SrcMemory [instruction.NumInstrSources]uint64

	// ASID 地址空间标识符 [虚拟ASID, 物理ASID]
	// 用于支持虚拟化和多地址空间模拟
	ASID [2]uint8
}

// TraceFormat 定义 trace 文件的格式类型
type TraceFormat int

const (
	// FormatStandard 标准 input_instr 格式
	FormatStandard TraceFormat = iota

	// FormatCloudSuite CloudSuite 格式
	FormatCloudSuite
)

// String 返回格式的字符串表示
func (tf TraceFormat) String() string {
	switch tf {
	case FormatStandard:
		return "standard"
	case FormatCloudSuite:
		return "cloudsuite"
	default:
		return "unknown"
	}
}

// InstrSize 返回该格式下每条指令的字节大小
func (tf TraceFormat) InstrSize() int {
	switch tf {
	case FormatStandard:
		// unsafe.Sizeof(InputInstr{}) = 64
		return 8 + 1 + 1 + 2 + 4 + 16 + 32 // = 64
	case FormatCloudSuite:
		// unsafe.Sizeof(CloudSuiteInstr{}) = 74
		return 8 + 1 + 1 + 4 + 4 + 32 + 32 + 2 // = 84
	default:
		return 0
	}
}
