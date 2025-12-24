// Package instruction 提供 ChampSim 指令模型的 Go 实现
//
// 这个包定义了乱序执行 CPU 模型中使用的指令数据结构和类型。
// 主要包括：
// - 分支类型定义
// - 物理寄存器 ID
// - 特殊寄存器常量
package instruction

// BranchType 定义分支指令的类型
//
// ChampSim 通过分析指令的寄存器读写模式来识别分支类型。
// 这些类型对应 x86 架构的典型分支模式。
type BranchType int

const (
	// BranchDirectJump 直接跳转 (JMP target)
	// 特征: 写 IP，不读 SP、Flags，不读其他寄存器
	BranchDirectJump BranchType = iota

	// BranchIndirect 间接跳转 (JMP *reg)
	// 特征: 写 IP，读其他寄存器，不读 SP、IP、Flags
	BranchIndirect

	// BranchConditional 条件分支 (JZ, JNE 等)
	// 特征: 读 IP，写 IP，读 Flags 或其他寄存器
	BranchConditional

	// BranchDirectCall 直接调用 (CALL target)
	// 特征: 读写 SP，读写 IP，不读 Flags 和其他寄存器
	BranchDirectCall

	// BranchIndirectCall 间接调用 (CALL *reg)
	// 特征: 读写 SP，读写 IP，读其他寄存器
	BranchIndirectCall

	// BranchReturn 函数返回 (RET)
	// 特征: 读 SP，写 SP 和 IP，不读 IP
	BranchReturn

	// BranchOther 其他分支类型
	// 不符合以上分类的写 IP 指令
	BranchOther

	// NotBranch 非分支指令
	NotBranch
)

// String 返回分支类型的字符串表示
func (bt BranchType) String() string {
	switch bt {
	case BranchDirectJump:
		return "BRANCH_DIRECT_JUMP"
	case BranchIndirect:
		return "BRANCH_INDIRECT"
	case BranchConditional:
		return "BRANCH_CONDITIONAL"
	case BranchDirectCall:
		return "BRANCH_DIRECT_CALL"
	case BranchIndirectCall:
		return "BRANCH_INDIRECT_CALL"
	case BranchReturn:
		return "BRANCH_RETURN"
	case BranchOther:
		return "BRANCH_OTHER"
	case NotBranch:
		return "NOT_BRANCH"
	default:
		return "UNKNOWN"
	}
}

// IsBranch 返回该类型是否为分支指令
func (bt BranchType) IsBranch() bool {
	return bt != NotBranch
}

// PhysicalRegisterID 物理寄存器标识符
//
// 使用 int16 允许用 -1 表示无效寄存器。
// 物理寄存器由寄存器分配器 (Register Allocator) 管理。
type PhysicalRegisterID int16

const (
	// InvalidRegister 无效寄存器 ID
	InvalidRegister PhysicalRegisterID = -1
)

// IsValid 返回寄存器 ID 是否有效
func (id PhysicalRegisterID) IsValid() bool {
	return id >= 0
}

// 特殊寄存器常量 (用于分支类型识别)
//
// 这些常量对应 x86 架构的特殊寄存器编号。
// ChampSim 通过检查指令是否读写这些寄存器来识别分支类型。
const (
	// RegStackPointer 栈指针寄存器 (RSP/ESP)
	RegStackPointer = 6

	// RegFlags 标志寄存器 (RFLAGS/EFLAGS)
	RegFlags = 25

	// RegInstructionPointer 指令指针寄存器 (RIP/EIP)
	RegInstructionPointer = 26
)

// Trace 指令格式常量
const (
	// NumInstrDestinations 标准 trace 格式的目标寄存器/内存数量
	NumInstrDestinations = 2

	// NumInstrDestinationsSparc SPARC/CloudSuite 格式的目标数量
	NumInstrDestinationsSparc = 4

	// NumInstrSources 源寄存器/内存数量
	NumInstrSources = 4
)
