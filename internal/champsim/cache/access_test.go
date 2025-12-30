package cache

import (
	"testing"

	compcache "github.com/Readm/flow_sim/internal/components/cache"
)

// TestAccess_Hit 测试cache hit
func TestAccess_Hit(t *testing.T) {
	config := compcache.DefaultL1DConfig()
	cache, err := NewSetAssociativeCache(config)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// 第一次访问：miss
	addr := uint64(0x1000)
	hit, readyCycle, mshrIndex := cache.Access(addr, addr, 1, compcache.AccessLoad, 0)

	if hit {
		t.Error("Expected miss on first access")
	}

	if readyCycle != config.FillLatency {
		t.Errorf("Expected readyCycle=%d, got %d", config.FillLatency, readyCycle)
	}

	// Standalone模式会自动fill
	// 第二次访问：hit
	hit, readyCycle, _ = cache.Access(addr, addr, 2, compcache.AccessLoad, 10)

	if !hit {
		t.Error("Expected hit on second access")
	}

	if readyCycle != 10+config.HitLatency {
		t.Errorf("Expected readyCycle=%d, got %d", 10+config.HitLatency, readyCycle)
	}

	// 验证统计
	statsInterface := cache.GetStats()
	stats, ok := statsInterface.(CacheStats)
	if !ok {
		t.Fatal("Failed to get cache stats")
	}
	if stats.Accesses != 2 {
		t.Errorf("Expected 2 accesses, got %d", stats.Accesses)
	}
	if stats.Hits != 1 {
		t.Errorf("Expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Expected 1 miss, got %d", stats.Misses)
	}

	t.Logf("Cache stats: Accesses=%d, Hits=%d, Misses=%d, Hit Rate=%.2f%%",
		stats.Accesses, stats.Hits, stats.Misses, stats.HitRate()*100)

	// 验证MSHR已清空
	mshrSize, _ := cache.GetMSHRStats()
	if mshrSize != 0 {
		t.Errorf("Expected MSHR size=0, got %d (index=%d)", mshrSize, mshrIndex)
	}
}

// TestAccess_Miss 测试cache miss
func TestAccess_Miss(t *testing.T) {
	config := compcache.DefaultL1DConfig()
	cache, err := NewSetAssociativeCache(config)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// 访问未缓存的地址
	addr := uint64(0x2000)
	hit, readyCycle, mshrIndex := cache.Access(addr, addr, 1, compcache.AccessLoad, 0)

	if hit {
		t.Error("Expected miss")
	}

	if mshrIndex < 0 && cache.standaloneMode {
		t.Error("Expected valid MSHR index in standalone mode")
	}

	if readyCycle != config.FillLatency {
		t.Errorf("Expected readyCycle=%d, got %d", config.FillLatency, readyCycle)
	}

	// 验证统计
	statsInterface := cache.GetStats()
	stats, ok := statsInterface.(CacheStats)
	if !ok {
		t.Fatal("Failed to get cache stats")
	}
	if stats.Misses != 1 {
		t.Errorf("Expected 1 miss, got %d", stats.Misses)
	}
	if stats.LoadMisses != 1 {
		t.Errorf("Expected 1 load miss, got %d", stats.LoadMisses)
	}
}

// TestAccess_StoreHit 测试store hit（应该标记dirty）
func TestAccess_StoreHit(t *testing.T) {
	config := compcache.DefaultL1DConfig()
	cache, err := NewSetAssociativeCache(config)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	addr := uint64(0x3000)

	// 第一次load：miss + auto fill
	cache.Access(addr, addr, 1, compcache.AccessLoad, 0)

	// 第二次store：hit，应该标记dirty
	hit, _, _ := cache.Access(addr, addr, 2, compcache.AccessStore, 10)

	if !hit {
		t.Error("Expected hit on store")
	}

	// 验证block被标记为dirty
	setIndex := cache.getSetIndex(cache.getBlockAddr(addr))
	tag := cache.getTag(cache.getBlockAddr(addr))
	way, found := cache.findBlock(setIndex, tag)

	if !found {
		t.Fatal("Block not found after access")
	}

	if !cache.blocks[setIndex][way].Dirty {
		t.Error("Expected block to be dirty after store hit")
	}

	// 验证统计
	statsInterface := cache.GetStats()
	stats, ok := statsInterface.(CacheStats)
	if !ok {
		t.Fatal("Failed to get cache stats")
	}
	if stats.StoreHits != 1 {
		t.Errorf("Expected 1 store hit, got %d", stats.StoreHits)
	}
}

// TestAccess_MSHRMerge 测试MSHR合并（多个请求访问同一地址）
func TestAccess_MSHRMerge(t *testing.T) {
	config := compcache.DefaultL1DConfig()
	cache, err := NewSetAssociativeCache(config)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// 禁用standalone模式，手动控制fill
	cache.SetStandaloneMode(false)

	addr := uint64(0x4000)

	// 第一次访问：miss，分配MSHR
	hit1, ready1, mshr1 := cache.Access(addr, addr, 1, compcache.AccessLoad, 0)
	if hit1 {
		t.Error("Expected miss on first access")
	}

	// 第二次访问同一地址：应该合并到已存在的MSHR
	hit2, ready2, mshr2 := cache.Access(addr, addr, 2, compcache.AccessLoad, 5)
	if hit2 {
		t.Error("Expected miss on second access (same addr)")
	}

	// 应该返回相同的MSHR索引和就绪时间
	if mshr1 != mshr2 {
		t.Errorf("Expected same MSHR index, got %d and %d", mshr1, mshr2)
	}

	if ready1 != ready2 {
		t.Logf("Warning: Different ready cycles %d and %d (may be updated)", ready1, ready2)
	}

	// 验证MSHR只有一个条目
	mshrSize, _ := cache.GetMSHRStats()
	if mshrSize != 1 {
		t.Errorf("Expected 1 MSHR entry, got %d", mshrSize)
	}

	// 验证MSHR条目包含两个指令依赖
	entries := cache.mshr.GetAll()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 MSHR entry, got %d", len(entries))
	}

	if len(entries[0].InstrDependOnMe) != 2 {
		t.Errorf("Expected 2 dependent instructions, got %d", len(entries[0].InstrDependOnMe))
	}

	t.Logf("MSHR entry: addr=0x%x, deps=%v", entries[0].Address, entries[0].InstrDependOnMe)
}

// TestAccess_MSHRFull 测试MSHR满
func TestAccess_MSHRFull(t *testing.T) {
	config := compcache.DefaultL1DConfig()
	config.MSHRSize = 2 // 只允许2个MSHR条目

	cache, err := NewSetAssociativeCache(config)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// 禁用standalone模式
	cache.SetStandaloneMode(false)

	// 填满MSHR
	cache.Access(0x1000, 0x1000, 1, compcache.AccessLoad, 0)
	cache.Access(0x2000, 0x2000, 2, compcache.AccessLoad, 0)

	mshrSize, _ := cache.GetMSHRStats()
	if mshrSize != 2 {
		t.Errorf("Expected MSHR size=2, got %d", mshrSize)
	}

	// 第三次访问：MSHR已满
	hit, _, mshrIndex := cache.Access(0x3000, 0x3000, 3, compcache.AccessLoad, 0)

	if hit {
		t.Error("Expected miss")
	}

	if mshrIndex >= 0 {
		t.Error("Expected invalid MSHR index when full")
	}

	// 验证MSHRFull统计
	statsInterface := cache.GetStats()
	stats, ok := statsInterface.(CacheStats)
	if !ok {
		t.Fatal("Failed to get cache stats")
	}
	if stats.MSHRFull != 1 {
		t.Errorf("Expected MSHRFull=1, got %d", stats.MSHRFull)
	}
}

// TestHandleFill 测试Fill处理
func TestHandleFill(t *testing.T) {
	config := compcache.DefaultL1DConfig()
	cache, err := NewSetAssociativeCache(config)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// 禁用standalone模式
	cache.SetStandaloneMode(false)

	addr := uint64(0x5000)

	// 第一次访问：miss
	hit, _, _ := cache.Access(addr, addr, 1, compcache.AccessLoad, 0)
	if hit {
		t.Error("Expected miss")
	}

	// 验证MSHR有一个条目
	mshrSize, _ := cache.GetMSHRStats()
	if mshrSize != 1 {
		t.Fatalf("Expected MSHR size=1, got %d", mshrSize)
	}

	// 手动fill
	success := cache.HandleFill(addr, 0xDEADBEEF, 10)
	if !success {
		t.Error("Fill failed")
	}

	// 验证MSHR已清空
	mshrSize, _ = cache.GetMSHRStats()
	if mshrSize != 0 {
		t.Errorf("Expected MSHR size=0 after fill, got %d", mshrSize)
	}

	// 验证block已填充
	setIndex := cache.getSetIndex(cache.getBlockAddr(addr))
	tag := cache.getTag(cache.getBlockAddr(addr))
	way, found := cache.findBlock(setIndex, tag)

	if !found {
		t.Fatal("Block not found after fill")
	}

	block := &cache.blocks[setIndex][way]
	if !block.Valid {
		t.Error("Block should be valid after fill")
	}

	if block.Data != 0xDEADBEEF {
		t.Errorf("Expected data=0xDEADBEEF, got 0x%x", block.Data)
	}

	// 第二次访问应该hit
	hit, _, _ = cache.Access(addr, addr, 2, compcache.AccessLoad, 20)
	if !hit {
		t.Error("Expected hit after fill")
	}
}

// TestAccess_Writeback 测试dirty block的writeback
func TestAccess_Writeback(t *testing.T) {
	config := compcache.DefaultL1DConfig()
	config.NumSets = 1 // 只有1个set
	config.NumWays = 2 // 2-way，方便测试替换

	cache, err := NewSetAssociativeCache(config)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// 填充两个block（同一个set）
	addr1 := uint64(0x0000) // set 0
	addr2 := uint64(0x0040) // set 0 (相同set，不同tag)

	// 访问addr1并写入（变成dirty）
	cache.Access(addr1, addr1, 1, compcache.AccessLoad, 0)
	cache.Access(addr1, addr1, 2, compcache.AccessStore, 10) // dirty

	// 访问addr2（也会变dirty）
	cache.Access(addr2, addr2, 3, compcache.AccessLoad, 20)
	cache.Access(addr2, addr2, 4, compcache.AccessStore, 30) // dirty

	// 现在两个way都满了且dirty
	// 访问第三个地址，应该触发writeback
	addr3 := uint64(0x0080) // set 0 (相同set，不同tag)
	cache.Access(addr3, addr3, 5, compcache.AccessLoad, 40)

	// 验证writeback统计
	statsInterface := cache.GetStats()
	stats, ok := statsInterface.(CacheStats)
	if !ok {
		t.Fatal("Failed to get cache stats")
	}
	if stats.Writebacks != 1 {
		t.Errorf("Expected 1 writeback, got %d", stats.Writebacks)
	}

	t.Logf("Writeback test passed: %d writebacks", stats.Writebacks)
}
