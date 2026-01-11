package cpu

import (
	"os"
	"testing"

	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/instruction"
	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/trace"
)

// TestCompleteInflightInstruction_Debug 调试 complete 阶段
func TestCompleteInflightInstruction_Debug(t *testing.T) {
	// Use environment variable if provided, otherwise fallback to repo's small trace
	traceFile := os.Getenv("CHAMPSIM_TRACE")
	if traceFile == "" {
		largeTrace := "../../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"
		if _, err := os.Stat(largeTrace); err == nil {
			traceFile = largeTrace
		} else {
			traceFile = "../../../../testdata/traces/small.champsimtrace"
		}
	}

	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Trace file not available: %v (CHAMPSIM_TRACE=%s)", err, traceFile)
	}
	defer traceReader.Close()

	config := DefaultO3CPUConfig()
	cpu := NewO3CPU(traceReader, config)
	cpu.SetStandaloneMode(true)

	// 运行 100 周期
	for cycle := 0; cycle < 100; cycle++ {
		cpu.Tick()

		// 每 10 周期打印一次状态
		if cycle > 0 && cycle%10 == 0 {
			// 检查 ROB 状态
			robSize := cpu.rob.Size()
			executedCount := 0
			completedCount := 0
			notReadyCount := 0
			memOpsNotDoneCount := 0

			for i := 0; i < robSize; i++ {
				instr := cpu.rob.PeekAt(i)
				if instr == nil {
					continue
				}
				if instr.Executed {
					executedCount++
				}
				if instr.Completed {
					completedCount++
				}
				if cpu.currentCycle < instr.ReadyTime {
					notReadyCount++
				}
				if instr.CompletedMemOps < instr.NumMemOps() {
					memOpsNotDoneCount++
				}
			}

			t.Logf("Cycle %d: ROB=%d, Executed=%d, Completed=%d, NotReady=%d, MemOpsNotDone=%d, Instructions=%d",
				cycle, robSize, executedCount, completedCount, notReadyCount, memOpsNotDoneCount,
				cpu.stats.TotalInstructions)

			// 打印前 3 条 ROB 指令的详细状态
			if robSize > 0 {
				t.Logf("  First ROB entry:")
				instr := cpu.rob.PeekAt(0)
				if instr != nil {
					t.Logf("    InstrID=%d, Executed=%v, Completed=%v, ReadyTime=%d (current=%d)",
						instr.InstrID, instr.Executed, instr.Completed, instr.ReadyTime, cpu.currentCycle)
					t.Logf("    CompletedMemOps=%d, NumMemOps=%d",
						instr.CompletedMemOps, instr.NumMemOps())
					t.Logf("    IsLoad=%v, IsStore=%v",
						instr.IsLoad(), instr.IsStore())
					t.Logf("    DestRegisters=%d", len(instr.DestRegisters))

					// 检查寄存器是否有效
					for idx, reg := range instr.DestRegisters {
						if reg.IsValid() {
							valid := cpu.regAlloc.IsValid(reg)
							t.Logf("    DestReg[%d]=%d, Valid=%v", idx, reg, valid)
						}
					}
				}
			}
		}
	}

	stats := cpu.GetStats()
	t.Logf("\nFinal: Instructions=%d, IPC=%.2f",
		stats.TotalInstructions,
		float64(stats.TotalInstructions)/float64(stats.TotalCycles))
}

// TestRegisterAllocator_Complete 测试寄存器完成机制
func TestRegisterAllocator_Complete(t *testing.T) {
	ra := NewRegisterAllocator(128)

	// 分配一个目标寄存器
	physReg := ra.RenameDestRegister(10, 100)

	t.Logf("Allocated physReg=%d for archReg=10", physReg)

	// 检查初始状态
	if ra.IsValid(physReg) {
		t.Error("Newly allocated register should not be valid")
	}

	// 完成寄存器
	ra.CompleteDestRegister(physReg)

	// 检查完成后状态
	if !ra.IsValid(physReg) {
		t.Error("Completed register should be valid")
	}

	t.Logf("After complete: IsValid=%v", ra.IsValid(physReg))

	// 测试依赖计数
	instr := &instruction.OOOModelInstr{
		SrcRegisters: []instruction.PhysicalRegisterID{physReg},
	}

	depCount := ra.CountRegDependencies(instr)
	if depCount != 0 {
		t.Errorf("Expected 0 dependencies, got %d", depCount)
	}

	t.Logf("Dependency count: %d", depCount)
}
