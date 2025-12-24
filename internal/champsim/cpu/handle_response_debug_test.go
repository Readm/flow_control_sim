package cpu

import (
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/instruction"
	"github.com/Readm/flow_sim/internal/champsim/trace"
)

// TestHandleLoadResponse_Debug 调试 HandleLoadResponse
func TestHandleLoadResponse_Debug(t *testing.T) {
	// 创建一个简单的 trace
	instrs := []trace.InputInstr{
		{
			IP:            0x400000,
			IsBranch:      0,
			DestRegisters: [2]uint8{1, 0xFF},
			SrcRegisters:  [4]uint8{2, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0x1000, 0, 0, 0}, // 一个 load
		},
	}

	filename := createTestTraceFile(t, instrs)
	defer deleteTestTraceFile(t, filename)

	traceReader, err := trace.NewTraceReader(filename, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create trace reader: %v", err)
	}
	defer traceReader.Close()

	config := DefaultO3CPUConfig()
	cpu := NewO3CPU(traceReader, config)
	cpu.SetStandaloneMode(false)

	// 运行 CPU 直到有 load 请求
	var loadEntry *LSQEntry
	for cycle := uint64(0); cycle < 50 && loadEntry == nil; cycle++ {
		cpu.Tick()

		readyLoads := cpu.GetReadyLoads(cycle)
		if len(readyLoads) > 0 {
			loadEntry = readyLoads[0]
			t.Logf("Cycle %d: Got load request for instr %d, addr 0x%x",
				cycle, loadEntry.InstrID, loadEntry.VirtualAddr)
			break
		}
	}

	if loadEntry == nil {
		t.Fatal("No load request generated")
	}

	instrID := loadEntry.InstrID

	// 检查 ROB 中的指令
	instr := cpu.rob.FindByInstrID(instrID)
	if instr == nil {
		t.Fatalf("Instruction %d not found in ROB", instrID)
	}

	t.Logf("Before HandleLoadResponse:")
	t.Logf("  Instr %d: Executed=%v, Completed=%v, CompletedMemOps=%d/%d",
		instrID, instr.Executed, instr.Completed, instr.CompletedMemOps, instr.NumMemOps())

	// 打印 LSQ loadQueue 内容
	loadQueue := cpu.lsq.loadQueue
	t.Logf("  LSQ loadQueue has %d entries:", len(loadQueue))
	for i, entry := range loadQueue {
		t.Logf("    [%d] InstrID=%d, Addr=0x%x, Completed=%v, FetchIssued=%v",
			i, entry.InstrID, entry.VirtualAddr, entry.Completed, entry.FetchIssued)
	}

	// 调用 HandleLoadResponse
	success := cpu.HandleLoadResponse(instrID, 100)
	t.Logf("HandleLoadResponse returned: %v", success)

	// 检查结果
	instr = cpu.rob.FindByInstrID(instrID)
	if instr == nil {
		t.Fatalf("Instruction %d disappeared from ROB!", instrID)
	}

	t.Logf("After HandleLoadResponse:")
	t.Logf("  Instr %d: Executed=%v, Completed=%v, CompletedMemOps=%d/%d",
		instrID, instr.Executed, instr.Completed, instr.CompletedMemOps, instr.NumMemOps())

	// 打印 LSQ loadQueue 内容
	loadQueue = cpu.lsq.loadQueue
	t.Logf("  LSQ loadQueue has %d entries:", len(loadQueue))
	for i, entry := range loadQueue {
		t.Logf("    [%d] InstrID=%d, Addr=0x%x, Completed=%v, FetchIssued=%v",
			i, entry.InstrID, entry.VirtualAddr, entry.Completed, entry.FetchIssued)
	}

	// 验证
	if !success {
		t.Error("HandleLoadResponse failed")
	}

	if instr.CompletedMemOps != 1 {
		t.Errorf("Expected CompletedMemOps=1, got %d", instr.CompletedMemOps)
	}
}
