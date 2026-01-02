package flowsim

import (
	"testing"

	"github.com/Readm/flow_sim/internal/components/coherence"
)

// 测试 ChampSim 拓扑的一致性树构建（Directory 在 L2）
func TestChampSimCoherenceTree_L2Directory(t *testing.T) {
	t.Log("测试: ChampSim 拓扑 - Directory 在 L2")

	cpuNodeIDs := []int{0, 1, 2, 3}
	l2NodeID := 4
	memCtrlNodeID := 8
	dramNodeIDs := []int{9, 10}

	config := coherence.AddressMappingConfig{
		Granularity: 64, // 64B cache line
		Strategy:    coherence.MappingInterleaved,
	}

	// 构建一致性树
	tree, err := BuildChampSimCoherenceTree(
		cpuNodeIDs,
		l2NodeID,
		memCtrlNodeID,
		dramNodeIDs,
		config,
	)
	if err != nil {
		t.Fatalf("构建一致性树失败: %v", err)
	}

	// 验证树结构
	if tree.Root == nil {
		t.Error("应该有根节点")
	}
	if tree.Root.NodeID != l2NodeID {
		t.Errorf("根节点应该是 L2 (node %d)，实际是 %d", l2NodeID, tree.Root.NodeID)
	}

	if len(tree.DirectoryNodes) != 1 {
		t.Errorf("应该只有 1 个 Directory 节点，实际有 %d 个", len(tree.DirectoryNodes))
	}

	// 验证 Domain
	l2Node := tree.DirectoryNodes[l2NodeID]
	if len(l2Node.Domain) != len(cpuNodeIDs) {
		t.Errorf("L2 应该管理 %d 个 CPU，实际管理 %d 个", len(cpuNodeIDs), len(l2Node.Domain))
	}

	// 测试路由：CPU 0 访问地址
	router := coherence.NewCoherenceRouter(cpuNodeIDs[0], tree)

	homeNodeID, err := router.GetHomeNode(0x1000)
	if err != nil {
		t.Fatalf("GetHomeNode 失败: %v", err)
	}

	if homeNodeID != l2NodeID {
		t.Errorf("地址 0x1000 的 Home Node 应该是 L2 (%d)，实际是 %d", l2NodeID, homeNodeID)
	}

	// 测试一致性路由
	nextHop, err := router.RouteForCoherence(0x1000)
	if err != nil {
		t.Fatalf("RouteForCoherence 失败: %v", err)
	}

	if nextHop != l2NodeID {
		t.Errorf("CPU 0 的一致性请求应该发送到 L2 (%d)，实际是 %d", l2NodeID, nextHop)
	}

	t.Log(" 测试通过：ChampSim 拓扑 (Directory 在 L2)")
}

// 测试 ChampSim 拓扑的一致性树构建（Directory 在 HA）
func TestChampSimCoherenceTree_HADirectory(t *testing.T) {
	t.Log("测试: ChampSim 拓扑 - Directory 在 HA")

	cpuNodeIDs := []int{0, 1, 2, 3}
	l2NodeID := 4
	haNodeID := 8
	dramNodeIDs := []int{9, 10}

	config := coherence.AddressMappingConfig{
		Granularity: 64,
		Strategy:    coherence.MappingInterleaved,
	}

	// 构建一致性树（Directory 在 HA）
	tree, err := BuildChampSimCoherenceTreeWithHADirectory(
		cpuNodeIDs,
		l2NodeID,
		haNodeID,
		dramNodeIDs,
		config,
	)
	if err != nil {
		t.Fatalf("构建一致性树失败: %v", err)
	}

	// 验证树结构
	if tree.Root == nil {
		t.Error("应该有根节点")
	}
	if tree.Root.NodeID != haNodeID {
		t.Errorf("根节点应该是 HA (node %d)，实际是 %d", haNodeID, tree.Root.NodeID)
	}

	if len(tree.DirectoryNodes) != 1 {
		t.Errorf("应该只有 1 个 Directory 节点，实际有 %d 个", len(tree.DirectoryNodes))
	}

	// 验证 Domain
	// HA 应该管理所有 CPU 和 L2（因为 L2 没有 Directory）
	haNode := tree.DirectoryNodes[haNodeID]
	expectedDomainSize := len(cpuNodeIDs) + 1 // CPU + L2
	if len(haNode.Domain) != expectedDomainSize {
		t.Errorf("HA 应该管理 %d 个节点（%d CPU + L2），实际管理 %d 个", expectedDomainSize, len(cpuNodeIDs), len(haNode.Domain))
	}

	// 测试路由：CPU 0 访问地址
	router := coherence.NewCoherenceRouter(cpuNodeIDs[0], tree)

	homeNodeID, err := router.GetHomeNode(0x1000)
	if err != nil {
		t.Fatalf("GetHomeNode 失败: %v", err)
	}

	if homeNodeID != haNodeID {
		t.Errorf("地址 0x1000 的 Home Node 应该是 HA (%d)，实际是 %d", haNodeID, homeNodeID)
	}

	// 测试一致性路由
	nextHop, err := router.RouteForCoherence(0x1000)
	if err != nil {
		t.Fatalf("RouteForCoherence 失败: %v", err)
	}

	if nextHop != haNodeID {
		t.Errorf("CPU 0 的一致性请求应该发送到 HA (%d)，实际是 %d", haNodeID, nextHop)
	}

	t.Log(" 测试通过：ChampSim 拓扑 (Directory 在 HA)")
}

// 测试地址映射到 DRAM
func TestChampSimCoherenceTree_AddressMapping(t *testing.T) {
	t.Log("测试: 地址映射到 DRAM Channel")

	cpuNodeIDs := []int{0, 1}
	l2NodeID := 4
	memCtrlNodeID := 8
	dramNodeIDs := []int{9, 10}

	config := coherence.AddressMappingConfig{
		Granularity: 64, // 64B 交错
		Strategy:    coherence.MappingInterleaved,
	}

	tree, err := BuildChampSimCoherenceTree(
		cpuNodeIDs,
		l2NodeID,
		memCtrlNodeID,
		dramNodeIDs,
		config,
	)
	if err != nil {
		t.Fatalf("构建一致性树失败: %v", err)
	}

	// 测试多个地址的映射
	addresses := []uint64{
		0x0000, // cache line 0
		0x0040, // cache line 1
		0x0080, // cache line 2
		0x00C0, // cache line 3
	}

	router := coherence.NewCoherenceRouter(cpuNodeIDs[0], tree)

	for _, addr := range addresses {
		homeNodeID, err := router.GetHomeNode(addr)
		if err != nil {
			t.Fatalf("GetHomeNode(0x%X) 失败: %v", addr, err)
		}

		// Home Node 应该是 L2（因为 L2 有 Directory）
		if homeNodeID != l2NodeID {
			t.Errorf("地址 0x%X 的 Home Node 应该是 L2 (%d)，实际是 %d", addr, l2NodeID, homeNodeID)
		}

		t.Logf("地址 0x%04X → Home Node %d (L2)", addr, homeNodeID)
	}

	t.Log(" 测试通过：地址映射")
}

// 测试多个 CPU 的路由
func TestChampSimCoherenceTree_MultiCPURouting(t *testing.T) {
	t.Log("测试: 多个 CPU 的路由")

	cpuNodeIDs := []int{0, 1, 2, 3}
	l2NodeID := 4
	memCtrlNodeID := 8
	dramNodeIDs := []int{9, 10}

	config := coherence.AddressMappingConfig{
		Granularity: 64,
		Strategy:    coherence.MappingInterleaved,
	}

	tree, err := BuildChampSimCoherenceTree(
		cpuNodeIDs,
		l2NodeID,
		memCtrlNodeID,
		dramNodeIDs,
		config,
	)
	if err != nil {
		t.Fatalf("构建一致性树失败: %v", err)
	}

	// 为每个 CPU 创建路由器
	for _, cpuID := range cpuNodeIDs {
		router := coherence.NewCoherenceRouter(cpuID, tree)

		// 测试路由
		nextHop, err := router.RouteForCoherence(0x1000)
		if err != nil {
			t.Fatalf("CPU %d RouteForCoherence 失败: %v", cpuID, err)
		}

		// 所有 CPU 都应该路由到 L2
		if nextHop != l2NodeID {
			t.Errorf("CPU %d 的一致性请求应该发送到 L2 (%d)，实际是 %d", cpuID, l2NodeID, nextHop)
		}

		t.Logf("CPU %d → L2 (%d) ", cpuID, nextHop)
	}

	t.Log(" 测试通过：多 CPU 路由")
}
