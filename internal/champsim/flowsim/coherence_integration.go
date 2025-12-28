package flowsim

import (
	"github.com/Readm/flow_sim/internal/components/coherence"
)

// BuildChampSimCoherenceTree 为 ChampSim 拓扑构建一致性树
// 参数：
//   - cpuNodeIDs: CPU 节点 IDs
//   - l2NodeID: L2 Cache 节点 ID（带 Directory）
//   - memCtrlNodeID: Memory Controller 节点 ID
//   - dramNodeIDs: DRAM 节点 IDs
//   - config: 地址映射配置
func BuildChampSimCoherenceTree(
	cpuNodeIDs []int,
	l2NodeID int,
	memCtrlNodeID int,
	dramNodeIDs []int,
	config coherence.AddressMappingConfig,
) (*coherence.CoherenceTree, error) {
	// 构建拓扑描述
	topology := &coherence.Topology{
		Nodes:       make(map[int]*coherence.NodeDescriptor),
		Connections: make(map[int][]int),
	}

	// 添加 CPU 节点
	for _, cpuID := range cpuNodeIDs {
		topology.Nodes[cpuID] = &coherence.NodeDescriptor{
			NodeID: cpuID,
			Capability: coherence.NodeCapability{
				Role:        coherence.RoleCompute,
				CanInitiate: true,
			},
		}
		// CPU 连接到 L2
		topology.Connections[cpuID] = []int{l2NodeID}
		topology.Connections[l2NodeID] = append(topology.Connections[l2NodeID], cpuID)
	}

	// 添加 L2 Cache 节点（带 Directory）
	topology.Nodes[l2NodeID] = &coherence.NodeDescriptor{
		NodeID: l2NodeID,
		Capability: coherence.NodeCapability{
			Role:         coherence.RoleCache,
			CacheLevel:   2,
			HasDirectory: true, // ⭐ L2 有 Directory
		},
	}
	// L2 连接到 Memory Controller
	topology.Connections[l2NodeID] = append(topology.Connections[l2NodeID], memCtrlNodeID)
	topology.Connections[memCtrlNodeID] = []int{l2NodeID}

	// 添加 Memory Controller 节点
	topology.Nodes[memCtrlNodeID] = &coherence.NodeDescriptor{
		NodeID: memCtrlNodeID,
		Capability: coherence.NodeCapability{
			Role:         coherence.RoleMemoryCtrl,
			HasDirectory: false, // L2 有 Directory，MemCtrl 不需要
		},
	}
	// Memory Controller 连接到 DRAM Channels
	for _, dramID := range dramNodeIDs {
		topology.Connections[memCtrlNodeID] = append(topology.Connections[memCtrlNodeID], dramID)
		topology.Connections[dramID] = []int{memCtrlNodeID}
	}

	// 添加 DRAM 节点
	for _, dramID := range dramNodeIDs {
		topology.Nodes[dramID] = &coherence.NodeDescriptor{
			NodeID: dramID,
			Capability: coherence.NodeCapability{
				Role: coherence.RoleMemory,
			},
		}
	}

	// 自动推断一致性树
	tree, err := coherence.BuildCoherenceTree(topology, config, nil)
	if err != nil {
		return nil, err
	}

	return tree, nil
}

// BuildChampSimCoherenceTreeWithHADirectory 为 ChampSim 拓扑构建一致性树（Directory 在 HA）
// 参数：
//   - cpuNodeIDs: CPU 节点 IDs
//   - l2NodeID: L2 Cache 节点 ID（无 Directory）
//   - haNodeID: Home Agent 节点 ID（带 Directory）
//   - dramNodeIDs: DRAM 节点 IDs
//   - config: 地址映射配置
func BuildChampSimCoherenceTreeWithHADirectory(
	cpuNodeIDs []int,
	l2NodeID int,
	haNodeID int,
	dramNodeIDs []int,
	config coherence.AddressMappingConfig,
) (*coherence.CoherenceTree, error) {
	// 构建拓扑描述
	topology := &coherence.Topology{
		Nodes:       make(map[int]*coherence.NodeDescriptor),
		Connections: make(map[int][]int),
	}

	// 添加 CPU 节点
	for _, cpuID := range cpuNodeIDs {
		topology.Nodes[cpuID] = &coherence.NodeDescriptor{
			NodeID: cpuID,
			Capability: coherence.NodeCapability{
				Role:        coherence.RoleCompute,
				CanInitiate: true,
			},
		}
		topology.Connections[cpuID] = []int{l2NodeID}
		topology.Connections[l2NodeID] = append(topology.Connections[l2NodeID], cpuID)
	}

	// 添加 L2 Cache 节点（无 Directory）
	topology.Nodes[l2NodeID] = &coherence.NodeDescriptor{
		NodeID: l2NodeID,
		Capability: coherence.NodeCapability{
			Role:         coherence.RoleCache,
			CacheLevel:   2,
			HasDirectory: false, // ❌ L2 没有 Directory
		},
	}
	topology.Connections[l2NodeID] = append(topology.Connections[l2NodeID], haNodeID)
	topology.Connections[haNodeID] = []int{l2NodeID}

	// 添加 Home Agent 节点（带 Directory）
	topology.Nodes[haNodeID] = &coherence.NodeDescriptor{
		NodeID: haNodeID,
		Capability: coherence.NodeCapability{
			Role:         coherence.RoleMemoryCtrl,
			HasDirectory: true, // ✅ HA 有 Directory
		},
	}
	for _, dramID := range dramNodeIDs {
		topology.Connections[haNodeID] = append(topology.Connections[haNodeID], dramID)
		topology.Connections[dramID] = []int{haNodeID}
	}

	// 添加 DRAM 节点
	for _, dramID := range dramNodeIDs {
		topology.Nodes[dramID] = &coherence.NodeDescriptor{
			NodeID: dramID,
			Capability: coherence.NodeCapability{
				Role: coherence.RoleMemory,
			},
		}
	}

	// 自动推断一致性树
	tree, err := coherence.BuildCoherenceTree(topology, config, nil)
	if err != nil {
		return nil, err
	}

	return tree, nil
}
