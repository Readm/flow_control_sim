package cpu

import (
	"testing"
)

// TestDIB_BasicCheckAndInsert 测试基本的检查和插入功能
func TestDIB_BasicCheckAndInsert(t *testing.T) {
	dib := NewDIB(10, DefaultDIBShift)

	// 初始时应该为空
	if dib.Size() != 0 {
		t.Errorf("Expected empty DIB, got size %d", dib.Size())
	}

	// 检查不存在的 IP
	ip := uint64(0x400000)
	if dib.Check(ip) {
		t.Error("Should not find IP before insertion")
	}

	// 插入 IP
	dib.Insert(ip)

	// 应该能找到
	if !dib.Check(ip) {
		t.Error("Should find IP after insertion")
	}

	// Size 应该增加
	if dib.Size() != 1 {
		t.Errorf("Expected size 1, got %d", dib.Size())
	}
}

// TestDIB_MultipleInserts 测试多次插入
func TestDIB_MultipleInserts(t *testing.T) {
	dib := NewDIB(100, DefaultDIBShift)

	// 插入多个不同的 IP
	ips := []uint64{
		0x400000,
		0x400040, // 同一缓存行（64字节对齐）
		0x400080,
		0x500000, // 不同缓存行
	}

	for _, ip := range ips {
		dib.Insert(ip)
	}

	// 验证所有 IP 都能找到
	for _, ip := range ips {
		if !dib.Check(ip) {
			t.Errorf("Should find IP %x after insertion", ip)
		}
	}

	if dib.Size() != len(ips) {
		t.Errorf("Expected size %d, got %d", len(ips), dib.Size())
	}
}

// TestDIB_SameIndexConflict 测试同一索引的冲突（别名）
func TestDIB_SameIndexConflict(t *testing.T) {
	dib := NewDIB(10, 6) // shift=6 表示 64 字节对齐

	// 这两个地址的索引相同（右移 6 位后相同），但完整地址不同
	// 0x400000 >> 6 = 0x10000
	// 0x400010 >> 6 = 0x10000（低 6 位内的差异会被右移消除）
	ip1 := uint64(0x400000)
	ip2 := uint64(0x400010) // 相差 16 字节，在同一 64 字节缓存行内

	// 插入第一个
	dib.Insert(ip1)
	if !dib.Check(ip1) {
		t.Error("Should find first IP")
	}

	// 插入第二个（会覆盖第一个，因为索引相同）
	dib.Insert(ip2)
	if !dib.Check(ip2) {
		t.Error("Should find second IP")
	}

	// 第一个应该找不到了（被覆盖）
	if dib.Check(ip1) {
		t.Error("First IP should be evicted by second IP with same index")
	}

	// Size 应该是 1（因为同一索引）
	if dib.Size() != 1 {
		t.Errorf("Expected size 1 after conflict, got %d", dib.Size())
	}
}

// TestDIB_LRUEviction 测试 LRU 驱逐策略
func TestDIB_LRUEviction(t *testing.T) {
	dib := NewDIB(3, DefaultDIBShift)

	// 插入 3 个条目（填满）
	ip1 := uint64(0x400000)
	ip2 := uint64(0x500000)
	ip3 := uint64(0x600000)

	dib.Insert(ip1) // time=0
	dib.Insert(ip2) // time=1
	dib.Insert(ip3) // time=2

	// 访问 ip1，更新其访问时间
	dib.Check(ip1) // time=3

	// 现在访问时间：ip2=1 (最旧), ip3=2, ip1=3 (最新)

	// 插入第 4 个条目，应该驱逐 ip2（最旧）
	ip4 := uint64(0x700000)
	dib.Insert(ip4) // time=4

	// ip2 应该被驱逐
	if dib.Check(ip2) {
		t.Error("ip2 should be evicted (oldest)")
	}

	// ip1, ip3, ip4 应该还在
	if !dib.Check(ip1) {
		t.Error("ip1 should still be present")
	}
	if !dib.Check(ip3) {
		t.Error("ip3 should still be present")
	}
	if !dib.Check(ip4) {
		t.Error("ip4 should still be present")
	}

	// Size 应该保持为 3
	if dib.Size() != 3 {
		t.Errorf("Expected size 3 after eviction, got %d", dib.Size())
	}
}

// TestDIB_Invalidate 测试使条目无效
func TestDIB_Invalidate(t *testing.T) {
	dib := NewDIB(10, DefaultDIBShift)

	ip := uint64(0x400000)
	dib.Insert(ip)

	// 确认存在
	if !dib.Check(ip) {
		t.Error("Should find IP before invalidation")
	}

	// 使其无效
	dib.Invalidate(ip)

	// 应该找不到了
	if dib.Check(ip) {
		t.Error("Should not find IP after invalidation")
	}

	// Size 应该减少
	if dib.Size() != 0 {
		t.Errorf("Expected size 0 after invalidation, got %d", dib.Size())
	}
}

// TestDIB_Clear 测试清空功能
func TestDIB_Clear(t *testing.T) {
	dib := NewDIB(10, DefaultDIBShift)

	// 插入几个条目
	dib.Insert(0x400000)
	dib.Insert(0x500000)
	dib.Insert(0x600000)

	if dib.Size() != 3 {
		t.Errorf("Expected size 3 before clear, got %d", dib.Size())
	}

	// 清空
	dib.Clear()

	// Size 应该为 0
	if dib.Size() != 0 {
		t.Errorf("Expected size 0 after clear, got %d", dib.Size())
	}

	// 所有 IP 都应该找不到
	if dib.Check(0x400000) {
		t.Error("Should not find any IP after clear")
	}

	// currentTime 应该重置
	if dib.currentTime != 0 {
		t.Errorf("Expected currentTime=0 after clear, got %d", dib.currentTime)
	}
}

// TestDIB_UpdateOnInsert 测试重复插入会更新访问时间
func TestDIB_UpdateOnInsert(t *testing.T) {
	dib := NewDIB(3, DefaultDIBShift)

	ip1 := uint64(0x400000)
	ip2 := uint64(0x500000)
	ip3 := uint64(0x600000)

	dib.Insert(ip1) // time=0
	dib.Insert(ip2) // time=1
	dib.Insert(ip3) // time=2

	// 重新插入 ip1（应该更新其访问时间）
	dib.Insert(ip1) // time=3

	// 现在访问时间：ip2=1 (最旧), ip3=2, ip1=3 (最新)

	// 插入第 4 个条目
	ip4 := uint64(0x700000)
	dib.Insert(ip4) // time=4

	// ip2 应该被驱逐（最旧）
	if dib.Check(ip2) {
		t.Error("ip2 should be evicted")
	}

	// ip1 应该还在（因为被重新插入更新了时间）
	if !dib.Check(ip1) {
		t.Error("ip1 should still be present after re-insertion")
	}
}

// TestDIB_CalculateOptimalShift 测试计算最优位移量
func TestDIB_CalculateOptimalShift(t *testing.T) {
	tests := []struct {
		cacheLineSize int
		expectedShift uint
	}{
		{64, 6},   // 2^6 = 64
		{128, 7},  // 2^7 = 128
		{32, 5},   // 2^5 = 32
		{256, 8},  // 2^8 = 256
		{0, DefaultDIBShift}, // 无效输入
		{-1, DefaultDIBShift}, // 无效输入
	}

	for _, tt := range tests {
		shift := CalculateOptimalShift(tt.cacheLineSize)
		if shift != tt.expectedShift {
			t.Errorf("CalculateOptimalShift(%d) = %d, want %d",
				tt.cacheLineSize, shift, tt.expectedShift)
		}
	}
}

// TestDIB_Stats 测试命中率统计
func TestDIB_Stats(t *testing.T) {
	stats := &DIBStats{
		Hits:   80,
		Misses: 20,
	}

	hitRate := stats.CalculateHitRate()
	expected := 0.8

	if hitRate != expected {
		t.Errorf("Expected hit rate %.2f, got %.2f", expected, hitRate)
	}

	// 测试零总数
	emptyStats := &DIBStats{}
	if emptyStats.CalculateHitRate() != 0.0 {
		t.Error("Expected 0.0 hit rate for empty stats")
	}
}
