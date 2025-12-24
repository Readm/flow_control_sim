package cpu

import (
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/trace"
)

// Test_Debug_Deadlock 调试死锁问题
func Test_Debug_Deadlock(t *testing.T) {
	traceFile := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"

	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Skipf("Skipping test, trace file not available: %v", err)
	}
	defer traceReader.Close()

	config := DefaultO3CPUConfig()
	cpu := NewO3CPU(traceReader, config)
	cpu.SetStandaloneMode(true)

	// 运行到死锁发生
	var prevInstrCount uint64
	stallCycles := 0

	for cycle := uint64(0); cycle < 5000; cycle++ {
		cpu.Tick()

		if cycle > 0 && cycle%500 == 0 {
			stats := cpu.GetStats()
			deltaInstrs := stats.TotalInstructions - prevInstrCount

			t.Logf("Cycle %d: Instructions=%d (+%d), ROB=%d/%d, PhysReg=%d/%d",
				cycle, stats.TotalInstructions, deltaInstrs,
				cpu.rob.Size(), cpu.rob.MaxSize(),
				cpu.regAlloc.AllocatedCount(), cpu.regAlloc.TotalCount())

			if deltaInstrs == 0 && cycle > 100 {
				stallCycles++
				if stallCycles >= 2 {
					// 发生死锁，打印详细信息
					t.Logf("\n===== DEADLOCK DETECTED at cycle %d =====", cycle)
					printDeadlockInfo(t, cpu)
					return
				}
			} else {
				stallCycles = 0
			}

			prevInstrCount = stats.TotalInstructions
		}
	}
}

func printDeadlockInfo(t *testing.T, cpu *O3CPU) {
	t.Logf("\n----- ROB Status -----")
	t.Logf("ROB Size: %d/%d", cpu.rob.Size(), cpu.rob.MaxSize())

	// 打印前 10 条指令
	for i := 0; i < min(10, cpu.rob.Size()); i++ {
		instr := cpu.rob.PeekAt(i)
		if instr == nil {
			continue
		}

		regDeps := cpu.regAlloc.CountRegDependencies(instr)

		t.Logf("\n[%d] InstrID=%d, IP=0x%x", i, instr.InstrID, instr.IP)
		t.Logf("    Scheduled=%v, Executed=%v, Completed=%v",
			instr.Scheduled, instr.Executed, instr.Completed)
		t.Logf("    ReadyTime=%d, CurrentCycle=%d", instr.ReadyTime, cpu.currentCycle)
		t.Logf("    RegDeps=%d, CompletedMemOps=%d/%d",
			regDeps, instr.CompletedMemOps, instr.NumMemOps())
		t.Logf("    IsLoad=%v, IsStore=%v", instr.IsLoad(), instr.IsStore())

		// 打印源寄存器依赖
		if regDeps > 0 {
			t.Logf("    Source Register Dependencies:")
			for j, srcReg := range instr.SrcRegisters {
				if srcReg.IsValid() {
					physReg := cpu.regAlloc.physicalRegisterFile[srcReg]
					t.Logf("      Src[%d]: PhysReg=%d, Valid=%v, Busy=%v, Producer=%d",
						j, srcReg, physReg.Valid, physReg.Busy, physReg.ProducingInstructionID)
				}
			}
		}
	}

	t.Logf("\n----- Register Allocator Status -----")
	t.Logf("Physical Registers: %d/%d allocated, %d free",
		cpu.regAlloc.AllocatedCount(), cpu.regAlloc.TotalCount(), cpu.regAlloc.AvailableCount())

	t.Logf("\n----- LSQ Status -----")
	lsqStats := cpu.GetLSQStats()
	t.Logf("Loads: %d, Stores: %d, Forwarded: %d",
		lsqStats.TotalLoads, lsqStats.TotalStores, lsqStats.ForwardedLoads)
	t.Logf("LoadQueue: %d entries", len(cpu.lsq.loadQueue))
	t.Logf("StoreQueue: %d entries", len(cpu.lsq.storeQueue))

	// 打印前几条 LSQ 条目
	for i := 0; i < min(5, len(cpu.lsq.loadQueue)); i++ {
		entry := cpu.lsq.loadQueue[i]
		t.Logf("  LoadQueue[%d]: InstrID=%d, Completed=%v, FetchIssued=%v",
			i, entry.InstrID, entry.Completed, entry.FetchIssued)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
