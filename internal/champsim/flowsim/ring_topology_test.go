package flowsim

import (
	"testing"

	"github.com/Readm/flow_sim/internal/components/coherence"
)

// 测试 Ring 拓扑节点 ID 分配
func TestRingTopologyNodeIDs(t *testing.T) {
	config := DefaultRingTopologyConfig()
	nodeIDs := BuildRingTopologyNodeIDs(config)

	// 验证数量
	expectedCPUs := config.NumClusters * config.CPUsPerL3 // 8 * 4 = 32
	if len(nodeIDs.CPUIDs) != expectedCPUs {
		t.Errorf("期望 %d 个 CPU，实际 %d 个", expectedCPUs, len(nodeIDs.CPUIDs))
	}

	if len(nodeIDs.L2IDs) != expectedCPUs { // L2PerCPU = 1
		t.Errorf("期望 %d 个 L2，实际 %d 个", expectedCPUs, len(nodeIDs.L2IDs))
	}

	if len(nodeIDs.L3IDs) != config.NumClusters {
		t.Errorf("期望 %d 个 L3，实际 %d 个", config.NumClusters, len(nodeIDs.L3IDs))
	}

	if len(nodeIDs.HAIDs) != config.NumClusters {
		t.Errorf("期望 %d 个 HA，实际 %d 个", config.NumClusters, len(nodeIDs.HAIDs))
	}

	if len(nodeIDs.DRAMIDs) != config.NumClusters {
		t.Errorf("期望 %d 个 DRAM，实际 %d 个", config.NumClusters, len(nodeIDs.DRAMIDs))
	}

	// 验证 ID 范围
	if nodeIDs.CPUIDs[0] != 0 || nodeIDs.CPUIDs[31] != 31 {
		t.Errorf("CPU ID 范围错误: [%d, %d]", nodeIDs.CPUIDs[0], nodeIDs.CPUIDs[31])
	}

	if nodeIDs.L2IDs[0] != 32 || nodeIDs.L2IDs[31] != 63 {
		t.Errorf("L2 ID 范围错误: [%d, %d]", nodeIDs.L2IDs[0], nodeIDs.L2IDs[31])
	}

	if nodeIDs.L3IDs[0] != 64 || nodeIDs.L3IDs[7] != 71 {
		t.Errorf("L3 ID 范围错误: [%d, %d]", nodeIDs.L3IDs[0], nodeIDs.L3IDs[7])
	}

	if nodeIDs.HAIDs[0] != 72 || nodeIDs.HAIDs[7] != 79 {
		t.Errorf("HA ID 范围错误: [%d, %d]", nodeIDs.HAIDs[0], nodeIDs.HAIDs[7])
	}

	if nodeIDs.DRAMIDs[0] != 80 || nodeIDs.DRAMIDs[7] != 87 {
		t.Errorf("DRAM ID 范围错误: [%d, %d]", nodeIDs.DRAMIDs[0], nodeIDs.DRAMIDs[7])
	}

	t.Log(" Ring 拓扑节点 ID 分配正确")
}

// 测试 Ring 拓扑映射关系
func TestRingTopologyMappings(t *testing.T) {
	config := DefaultRingTopologyConfig()
	nodeIDs := BuildRingTopologyNodeIDs(config)

	// 测试 CPU -> L2 映射
	cpuToL2 := GetCPUToL2Mapping(config, nodeIDs)
	if len(cpuToL2) != 32 {
		t.Errorf("CPU->L2 映射数量错误: %d", len(cpuToL2))
	}

	// 验证几个样本
	if cpuToL2[0] != 32 {
		t.Errorf("CPU 0 应该连接 L2 32，实际 %d", cpuToL2[0])
	}
	if cpuToL2[31] != 63 {
		t.Errorf("CPU 31 应该连接 L2 63，实际 %d", cpuToL2[31])
	}

	// 测试 L2 -> L3 映射
	l2ToL3 := GetL2ToL3Mapping(config, nodeIDs)
	if len(l2ToL3) != 32 {
		t.Errorf("L2->L3 映射数量错误: %d", len(l2ToL3))
	}

	// 验证每个 L3 连接 4 个 L2
	if l2ToL3[32] != 64 || l2ToL3[33] != 64 || l2ToL3[34] != 64 || l2ToL3[35] != 64 {
		t.Errorf("L2[32-35] 应该连接 L3 64")
	}
	if l2ToL3[60] != 71 || l2ToL3[61] != 71 || l2ToL3[62] != 71 || l2ToL3[63] != 71 {
		t.Errorf("L2[60-63] 应该连接 L3 71")
	}

	// 测试 L3 -> HA 映射
	l3ToHA := GetL3ToHAMapping(nodeIDs)
	if len(l3ToHA) != 8 {
		t.Errorf("L3->HA 映射数量错误: %d", len(l3ToHA))
	}

	if l3ToHA[64] != 72 {
		t.Errorf("L3 64 应该连接 HA 72，实际 %d", l3ToHA[64])
	}
	if l3ToHA[71] != 79 {
		t.Errorf("L3 71 应该连接 HA 79，实际 %d", l3ToHA[71])
	}

	// 测试 HA -> DRAM 映射
	haToDRAM := GetHAToDRAMMapping(nodeIDs)
	if len(haToDRAM) != 8 {
		t.Errorf("HA->DRAM 映射数量错误: %d", len(haToDRAM))
	}

	if haToDRAM[72] != 80 {
		t.Errorf("HA 72 应该连接 DRAM 80，实际 %d", haToDRAM[72])
	}
	if haToDRAM[79] != 87 {
		t.Errorf("HA 79 应该连接 DRAM 87，实际 %d", haToDRAM[79])
	}

	t.Log(" Ring 拓扑映射关系正确")
}

// 测试 Ring 邻居关系
func TestRingNeighbors(t *testing.T) {
	config := DefaultRingTopologyConfig()
	nodeIDs := BuildRingTopologyNodeIDs(config)

	// 测试 HA72 的邻居
	left, right := GetRingNeighbors(nodeIDs, 72)
	if left != 79 || right != 73 {
		t.Errorf("HA 72 的邻居应该是 (79, 73)，实际 (%d, %d)", left, right)
	}

	// 测试 HA79 的邻居 (最后一个，应该环回)
	left, right = GetRingNeighbors(nodeIDs, 79)
	if left != 78 || right != 72 {
		t.Errorf("HA 79 的邻居应该是 (78, 72)，实际 (%d, %d)", left, right)
	}

	// 测试中间的 HA
	left, right = GetRingNeighbors(nodeIDs, 75)
	if left != 74 || right != 76 {
		t.Errorf("HA 75 的邻居应该是 (74, 76)，实际 (%d, %d)", left, right)
	}

	t.Log(" Ring 邻居关系正确")
}

// 测试 Ring 拓扑一致性树构建
func TestBuildRingCoherenceTree(t *testing.T) {
	config := DefaultRingTopologyConfig()
	nodeIDs := BuildRingTopologyNodeIDs(config)

	addrConfig := coherence.AddressMappingConfig{
		Granularity: 64, // 64B cache line
		Strategy:    coherence.MappingInterleaved,
	}

	tree, err := BuildRingCoherenceTree(config, nodeIDs, addrConfig)
	if err != nil {
		t.Fatalf("构建一致性树失败: %v", err)
	}

	// 验证 Directory 数量（8 个 HA + 1 个虚拟根节点）
	if len(tree.DirectoryNodes) != 9 {
		t.Errorf("应该有 9 个 Directory (8 HA + 1 虚拟根节点)，实际 %d 个", len(tree.DirectoryNodes))
	}

	// 验证虚拟根节点存在
	virtualRootID := 1000
	if _, exists := tree.DirectoryNodes[virtualRootID]; !exists {
		t.Errorf("虚拟根节点 %d 不存在", virtualRootID)
	}

	// 验证每个 HA 的 Domain
	for clusterIdx := 0; clusterIdx < config.NumClusters; clusterIdx++ {
		haID := nodeIDs.HAIDs[clusterIdx]
		haNode, exists := tree.DirectoryNodes[haID]
		if !exists {
			t.Errorf("HA %d 不在 DirectoryNodes 中", haID)
			continue
		}

		// 每个 HA 应该管理 1 个 L3 + 4 个 L2 + 4 个 CPU = 9 个节点
		expectedDomainSize := 1 + config.CPUsPerL3 + config.CPUsPerL3
		if len(haNode.Domain) != expectedDomainSize {
			t.Errorf("HA %d 应该管理 %d 个节点，实际 %d 个",
				haID, expectedDomainSize, len(haNode.Domain))
		}

		t.Logf("HA %d Domain: %v", haID, haNode.Domain)
	}

	// 测试地址映射
	router := coherence.NewCoherenceRouter(0, tree) // CPU 0 的路由器

	// 测试几个地址
	testAddresses := []struct {
		addr           uint64
		expectedHomeHA int
	}{
		{0x0000, 72}, // (0 / 64) % 8 = 0 -> HA72
		{0x0040, 73}, // (64 / 64) % 8 = 1 -> HA73
		{0x0080, 74}, // (128 / 64) % 8 = 2 -> HA74
		{0x0200, 72}, // (512 / 64) % 8 = 0 -> HA72
	}

	for _, tc := range testAddresses {
		homeNodeID, err := router.GetHomeNode(tc.addr)
		if err != nil {
			t.Errorf("GetHomeNode(0x%X) 失败: %v", tc.addr, err)
			continue
		}

		if homeNodeID != tc.expectedHomeHA {
			t.Errorf("地址 0x%X 的 Home Node 应该是 %d，实际 %d",
				tc.addr, tc.expectedHomeHA, homeNodeID)
		}
	}

	// 验证树结构
	if err := tree.Validate(); err != nil {
		t.Errorf("一致性树验证失败: %v", err)
	}

	t.Log(" Ring 拓扑一致性树构建正确")
}

// 测试跨 Domain 路由
func TestRingCrossDomainRouting(t *testing.T) {
	config := DefaultRingTopologyConfig()
	nodeIDs := BuildRingTopologyNodeIDs(config)

	addrConfig := coherence.AddressMappingConfig{
		Granularity: 64,
		Strategy:    coherence.MappingInterleaved,
	}

	tree, err := BuildRingCoherenceTree(config, nodeIDs, addrConfig)
	if err != nil {
		t.Fatalf("构建一致性树失败: %v", err)
	}

	// CPU 0 (属于 HA72 Domain) 访问 HA73 管理的地址
	router := coherence.NewCoherenceRouter(0, tree)

	// 地址 0x0040 由 HA73 管理
	nextHop, err := router.RouteForCoherence(0x0040)
	if err != nil {
		t.Fatalf("RouteForCoherence 失败: %v", err)
	}

	// CPU 0 的一致性下一跳应该是其所属的 HA (72)
	// 注：CoherenceRouter 返回的是一致性路径，不是物理路径
	// 物理上 CPU 0 -> L2 32 -> L3 64 -> HA 72，但一致性路径是 CPU 0 -> HA 72
	expectedNextHop := 72 // HA72
	if nextHop != expectedNextHop {
		t.Errorf("CPU 0 的一致性下一跳应该是 %d (HA72)，实际 %d", expectedNextHop, nextHop)
	}

	t.Logf("CPU 0 访问 HA73 管理的地址，一致性下一跳: %d (HA72)", nextHop)

	// 获取完整的一致性路径
	path, err := tree.GetCoherencePath(0, 0x0040)
	if err != nil {
		t.Fatalf("GetCoherencePath 失败: %v", err)
	}

	// 路径应该是：CPU 0 -> HA72 -> 虚拟根节点 -> HA73
	expectedPath := []int{0, 72, 1000, 73}
	if len(path) != len(expectedPath) {
		t.Errorf("路径长度错误，期望 %d，实际 %d", len(expectedPath), len(path))
	} else {
		pathMatch := true
		for i := range path {
			if path[i] != expectedPath[i] {
				pathMatch = false
				break
			}
		}
		if !pathMatch {
			t.Errorf("路径错误，期望 %v，实际 %v", expectedPath, path)
		}
	}

	t.Logf("完整一致性路径: %v", path)

	t.Log(" Ring 拓扑跨 Domain 路由正确")
}
