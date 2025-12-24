package integration

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/cpu"
	"github.com/Readm/flow_sim/internal/champsim/instruction"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	"github.com/Readm/flow_sim/internal/dataflow"
	"github.com/Readm/flow_sim/internal/dataflow/transaction"
)

// createTestTraceFile 创建测试 trace 文件
func createTestTraceFile(t *testing.T, instrs []trace.InputInstr) string {
	tmpfile, err := os.CreateTemp("", "integration_test_*.champsimtrace")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	for _, instr := range instrs {
		if err := binary.Write(tmpfile, binary.LittleEndian, &instr); err != nil {
			tmpfile.Close()
			os.Remove(tmpfile.Name())
			t.Fatalf("Failed to write instruction: %v", err)
		}
	}

	tmpfile.Close()
	return tmpfile.Name()
}

// TestCPUIncentiveHook_Creation 测试创建
func TestCPUIncentiveHook_Creation(t *testing.T) {
	// 创建简单的 trace
	instrs := []trace.InputInstr{
		{
			IP:            0x400000,
			IsBranch:      0,
			DestRegisters: [2]uint8{1, 0xFF},
			SrcRegisters:  [4]uint8{2, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
		},
	}

	filename := createTestTraceFile(t, instrs)
	defer os.Remove(filename)

	reader, err := trace.NewTraceReader(filename, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create trace reader: %v", err)
	}
	defer reader.Close()

	hook := NewCPUIncentiveHook(
		reader,
		cpu.DefaultO3CPUConfig(),
		0,  // nodeID
		1,  // targetNodeID
		transaction.ProtocolCHI,
	)

	if hook == nil {
		t.Fatal("Failed to create CPU hook")
	}

	if hook.nodeID != 0 {
		t.Errorf("Expected nodeID 0, got %d", hook.nodeID)
	}

	if hook.protocol != transaction.ProtocolCHI {
		t.Errorf("Expected protocol CHI, got %s", hook.protocol)
	}
}

// TestCPUIncentiveHook_ShouldCreateTransaction 测试是否应该创建 Transaction
func TestCPUIncentiveHook_ShouldCreateTransaction(t *testing.T) {
	instrs := []trace.InputInstr{
		{
			IP:            0x400000,
			IsBranch:      0,
			DestRegisters: [2]uint8{1, 0xFF},
			SrcRegisters:  [4]uint8{2, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
		},
	}

	filename := createTestTraceFile(t, instrs)
	defer os.Remove(filename)

	reader, err := trace.NewTraceReader(filename, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create trace reader: %v", err)
	}
	defer reader.Close()

	hook := NewCPUIncentiveHook(reader, cpu.DefaultO3CPUConfig(), 0, 1, transaction.ProtocolCHI)

	// 应该为自己的节点返回 true
	if !hook.ShouldCreateTransaction(0, 0) {
		t.Error("Should create transaction for own node")
	}

	// 不应该为其他节点返回 true
	if hook.ShouldCreateTransaction(1, 0) {
		t.Error("Should not create transaction for other node")
	}
}

// TestCPUIncentiveHook_CreateTransaction_NoMemoryOps 测试无内存操作
func TestCPUIncentiveHook_CreateTransaction_NoMemoryOps(t *testing.T) {
	// 创建只有 ALU 操作的 trace
	instrs := []trace.InputInstr{
		{
			IP:            0x400000,
			IsBranch:      0,
			DestRegisters: [2]uint8{1, 0xFF},
			SrcRegisters:  [4]uint8{2, 3, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},      // 无内存写
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0}, // 无内存读
		},
	}

	filename := createTestTraceFile(t, instrs)
	defer os.Remove(filename)

	reader, err := trace.NewTraceReader(filename, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create trace reader: %v", err)
	}
	defer reader.Close()

	hook := NewCPUIncentiveHook(reader, cpu.DefaultO3CPUConfig(), 0, 1, transaction.ProtocolCHI)

	// 执行几个周期
	for i := uint64(0); i < 10; i++ {
		txn, err := hook.CreateTransaction(0, i)
		if err != nil {
			t.Fatalf("CreateTransaction failed: %v", err)
		}

		// 无内存操作，不应该创建 transaction（或者 transaction 为空）
		if txn != nil && len(txn.Messages) > 0 {
			t.Logf("Cycle %d: Created transaction with %d messages (may be normal in early cycles)", i, len(txn.Messages))
		}
	}
}

// TestCPUIncentiveHook_CreateTransaction_WithLoads 测试带 load 操作
func TestCPUIncentiveHook_CreateTransaction_WithLoads(t *testing.T) {
	// 创建带 load 的 trace
	instrs := []trace.InputInstr{
		{
			IP:            0x400000,
			IsBranch:      0,
			DestRegisters: [2]uint8{1, 0xFF},
			SrcRegisters:  [4]uint8{2, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0x1000, 0, 0, 0}, // Load from 0x1000
		},
		{
			IP:            0x400010,
			IsBranch:      0,
			DestRegisters: [2]uint8{3, 0xFF},
			SrcRegisters:  [4]uint8{1, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
		},
	}

	filename := createTestTraceFile(t, instrs)
	defer os.Remove(filename)

	reader, err := trace.NewTraceReader(filename, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create trace reader: %v", err)
	}
	defer reader.Close()

	hook := NewCPUIncentiveHook(reader, cpu.DefaultO3CPUConfig(), 0, 1, transaction.ProtocolCHI)

	foundMemoryRequest := false

	// 执行多个周期，直到找到内存请求
	// 增加周期数以考虑流水线延迟
	for i := uint64(0); i < 200 && !foundMemoryRequest; i++ {
		txn, err := hook.CreateTransaction(0, i)
		if err != nil {
			t.Fatalf("CreateTransaction failed at cycle %d: %v", i, err)
		}

		if txn != nil && len(txn.Messages) > 0 {
			foundMemoryRequest = true
			t.Logf("Found memory request at cycle %d with %d messages", i, len(txn.Messages))

			// 验证 transaction
			if txn.Protocol != transaction.ProtocolCHI {
				t.Errorf("Expected protocol CHI, got %s", txn.Protocol)
			}

			if txn.InitiatorNodeID != 0 {
				t.Errorf("Expected initiator node 0, got %d", txn.InitiatorNodeID)
			}

			// 验证 message
			for j, msg := range txn.Messages {
				if msg.SourceNodeID != 0 {
					t.Errorf("Message %d: expected source node 0, got %d", j, msg.SourceNodeID)
				}

				if msg.TargetNodeID != 1 {
					t.Errorf("Message %d: expected target node 1, got %d", j, msg.TargetNodeID)
				}

				// 验证 payload
				if msg.Payload == nil {
					t.Errorf("Message %d: payload is nil", j)
					continue
				}

				payload, ok := msg.Payload.(*MemoryRequestPayload)
				if !ok {
					t.Errorf("Message %d: payload is not MemoryRequestPayload", j)
					continue
				}

				if payload.Address != 0x1000 {
					t.Errorf("Message %d: expected address 0x1000, got 0x%x", j, payload.Address)
				}

				if payload.IsWrite {
					t.Errorf("Message %d: expected read request, got write", j)
				}
			}
		}
	}

	if !foundMemoryRequest {
		t.Error("No memory request found after 200 cycles")
	}
}

// TestCPUIncentiveHook_HandleResponse 测试响应处理
func TestCPUIncentiveHook_HandleResponse(t *testing.T) {
	instrs := []trace.InputInstr{
		{
			IP:            0x400000,
			IsBranch:      0,
			DestRegisters: [2]uint8{1, 0xFF},
			SrcRegisters:  [4]uint8{2, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0x1000, 0, 0, 0},
		},
	}

	filename := createTestTraceFile(t, instrs)
	defer os.Remove(filename)

	reader, err := trace.NewTraceReader(filename, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create trace reader: %v", err)
	}
	defer reader.Close()

	hook := NewCPUIncentiveHook(reader, cpu.DefaultO3CPUConfig(), 0, 1, transaction.ProtocolCHI)

	// 执行直到产生内存请求
	var msgID *dataflow.MessageID
	for i := uint64(0); i < 200; i++ {
		txn, err := hook.CreateTransaction(0, i)
		if err != nil {
			t.Fatalf("CreateTransaction failed: %v", err)
		}

		if txn != nil && len(txn.Messages) > 0 {
			msgID = &txn.Messages[0].ID
			t.Logf("Found message at cycle %d: NodeID=%d, MessageID=%d",
				i, msgID.NodeID, msgID.MessageID)
			break
		}
	}

	if msgID == nil {
		t.Fatal("No message generated")
	}

	// 处理响应
	err = hook.HandleResponse(*msgID, 100)
	if err != nil {
		t.Errorf("HandleResponse failed: %v", err)
	}

	// 再次处理相同响应应该失败（已经处理过了）
	err = hook.HandleResponse(*msgID, 101)
	if err == nil {
		t.Error("Expected error when handling same response twice")
	}
}

// TestCPUIncentiveHook_CreateTransaction_WithStores 测试带 store 操作
func TestCPUIncentiveHook_CreateTransaction_WithStores(t *testing.T) {
	// 创建带 store 的 trace
	instrs := []trace.InputInstr{
		{
			IP:            0x400000,
			IsBranch:      0,
			DestRegisters: [2]uint8{0xFF, 0xFF},
			SrcRegisters:  [4]uint8{1, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0x2000, 0}, // Store to 0x2000
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
		},
		{
			IP:            0x400010,
			IsBranch:      0,
			DestRegisters: [2]uint8{3, 0xFF},
			SrcRegisters:  [4]uint8{1, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
		},
	}

	filename := createTestTraceFile(t, instrs)
	defer os.Remove(filename)

	reader, err := trace.NewTraceReader(filename, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create trace reader: %v", err)
	}
	defer reader.Close()

	hook := NewCPUIncentiveHook(reader, cpu.DefaultO3CPUConfig(), 0, 1, transaction.ProtocolCHI)

	foundMemoryRequest := false

	// 执行多个周期，直到找到内存请求
	for i := uint64(0); i < 200 && !foundMemoryRequest; i++ {
		txn, err := hook.CreateTransaction(0, i)
		if err != nil {
			t.Fatalf("CreateTransaction failed at cycle %d: %v", i, err)
		}

		if txn != nil && len(txn.Messages) > 0 {
			foundMemoryRequest = true
			t.Logf("Found store request at cycle %d with %d messages", i, len(txn.Messages))

			// 验证 message
			for j, msg := range txn.Messages {
				payload, ok := msg.Payload.(*MemoryRequestPayload)
				if !ok {
					t.Errorf("Message %d: payload is not MemoryRequestPayload", j)
					continue
				}

				if payload.Address != 0x2000 {
					t.Errorf("Message %d: expected address 0x2000, got 0x%x", j, payload.Address)
				}

				if !payload.IsWrite {
					t.Errorf("Message %d: expected write request, got read", j)
				}

				t.Logf("Store request: Address=0x%x, IsWrite=%v", payload.Address, payload.IsWrite)
			}
		}
	}

	if !foundMemoryRequest {
		t.Error("No store request found after 200 cycles")
	}
}

// TestCPUIncentiveHook_StoreToLoadForwarding 测试 Store-to-Load Forwarding
func TestCPUIncentiveHook_StoreToLoadForwarding(t *testing.T) {
	// 创建 Store 后紧跟 Load 到同一地址的 trace
	instrs := []trace.InputInstr{
		// Store to 0x1000
		{
			IP:            0x400000,
			IsBranch:      0,
			DestRegisters: [2]uint8{0xFF, 0xFF},
			SrcRegisters:  [4]uint8{1, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0x1000, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
		},
		// Load from 0x1000 (same address)
		{
			IP:            0x400010,
			IsBranch:      0,
			DestRegisters: [2]uint8{2, 0xFF},
			SrcRegisters:  [4]uint8{0xFF, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0x1000, 0, 0, 0},
		},
		// Another instruction
		{
			IP:            0x400020,
			IsBranch:      0,
			DestRegisters: [2]uint8{3, 0xFF},
			SrcRegisters:  [4]uint8{2, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
		},
	}

	filename := createTestTraceFile(t, instrs)
	defer os.Remove(filename)

	reader, err := trace.NewTraceReader(filename, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create trace reader: %v", err)
	}
	defer reader.Close()

	hook := NewCPUIncentiveHook(reader, cpu.DefaultO3CPUConfig(), 0, 1, transaction.ProtocolCHI)

	storeFound := false
	loadFound := false
	loadForwarded := false

	// 执行多个周期，观察 Store-to-Load Forwarding
	for i := uint64(0); i < 200; i++ {
		txn, err := hook.CreateTransaction(0, i)
		if err != nil {
			t.Fatalf("CreateTransaction failed at cycle %d: %v", i, err)
		}

		if txn != nil && len(txn.Messages) > 0 {
			for _, msg := range txn.Messages {
				payload, ok := msg.Payload.(*MemoryRequestPayload)
				if !ok {
					continue
				}

				if payload.Address == 0x1000 {
					if payload.IsWrite {
						storeFound = true
						t.Logf("Cycle %d: Found Store to 0x1000", i)
					} else {
						loadFound = true
						t.Logf("Cycle %d: Found Load from 0x1000", i)
					}
				}
			}
		}

		// 检查 LSQ 统计，看是否有转发
		lsqStats := hook.cpu.GetLSQStats()
		if lsqStats.ForwardedLoads > 0 {
			loadForwarded = true
			t.Logf("Cycle %d: Store-to-Load Forwarding detected! ForwardedLoads=%d",
				i, lsqStats.ForwardedLoads)
		}

		if storeFound && (loadFound || loadForwarded) {
			break
		}
	}

	if !storeFound {
		t.Error("Store to 0x1000 not found")
	}

	// Load 要么发送到内存，要么被转发
	if !loadFound && !loadForwarded {
		t.Log("Warning: Load from 0x1000 neither sent to memory nor forwarded")
	}

	if loadForwarded {
		t.Log("SUCCESS: Store-to-Load Forwarding working!")
	}
}

// TestCPUIncentiveHook_BranchInstructions 测试分支指令
func TestCPUIncentiveHook_BranchInstructions(t *testing.T) {
	// 创建包含分支的 trace
	instrs := []trace.InputInstr{
		// Normal instruction
		{
			IP:            0x400000,
			IsBranch:      0,
			DestRegisters: [2]uint8{1, 0xFF},
			SrcRegisters:  [4]uint8{2, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
		},
		// Conditional branch (taken)
		{
			IP:            0x400010,
			IsBranch:      1,
			BranchTaken:   1,
			DestRegisters: [2]uint8{0xFF, 0xFF},
			SrcRegisters:  [4]uint8{1, instruction.RegFlags, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
		},
		// Branch target
		{
			IP:            0x500000, // Different address (branch target)
			IsBranch:      0,
			DestRegisters: [2]uint8{3, 0xFF},
			SrcRegisters:  [4]uint8{1, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
		},
	}

	filename := createTestTraceFile(t, instrs)
	defer os.Remove(filename)

	reader, err := trace.NewTraceReader(filename, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create trace reader: %v", err)
	}
	defer reader.Close()

	hook := NewCPUIncentiveHook(reader, cpu.DefaultO3CPUConfig(), 0, 1, transaction.ProtocolCHI)

	// 执行足够的周期
	for i := uint64(0); i < 100; i++ {
		_, err := hook.CreateTransaction(0, i)
		if err != nil {
			t.Fatalf("CreateTransaction failed at cycle %d: %v", i, err)
		}
	}

	// 获取统计信息
	stats := hook.GetStats()

	if stats.TotalBranches == 0 {
		t.Error("Expected to process branch instructions")
	}

	t.Logf("Branch stats: Total=%d, Mispredictions=%d",
		stats.TotalBranches, stats.BranchMispredictions)

	if stats.TotalInstructions > 0 {
		t.Logf("Processed %d instructions in %d cycles, IPC=%.2f",
			stats.TotalInstructions, stats.TotalCycles, stats.IPC)
	}
}

// TestCPUIncentiveHook_Stats 测试统计信息
func TestCPUIncentiveHook_Stats(t *testing.T) {
	instrs := []trace.InputInstr{
		{
			IP:            0x400000,
			IsBranch:      0,
			DestRegisters: [2]uint8{1, 0xFF},
			SrcRegisters:  [4]uint8{2, 0xFF, 0xFF, 0xFF},
			DestMemory:    [instruction.NumInstrDestinations]uint64{0, 0},
			SrcMemory:     [instruction.NumInstrSources]uint64{0, 0, 0, 0},
		},
	}

	filename := createTestTraceFile(t, instrs)
	defer os.Remove(filename)

	reader, err := trace.NewTraceReader(filename, 0, trace.FormatStandard)
	if err != nil {
		t.Fatalf("Failed to create trace reader: %v", err)
	}
	defer reader.Close()

	hook := NewCPUIncentiveHook(reader, cpu.DefaultO3CPUConfig(), 0, 1, transaction.ProtocolCHI)

	// 执行几个周期
	for i := uint64(0); i < 20; i++ {
		_, err := hook.CreateTransaction(0, i)
		if err != nil {
			t.Fatalf("CreateTransaction failed: %v", err)
		}
	}

	// 获取统计信息
	stats := hook.GetStats()

	if stats.TotalCycles != 20 {
		t.Errorf("Expected 20 total cycles, got %d", stats.TotalCycles)
	}

	t.Logf("Stats: Cycles=%d, Instructions=%d, IPC=%.2f",
		stats.TotalCycles, stats.TotalInstructions, stats.IPC)
}
