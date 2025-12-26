package cache

import (
	"testing"
)

// TestNewSetAssociativeCache 测试创建cache
func TestNewSetAssociativeCache(t *testing.T) {
	config := DefaultL1DConfig()
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

	// 验证blocks数组
	if len(cache.blocks) != 64 {
		t.Errorf("Expected 64 sets, got %d", len(cache.blocks))
	}

	for i, set := range cache.blocks {
		if len(set) != 8 {
			t.Errorf("Set %d: expected 8 ways, got %d", i, len(set))
		}
	}
}

// TestValidateConfig 测试配置验证
func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  CacheConfig
		wantErr bool
	}{
		{
			name: "Valid config",
			config: CacheConfig{
				NumSets:   64,
				NumWays:   8,
				BlockSize: 64,
				MSHRSize:  8,
			},
			wantErr: false,
		},
		{
			name: "NumSets not power of 2",
			config: CacheConfig{
				NumSets:   63,
				NumWays:   8,
				BlockSize: 64,
				MSHRSize:  8,
			},
			wantErr: true,
		},
		{
			name: "NumWays zero",
			config: CacheConfig{
				NumSets:   64,
				NumWays:   0,
				BlockSize: 64,
				MSHRSize:  8,
			},
			wantErr: true,
		},
		{
			name: "BlockSize not power of 2",
			config: CacheConfig{
				NumSets:   64,
				NumWays:   8,
				BlockSize: 63,
				MSHRSize:  8,
			},
			wantErr: true,
		},
		{
			name: "MSHRSize zero",
			config: CacheConfig{
				NumSets:   64,
				NumWays:   8,
				BlockSize: 64,
				MSHRSize:  0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAddressDecomposition 测试地址分解
func TestAddressDecomposition(t *testing.T) {
	config := CacheConfig{
		NumSets:   64,   // 6 bits for set index
		NumWays:   8,
		BlockSize: 64,   // 6 bits for offset
		MSHRSize:  8,
	}

	cache, err := NewSetAssociativeCache(config)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// 验证位数计算
	if cache.offsetBits != 6 {
		t.Errorf("Expected offsetBits=6, got %d", cache.offsetBits)
	}

	if cache.setIndexBits != 6 {
		t.Errorf("Expected setIndexBits=6, got %d", cache.setIndexBits)
	}

	if cache.tagShift != 12 {
		t.Errorf("Expected tagShift=12, got %d", cache.tagShift)
	}

	// 测试地址分解
	// 地址: 0x123456
	// 二进制: 0001 0010 0011 0100 0101 0110
	//
	// 假设 offset=6 bits, set_index=6 bits, tag=剩余
	//
	// offset     = 0x56 & 0x3F = 0x16 = 22
	// set_index  = (0x123456 >> 6) & 0x3F = 0x4891 & 0x3F = 0x11 = 17
	// tag        = 0x123456 >> 12 = 0x123

	addr := uint64(0x123456)

	setIndex := cache.getSetIndex(addr)
	tag := cache.getTag(addr)
	blockAddr := cache.getBlockAddr(addr)

	t.Logf("Address: 0x%x", addr)
	t.Logf("  Set Index: %d (0x%x)", setIndex, setIndex)
	t.Logf("  Tag: %d (0x%x)", tag, tag)
	t.Logf("  Block Addr: 0x%x", blockAddr)

	expectedSetIndex := uint32(17)
	expectedTag := uint64(0x123)
	expectedBlockAddr := uint64(0x123440)

	if setIndex != expectedSetIndex {
		t.Errorf("Set index: expected %d, got %d", expectedSetIndex, setIndex)
	}

	if tag != expectedTag {
		t.Errorf("Tag: expected 0x%x, got 0x%x", expectedTag, tag)
	}

	if blockAddr != expectedBlockAddr {
		t.Errorf("Block addr: expected 0x%x, got 0x%x", expectedBlockAddr, blockAddr)
	}
}

// TestFindBlockAndLRU 测试查找和LRU
func TestFindBlockAndLRU(t *testing.T) {
	config := DefaultL1DConfig()
	cache, err := NewSetAssociativeCache(config)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// 测试地址
	addr := uint64(0x1000)
	setIndex := cache.getSetIndex(addr)
	tag := cache.getTag(addr)

	// 初始状态：所有block都无效
	way, hit := cache.findBlock(setIndex, tag)
	if hit {
		t.Error("Expected miss on empty cache")
	}

	// 填充一个block
	cache.blocks[setIndex][0].Valid = true
	cache.blocks[setIndex][0].Address = addr
	cache.blocks[setIndex][0].LRU = 100

	// 现在应该命中
	way, hit = cache.findBlock(setIndex, tag)
	if !hit {
		t.Error("Expected hit after filling block")
	}
	if way != 0 {
		t.Errorf("Expected way 0, got %d", way)
	}

	// 测试findVictim：应该返回第一个无效的way
	victimWay := cache.findVictim(setIndex)
	if victimWay != 1 {
		t.Errorf("Expected victim way 1 (first invalid), got %d", victimWay)
	}

	// 填充所有ways
	for i := uint32(0); i < cache.config.NumWays; i++ {
		cache.blocks[setIndex][i].Valid = true
		cache.blocks[setIndex][i].Address = addr + uint64(i)*64
		cache.blocks[setIndex][i].LRU = uint64(i * 10)
	}

	// 现在findVictim应该返回LRU最小的way
	victimWay = cache.findVictim(setIndex)
	if victimWay != 0 {
		t.Errorf("Expected victim way 0 (smallest LRU), got %d", victimWay)
	}

	// 更新LRU
	cache.SetCycle(200)
	cache.updateLRU(setIndex, 0)

	// 现在way 0的LRU应该是200
	if cache.blocks[setIndex][0].LRU != 200 {
		t.Errorf("Expected LRU=200, got %d", cache.blocks[setIndex][0].LRU)
	}

	// 现在findVictim应该返回way 1（LRU=10）
	victimWay = cache.findVictim(setIndex)
	if victimWay != 1 {
		t.Errorf("Expected victim way 1 (LRU=10), got %d", victimWay)
	}
}

// TestDefaultConfigs 测试默认配置
func TestDefaultConfigs(t *testing.T) {
	tests := []struct {
		name   string
		config CacheConfig
	}{
		{"L1D", DefaultL1DConfig()},
		{"L2C", DefaultL2CConfig()},
		{"LLC", DefaultLLCConfig()},
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
