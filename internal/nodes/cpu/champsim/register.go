package cpu

import (
	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/instruction"
)

// PhysicalRegister 物理寄存器
//
// 对应 ChampSim 的 physical_register 结构
type PhysicalRegister struct {
	// ArchRegIndex 对应的架构寄存器索引
	ArchRegIndex uint8

	// ProducingInstructionID 产生该寄存器值的指令 ID
	ProducingInstructionID uint64

	// Valid 数据是否有效（产生指令是否已 complete）
	// 当 Valid=false 时，依赖该寄存器的指令无法 schedule
	Valid bool

	// Busy 寄存器是否在流水线中使用
	Busy bool
}

// RegisterAllocator 物理寄存器分配器
//
// 完全对应 ChampSim 的 RegisterAllocator，实现寄存器重命名和依赖跟踪。
//
// 核心概念：
// - Frontend RAT: Dispatch 时使用，维护最新的架构→物理寄存器映射
// - Backend RAT: Retire 时更新，表示已提交的映射
// - 物理寄存器有 valid 标志，只有 valid=true 时依赖指令才能 schedule
type RegisterAllocator struct {
	// frontendRAT Frontend 寄存器分配表 [架构寄存器 -> 物理寄存器]
	// Dispatch 时使用，大小为 256（支持所有可能的 uint8 架构寄存器）
	frontendRAT [256]instruction.PhysicalRegisterID

	// backendRAT Backend 寄存器分配表 [架构寄存器 -> 物理寄存器]
	// Retire 时更新，表示已提交的映射
	backendRAT [256]instruction.PhysicalRegisterID

	// freeRegisters 空闲物理寄存器队列（FIFO）
	freeRegisters []instruction.PhysicalRegisterID

	// physicalRegisterFile 物理寄存器文件
	physicalRegisterFile []PhysicalRegister

	// numPhysical 物理寄存器总数
	numPhysical int
}

// InvalidPhysicalRegister 无效的物理寄存器 ID（用于 RAT 初始化）
const InvalidPhysicalRegister = instruction.PhysicalRegisterID(-1)

// NewRegisterAllocator 创建寄存器分配器
//
// 参数：
//   - numPhysical: 物理寄存器总数（典型值：128-256）
//
// 对应 ChampSim 的构造函数
func NewRegisterAllocator(numPhysical int) *RegisterAllocator {
	ra := &RegisterAllocator{
		numPhysical:          numPhysical,
		freeRegisters:        make([]instruction.PhysicalRegisterID, 0, numPhysical),
		physicalRegisterFile: make([]PhysicalRegister, numPhysical),
	}

	// 初始化空闲寄存器队列
	for i := 0; i < numPhysical; i++ {
		ra.freeRegisters = append(ra.freeRegisters, instruction.PhysicalRegisterID(i))
	}

	// 初始化 RAT 为无效映射
	for i := range ra.frontendRAT {
		ra.frontendRAT[i] = InvalidPhysicalRegister
		ra.backendRAT[i] = InvalidPhysicalRegister
	}

	return ra
}

// ==================== Rename 操作 ====================

// RenameDestRegister 重命名目标寄存器
//
// 在 Dispatch 阶段调用，为写操作分配新的物理寄存器。
//
// 参数：
//   - archReg: 架构寄存器编号
//   - producerID: 产生该寄存器值的指令 ID
//
// 返回：
//   - 分配的物理寄存器 ID
//
// 对应 ChampSim 的 rename_dest_register()
func (ra *RegisterAllocator) RenameDestRegister(archReg uint8, producerID uint64) instruction.PhysicalRegisterID {
	if len(ra.freeRegisters) == 0 {
		panic("RegisterAllocator: no free physical registers")
	}

	// 从空闲队列取出物理寄存器
	physReg := ra.freeRegisters[0]
	ra.freeRegisters = ra.freeRegisters[1:]

	// 更新 Frontend RAT
	ra.frontendRAT[archReg] = physReg

	// 设置物理寄存器属性
	ra.physicalRegisterFile[physReg] = PhysicalRegister{
		ArchRegIndex:           archReg,
		ProducingInstructionID: producerID,
		Valid:                  false, // 数据尚未产生
		Busy:                   true,  // 正在使用中
	}

	return physReg
}

// RenameSrcRegister 重命名源寄存器
//
// 在 Dispatch 阶段调用，查找架构寄存器对应的物理寄存器。
// 如果架构寄存器尚未映射（trace 从程序中间开始），则分配一个物理寄存器。
//
// 参数：
//   - archReg: 架构寄存器编号
//
// 返回：
//   - 物理寄存器 ID
//
// 对应 ChampSim 的 rename_src_register()
func (ra *RegisterAllocator) RenameSrcRegister(archReg uint8) instruction.PhysicalRegisterID {
	physReg := ra.frontendRAT[archReg]

	if physReg == InvalidPhysicalRegister {
		// 架构寄存器尚未映射（常见于 trace 从程序中间开始）
		// 分配一个物理寄存器，并假设其值已经有效
		if len(ra.freeRegisters) == 0 {
			panic("RegisterAllocator: no free physical registers for source")
		}

		physReg = ra.freeRegisters[0]
		ra.freeRegisters = ra.freeRegisters[1:]

		ra.frontendRAT[archReg] = physReg
		ra.backendRAT[archReg] = physReg // 假设最后一次写已提交

		ra.physicalRegisterFile[physReg] = PhysicalRegister{
			ArchRegIndex:           archReg,
			ProducingInstructionID: 0,
			Valid:                  true, // 假设值已有效
			Busy:                   true,
		}
	}

	return physReg
}

// ==================== Complete/Retire 操作 ====================

// CompleteDestRegister 标记物理寄存器数据为有效
//
// 在 Execute 完成后调用，表示寄存器的数据已经产生。
// 这会允许依赖该寄存器的指令进行 schedule。
//
// 参数：
//   - physReg: 物理寄存器 ID
//
// 对应 ChampSim 的 complete_dest_register()
func (ra *RegisterAllocator) CompleteDestRegister(physReg instruction.PhysicalRegisterID) {
	if int(physReg) >= len(ra.physicalRegisterFile) {
		return // 无效寄存器
	}
	ra.physicalRegisterFile[physReg].Valid = true
}

// RetireDestRegister 在 Retire 阶段更新 Backend RAT
//
// 更新 Backend RAT 到新的物理寄存器，并释放旧的物理寄存器。
//
// 参数：
//   - physReg: 新的物理寄存器 ID
//
// 对应 ChampSim 的 retire_dest_register()
func (ra *RegisterAllocator) RetireDestRegister(physReg instruction.PhysicalRegisterID) {
	if int(physReg) >= len(ra.physicalRegisterFile) {
		return // 无效寄存器
	}

	// 获取架构寄存器索引
	archReg := ra.physicalRegisterFile[physReg].ArchRegIndex

	// 获取旧的物理寄存器
	oldPhysReg := ra.backendRAT[archReg]

	// 更新 Backend RAT
	ra.backendRAT[archReg] = physReg

	// 释放旧的物理寄存器
	if oldPhysReg != InvalidPhysicalRegister {
		ra.FreeRegister(oldPhysReg)
	}
}

// FreeRegister 释放物理寄存器
//
// 清空寄存器状态并加入空闲队列。
//
// 参数：
//   - physReg: 物理寄存器 ID
//
// 对应 ChampSim 的 free_register()
func (ra *RegisterAllocator) FreeRegister(physReg instruction.PhysicalRegisterID) {
	if int(physReg) >= len(ra.physicalRegisterFile) {
		return // 无效寄存器
	}

	// 清空寄存器状态
	ra.physicalRegisterFile[physReg] = PhysicalRegister{
		ArchRegIndex:           255, // 无效架构寄存器
		ProducingInstructionID: 0,
		Valid:                  false,
		Busy:                   false,
	}

	// 加入空闲队列
	ra.freeRegisters = append(ra.freeRegisters, physReg)
}

// ==================== 查询操作 ====================

// IsValid 检查物理寄存器数据是否有效
//
// 参数：
//   - physReg: 物理寄存器 ID
//
// 返回：
//   - true 如果寄存器数据有效
//
// 对应 ChampSim 的 isValid()
func (ra *RegisterAllocator) IsValid(physReg instruction.PhysicalRegisterID) bool {
	if int(physReg) >= len(ra.physicalRegisterFile) || physReg == InvalidPhysicalRegister {
		return true // 无效寄存器视为有效（不阻塞）
	}
	return ra.physicalRegisterFile[physReg].Valid
}

// IsAllocated 检查架构寄存器是否已分配
//
// 参数：
//   - archReg: 架构寄存器编号
//
// 返回：
//   - true 如果架构寄存器已有物理寄存器映射
//
// 对应 ChampSim 的 isAllocated()
func (ra *RegisterAllocator) IsAllocated(archReg uint8) bool {
	return ra.frontendRAT[archReg] != InvalidPhysicalRegister
}

// CountRegDependencies 计算指令的寄存器依赖数量
//
// 统计源寄存器中有多少个数据尚未有效（Valid=false）。
//
// 参数：
//   - instr: 指令
//
// 返回：
//   - 未就绪的源寄存器数量
//
// 对应 ChampSim 的 count_reg_dependencies()
func (ra *RegisterAllocator) CountRegDependencies(instr *instruction.OOOModelInstr) int {
	count := 0
	for _, reg := range instr.SrcRegisters {
		if !reg.IsValid() {
			continue // 跳过无效寄存器
		}
		if !ra.IsValid(reg) {
			count++
		}
	}
	return count
}

// ==================== 其他操作 ====================

// ResetFrontendRAT 重置 Frontend RAT 为 Backend RAT
//
// 用于分支预测错误后恢复正确状态。
//
// 对应 ChampSim 的 reset_frontend_RAT()
func (ra *RegisterAllocator) ResetFrontendRAT() {
	copy(ra.frontendRAT[:], ra.backendRAT[:])
	// TODO: 如果实现了错误路径，需要释放错误路径分配的寄存器
}

// AvailableCount 返回空闲寄存器数量
func (ra *RegisterAllocator) AvailableCount() int {
	return len(ra.freeRegisters)
}

// TotalCount 返回物理寄存器总数
func (ra *RegisterAllocator) TotalCount() int {
	return ra.numPhysical
}

// AllocatedCount 返回已分配的寄存器数量
func (ra *RegisterAllocator) AllocatedCount() int {
	return ra.numPhysical - len(ra.freeRegisters)
}

// Reset 重置分配器
func (ra *RegisterAllocator) Reset() {
	// 清空空闲队列
	ra.freeRegisters = ra.freeRegisters[:0]

	// 重新初始化
	for i := 0; i < ra.numPhysical; i++ {
		ra.freeRegisters = append(ra.freeRegisters, instruction.PhysicalRegisterID(i))
		ra.physicalRegisterFile[i] = PhysicalRegister{}
	}

	// 清空 RAT
	for i := range ra.frontendRAT {
		ra.frontendRAT[i] = InvalidPhysicalRegister
		ra.backendRAT[i] = InvalidPhysicalRegister
	}
}
