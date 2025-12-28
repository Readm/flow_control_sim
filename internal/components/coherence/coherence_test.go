package coherence

import (
	"testing"
)

// 测试配置 1: 单层 Directory（只在 L3）
func TestCoherenceTree_SingleLayer_L3Directory(t *testing.T) {
	t.Log("测试: 单层 Directory - L3 有 Directory，HA 没有")

	// 创建拓扑
	topology := &Topology{
		Nodes: map[int]*NodeDescriptor{
			0: {NodeID: 0, Capability: NodeCapability{Role: RoleCompute, CanInitiate: true}},
			1: {NodeID: 1, Capability: NodeCapability{Role: RoleCompute, CanInitiate: true}},
			2: {NodeID: 2, Capability: NodeCapability{Role: RoleCache, CacheLevel: 2, HasDirectory: false}},
			3: {NodeID: 3, Capability: NodeCapability{Role: RoleCache, CacheLevel: 3, HasDirectory: true}},  // ✅ L3 有 Directory
			4: {NodeID: 4, Capability: NodeCapability{Role: RoleMemoryCtrl, HasDirectory: false}},          // ❌ HA 没有 Directory
			5: {NodeID: 5, Capability: NodeCapability{Role: RoleMemory}},
		},
		Connections: map[int][]int{
			0: {2},
			1: {2},
			2: {0, 1, 3},
			3: {2, 4},
			4: {3, 5},
			5: {4},
		},
	}

	config := AddressMappingConfig{
		Granularity: 64,
		Strategy:    MappingInterleaved,
	}

	// 自动推断
	tree, err := BuildCoherenceTree(topology, config, nil)
	if err != nil {
		t.Fatalf("自动推断失败: %v", err)
	}

	// 验证
	if tree.Root == nil {
		t.Error("应该有根节点")
	}
	if tree.Root.NodeID != 3 {
		t.Errorf("根节点应该是 L3 (node 3)，实际是 %d", tree.Root.NodeID)
	}

	if len(tree.DirectoryNodes) != 1 {
		t.Errorf("应该只有 1 个 Directory 节点，实际有 %d 个", len(tree.DirectoryNodes))
	}

	// 测试路由
	router := NewCoherenceRouter(0, tree) // CPU 0 的路由器

	nextHop, err := router.RouteForCoherence(0x1000)
	if err != nil {
		t.Fatalf("路由失败: %v", err)
	}

	// CPU 0 应该路由到 L3 (node 3)
	// 一致性路径：CPU 0 → L3 (3)（一致性节点）
	// 物理路径：CPU 0 → L2 (2) → L3 (3)（实际传输路径）
	// 但 GetCoherencePath 返回的是一致性路径，只包含 Directory 节点
	// 所以下一跳应该是 L3 (3)
	if nextHop != 3 {
		t.Errorf("CPU 0 的下一跳应该是 L3 (3)，实际是 %d", nextHop)
	}

	t.Log("✅ 测试通过：单层 Directory (L3)")
}

// 测试配置 2: 单层 Directory（只在 HA）
func TestCoherenceTree_SingleLayer_HADirectory(t *testing.T) {
	t.Log("测试: 单层 Directory - L3 没有 Directory，HA 有")

	topology := &Topology{
		Nodes: map[int]*NodeDescriptor{
			0: {NodeID: 0, Capability: NodeCapability{Role: RoleCompute, CanInitiate: true}},
			1: {NodeID: 1, Capability: NodeCapability{Role: RoleCompute, CanInitiate: true}},
			2: {NodeID: 2, Capability: NodeCapability{Role: RoleCache, CacheLevel: 2, HasDirectory: false}},
			3: {NodeID: 3, Capability: NodeCapability{Role: RoleCache, CacheLevel: 3, HasDirectory: false}},  // ❌ L3 没有 Directory
			4: {NodeID: 4, Capability: NodeCapability{Role: RoleMemoryCtrl, HasDirectory: true}},            // ✅ HA 有 Directory
			5: {NodeID: 5, Capability: NodeCapability{Role: RoleMemory}},
		},
		Connections: map[int][]int{
			0: {2},
			1: {2},
			2: {0, 1, 3},
			3: {2, 4},
			4: {3, 5},
			5: {4},
		},
	}

	config := AddressMappingConfig{
		Granularity: 64,
		Strategy:    MappingInterleaved,
	}

	tree, err := BuildCoherenceTree(topology, config, nil)
	if err != nil {
		t.Fatalf("自动推断失败: %v", err)
	}

	// 验证
	if tree.Root == nil {
		t.Error("应该有根节点")
	}
	if tree.Root.NodeID != 4 {
		t.Errorf("根节点应该是 HA (node 4)，实际是 %d", tree.Root.NodeID)
	}

	if len(tree.DirectoryNodes) != 1 {
		t.Errorf("应该只有 1 个 Directory 节点，实际有 %d 个", len(tree.DirectoryNodes))
	}

	// 测试路由
	router := NewCoherenceRouter(0, tree)

	nextHop, err := router.RouteForCoherence(0x1000)
	if err != nil {
		t.Fatalf("路由失败: %v", err)
	}

	// CPU 0 应该路由到 HA (node 4)
	// 一致性路径：CPU 0 → HA (4)（只有一个 Directory）
	// 物理路径：CPU 0 → L2 (2) → L3 (3) → HA (4)
	// 下一跳应该是 HA (4)
	if nextHop != 4 {
		t.Errorf("CPU 0 的下一跳应该是 HA (4)，实际是 %d", nextHop)
	}

	t.Log("✅ 测试通过：单层 Directory (HA)")
}

// 测试配置 3: 两层 Directory（L3 + HA）
func TestCoherenceTree_TwoLayer_L3AndHA(t *testing.T) {
	t.Log("测试: 两层 Directory - L3 和 HA 都有 Directory")

	topology := &Topology{
		Nodes: map[int]*NodeDescriptor{
			0: {NodeID: 0, Capability: NodeCapability{Role: RoleCompute, CanInitiate: true}},
			1: {NodeID: 1, Capability: NodeCapability{Role: RoleCompute, CanInitiate: true}},
			2: {NodeID: 2, Capability: NodeCapability{Role: RoleCache, CacheLevel: 2, HasDirectory: false}},
			3: {NodeID: 3, Capability: NodeCapability{Role: RoleCache, CacheLevel: 3, HasDirectory: true}},  // ✅ L3 有 Directory
			4: {NodeID: 4, Capability: NodeCapability{Role: RoleMemoryCtrl, HasDirectory: true}},           // ✅ HA 有 Directory
			5: {NodeID: 5, Capability: NodeCapability{Role: RoleMemory}},
		},
		Connections: map[int][]int{
			0: {2},
			1: {2},
			2: {0, 1, 3},
			3: {2, 4},
			4: {3, 5},
			5: {4},
		},
	}

	config := AddressMappingConfig{
		Granularity: 64,
		Strategy:    MappingInterleaved,
	}

	tree, err := BuildCoherenceTree(topology, config, nil)
	if err != nil {
		t.Fatalf("自动推断失败: %v", err)
	}

	// 验证
	if tree.Root == nil {
		t.Error("应该有根节点")
	}
	if tree.Root.NodeID != 4 {
		t.Errorf("根节点应该是 HA (node 4)，实际是 %d", tree.Root.NodeID)
	}

	if len(tree.DirectoryNodes) != 2 {
		t.Errorf("应该有 2 个 Directory 节点，实际有 %d 个", len(tree.DirectoryNodes))
	}

	// 验证层次关系
	l3Node := tree.DirectoryNodes[3]
	haNode := tree.DirectoryNodes[4]

	if l3Node.Parent != haNode {
		t.Error("L3 的父节点应该是 HA")
	}

	if len(haNode.Children) != 1 || haNode.Children[0].NodeID != 3 {
		t.Error("HA 的子节点应该是 L3")
	}

	// 测试路由：CPU 0 访问地址
	router := NewCoherenceRouter(0, tree)

	homeNodeID, err := router.GetHomeNode(0x1000)
	if err != nil {
		t.Fatalf("GetHomeNode 失败: %v", err)
	}

	// 在两层 Directory 中，Home Node 应该是 L3（最低层的 Directory）
	// 修正后的实现会返回最低层的 Directory 节点
	if homeNodeID != 3 {
		t.Errorf("Home Node 应该是 L3 (3)，实际是 %d", homeNodeID)
	}

	// 测试从 CPU 0 到 Home Node 的路径
	path, err := tree.GetCoherencePath(0, 0x1000)
	if err != nil {
		t.Fatalf("GetCoherencePath 失败: %v", err)
	}

	// 一致性路径应该是：CPU 0 → L3 (3)
	expectedPath := []int{0, 3}
	if len(path) != len(expectedPath) {
		t.Errorf("路径长度不匹配，期望 %d，实际 %d。路径: %v", len(expectedPath), len(path), path)
	}

	t.Log("✅ 测试通过：两层 Directory (L3 + HA)")
}

// 测试配置 4: 多个 L3 Slice（分布式 Directory）
func TestCoherenceTree_MultipleL3Slices(t *testing.T) {
	t.Log("测试: 多个 L3 Slice - 每个 L3 都有 Directory")

	topology := &Topology{
		Nodes: map[int]*NodeDescriptor{
			0: {NodeID: 0, Capability: NodeCapability{Role: RoleCompute, CanInitiate: true}},
			1: {NodeID: 1, Capability: NodeCapability{Role: RoleCompute, CanInitiate: true}},
			2: {NodeID: 2, Capability: NodeCapability{Role: RoleCompute, CanInitiate: true}},
			3: {NodeID: 3, Capability: NodeCapability{Role: RoleCompute, CanInitiate: true}},

			4: {NodeID: 4, Capability: NodeCapability{Role: RoleCache, CacheLevel: 2, HasDirectory: false}},
			5: {NodeID: 5, Capability: NodeCapability{Role: RoleCache, CacheLevel: 2, HasDirectory: false}},

			6: {NodeID: 6, Capability: NodeCapability{Role: RoleCache, CacheLevel: 3, HasDirectory: true}},  // ✅ L3[0]
			7: {NodeID: 7, Capability: NodeCapability{Role: RoleCache, CacheLevel: 3, HasDirectory: true}},  // ✅ L3[1]

			8: {NodeID: 8, Capability: NodeCapability{Role: RoleMemoryCtrl, HasDirectory: false}},
			9: {NodeID: 9, Capability: NodeCapability{Role: RoleMemory}},
		},
		Connections: map[int][]int{
			0: {4}, 1: {4},
			2: {5}, 3: {5},
			4: {0, 1, 6},
			5: {2, 3, 7},
			6: {4, 8},
			7: {5, 8},
			8: {6, 7, 9},
			9: {8},
		},
	}

	config := AddressMappingConfig{
		Granularity: 64,
		Strategy:    MappingInterleaved,
	}

	tree, err := BuildCoherenceTree(topology, config, nil)
	if err != nil {
		t.Fatalf("自动推断失败: %v", err)
	}

	// 验证
	if tree.Root != nil {
		t.Error("多个同层级的根节点，Root 应该为 nil")
	}

	if len(tree.DirectoryNodes) != 2 {
		t.Errorf("应该有 2 个 Directory 节点，实际有 %d 个", len(tree.DirectoryNodes))
	}

	// 测试地址映射
	homeNode0, _ := tree.GetHomeNode(0x0000) // 地址 0 → L3[0] (node 6)
	homeNode1, _ := tree.GetHomeNode(0x0040) // 地址 64 → L3[1] (node 7)

	if homeNode0 == homeNode1 {
		t.Error("不同地址应该映射到不同的 L3 Slice")
	}

	t.Logf("地址 0x0000 → Home Node %d", homeNode0)
	t.Logf("地址 0x0040 → Home Node %d", homeNode1)

	t.Log("✅ 测试通过：多个 L3 Slice")
}

// 测试配置 5: 无 Directory 节点（应该失败）
func TestCoherenceTree_NoDirectory_ShouldFail(t *testing.T) {
	t.Log("测试: 无 Directory 节点 - 应该报警并失败")

	topology := &Topology{
		Nodes: map[int]*NodeDescriptor{
			0: {NodeID: 0, Capability: NodeCapability{Role: RoleCompute, CanInitiate: true}},
			1: {NodeID: 1, Capability: NodeCapability{Role: RoleCache, CacheLevel: 2, HasDirectory: false}}, // ❌ 没有 Directory
			2: {NodeID: 2, Capability: NodeCapability{Role: RoleMemoryCtrl, HasDirectory: false}},           // ❌ 没有 Directory
			3: {NodeID: 3, Capability: NodeCapability{Role: RoleMemory}},
		},
		Connections: map[int][]int{
			0: {1}, 1: {0, 2}, 2: {1, 3}, 3: {2},
		},
	}

	config := AddressMappingConfig{
		Granularity: 64,
		Strategy:    MappingInterleaved,
	}

	tree, err := BuildCoherenceTree(topology, config, nil)
	if err == nil {
		t.Error("❌ 应该失败（没有 Directory 节点），但成功了")
	}

	if tree != nil {
		t.Error("❌ 应该返回 nil tree，但返回了非 nil")
	}

	t.Logf("✅ 正确失败，错误信息: %v", err)
}

// 测试配置 6: 用户显式指定（复杂拓扑）
func TestCoherenceTree_ExplicitBuilder(t *testing.T) {
	t.Log("测试: 用户显式指定一致性树")

	config := AddressMappingConfig{
		Granularity: 128, // 128B 交错
		Strategy:    MappingInterleaved,
	}

	builder := NewCoherenceTreeBuilder(config)

	// 构建一个两层 Directory 结构
	builder.
		AddDirectory(10, RoleCache, CoherenceDomain{
			ManagedNodes: []int{0, 1}, // L3[0] 管理 CPU 0, 1
		}).
		AddDirectory(11, RoleCache, CoherenceDomain{
			ManagedNodes: []int{2, 3}, // L3[1] 管理 CPU 2, 3
		}).
		AddDirectory(20, RoleMemoryCtrl, CoherenceDomain{
			ManagedNodes: []int{10, 11}, // HA 管理两个 L3
		}).
		SetParent(10, 20).
		SetParent(11, 20)

	tree, err := builder.Build()
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}

	// 验证
	if tree.Root == nil {
		t.Error("应该有根节点")
	}
	if tree.Root.NodeID != 20 {
		t.Errorf("根节点应该是 HA (20)，实际是 %d", tree.Root.NodeID)
	}

	if len(tree.DirectoryNodes) != 3 {
		t.Errorf("应该有 3 个 Directory 节点，实际有 %d 个", len(tree.DirectoryNodes))
	}

	// 验证 Domain
	l3_0 := tree.DirectoryNodes[10]
	if len(l3_0.Domain) != 2 {
		t.Errorf("L3[0] 应该管理 2 个节点，实际管理 %d 个", len(l3_0.Domain))
	}

	t.Log("✅ 测试通过：用户显式指定")
}

// 测试配置 7: 地址映射粒度验证
func TestAddressMappingConfig_Validation(t *testing.T) {
	t.Log("测试: 地址映射配置验证")

	// 有效配置
	validConfigs := []AddressMappingConfig{
		{Granularity: 64, Strategy: MappingInterleaved},
		{Granularity: 128, Strategy: MappingInterleaved},
		{Granularity: 1024, Strategy: MappingInterleaved},
		{Granularity: 4096, Strategy: MappingInterleaved},
	}

	for _, config := range validConfigs {
		if err := config.Validate(); err != nil {
			t.Errorf("有效配置验证失败 (Granularity=%d): %v", config.Granularity, err)
		}
	}

	// 无效配置
	invalidConfigs := []AddressMappingConfig{
		{Granularity: 0, Strategy: MappingInterleaved},    // 0 无效
		{Granularity: 32, Strategy: MappingInterleaved},   // 32 不是 64 的倍数
		{Granularity: 100, Strategy: MappingInterleaved},  // 100 不是 64 的倍数
		{Granularity: 63, Strategy: MappingInterleaved},   // 63 不是 64 的倍数
	}

	for _, config := range invalidConfigs {
		if err := config.Validate(); err == nil {
			t.Errorf("无效配置应该失败 (Granularity=%d)，但通过了", config.Granularity)
		}
	}

	t.Log("✅ 测试通过：地址映射配置验证")
}

// 测试配置 8: 路由路径验证
func TestCoherenceRouter_PathValidation(t *testing.T) {
	t.Log("测试: 路由路径验证（跨 Domain 访问）")

	// 构建一个两层 Directory 结构
	config := AddressMappingConfig{
		Granularity: 64,
		Strategy:    MappingInterleaved,
	}

	builder := NewCoherenceTreeBuilder(config)
	builder.
		AddDirectory(10, RoleCache, CoherenceDomain{
			ManagedNodes: []int{0, 1}, // L3[0] 管理 CPU 0, 1
			AddressRange: &AddressRange{Start: 0x0000, End: 0x7FFF},
		}).
		AddDirectory(11, RoleCache, CoherenceDomain{
			ManagedNodes: []int{2, 3}, // L3[1] 管理 CPU 2, 3
			AddressRange: &AddressRange{Start: 0x8000, End: 0xFFFF},
		}).
		AddDirectory(20, RoleMemoryCtrl, CoherenceDomain{
			ManagedNodes: []int{10, 11},
		}).
		SetParent(10, 20).
		SetParent(11, 20)

	tree, _ := builder.Build()

	// CPU 0 访问属于 L3[1] 的地址 0x9000
	router := NewCoherenceRouter(0, tree)

	homeNodeID, err := router.GetHomeNode(0x9000)
	if err != nil {
		t.Fatalf("GetHomeNode 失败: %v", err)
	}

	if homeNodeID != 11 {
		t.Errorf("地址 0x9000 的 Home Node 应该是 L3[1] (11)，实际是 %d", homeNodeID)
	}

	// 先检查 CPU 0 的 Directory
	var cpu0Dir int
	for dirID, dirNode := range tree.DirectoryNodes {
		for _, memberID := range dirNode.Domain {
			if memberID == 0 {
				cpu0Dir = dirID
				break
			}
		}
	}
	t.Logf("CPU 0 的 Directory: %d", cpu0Dir)

	// 获取一致性路径：CPU 0 → L3[0] (10) → HA (20) → L3[1] (11)
	path, err := tree.GetCoherencePath(0, 0x9000)
	if err != nil {
		t.Fatalf("GetCoherencePath 失败: %v", err)
	}

	t.Logf("实际路径: %v", path)

	// 一致性路径应该包含：CPU 0, L3[0] (10), HA (20), L3[1] (11)
	expectedPath := []int{0, 10, 20, 11}
	if len(path) != len(expectedPath) {
		t.Errorf("路径长度不匹配，期望 %d，实际 %d。路径: %v", len(expectedPath), len(path), path)

		// 打印树结构帮助调试
		t.Logf("L3[0] (10) Domain: %v, Parent: %v", tree.DirectoryNodes[10].Domain, tree.DirectoryNodes[10].Parent)
		t.Logf("L3[1] (11) Domain: %v, Parent: %v", tree.DirectoryNodes[11].Domain, tree.DirectoryNodes[11].Parent)
		t.Logf("HA (20) Domain: %v, Children: %d", tree.DirectoryNodes[20].Domain, len(tree.DirectoryNodes[20].Children))

		return // 避免后续 panic
	}

	for i, nodeID := range expectedPath {
		if path[i] != nodeID {
			t.Errorf("路径第 %d 个节点应该是 %d，实际是 %d", i, nodeID, path[i])
		}
	}

	t.Logf("路径: %v", path)
	t.Log("✅ 测试通过：跨 Domain 路由路径正确")
}
