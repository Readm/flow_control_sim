// Package instruction 提供 ChampSim 乱序 CPU 模型的指令定义
package instruction

import (
	"fmt"
)

// OOOModelInstr 表示乱序执行 CPU 模型中的一条指令
//
// 这个结构对应 ChampSim 的 ooo_model_instr，包含了指令在流水线中
// 执行所需的所有信息：基本属性、分支信息、流水线状态、操作数和依赖关系。
//
// 流水线阶段标记：
//   DIBChecked -> FetchIssued -> FetchCompleted -> Decoded ->
//   Scheduled -> Executed -> Completed -> (退休时从 ROB 移除)
type OOOModelInstr struct {
	// ==================== 基本标识 ====================

	// InstrID 全局唯一的指令 ID (程序顺序)
	// 用于跟踪指令的程序顺序，即使在乱序执行中也能保持
	InstrID uint64

	// IP 指令地址 (Program Counter)
	IP uint64

	// CPUID 执行该指令的 CPU 核心 ID
	CPUID uint8

	// ASID 地址空间标识符 [虚拟ASID, 物理ASID]
	// 用于多地址空间和虚拟化支持
	ASID [2]uint8

	// ==================== 时间信息 ====================

	// ReadyTime 指令准备好执行的时间 (周期数)
	// 当所有依赖解除后设置
	ReadyTime uint64

	// ==================== 分支信息 ====================

	// IsBranch 该指令是否为分支
	IsBranch bool

	// BranchTaken 分支是否跳转 (从 trace 读取或实际执行结果)
	BranchTaken bool

	// BranchPrediction 分支预测结果
	BranchPrediction bool

	// BranchMispredicted 分支是否预测错误
	// 即使方向预测正确，如果目标地址错误也算预测错误
	BranchMispredicted bool

	// BranchType 分支指令的类型
	BranchType BranchType

	// BranchTarget 分支目标地址
	// 对于直接分支，这是固定的；对于间接分支，运行时确定
	BranchTarget uint64

	// ==================== 流水线状态 ====================

	// DIBChecked 是否已检查 DIB (Decoded Instruction Buffer)
	DIBChecked bool

	// FetchIssued 是否已发出取指请求
	FetchIssued bool

	// FetchCompleted 取指是否完成
	FetchCompleted bool

	// Decoded 是否已译码
	Decoded bool

	// Scheduled 是否已调度到执行单元
	Scheduled bool

	// Executed 是否已执行完成
	Executed bool

	// Completed 所有操作（包括内存）是否完成
	Completed bool

	// ==================== 操作数 ====================

	// DestRegisters 目标物理寄存器 ID 列表
	// 由寄存器重命名 (Register Renaming) 分配
	DestRegisters []PhysicalRegisterID

	// SrcRegisters 源物理寄存器 ID 列表
	SrcRegisters []PhysicalRegisterID

	// DestMemory 写内存地址列表 (store 操作)
	DestMemory []uint64

	// SrcMemory 读内存地址列表 (load 操作)
	SrcMemory []uint64

	// ==================== 依赖跟踪 ====================

	// CompletedMemOps 已完成的内存操作数量
	// 当等于总内存操作数时，指令可以标记为 Completed
	CompletedMemOps int

	// NumRegDependent 有多少指令依赖于我的寄存器结果
	NumRegDependent int

	// RegistersInstrsDependOnMe 寄存器依赖于我的指令列表
	// 当我的寄存器结果就绪时，需要唤醒这些指令
	RegistersInstrsDependOnMe []*OOOModelInstr
}

// NewOOOModelInstrFromInput 从标准 trace 格式创建 OOOModelInstr
//
// 这个构造函数会：
// 1. 复制基本字段
// 2. 过滤掉值为 0 的寄存器和内存地址
// 3. 通过寄存器读写模式识别分支类型
//
// 参数：
//   - cpuID: CPU 核心 ID
//   - ip: 指令地址
//   - isBranch: 是否为分支 (来自 trace，可能不准确)
//   - branchTaken: 分支是否跳转
//   - destRegs: 目标寄存器数组
//   - srcRegs: 源寄存器数组
//   - destMem: 目标内存地址数组
//   - srcMem: 源内存地址数组
func NewOOOModelInstrFromInput(
	cpuID uint8,
	ip uint64,
	isBranch uint8,
	branchTaken uint8,
	destRegs []uint8,
	srcRegs []uint8,
	destMem []uint64,
	srcMem []uint64,
) *OOOModelInstr {
	instr := &OOOModelInstr{
		IP:           ip,
		CPUID:        cpuID,
		ASID:         [2]uint8{cpuID, cpuID}, // 默认 ASID 与 CPU ID 相同
		IsBranch:     isBranch != 0,
		BranchTaken:  branchTaken != 0,
		BranchType:   NotBranch,
		BranchTarget: 0,
		ReadyTime:    0, // 初始时间为 0，会在流水线中更新
	}

	// 过滤并复制寄存器 (0 表示无寄存器)
	for _, reg := range destRegs {
		if reg != 0 {
			instr.DestRegisters = append(instr.DestRegisters, PhysicalRegisterID(reg))
		}
	}
	for _, reg := range srcRegs {
		if reg != 0 {
			instr.SrcRegisters = append(instr.SrcRegisters, PhysicalRegisterID(reg))
		}
	}

	// 过滤并复制内存地址 (0 表示无内存操作)
	for _, addr := range destMem {
		if addr != 0 {
			instr.DestMemory = append(instr.DestMemory, addr)
		}
	}
	for _, addr := range srcMem {
		if addr != 0 {
			instr.SrcMemory = append(instr.SrcMemory, addr)
		}
	}

	// 识别分支类型 (基于寄存器读写模式)
	instr.identifyBranchType()

	return instr
}

// NewOOOModelInstrFromCloudSuite 从 CloudSuite trace 格式创建 OOOModelInstr
func NewOOOModelInstrFromCloudSuite(
	ip uint64,
	isBranch uint8,
	branchTaken uint8,
	destRegs []uint8,
	srcRegs []uint8,
	destMem []uint64,
	srcMem []uint64,
	asid [2]uint8,
) *OOOModelInstr {
	instr := &OOOModelInstr{
		IP:           ip,
		CPUID:        asid[0], // CloudSuite 使用 ASID[0] 作为 CPU ID
		ASID:         asid,
		IsBranch:     isBranch != 0,
		BranchTaken:  branchTaken != 0,
		BranchType:   NotBranch,
		BranchTarget: 0,
		ReadyTime:    0,
	}

	// 过滤并复制寄存器和内存 (同上)
	for _, reg := range destRegs {
		if reg != 0 {
			instr.DestRegisters = append(instr.DestRegisters, PhysicalRegisterID(reg))
		}
	}
	for _, reg := range srcRegs {
		if reg != 0 {
			instr.SrcRegisters = append(instr.SrcRegisters, PhysicalRegisterID(reg))
		}
	}
	for _, addr := range destMem {
		if addr != 0 {
			instr.DestMemory = append(instr.DestMemory, addr)
		}
	}
	for _, addr := range srcMem {
		if addr != 0 {
			instr.SrcMemory = append(instr.SrcMemory, addr)
		}
	}

	instr.identifyBranchType()

	return instr
}

// identifyBranchType 通过寄存器读写模式识别分支类型
//
// ChampSim 使用启发式规则根据指令对特殊寄存器的访问模式来识别分支：
// - SP (Stack Pointer): 用于 call/ret
// - IP (Instruction Pointer): 所有分支都会写 IP
// - Flags: 条件分支读 Flags
// - 其他寄存器: 间接分支读其他寄存器
func (instr *OOOModelInstr) identifyBranchType() {
	// 检查寄存器访问模式
	writesSP := instr.hasDestReg(RegStackPointer)
	writesIP := instr.hasDestReg(RegInstructionPointer)
	readsSP := instr.hasSrcReg(RegStackPointer)
	readsIP := instr.hasSrcReg(RegInstructionPointer)
	readsFlags := instr.hasSrcReg(RegFlags)
	readsOther := instr.hasSrcRegOtherThan(RegStackPointer, RegInstructionPointer, RegFlags)

	// 应用分支识别规则
	switch {
	case !readsSP && !readsFlags && writesIP && !readsOther:
		// 直接跳转: JMP target
		instr.IsBranch = true
		instr.BranchTaken = true
		instr.BranchType = BranchDirectJump

	case !readsSP && !readsIP && !readsFlags && writesIP && readsOther:
		// 间接跳转: JMP *reg
		instr.IsBranch = true
		instr.BranchTaken = true
		instr.BranchType = BranchIndirect

	case !readsSP && readsIP && !writesSP && writesIP && (readsFlags || readsOther):
		// 条件分支: JZ, JNE 等
		instr.IsBranch = true
		// BranchTaken 保持 trace 中的值
		instr.BranchType = BranchConditional

	case readsSP && readsIP && writesSP && writesIP && !readsFlags && !readsOther:
		// 直接调用: CALL target
		instr.IsBranch = true
		instr.BranchTaken = true
		instr.BranchType = BranchDirectCall

	case readsSP && readsIP && writesSP && writesIP && !readsFlags && readsOther:
		// 间接调用: CALL *reg
		instr.IsBranch = true
		instr.BranchTaken = true
		instr.BranchType = BranchIndirectCall

	case readsSP && !readsIP && writesSP && writesIP:
		// 返回: RET
		instr.IsBranch = true
		instr.BranchTaken = true
		instr.BranchType = BranchReturn

	case writesIP:
		// 其他写 IP 的指令
		instr.IsBranch = true
		// BranchTaken 保持 trace 中的值
		instr.BranchType = BranchOther

	default:
		// 非分支指令
		instr.BranchTaken = false
		instr.BranchType = NotBranch
	}
}

// hasDestReg 检查是否写某个寄存器
func (instr *OOOModelInstr) hasDestReg(reg uint8) bool {
	for _, r := range instr.DestRegisters {
		if uint8(r) == reg {
			return true
		}
	}
	return false
}

// hasSrcReg 检查是否读某个寄存器
func (instr *OOOModelInstr) hasSrcReg(reg uint8) bool {
	for _, r := range instr.SrcRegisters {
		if uint8(r) == reg {
			return true
		}
	}
	return false
}

// hasSrcRegOtherThan 检查是否读除了指定寄存器之外的其他寄存器
func (instr *OOOModelInstr) hasSrcRegOtherThan(excludeRegs ...uint8) bool {
	for _, r := range instr.SrcRegisters {
		isExcluded := false
		for _, ex := range excludeRegs {
			if uint8(r) == ex {
				isExcluded = true
				break
			}
		}
		if !isExcluded {
			return true
		}
	}
	return false
}

// NumMemOps 返回该指令的内存操作总数 (load + store)
func (instr *OOOModelInstr) NumMemOps() int {
	return len(instr.DestMemory) + len(instr.SrcMemory)
}

// HasMemOp 返回该指令是否有内存操作
func (instr *OOOModelInstr) HasMemOp() bool {
	return instr.NumMemOps() > 0
}

// IsLoad 返回该指令是否包含 load 操作
func (instr *OOOModelInstr) IsLoad() bool {
	return len(instr.SrcMemory) > 0
}

// IsStore 返回该指令是否包含 store 操作
func (instr *OOOModelInstr) IsStore() bool {
	return len(instr.DestMemory) > 0
}

// String 返回指令的字符串表示 (用于调试)
func (instr *OOOModelInstr) String() string {
	return fmt.Sprintf("Instr[ID=%d, IP=0x%x, Type=%s, MemOps=%d, Ready=%d]",
		instr.InstrID, instr.IP, instr.BranchType, instr.NumMemOps(), instr.ReadyTime)
}

// ProgramOrder 比较两条指令的程序顺序
// 返回 true 如果 instr 在程序顺序上早于 other
func (instr *OOOModelInstr) ProgramOrder(other *OOOModelInstr) bool {
	return instr.InstrID < other.InstrID
}
