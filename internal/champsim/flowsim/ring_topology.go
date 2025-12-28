package flowsim

import (
	"github.com/Readm/flow_sim/internal/components/coherence"
)

// RingTopologyConfig Ring 拓扑配置
type RingTopologyConfig struct {
	NumClusters   int    // L3 集群数量 (默认 8)
	CPUsPerL3     int    // 每个 L3 下的 CPU 数量 (默认 4)
	L2PerCPU      int    // 每个 CPU 的 L2 数量 (默认 1)

	// 节点 ID 起始位置
	CPUIDStart    int    // CPU ID 起始 (默认 0)
	L2IDStart     int    // L2 ID 起始 (默认 32)
	L3IDStart     int    // L3 ID 起始 (默认 64)
	HAIDStart     int    // HA ID 起始 (默认 72)
	DRAMIDStart   int    // DRAM ID 起始 (默认 80)
}

// DefaultRingTopologyConfig 默认配置：32 核系统
func DefaultRingTopologyConfig() RingTopologyConfig {
	return RingTopologyConfig{
		NumClusters:   8,
		CPUsPerL3:     4,
		L2PerCPU:      1,
		CPUIDStart:    0,
		L2IDStart:     32,
		L3IDStart:     64,
		HAIDStart:     72,
		DRAMIDStart:   80,
	}
}

// RingTopologyNodeIDs Ring 拓扑的节点 ID 集合
type RingTopologyNodeIDs struct {
	CPUIDs  []int
	L2IDs   []int
	L3IDs   []int
	HAIDs   []int
	DRAMIDs []int
}

// BuildRingTopologyNodeIDs 构建 Ring 拓扑的节点 ID
func BuildRingTopologyNodeIDs(config RingTopologyConfig) *RingTopologyNodeIDs {
	numCPUs := config.NumClusters * config.CPUsPerL3
	numL2s := numCPUs * config.L2PerCPU

	nodeIDs := &RingTopologyNodeIDs{
		CPUIDs:  make([]int, numCPUs),
		L2IDs:   make([]int, numL2s),
		L3IDs:   make([]int, config.NumClusters),
		HAIDs:   make([]int, config.NumClusters),
		DRAMIDs: make([]int, config.NumClusters),
	}

	// 分配 CPU IDs
	for i := 0; i < numCPUs; i++ {
		nodeIDs.CPUIDs[i] = config.CPUIDStart + i
	}

	// 分配 L2 IDs
	for i := 0; i < numL2s; i++ {
		nodeIDs.L2IDs[i] = config.L2IDStart + i
	}

	// 分配 L3 IDs
	for i := 0; i < config.NumClusters; i++ {
		nodeIDs.L3IDs[i] = config.L3IDStart + i
	}

	// 分配 HA IDs
	for i := 0; i < config.NumClusters; i++ {
		nodeIDs.HAIDs[i] = config.HAIDStart + i
	}

	// 分配 DRAM IDs
	for i := 0; i < config.NumClusters; i++ {
		nodeIDs.DRAMIDs[i] = config.DRAMIDStart + i
	}

	return nodeIDs
}

// BuildRingCoherenceTree 为 Ring 拓扑构建一致性树
// 每个 HA 作为一个 Directory，管理其对应的 L3 及其下属的 CPU 和 L2
// 添加一个虚拟根节点（ID = 1000）作为所有 HA 的父节点，用于跨 Domain 路由
func BuildRingCoherenceTree(
	config RingTopologyConfig,
	nodeIDs *RingTopologyNodeIDs,
	addrConfig coherence.AddressMappingConfig,
) (*coherence.CoherenceTree, error) {
	// 使用 Builder 显式构建一致性树
	builder := coherence.NewCoherenceTreeBuilder(addrConfig)

	cpusPerCluster := config.CPUsPerL3

	// 虚拟根节点 ID（确保不与其他节点冲突）
	virtualRootID := 1000

	// 添加虚拟根节点（管理所有 HA）
	allHAIDs := make([]int, len(nodeIDs.HAIDs))
	copy(allHAIDs, nodeIDs.HAIDs)
	builder.AddDirectory(virtualRootID, coherence.RoleDirectory, coherence.CoherenceDomain{
		ManagedNodes: allHAIDs,
	})

	// 为每个 HA 添加 Directory
	for clusterIdx := 0; clusterIdx < config.NumClusters; clusterIdx++ {
		haID := nodeIDs.HAIDs[clusterIdx]

		// 计算这个 HA 管理的节点
		managedNodes := []int{}

		// 添加 L3
		l3ID := nodeIDs.L3IDs[clusterIdx]
		managedNodes = append(managedNodes, l3ID)

		// 添加这个集群的所有 L2
		l2StartIdx := clusterIdx * cpusPerCluster
		for i := 0; i < cpusPerCluster; i++ {
			l2ID := nodeIDs.L2IDs[l2StartIdx+i]
			managedNodes = append(managedNodes, l2ID)
		}

		// 添加这个集群的所有 CPU
		cpuStartIdx := clusterIdx * cpusPerCluster
		for i := 0; i < cpusPerCluster; i++ {
			cpuID := nodeIDs.CPUIDs[cpuStartIdx+i]
			managedNodes = append(managedNodes, cpuID)
		}

		// 添加 Directory（地址范围暂时不设置，使用交错映射）
		builder.AddDirectory(haID, coherence.RoleMemoryCtrl, coherence.CoherenceDomain{
			ManagedNodes: managedNodes,
		})

		// 设置父节点为虚拟根节点
		builder.SetParent(haID, virtualRootID)
	}

	// 构建树
	tree, err := builder.Build()
	if err != nil {
		return nil, err
	}

	return tree, nil
}

// GetCPUToL2Mapping 获取 CPU 到 L2 的映射关系
func GetCPUToL2Mapping(config RingTopologyConfig, nodeIDs *RingTopologyNodeIDs) map[int]int {
	mapping := make(map[int]int)
	numCPUs := config.NumClusters * config.CPUsPerL3

	for i := 0; i < numCPUs; i++ {
		cpuID := nodeIDs.CPUIDs[i]
		l2ID := nodeIDs.L2IDs[i] // 1:1 映射
		mapping[cpuID] = l2ID
	}

	return mapping
}

// GetL2ToL3Mapping 获取 L2 到 L3 的映射关系
func GetL2ToL3Mapping(config RingTopologyConfig, nodeIDs *RingTopologyNodeIDs) map[int]int {
	mapping := make(map[int]int)
	numL2s := config.NumClusters * config.CPUsPerL3

	for i := 0; i < numL2s; i++ {
		l2ID := nodeIDs.L2IDs[i]
		clusterIdx := i / config.CPUsPerL3
		l3ID := nodeIDs.L3IDs[clusterIdx]
		mapping[l2ID] = l3ID
	}

	return mapping
}

// GetL3ToHAMapping 获取 L3 到 HA 的映射关系
func GetL3ToHAMapping(nodeIDs *RingTopologyNodeIDs) map[int]int {
	mapping := make(map[int]int)

	for i := 0; i < len(nodeIDs.L3IDs); i++ {
		l3ID := nodeIDs.L3IDs[i]
		haID := nodeIDs.HAIDs[i]
		mapping[l3ID] = haID
	}

	return mapping
}

// GetHAToDRAMMapping 获取 HA 到 DRAM 的映射关系
func GetHAToDRAMMapping(nodeIDs *RingTopologyNodeIDs) map[int]int {
	mapping := make(map[int]int)

	for i := 0; i < len(nodeIDs.HAIDs); i++ {
		haID := nodeIDs.HAIDs[i]
		dramID := nodeIDs.DRAMIDs[i]
		mapping[haID] = dramID
	}

	return mapping
}

// GetRingNeighbors 获取 Ring 上的邻居节点
// 返回 (左邻居, 右邻居)
func GetRingNeighbors(nodeIDs *RingTopologyNodeIDs, haID int) (int, int) {
	numHAs := len(nodeIDs.HAIDs)

	// 找到当前 HA 在数组中的索引
	idx := -1
	for i, id := range nodeIDs.HAIDs {
		if id == haID {
			idx = i
			break
		}
	}

	if idx == -1 {
		return -1, -1
	}

	// 左邻居 (前一个)
	leftIdx := (idx - 1 + numHAs) % numHAs
	left := nodeIDs.HAIDs[leftIdx]

	// 右邻居 (后一个)
	rightIdx := (idx + 1) % numHAs
	right := nodeIDs.HAIDs[rightIdx]

	return left, right
}
