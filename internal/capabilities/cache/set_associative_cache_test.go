package cache

import (
	"testing"

	compcache "github.com/Readm/flow_sim/internal/components/cache"
)

// TestNewSetAssociativeCache 测试创建cache
func TestNewSetAssociativeCache(t *testing.T) {
	config := compcache.DefaultL1DConfig()
	cache, err := NewSetAssociativeCache(config)

	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	if cache == nil {
		t.Fatal("Cache is nil")
	}

	// 验证配置
	if cache.config.NumSets != 64 {
		t.Errorf("Expected NumSets=64, got %d", cache.config.NumSets)
	}

	if cache.config.NumWays != 8 {
		t.Errorf("Expected NumWays=8, got %d", cache.config.NumWays)
	}

	// 验证blocks数组 (已移至 core，不再直接验证)
	// if len(cache.blocks) != 64 {
	// 	t.Errorf("Expected 64 sets, got %d", len(cache.blocks))
	// }
}

// TestAddressDecomposition 已移除，逻辑移至 internal/components/cache

// TestFindBlockAndLRU 已移除，逻辑移至 internal/components/cache

// TestDefaultConfigs 测试默认配置
func TestDefaultConfigs(t *testing.T) {
	tests := []struct {
		name   string
		config compcache.CacheConfig
	}{
		{"L1D", compcache.DefaultL1DConfig()},
		{"L2C", compcache.DefaultL2CConfig()},
		{"LLC", compcache.DefaultLLCConfig()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache, err := NewSetAssociativeCache(tt.config)
			if err != nil {
				t.Errorf("Failed to create %s cache: %v", tt.name, err)
			}

			if cache == nil {
				t.Errorf("%s cache is nil", tt.name)
			}

			t.Logf("%s Cache created:", tt.name)
			t.Logf("  Capacity: %d sets x %d ways = %d blocks",
				tt.config.NumSets, tt.config.NumWays,
				tt.config.NumSets*tt.config.NumWays)
			t.Logf("  Total size: %d KB",
				tt.config.NumSets*tt.config.NumWays*tt.config.BlockSize/1024)
			t.Logf("  MSHR: %d", tt.config.MSHRSize)
			t.Logf("  Latency: Hit=%d, Fill=%d",
				tt.config.HitLatency, tt.config.FillLatency)
		})
	}
}
