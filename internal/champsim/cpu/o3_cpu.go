package cpu

import (
	"github.com/Readm/flow_sim/internal/champsim/instruction"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	compcache "github.com/Readm/flow_sim/internal/components/cache"
)

// O3CPU Out-of-Order CPU 模型
//
// 实现完整的乱序执行流水线：
//  1. Fetch: 从 trace 读取指令（检查 DIB）
//  2. Decode: 译码和寄存器重命名
//  3. Dispatch: 分发到保留站
//  4. Schedule: 选择就绪指令执行
//  5. Execute: 执行和内存操作
//  6. Retire: 按序退休
type O3CPU struct {
	// ==================== 核心组件 ====================

	// traceReader Trace 文件读取器
	traceReader trace.TraceReader

	// rob Reorder Buffer（重排序缓冲区）
	rob *ReorderBuffer

	// lsq Load-Store Queue
	lsq *LoadStoreQueue

	// regAlloc 寄存器分配器
	regAlloc *RegisterAllocator

	// dib Decoded Instruction Buffer
	dib *DIB

	// ==================== 内存层次 ====================

	// l1dCache L1 Data Cache
	// 在集成模式下使用，standalone模式下为nil
	l1dCache interface {
		Access(addr uint64, vaddr uint64, instrID uint64, accessType compcache.AccessType, cycle uint64) (hit bool, readyCycle uint64, mshrIndex int)
		HandleFill(addr uint64, data uint64, cycle uint64) bool
		SetStandaloneMode(standalone bool)
		GetStats() interface{}
	}

	// ==================== 流水线缓冲区 ====================

	// fetchQueue Fetch 队列（从 trace 读取的指令）
	fetchQueue []*instruction.OOOModelInstr

	// decodeQueue Decode 队列（已译码的指令）
	decodeQueue []*instruction.OOOModelInstr

	// ==================== 配置参数 ====================

	// config CPU 配置
	config O3CPUConfig

	// ==================== 运行时状态 ====================

	// currentCycle 当前周期数
	currentCycle uint64

	// instrCounter 指令计数器（用于生成唯一 ID）
	instrCounter uint64

	// warmupInstructions 预热指令数（统计前跳过）
	warmupInstructions uint64

	// simulationInstructions 仿真指令数（统计）
	simulationInstructions uint64

	// standaloneMode 独立模式（自动完成内存操作，不等待框架）
	standaloneMode bool

	// ==================== 待处理的内存操作 ====================

	// pendingLoads 待处理的load操作列表
	// 跟踪每个load的完成时间，用于非standalone模式
	pendingLoads []PendingMemOp

	// pendingStores 待处理的store操作列表
	pendingStores []PendingMemOp

	// ==================== 统计信息 ====================

	// stats CPU 统计信息
	stats O3CPUStats
}

// PendingMemOp 待处理的内存操作
type PendingMemOp struct {
	InstrID    uint64 // 指令ID
	ReadyCycle uint64 // 数据就绪周期
}

// O3CPUConfig CPU 配置参数
type O3CPUConfig struct {
	// ==================== 流水线宽度 ====================

	// FetchWidth 每周期 Fetch 的指令数（典型：4-6）
	FetchWidth int

	// DecodeWidth 每周期 Decode 的指令数（典型：4-6）
	DecodeWidth int

	// DispatchWidth 每周期 Dispatch 的指令数（典型：4-6）
	DispatchWidth int

	// ScheduleWidth 每周期 Schedule 的指令数（典型：8-10）
	ScheduleWidth int

	// ExecuteWidth 每周期 Execute 的指令数（典型：8-10）
	ExecuteWidth int

	// RetireWidth 每周期 Retire 的指令数（典型：4-6）
	RetireWidth int

	// ==================== 缓冲区大小 ====================

	// FetchQueueSize Fetch 队列大小
	FetchQueueSize int

	// DecodeQueueSize Decode 队列大小
	DecodeQueueSize int

	// ROBSize ROB 大小
	ROBSize int

	// LQSize Load Queue 大小
	LQSize int

	// SQSize Store Queue 大小
	SQSize int

	// PhysicalRegisters 物理寄存器数量
	PhysicalRegisters int

	// DIBSize DIB 大小
	DIBSize int

	// ==================== 延迟参数 ====================

	// FetchLatency Fetch 延迟（周期）
	FetchLatency uint64

	// DecodeLatency Decode 延迟（周期）
	DecodeLatency uint64

	// DispatchLatency Dispatch 延迟（周期）
	DispatchLatency uint64

	// ExecuteLatency 执行延迟（周期）
	ExecuteLatency uint64
}

// O3CPUStats CPU 统计信息
type O3CPUStats struct {
	// 总周期数
	TotalCycles uint64

	// 总指令数
	TotalInstructions uint64

	// IPC (Instructions Per Cycle)
	IPC float64

	// 各阶段停顿
	FetchStalls    uint64
	DecodeStalls   uint64
	DispatchStalls uint64
	ExecuteStalls  uint64

	// 分支预测
	BranchMispredictions uint64
	TotalBranches        uint64
}

// NewO3CPU 创建新的 O3 CPU
func NewO3CPU(traceReader trace.TraceReader, config O3CPUConfig) *O3CPU {
	return &O3CPU{
		traceReader:    traceReader,
		rob:            NewReorderBuffer(config.ROBSize),
		lsq:            NewLoadStoreQueue(config.LQSize, config.SQSize),
		regAlloc:       NewRegisterAllocator(config.PhysicalRegisters),
		dib:            NewDIB(config.DIBSize, DefaultDIBShift),
		fetchQueue:     make([]*instruction.OOOModelInstr, 0, config.FetchQueueSize),
		decodeQueue:    make([]*instruction.OOOModelInstr, 0, config.DecodeQueueSize),
		config:         config,
		currentCycle:   0,
		instrCounter:   0,
		stats:          O3CPUStats{},
		standaloneMode: true, // 默认为独立模式
	}
}

// SetStandaloneMode 设置独立模式
//
// standaloneMode=true: 内存操作自动完成（用于测试）
// standaloneMode=false: 内存操作等待框架响应（用于集成）
func (cpu *O3CPU) SetStandaloneMode(standalone bool) {
	cpu.standaloneMode = standalone
	// 同时设置Cache的standalone模式
	if cpu.l1dCache != nil {
		cpu.l1dCache.SetStandaloneMode(standalone)
	}
}

// SetL1DCache 设置 L1D Cache
//
// 参数：
// - cache: 实现了Cache接口的对象（通常是*cache.SetAssociativeCache）
func (cpu *O3CPU) SetL1DCache(cache interface {
	Access(addr uint64, vaddr uint64, instrID uint64, accessType compcache.AccessType, cycle uint64) (hit bool, readyCycle uint64, mshrIndex int)
	HandleFill(addr uint64, data uint64, cycle uint64) bool
	SetStandaloneMode(standalone bool)
	GetStats() interface{}
}) {
	cpu.l1dCache = cache
	// 同步standalone模式
	if cache != nil {
		cache.SetStandaloneMode(cpu.standaloneMode)
	}
}

// DefaultO3CPUConfig 返回默认配置（基于 Intel Skylake）
func DefaultO3CPUConfig() O3CPUConfig {
	return O3CPUConfig{
		// 流水线宽度
		FetchWidth:    6,
		DecodeWidth:   6,
		DispatchWidth: 6,
		ScheduleWidth: 10,
		ExecuteWidth:  10,
		RetireWidth:   4,

		// 缓冲区大小
		FetchQueueSize:    64,
		DecodeQueueSize:   64,
		ROBSize:           DefaultROBSize,
		LQSize:            DefaultLQSize,
		SQSize:            DefaultSQSize,
		PhysicalRegisters: 180, // Skylake: 180 integer registers
		DIBSize:           DefaultDIBSize,

		// 延迟
		FetchLatency:    1,
		DecodeLatency:   1,
		DispatchLatency: 1,
		ExecuteLatency:  1,
	}
}

// ==================== 主循环 ====================

// Tick 执行一个时钟周期
//
// 执行顺序完全对应 ChampSim 的 O3_CPU::operate()：
// 1. Retire: 退休已完成的指令
// 2. Complete: 标记已执行指令的寄存器为有效
// 3. Execute: 执行已调度的指令
// 4. Schedule: 调度就绪的指令
// 5. Dispatch: 分发指令到保留站
// 6. Decode: 译码指令
// 7. Fetch: 从 trace 读取指令
func (cpu *O3CPU) Tick() {
	cpu.currentCycle++

	// 按照 ChampSim 的顺序执行流水线阶段
	cpu.retire()                      // retire_rob()
	cpu.processPendingMemOps()        // 处理待处理的内存操作（非standalone模式）
	cpu.completeInflightInstruction() // complete_inflight_instruction()
	cpu.execute()                     // execute_instruction()
	cpu.schedule()                    // schedule_instruction()
	// handle_memory_return() 由集成框架处理
	// operate_lsq() 已集成在 execute 中
	cpu.dispatch() // dispatch_instruction()
	cpu.decode()   // decode_instruction()
	cpu.fetch()    // fetch_instruction()

	// 更新统计
	cpu.stats.TotalCycles++
}

// ==================== Fetch 阶段 ====================

// fetch 从 trace 读取指令
//
// 流程：
// 1. 检查 fetchQueue 是否有空间
// 2. 检查 ROB 是否有空间
// 3. 检查 DIB（如果命中，跳过实际 fetch）
// 4. 从 traceReader 读取指令
// 5. 设置指令 ID 和状态
// 6. 添加到 fetchQueue
func (cpu *O3CPU) fetch() {
	// 检查 fetchQueue 是否已满
	if len(cpu.fetchQueue) >= cpu.config.FetchQueueSize {
		cpu.stats.FetchStalls++
		return
	}

	// 检查 ROB 是否有空间
	if cpu.rob.IsFull() {
		cpu.stats.FetchStalls++
		return
	}

	// 每周期最多 fetch FetchWidth 条指令
	fetchCount := 0
	for fetchCount < cpu.config.FetchWidth {
		// 检查队列空间
		if len(cpu.fetchQueue) >= cpu.config.FetchQueueSize {
			break
		}

		// 从 trace 读取指令
		instr, err := cpu.traceReader.ReadInstruction()
		if err != nil {
			// trace 读取结束或错误
			break
		}

		// 设置指令 ID
		instr.InstrID = cpu.instrCounter
		cpu.instrCounter++

		// 设置就绪时间
		instr.ReadyTime = cpu.currentCycle + cpu.config.FetchLatency

		// 标记为已 Fetch
		instr.FetchIssued = true

		// 添加到 fetchQueue
		cpu.fetchQueue = append(cpu.fetchQueue, instr)
		fetchCount++

		// 如果遇到分支，这一周期停止 fetch
		if instr.IsBranch {
			break
		}
	}
}

// ==================== Decode 阶段 ====================

// decode 译码指令
//
// 流程：
// 1. 从 fetchQueue 取出指令
// 2. 检查是否到达就绪时间
// 3. 执行寄存器重命名
// 4. 添加到 decodeQueue
func (cpu *O3CPU) decode() {
	// 检查 decodeQueue 是否已满
	if len(cpu.decodeQueue) >= cpu.config.DecodeQueueSize {
		cpu.stats.DecodeStalls++
		return
	}

	// 每周期最多 decode DecodeWidth 条指令
	decodeCount := 0
	for decodeCount < cpu.config.DecodeWidth && len(cpu.fetchQueue) > 0 {
		// 检查队列空间
		if len(cpu.decodeQueue) >= cpu.config.DecodeQueueSize {
			break
		}

		// 获取队首指令
		instr := cpu.fetchQueue[0]

		// 检查是否到达就绪时间
		if cpu.currentCycle < instr.ReadyTime {
			break
		}

		// 设置 decode 完成时间
		instr.ReadyTime = cpu.currentCycle + cpu.config.DecodeLatency
		instr.Decoded = true

		// 移动到 decodeQueue
		cpu.decodeQueue = append(cpu.decodeQueue, instr)
		cpu.fetchQueue = cpu.fetchQueue[1:]

		decodeCount++
	}
}

// ==================== Dispatch 阶段 ====================

// dispatch 分发指令到 ROB
//
// 完全对应 ChampSim 的 dispatch_instruction()
//
// 流程：
// 1. 从 decodeQueue 取出指令
// 2. 检查 ROB 是否有空间
// 3. 检查 LSQ 是否有空间（如果是 load/store）
// 4. 添加到 ROB 和 LSQ
// 5. 调用 doMemoryScheduling（添加内存操作到 LSQ）
func (cpu *O3CPU) dispatch() {
	// 每周期最多 dispatch DispatchWidth 条指令
	dispatchCount := 0
	for dispatchCount < cpu.config.DispatchWidth && len(cpu.decodeQueue) > 0 {
		// 获取队首指令
		instr := cpu.decodeQueue[0]

		// 检查是否到达就绪时间
		if cpu.currentCycle < instr.ReadyTime {
			break
		}

		// 检查 ROB 是否有空间
		if cpu.rob.IsFull() {
			cpu.stats.DispatchStalls++
			break
		}

		// 检查 LSQ 是否有空间（如果是内存操作）
		if instr.IsLoad() && cpu.lsq.IsLoadQueueFull() {
			cpu.stats.DispatchStalls++
			break
		}
		if instr.IsStore() && cpu.lsq.IsStoreQueueFull() {
			cpu.stats.DispatchStalls++
			break
		}

		// 添加到 ROB
		if err := cpu.rob.Add(instr); err != nil {
			cpu.stats.DispatchStalls++
			break
		}

		// 调用 doMemoryScheduling（对应 ChampSim）
		cpu.doMemoryScheduling(instr)

		// 设置调度延迟（对应 ChampSim 的 SCHEDULING_LATENCY）
		instr.ReadyTime = cpu.currentCycle + cpu.config.DispatchLatency

		// 从 decodeQueue 移除
		cpu.decodeQueue = cpu.decodeQueue[1:]

		dispatchCount++
	}
}

// doMemoryScheduling 处理内存操作的调度
//
// 对应 ChampSim 的 do_memory_scheduling()
func (cpu *O3CPU) doMemoryScheduling(instr *instruction.OOOModelInstr) {
	// 如果是 load，添加到 LSQ
	if instr.IsLoad() {
		for _, addr := range instr.SrcMemory {
			entry := NewLSQEntry(instr.InstrID, addr, instr.IP, instr.ASID)
			entry.ReadyTime = cpu.currentCycle + cpu.config.DispatchLatency
			cpu.lsq.AddLoad(entry)
		}
	}

	// 如果是 store，添加到 LSQ
	if instr.IsStore() {
		for _, addr := range instr.DestMemory {
			entry := NewLSQEntry(instr.InstrID, addr, instr.IP, instr.ASID)
			entry.ReadyTime = cpu.currentCycle + cpu.config.DispatchLatency
			cpu.lsq.AddStore(entry)
		}
	}
}

// ==================== Schedule 阶段 ====================

// schedule 调度就绪指令执行
//
// 完全对应 ChampSim 的 schedule_instruction()
//
// 流程：
// 1. 从 ROB 扫描未调度的指令
// 2. 检查物理寄存器是否足够
// 3. 调用 doScheduling() 进行寄存器重命名
// 4. 标记为 Scheduled
func (cpu *O3CPU) schedule() {
	scheduleCount := 0
	searchCount := 0

	// 扫描整个 ROB（对应 ChampSim 的 search_bw）
	for i := 0; i < cpu.rob.Size() && searchCount < cpu.config.ScheduleWidth; i++ {
		instr := cpu.rob.PeekAt(i)
		if instr == nil {
			continue
		}

		// 检查物理寄存器是否足够
		// 需要分配：未分配的源寄存器 + 所有目标寄存器
		sourcesToAllocate := 0
		for _, srcReg := range instr.SrcRegisters {
			if srcReg.IsValid() && !cpu.regAlloc.IsAllocated(uint8(srcReg)) {
				sourcesToAllocate++
			}
		}

		destCount := 0
		for _, destReg := range instr.DestRegisters {
			if destReg.IsValid() {
				destCount++
			}
		}

		if cpu.regAlloc.AvailableCount() < (sourcesToAllocate + destCount) {
			// 物理寄存器不足，停止调度
			break
		}

		// 检查是否未调度且已就绪
		if !instr.Scheduled && cpu.currentCycle >= instr.ReadyTime {
			cpu.doScheduling(instr)
			scheduleCount++
		}

		// 统计未执行的指令（用于带宽限制）
		if !instr.Executed {
			searchCount++
		}
	}
}

// doScheduling 执行寄存器重命名
//
// 完全对应 ChampSim 的 do_scheduling()
func (cpu *O3CPU) doScheduling(instr *instruction.OOOModelInstr) {
	// 重命名源寄存器
	for i := range instr.SrcRegisters {
		if instr.SrcRegisters[i].IsValid() {
			instr.SrcRegisters[i] = cpu.regAlloc.RenameSrcRegister(uint8(instr.SrcRegisters[i]))
		}
	}

	// 重命名目标寄存器
	for i := range instr.DestRegisters {
		if instr.DestRegisters[i].IsValid() {
			instr.DestRegisters[i] = cpu.regAlloc.RenameDestRegister(uint8(instr.DestRegisters[i]), instr.InstrID)
		}
	}

	// 标记为已调度
	instr.Scheduled = true
}

// checkDependencies 检查指令的寄存器依赖是否满足
//
// 使用 RegisterAllocator.CountRegDependencies() 检查有多少源寄存器尚未就绪
func (cpu *O3CPU) checkDependencies(instr *instruction.OOOModelInstr) bool {
	return cpu.regAlloc.CountRegDependencies(instr) == 0
}

// ==================== Complete 阶段 ====================

// completeInflightInstruction 完成已执行指令的处理
//
// 对应 ChampSim 的 complete_inflight_instruction()
//
// 流程：
// 1. 扫描 ROB 中所有已执行但未完成的指令
// 2. 检查内存操作是否全部完成 (completed_mem_ops == num_mem_ops())
// 3. 调用 doCompleteExecution() 标记寄存器有效并设置 Completed=true
// 4. 有带宽限制 (ExecuteWidth)
func (cpu *O3CPU) completeInflightInstruction() {
	completeCount := 0

	// 扫描整个 ROB (ChampSim 也是扫描整个 ROB)
	for i := 0; i < cpu.rob.Size() && completeCount < cpu.config.ExecuteWidth; i++ {
		instr := cpu.rob.PeekAt(i)
		if instr == nil {
			continue
		}

		// 检查条件：executed && !completed && ready_time <= current_time && completed_mem_ops == num_mem_ops()
		if instr.Executed && !instr.Completed &&
			cpu.currentCycle >= instr.ReadyTime &&
			instr.CompletedMemOps >= instr.NumMemOps() {

			cpu.doCompleteExecution(instr)
			completeCount++
		}
	}
}

// doCompleteExecution 完成指令执行
//
// 对应 ChampSim 的 do_complete_execution()
//
// 流程：
// 1. 对所有目标寄存器调用 CompleteDestRegister() 标记数据有效
// 2. 设置 instr.Completed = true
// 3. 处理分支预测错误（如果有）
func (cpu *O3CPU) doCompleteExecution(instr *instruction.OOOModelInstr) {
	// 标记所有目标寄存器的数据为有效
	// 这会允许依赖该寄存器的指令进行 schedule
	for _, dreg := range instr.DestRegisters {
		if dreg.IsValid() {
			cpu.regAlloc.CompleteDestRegister(dreg)
		}
	}

	// 标记指令完成
	instr.Completed = true

	// 处理分支预测错误
	// ChampSim 在这里设置 fetch_resume_time，我们暂时简化处理
	if instr.BranchMispredicted {
		// TODO: 实现分支预测错误的惩罚延迟
		// fetch_resume_time = current_time + BRANCH_MISPREDICT_PENALTY
	}
}

// ==================== Execute 阶段 ====================

// execute 执行指令
//
// 完全对应 ChampSim 的 execute_instruction()
//
// 流程：
// 1. 从 ROB 扫描已调度但未执行的指令
// 2. 检查依赖和就绪时间
// 3. 执行指令（包括内存操作）
// 4. 标记为已执行
func (cpu *O3CPU) execute() {
	executeCount := 0

	// 从 ROB 扫描（对应 ChampSim 的 for (auto rob_it = std::begin(ROB); ...)）
	for i := 0; i < cpu.rob.Size() && executeCount < cpu.config.ExecuteWidth; i++ {
		instr := cpu.rob.PeekAt(i)
		if instr == nil {
			continue
		}

		// 只处理已调度但未执行的指令
		if !instr.Scheduled || instr.Executed {
			continue
		}

		// 检查是否到达就绪时间
		if cpu.currentCycle < instr.ReadyTime {
			continue
		}

		// 检查寄存器依赖是否满足
		if !cpu.checkDependencies(instr) {
			continue
		}

		// 执行指令
		instr.Executed = true
		instr.ReadyTime = cpu.currentCycle + cpu.config.ExecuteLatency

		// 处理内存操作
		if instr.IsLoad() || instr.IsStore() {
			cpu.executeMemoryOperation(instr)
		}
		// 注意：不在这里设置 Completed = true
		// 所有指令都应该由 complete_inflight_instruction() 设置 Completed

		executeCount++
	}
}

// executeMemoryOperation 执行内存操作
//
// standaloneMode=true: 自动完成内存操作（简化模拟）
// standaloneMode=false: 等待框架响应（真实集成）
func (cpu *O3CPU) executeMemoryOperation(instr *instruction.OOOModelInstr) {
	// 如果有Cache，使用Cache
	if cpu.l1dCache != nil {
		// 处理所有load操作
		for _, addr := range instr.SrcMemory {
			if addr != 0 {
				// 0 = Load (对应cache.AccessLoad)
				hit, readyCycle, _ := cpu.l1dCache.Access(
					addr,
					addr, // vaddr = paddr (简化)
					instr.InstrID,
					0, // AccessLoad
					cpu.currentCycle,
				)

				// 在非standalone模式下，跟踪pending loads
				if !cpu.standaloneMode {
					cpu.pendingLoads = append(cpu.pendingLoads, PendingMemOp{
						InstrID:    instr.InstrID,
						ReadyCycle: readyCycle,
					})
				}

				_ = hit
			}
		}

		// 处理所有store操作
		for _, addr := range instr.DestMemory {
			if addr != 0 {
				// 1 = Store (对应cache.AccessStore)
				hit, readyCycle, _ := cpu.l1dCache.Access(
					addr,
					addr, // vaddr = paddr
					instr.InstrID,
					1, // AccessStore
					cpu.currentCycle,
				)

				// 在非standalone模式下，跟踪pending stores
				if !cpu.standaloneMode {
					cpu.pendingStores = append(cpu.pendingStores, PendingMemOp{
						InstrID:    instr.InstrID,
						ReadyCycle: readyCycle,
					})
				}

				_ = hit
			}
		}

		// 在Cache standalone模式下，数据会自动就绪
		// 需要立即调用HandleLoadResponse/HandleStoreResponse
		if cpu.standaloneMode {
			loadCount := len(instr.SrcMemory)
			for i := 0; i < loadCount; i++ {
				cpu.HandleLoadResponse(instr.InstrID, cpu.currentCycle+1)
			}

			storeCount := len(instr.DestMemory)
			for i := 0; i < storeCount; i++ {
				cpu.HandleStoreResponse(instr.InstrID, cpu.currentCycle+1)
			}
		}
	} else if cpu.standaloneMode {
		// 没有Cache但是standalone模式：自动完成内存操作
		// 注意：一条指令可能有多个load/store操作，需要为每个操作调用一次Handle函数

		// 处理所有load操作
		loadCount := len(instr.SrcMemory)
		for i := 0; i < loadCount; i++ {
			cpu.HandleLoadResponse(instr.InstrID, cpu.currentCycle+1)
		}

		// 处理所有store操作
		storeCount := len(instr.DestMemory)
		for i := 0; i < storeCount; i++ {
			cpu.HandleStoreResponse(instr.InstrID, cpu.currentCycle+1)
		}
	}
	// 集成模式 + 无Cache：什么都不做，等待框架调用 HandleLoadResponse/HandleStoreResponse
}

// ==================== Retire 阶段 ====================

// retire 按序退休指令
//
// 流程：
// 1. 从 ROB head 取出指令
// 2. 检查是否完成
// 3. 释放资源（物理寄存器、LSQ 条目）
// 4. 处理分支预测错误
// 5. 更新统计
func (cpu *O3CPU) retire() {
	retireCount := 0

	for retireCount < cpu.config.RetireWidth {
		instr := cpu.rob.Retire()
		if instr == nil {
			break
		}

		// 更新 Backend RAT 并释放旧的物理寄存器
		// 对应 ChampSim 的 retire_dest_register()
		for _, reg := range instr.DestRegisters {
			if reg.IsValid() {
				cpu.regAlloc.RetireDestRegister(reg)
			}
		}

		// 从 LSQ 移除
		if instr.IsLoad() {
			cpu.lsq.RemoveLoad(instr.InstrID)
		}
		if instr.IsStore() {
			cpu.lsq.RemoveStore(instr.InstrID)
		}

		// 处理分支预测错误
		if instr.IsBranch && instr.BranchMispredicted {
			cpu.handleBranchMisprediction(instr)
		}

		// 更新统计
		cpu.stats.TotalInstructions++
		cpu.simulationInstructions++

		if instr.IsBranch {
			cpu.stats.TotalBranches++
			if instr.BranchMispredicted {
				cpu.stats.BranchMispredictions++
			}
		}

		retireCount++
	}
}

// handleBranchMisprediction 处理分支预测错误
func (cpu *O3CPU) handleBranchMisprediction(branchInstr *instruction.OOOModelInstr) {
	// 清空流水线
	cpu.fetchQueue = cpu.fetchQueue[:0]
	cpu.decodeQueue = cpu.decodeQueue[:0]

	// 清空 ROB（从分支指令之后）
	cpu.rob.Flush(branchInstr.InstrID)

	// 重新开始 fetch（从正确的目标地址）
	// 这里简化处理，实际需要更新 PC
}

// ==================== 运行控制 ====================

// Run 运行仿真直到指定的指令数
func (cpu *O3CPU) Run(warmupInstrs, simInstrs uint64) {
	cpu.warmupInstructions = warmupInstrs
	targetInstrs := warmupInstrs + simInstrs

	for cpu.stats.TotalInstructions < targetInstrs {
		cpu.Tick()

		// 检查是否完成（ROB 和所有队列都为空）
		if cpu.rob.IsEmpty() &&
			len(cpu.fetchQueue) == 0 &&
			len(cpu.decodeQueue) == 0 {
			// 尝试 fetch，如果没有新指令则结束
			cpu.fetch()
			if len(cpu.fetchQueue) == 0 {
				break
			}
		}
	}

	// 计算 IPC
	if cpu.stats.TotalCycles > 0 {
		cpu.stats.IPC = float64(cpu.stats.TotalInstructions) / float64(cpu.stats.TotalCycles)
	}
}

// GetStats 返回统计信息
func (cpu *O3CPU) GetStats() O3CPUStats {
	return cpu.stats
}

// ==================== 集成接口 ====================

// GetReadyLoads 返回准备好的 load 请求（用于框架集成）
func (cpu *O3CPU) GetReadyLoads(currentCycle uint64) []*LSQEntry {
	return cpu.lsq.GetReadyLoads(currentCycle)
}

// GetReadyStores 返回准备好的 store 请求（用于框架集成）
func (cpu *O3CPU) GetReadyStores(currentCycle uint64) []*LSQEntry {
	return cpu.lsq.GetReadyStores(currentCycle)
}

// HandleLoadResponse 处理 load 响应（用于框架集成）
func (cpu *O3CPU) HandleLoadResponse(instrID uint64, cycle uint64) bool {
	// 更新 LSQ
	if !cpu.lsq.HandleLoadResponse(instrID, cycle) {
		return false
	}

	// 同时更新 ROB 中的指令状态
	instr := cpu.rob.FindByInstrID(instrID)
	if instr != nil {
		// 增加已完成的内存操作计数
		// 当 CompletedMemOps == NumMemOps() 时，complete_inflight_instruction() 会标记指令完成
		instr.CompletedMemOps++

		// 注意：不在这里设置 Completed=true
		// 应该由 complete_inflight_instruction() 在检查所有内存操作完成后设置
	}

	return true
}

// HandleStoreResponse 处理 store 响应（用于框架集成）
func (cpu *O3CPU) HandleStoreResponse(instrID uint64, cycle uint64) bool {
	// 更新 LSQ
	if !cpu.lsq.HandleStoreResponse(instrID, cycle) {
		return false
	}

	// 同时更新 ROB 中的指令状态
	instr := cpu.rob.FindByInstrID(instrID)
	if instr != nil {
		// 增加已完成的内存操作计数
		instr.CompletedMemOps++

		// 注意：不在这里设置 Completed=true
		// 应该由 complete_inflight_instruction() 在检查所有内存操作完成后设置
	}

	return true
}

// processPendingMemOps 处理待处理的内存操作
//
// 在非standalone模式下，检查pending loads/stores是否完成
// 如果完成，调用HandleLoadResponse/HandleStoreResponse
func (cpu *O3CPU) processPendingMemOps() {
	// 处理 pending loads
	remaining := cpu.pendingLoads[:0]
	for _, op := range cpu.pendingLoads {
		if op.ReadyCycle <= cpu.currentCycle {
			// Load 完成，调用 HandleLoadResponse
			cpu.HandleLoadResponse(op.InstrID, cpu.currentCycle)
		} else {
			// 还未完成，保留在列表中
			remaining = append(remaining, op)
		}
	}
	cpu.pendingLoads = remaining

	// 处理 pending stores
	remaining = cpu.pendingStores[:0]
	for _, op := range cpu.pendingStores {
		if op.ReadyCycle <= cpu.currentCycle {
			// Store 完成，调用 HandleStoreResponse
			cpu.HandleStoreResponse(op.InstrID, cpu.currentCycle)
		} else {
			// 还未完成，保留在列表中
			remaining = append(remaining, op)
		}
	}
	cpu.pendingStores = remaining
}

// GetLSQStats 返回 LSQ 统计信息（用于测试）
func (cpu *O3CPU) GetLSQStats() LSQStats {
	return cpu.lsq.GetStats()
}
