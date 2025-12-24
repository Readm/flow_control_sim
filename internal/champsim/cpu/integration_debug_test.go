package cpu

import (
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/trace"
)

// TestIntegrationMode_Debug 调试集成模式
func TestIntegrationMode_Debug(t *testing.T) {
	traceFile := "../../../testdata/traces/400.perlbench-41B.champsimtrace.xz"

	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		t.Skipf("Skipping test, trace file not available: %v", err)
	}
	defer traceReader.Close()

	config := DefaultO3CPUConfig()
	cpu := NewO3CPU(traceReader, config)

	// 设置为集成模式
	cpu.SetStandaloneMode(false)

	// 用于跟踪响应（用 slice 代替 map，因为一个指令可能有多个 load/store）
	type PendingRequest struct {
		InstrID uint64
		MsgID   int
	}
	var pendingLoads []PendingRequest
	var pendingStores []PendingRequest
	msgCounter := 0

	// 运行 200 周期
	for cycle := uint64(0); cycle < 200; cycle++ {
		// 执行 CPU Tick
		cpu.Tick()

		// 获取准备好的内存请求
		readyLoads := cpu.GetReadyLoads(cycle)
		readyStores := cpu.GetReadyStores(cycle)

		// 添加到 pending（每个 LSQ entry 都分配一个唯一的 message ID）
		for _, load := range readyLoads {
			pendingLoads = append(pendingLoads, PendingRequest{
				InstrID: load.InstrID,
				MsgID:   msgCounter,
			})
			msgCounter++
			// 标记为已发出，防止重复发送
			load.FetchIssued = true
		}
		for _, store := range readyStores {
			pendingStores = append(pendingStores, PendingRequest{
				InstrID: store.InstrID,
				MsgID:   msgCounter,
			})
			msgCounter++
			// 标记为已发出，防止重复发送
			store.FetchIssued = true
		}

		// 立即响应所有 pending 的请求
		newPendingLoads := []PendingRequest{}
		for _, req := range pendingLoads {
			if !cpu.HandleLoadResponse(req.InstrID, cycle+1) {
				// 响应失败，保留请求
				newPendingLoads = append(newPendingLoads, req)
			}
		}
		pendingLoads = newPendingLoads

		newPendingStores := []PendingRequest{}
		for _, req := range pendingStores {
			if !cpu.HandleStoreResponse(req.InstrID, cycle+1) {
				// 响应失败，保留请求
				newPendingStores = append(newPendingStores, req)
			}
		}
		pendingStores = newPendingStores

		// 每 20 周期打印一次状态
		if cycle > 0 && cycle%20 == 0 {
			robSize := cpu.rob.Size()
			executedCount := 0
			completedCount := 0

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
			}

			t.Logf("Cycle %d: ROB=%d, Executed=%d, Completed=%d, PendingLoads=%d, PendingStores=%d, Instructions=%d",
				cycle, robSize, executedCount, completedCount,
				len(pendingLoads), len(pendingStores),
				cpu.stats.TotalInstructions)

			// 打印第一条 ROB 指令的状态
			if robSize > 0 {
				instr := cpu.rob.PeekAt(0)
				if instr != nil {
					t.Logf("  Head: ID=%d, Exec=%v, Comp=%v, CompletedMemOps=%d/%d",
						instr.InstrID, instr.Executed, instr.Completed,
						instr.CompletedMemOps, instr.NumMemOps())
				}
			}
		}
	}

	stats := cpu.GetStats()
	t.Logf("\nFinal: Instructions=%d, IPC=%.2f",
		stats.TotalInstructions,
		float64(stats.TotalInstructions)/float64(stats.TotalCycles))

	// 验证 IPC 应该在合理范围内
	if stats.TotalInstructions < 50 {
		t.Errorf("Too few instructions retired: %d", stats.TotalInstructions)
	}
}
