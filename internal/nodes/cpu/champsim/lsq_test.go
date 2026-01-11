package cpu

import (
	"testing"

	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/instruction"
)

// TestLSQ_BasicLoadOperations 测试基本的 load 操作
func TestLSQ_BasicLoadOperations(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 初始时队列应该为空
	if lsq.LoadQueueSize() != 0 {
		t.Errorf("Expected empty load queue, got size %d", lsq.LoadQueueSize())
	}

	// 添加一个 load
	entry := NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0})
	err := lsq.AddLoad(entry)
	if err != nil {
		t.Fatalf("Failed to add load: %v", err)
	}

	if lsq.LoadQueueSize() != 1 {
		t.Errorf("Expected load queue size 1, got %d", lsq.LoadQueueSize())
	}

	// 移除 load
	if !lsq.RemoveLoad(1) {
		t.Error("Failed to remove load")
	}

	if lsq.LoadQueueSize() != 0 {
		t.Errorf("Expected empty load queue after removal, got size %d", lsq.LoadQueueSize())
	}
}

// TestLSQ_BasicStoreOperations 测试基本的 store 操作
func TestLSQ_BasicStoreOperations(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 添加一个 store
	entry := NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0})
	err := lsq.AddStore(entry)
	if err != nil {
		t.Fatalf("Failed to add store: %v", err)
	}

	if lsq.StoreQueueSize() != 1 {
		t.Errorf("Expected store queue size 1, got %d", lsq.StoreQueueSize())
	}

	// 移除 store
	if !lsq.RemoveStore(1) {
		t.Error("Failed to remove store")
	}

	if lsq.StoreQueueSize() != 0 {
		t.Errorf("Expected empty store queue after removal, got size %d", lsq.StoreQueueSize())
	}
}

// TestLSQ_LoadQueueFull 测试 load 队列满的情况
func TestLSQ_LoadQueueFull(t *testing.T) {
	lsq := NewLoadStoreQueue(2, 10) // 小的 LQ 容量

	// 填满 load queue
	for i := 0; i < 2; i++ {
		entry := NewLSQEntry(uint64(i), 0x1000+uint64(i*8), 0x400000, [2]uint8{0, 0})
		if err := lsq.AddLoad(entry); err != nil {
			t.Fatalf("Failed to add load %d: %v", i, err)
		}
	}

	// 尝试再添加一个应该失败
	entry := NewLSQEntry(100, 0x2000, 0x400000, [2]uint8{0, 0})
	err := lsq.AddLoad(entry)
	if err == nil {
		t.Error("Expected error when adding to full load queue")
	}

	if !lsq.IsLoadQueueFull() {
		t.Error("Load queue should be full")
	}

	// 统计应该增加
	stats := lsq.GetStats()
	if stats.LoadQueueFull != 1 {
		t.Errorf("Expected LoadQueueFull count 1, got %d", stats.LoadQueueFull)
	}
}

// TestLSQ_StoreQueueFull 测试 store 队列满的情况
func TestLSQ_StoreQueueFull(t *testing.T) {
	lsq := NewLoadStoreQueue(10, 2) // 小的 SQ 容量

	// 填满 store queue
	for i := 0; i < 2; i++ {
		entry := NewLSQEntry(uint64(i), 0x1000+uint64(i*8), 0x400000, [2]uint8{0, 0})
		if err := lsq.AddStore(entry); err != nil {
			t.Fatalf("Failed to add store %d: %v", i, err)
		}
	}

	// 尝试再添加一个应该失败
	entry := NewLSQEntry(100, 0x2000, 0x400000, [2]uint8{0, 0})
	err := lsq.AddStore(entry)
	if err == nil {
		t.Error("Expected error when adding to full store queue")
	}

	if !lsq.IsStoreQueueFull() {
		t.Error("Store queue should be full")
	}
}

// TestLSQ_StoreToLoadForwarding 测试 Store-to-Load Forwarding（核心功能）
func TestLSQ_StoreToLoadForwarding(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 添加一个 store（地址 0x1000）
	storeEntry := NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0})
	storeEntry.ReadyTime = 10 // store 在周期 10 准备好
	lsq.AddStore(storeEntry)

	// 添加一个 load（同一地址 0x1000，指令 ID 更大）
	loadEntry := NewLSQEntry(2, 0x1000, 0x400010, [2]uint8{0, 0})
	loadEntry.ReadyTime = 15 // load 在周期 15 准备好
	lsq.AddLoad(loadEntry)

	// 检查是否可以转发
	canForward, forwardedStore := lsq.CheckStoreToLoadForwarding(loadEntry)

	if !canForward {
		t.Error("Should be able to forward from store to load")
	}

	if forwardedStore == nil {
		t.Fatal("Forwarded store should not be nil")
	}

	if forwardedStore.InstrID != 1 {
		t.Errorf("Expected forwarded store ID 1, got %d", forwardedStore.InstrID)
	}

	// 统计应该增加
	stats := lsq.GetStats()
	if stats.ForwardedLoads != 1 {
		t.Errorf("Expected ForwardedLoads count 1, got %d", stats.ForwardedLoads)
	}
}

// TestLSQ_NoForwardingDifferentAddress 测试不同地址不能转发
func TestLSQ_NoForwardingDifferentAddress(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 添加一个 store（地址 0x1000）
	storeEntry := NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0})
	storeEntry.ReadyTime = 10
	lsq.AddStore(storeEntry)

	// 添加一个 load（不同地址 0x2000）
	loadEntry := NewLSQEntry(2, 0x2000, 0x400010, [2]uint8{0, 0})
	loadEntry.ReadyTime = 15
	lsq.AddLoad(loadEntry)

	// 不应该能转发
	canForward, _ := lsq.CheckStoreToLoadForwarding(loadEntry)
	if canForward {
		t.Error("Should not forward with different addresses")
	}
}

// TestLSQ_NoForwardingStoreNotReady 测试 store 未准备好不能转发
func TestLSQ_NoForwardingStoreNotReady(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 添加一个 store（地址 0x1000）
	storeEntry := NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0})
	storeEntry.ReadyTime = 20 // store 比 load 晚准备好
	lsq.AddStore(storeEntry)

	// 添加一个 load（同一地址）
	loadEntry := NewLSQEntry(2, 0x1000, 0x400010, [2]uint8{0, 0})
	loadEntry.ReadyTime = 15 // load 更早准备好
	lsq.AddLoad(loadEntry)

	// 不应该能转发（store 还没准备好）
	canForward, _ := lsq.CheckStoreToLoadForwarding(loadEntry)
	if canForward {
		t.Error("Should not forward when store is not ready")
	}
}

// TestLSQ_NoForwardingWrongOrder 测试程序顺序错误不能转发
func TestLSQ_NoForwardingWrongOrder(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 添加一个 store（InstrID=2）
	storeEntry := NewLSQEntry(2, 0x1000, 0x400000, [2]uint8{0, 0})
	storeEntry.ReadyTime = 10
	lsq.AddStore(storeEntry)

	// 添加一个 load（InstrID=1，程序顺序更早）
	loadEntry := NewLSQEntry(1, 0x1000, 0x400010, [2]uint8{0, 0})
	loadEntry.ReadyTime = 15
	lsq.AddLoad(loadEntry)

	// 不应该能转发（load 在 store 之前）
	canForward, _ := lsq.CheckStoreToLoadForwarding(loadEntry)
	if canForward {
		t.Error("Should not forward when load comes before store in program order")
	}
}

// TestLSQ_GetReadyLoads 测试获取准备好的 load 请求
func TestLSQ_GetReadyLoads(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 添加几个 load
	load1 := NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0})
	load1.ReadyTime = 10
	lsq.AddLoad(load1)

	load2 := NewLSQEntry(2, 0x2000, 0x400010, [2]uint8{0, 0})
	load2.ReadyTime = 20
	lsq.AddLoad(load2)

	load3 := NewLSQEntry(3, 0x3000, 0x400020, [2]uint8{0, 0})
	load3.ReadyTime = 5
	lsq.AddLoad(load3)

	// 在周期 15，应该有 2 个 load 准备好（load1 和 load3）
	readyLoads := lsq.GetReadyLoads(15)

	if len(readyLoads) != 2 {
		t.Errorf("Expected 2 ready loads at cycle 15, got %d", len(readyLoads))
	}
}

// TestLSQ_GetReadyLoadsWithForwarding 测试 GetReadyLoads 返回可以转发的 loads
// 调用者需要检查转发并处理
func TestLSQ_GetReadyLoadsWithForwarding(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 添加一个 store
	store := NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0})
	store.ReadyTime = 10
	lsq.AddStore(store)

	// 添加一个可以转发的 load
	load := NewLSQEntry(2, 0x1000, 0x400010, [2]uint8{0, 0})
	load.ReadyTime = 15
	lsq.AddLoad(load)

	// 在周期 20 获取准备好的 load
	readyLoads := lsq.GetReadyLoads(20)

	// 应该返回 1 个 load（即使可以转发，也由调用者处理）
	if len(readyLoads) != 1 {
		t.Errorf("Expected 1 load, got %d", len(readyLoads))
	}

	// 检查这个 load 可以转发
	if len(readyLoads) > 0 {
		canForward, _ := lsq.CheckStoreToLoadForwarding(readyLoads[0])
		if !canForward {
			t.Error("Load should be forwardable")
		}

		// 模拟调用者处理转发（标记为完成）
		readyLoads[0].Completed = true
		readyLoads[0].CompleteCycle = 20
		readyLoads[0].FetchIssued = true

		// 验证标记成功
		if !load.Completed {
			t.Error("Load should be marked as completed")
		}
		if load.CompleteCycle != 20 {
			t.Errorf("Expected complete cycle 20, got %d", load.CompleteCycle)
		}
	}
}

// TestLSQ_GetReadyStores 测试获取准备好的 store 请求
func TestLSQ_GetReadyStores(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 添加几个 store
	store1 := NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0})
	store1.ReadyTime = 10
	lsq.AddStore(store1)

	store2 := NewLSQEntry(2, 0x2000, 0x400010, [2]uint8{0, 0})
	store2.ReadyTime = 20
	lsq.AddStore(store2)

	// 在周期 15，应该有 1 个 store 准备好
	readyStores := lsq.GetReadyStores(15)

	if len(readyStores) != 1 {
		t.Errorf("Expected 1 ready store at cycle 15, got %d", len(readyStores))
	}
}

// TestLSQ_HandleLoadResponse 测试处理 load 响应
func TestLSQ_HandleLoadResponse(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	load := NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0})
	lsq.AddLoad(load)

	// 处理响应
	if !lsq.HandleLoadResponse(1, 100) {
		t.Error("Failed to handle load response")
	}

	// load 应该被标记为完成
	if !load.Completed {
		t.Error("Load should be marked as completed")
	}

	if load.CompleteCycle != 100 {
		t.Errorf("Expected complete cycle 100, got %d", load.CompleteCycle)
	}
}

// TestLSQ_HandleStoreResponse 测试处理 store 响应
func TestLSQ_HandleStoreResponse(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	store := NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0})
	lsq.AddStore(store)

	// 处理响应
	if !lsq.HandleStoreResponse(1, 100) {
		t.Error("Failed to handle store response")
	}

	// store 应该被标记为完成
	if !store.Completed {
		t.Error("Store should be marked as completed")
	}

	if store.CompleteCycle != 100 {
		t.Errorf("Expected complete cycle 100, got %d", store.CompleteCycle)
	}
}

// TestLSQ_FindByInstrID 测试通过指令 ID 查找条目
func TestLSQ_FindByInstrID(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	load := NewLSQEntry(10, 0x1000, 0x400000, [2]uint8{0, 0})
	lsq.AddLoad(load)

	store := NewLSQEntry(20, 0x2000, 0x400010, [2]uint8{0, 0})
	lsq.AddStore(store)

	// 查找 load
	foundLoad := lsq.FindLoadByInstrID(10)
	if foundLoad == nil {
		t.Error("Should find load by instr ID")
	}
	if foundLoad.InstrID != 10 {
		t.Errorf("Expected instr ID 10, got %d", foundLoad.InstrID)
	}

	// 查找 store
	foundStore := lsq.FindStoreByInstrID(20)
	if foundStore == nil {
		t.Error("Should find store by instr ID")
	}
	if foundStore.InstrID != 20 {
		t.Errorf("Expected instr ID 20, got %d", foundStore.InstrID)
	}

	// 查找不存在的
	if lsq.FindLoadByInstrID(999) != nil {
		t.Error("Should not find non-existent load")
	}
}

// TestLSQ_CheckMemoryOrdering 测试内存顺序检查
func TestLSQ_CheckMemoryOrdering(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 添加一个未完成的 store（InstrID=1，地址 0x1000）
	store := NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0})
	store.Completed = false
	lsq.AddStore(store)

	// 创建一个 load 指令（InstrID=2，地址 0x1000）
	loadInstr := &instruction.OOOModelInstr{
		InstrID:   2,
		SrcMemory: []uint64{0x1000},
	}

	// Load 必须等待同地址的 store 完成
	if lsq.CheckMemoryOrdering(loadInstr) {
		t.Error("Load should not be allowed to proceed (waiting for store)")
	}

	// 标记 store 为完成
	store.Completed = true

	// 现在 load 应该可以执行
	if !lsq.CheckMemoryOrdering(loadInstr) {
		t.Error("Load should be allowed to proceed after store completes")
	}
}

// TestLSQ_HasPendingMemoryRequest 测试是否有待处理的内存请求
func TestLSQ_HasPendingMemoryRequest(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 初始时没有待处理请求
	if lsq.HasPendingMemoryRequest() {
		t.Error("Should have no pending requests initially")
	}

	// 添加一个未发出的 load
	load := NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0})
	load.FetchIssued = false
	load.Completed = false
	lsq.AddLoad(load)

	// 现在应该有待处理请求
	if !lsq.HasPendingMemoryRequest() {
		t.Error("Should have pending request (not issued)")
	}

	// 标记为已发出
	load.FetchIssued = true

	// 一旦发出，就不再算作 pending（即使未完成）
	// pending 的含义是"未发出"而非"未完成"
	if lsq.HasPendingMemoryRequest() {
		t.Error("Should have no pending request after issue (pending means not issued)")
	}

	// 添加另一个未发出的 load
	load2 := NewLSQEntry(2, 0x2000, 0x400010, [2]uint8{0, 0})
	load2.FetchIssued = false
	load2.Completed = false
	lsq.AddLoad(load2)

	// 应该又有待处理请求了
	if !lsq.HasPendingMemoryRequest() {
		t.Error("Should have pending request from load2")
	}

	// 标记为完成（跳过发出步骤，直接完成）
	load2.Completed = true

	// 完成了也不算 pending
	if lsq.HasPendingMemoryRequest() {
		t.Error("Should have no pending requests after completion")
	}
}

// TestLSQ_Reset 测试重置功能
func TestLSQ_Reset(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 添加一些条目
	lsq.AddLoad(NewLSQEntry(1, 0x1000, 0x400000, [2]uint8{0, 0}))
	lsq.AddStore(NewLSQEntry(2, 0x2000, 0x400010, [2]uint8{0, 0}))

	// 重置
	lsq.Reset()

	// 所有队列应该为空
	if lsq.LoadQueueSize() != 0 {
		t.Errorf("Expected empty load queue after reset, got size %d", lsq.LoadQueueSize())
	}

	if lsq.StoreQueueSize() != 0 {
		t.Errorf("Expected empty store queue after reset, got size %d", lsq.StoreQueueSize())
	}

	// 统计应该重置
	stats := lsq.GetStats()
	if stats.TotalLoads != 0 || stats.TotalStores != 0 {
		t.Error("Stats should be reset")
	}
}

// TestLSQ_Statistics 测试统计信息
func TestLSQ_Statistics(t *testing.T) {
	lsq := NewLoadStoreQueue(128, 72)

	// 添加几个 load 和 store
	for i := 0; i < 5; i++ {
		lsq.AddLoad(NewLSQEntry(uint64(i), 0x1000+uint64(i*8), 0x400000, [2]uint8{0, 0}))
	}

	for i := 0; i < 3; i++ {
		lsq.AddStore(NewLSQEntry(uint64(100+i), 0x2000+uint64(i*8), 0x400010, [2]uint8{0, 0}))
	}

	stats := lsq.GetStats()

	if stats.TotalLoads != 5 {
		t.Errorf("Expected 5 total loads, got %d", stats.TotalLoads)
	}

	if stats.TotalStores != 3 {
		t.Errorf("Expected 3 total stores, got %d", stats.TotalStores)
	}
}
