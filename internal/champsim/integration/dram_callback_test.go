package integration

import (
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/cache"
	"github.com/Readm/flow_sim/internal/champsim/dram"
	"github.com/Readm/flow_sim/internal/champsim/memory"
)

// Test_DRAM_Callback_Basic 测试DRAM callback是否被调用
func Test_DRAM_Callback_Basic(t *testing.T) {
	// 创建DRAM
	dramChannel, err := dram.NewDRAMChannel(dram.DefaultDRAMConfig())
	if err != nil {
		t.Fatalf("Failed to create DRAM: %v", err)
	}

	// 创建DRAM适配器
	dramAdapter := memory.NewDRAMAdapter(dramChannel)

	// 创建Cache
	l1dCache, err := cache.NewSetAssociativeCache(cache.DefaultL1DConfig())
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// 连接Cache到DRAM
	l1dCache.SetLowerLevel(dramAdapter)

	// 测试地址
	addr := uint64(0x1000)

	t.Log("========== 测试Cache Miss → DRAM Request → Callback ==========")

	// 1. 访问Cache（应该miss）
	hit, readyCycle, mshrIdx := l1dCache.Access(addr, addr, 1, 0, 0)
	t.Logf("Cache Access: hit=%v, readyCycle=%d, mshrIdx=%d", hit, readyCycle, mshrIdx)

	if hit {
		t.Fatal("Expected cache miss, got hit")
	}

	// 检查DRAM是否收到请求
	dramStats := dramChannel.GetStats()
	t.Logf("DRAM RQ size after access: %d", dramStats.RQAccesses)

	if dramStats.RQAccesses == 0 {
		t.Fatal("DRAM should have received a request")
	}

	// 2. 推进时钟，等待DRAM完成
	callbackCalled := false

	// 替换callback来跟踪是否被调用（注意：这需要在Access之前设置，所以这个测试需要重构）
	// 实际上，我们无法在外部替换callback，因为它在Access内部创建

	// 运行100个周期
	for cycle := uint64(0); cycle < 100; cycle++ {
		dramAdapter.Tick()
		dramAdapter.SetCycle(cycle)
	}

	// 检查DRAM统计
	finalStats := dramChannel.GetStats()
	t.Logf("Final DRAM stats:")
	t.Logf("  RQ Accesses: %d", finalStats.RQAccesses)
	t.Logf("  Row Buffer Hits: %d", finalStats.RQRowBufferHit)
	t.Logf("  Row Buffer Misses: %d", finalStats.RQRowBufferMiss)

	// 检查Cache的MSHR
	mshrSize, mshrCap := l1dCache.GetMSHRStats()
	t.Logf("Cache MSHR: %d/%d", mshrSize, mshrCap)

	// 检查Cache统计
	cacheStats := l1dCache.GetStats().(cache.CacheStats)
	t.Logf("Cache stats:")
	t.Logf("  Accesses: %d", cacheStats.Accesses)
	t.Logf("  Hits: %d", cacheStats.Hits)
	t.Logf("  Misses: %d", cacheStats.Misses)

	// 如果MSHR为空，说明HandleFill被调用了
	if mshrSize == 0 {
		t.Log("✅ MSHR is empty - HandleFill was called")
		callbackCalled = true
	} else {
		t.Errorf("❌ MSHR still has %d entries - HandleFill was NOT called", mshrSize)
	}

	// 验证第二次访问应该hit
	hit2, _, _ := l1dCache.Access(addr, addr, 2, 0, 100)
	t.Logf("Second access: hit=%v", hit2)

	if !callbackCalled {
		t.Error("❌ Callback was not called")
	} else if !hit2 {
		t.Error("❌ Second access should hit, but missed")
	} else {
		t.Log("✅ Test passed: Callback was called and data was filled")
	}
}
