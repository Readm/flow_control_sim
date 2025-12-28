package coherence

import (
	"fmt"
	"sort"
)

// NodeRole 节点角色
type NodeRole string

const (
	RoleCompute      NodeRole = "compute"       // CPU, GPU - 发起请求
	RoleCache        NodeRole = "cache"         // L1, L2, L3 - 缓存
	RoleDirectory    NodeRole = "directory"     // 一致性目录
	RoleMemoryCtrl   NodeRole = "memory_ctrl"   // Memory Controller / Home Agent
	RoleMemory       NodeRole = "memory"        // DRAM, NVM
	RoleInterconnect NodeRole = "interconnect"  // Router, Switch
)

// NodeCapability 节点能力描述
type NodeCapability struct {
	Role         NodeRole // 节点角色
	CacheLevel   int      // Cache 级别 (1, 2, 3, ...)
	HasDirectory bool     // 是否有 Directory
	CanInitiate  bool     // 是否能发起请求（CPU 能，DRAM 不能）
}

// NodeDescriptor 节点描述符（每个节点提供）
type NodeDescriptor struct {
	NodeID     int
	Capability NodeCapability
}

// AddressRange 地址范围
type AddressRange struct {
	Start uint64
	End   uint64
}

// Contains 检查地址是否在范围内
func (r *AddressRange) Contains(addr uint64) bool {
	return addr >= r.Start && addr <= r.End
}

// CoherenceNode 一致性树节点
type CoherenceNode struct {
	NodeID int
	Role   NodeRole

	// 一致性域：这个 Directory 负责哪些节点
	Domain []int // 管理的节点 IDs (CPU, L1, L2)

	// 子节点：下一层的 Directory
	Children []*CoherenceNode

	// 父节点：上一层的 Directory
	Parent *CoherenceNode

	// 地址责任：这个节点负责哪些地址（可选，用于分区）
	AddressResponsibility *AddressRange
}

// CoherenceTree 一致性责任树
type CoherenceTree struct {
	// 根节点：最高层的 Directory（如果存在）
	// 如果有多个根节点（多个独立的 HA），Root 为 nil
	Root *CoherenceNode

	// 所有 Directory 节点
	DirectoryNodes map[int]*CoherenceNode

	// 地址映射配置
	AddressMappingConfig AddressMappingConfig
}

// AddressMappingConfig 地址映射配置
type AddressMappingConfig struct {
	// Granularity 交错粒度（字节），必须是 64 的倍数
	Granularity uint64

	// Strategy 映射策略
	Strategy MappingStrategy
}

// MappingStrategy 地址映射策略
type MappingStrategy string

const (
	// MappingInterleaved 交错映射（默认）
	MappingInterleaved MappingStrategy = "interleaved"

	// MappingRanged 范围映射
	MappingRanged MappingStrategy = "ranged"
)

// Validate 验证配置
func (c *AddressMappingConfig) Validate() error {
	if c.Granularity == 0 {
		return fmt.Errorf("Granularity 不能为 0")
	}
	if c.Granularity%64 != 0 {
		return fmt.Errorf("Granularity 必须是 64 的倍数，当前值: %d", c.Granularity)
	}
	return nil
}

// GetHomeNode 返回负责该地址的 Directory 节点 ID
func (t *CoherenceTree) GetHomeNode(addr uint64) (int, error) {
	if t.Root == nil && len(t.DirectoryNodes) == 0 {
		return -1, fmt.Errorf("一致性树为空")
	}

	// 如果只有一个 Directory 节点，直接返回
	if len(t.DirectoryNodes) == 1 {
		for nodeID := range t.DirectoryNodes {
			return nodeID, nil
		}
	}

	// 如果有多个 Directory 节点，使用地址映射
	return t.mapAddressToDirectory(addr)
}

// mapAddressToDirectory 根据地址映射策略选择 Directory
func (t *CoherenceTree) mapAddressToDirectory(addr uint64) (int, error) {
	// 收集所有根节点或叶子 Directory 节点
	var targetDirs []int

	if t.Root != nil {
		// 如果有根节点，查找负责该地址的叶子节点
		node := t.findResponsibleNode(t.Root, addr)
		if node != nil {
			return node.NodeID, nil
		}
		return t.Root.NodeID, nil
	}

	// 如果没有根节点（多个独立的树），收集所有根
	for nodeID, node := range t.DirectoryNodes {
		if node.Parent == nil {
			targetDirs = append(targetDirs, nodeID)
		}
	}

	if len(targetDirs) == 0 {
		return -1, fmt.Errorf("没有找到 Directory 节点")
	}

	// 排序确保一致性（map 迭代顺序不确定）
	sort.Ints(targetDirs)

	// 使用交错映射
	index := (addr / t.AddressMappingConfig.Granularity) % uint64(len(targetDirs))
	return targetDirs[index], nil
}

// findResponsibleNode 查找负责该地址的节点（递归）
// 返回最低层（最接近 CPU）的 Directory 节点
func (t *CoherenceTree) findResponsibleNode(node *CoherenceNode, addr uint64) *CoherenceNode {
	// 如果没有子节点，当前节点就是负责节点
	if len(node.Children) == 0 {
		return node
	}

	// 检查是否所有子节点都有明确的地址责任
	hasAnyRange := false
	for _, child := range node.Children {
		if child.AddressResponsibility != nil {
			hasAnyRange = true
			break
		}
	}

	if hasAnyRange {
		// 如果有子节点有明确的地址责任，使用范围匹配
		for _, child := range node.Children {
			if child.AddressResponsibility != nil {
				if child.AddressResponsibility.Contains(addr) {
					// 递归查找子节点
					responsible := t.findResponsibleNode(child, addr)
					if responsible != nil {
						return responsible
					}
					return child
				}
			}
		}
		// 没有子节点匹配该地址，返回当前节点
		return node
	} else {
		// 如果所有子节点都没有地址范围，使用交错映射
		// 收集所有子节点 ID 并排序
		childIDs := make([]int, len(node.Children))
		for i, child := range node.Children {
			childIDs[i] = child.NodeID
		}
		sort.Ints(childIDs)

		// 使用交错映射选择子节点
		index := (addr / t.AddressMappingConfig.Granularity) % uint64(len(childIDs))
		selectedChildID := childIDs[index]

		// 找到对应的子节点
		for _, child := range node.Children {
			if child.NodeID == selectedChildID {
				// 递归查找
				responsible := t.findResponsibleNode(child, addr)
				if responsible != nil {
					return responsible
				}
				return child
			}
		}

		// 理论上不应该到这里
		return node
	}
}

// GetCoherencePath 返回从 requester 到 Home Node 的路径
func (t *CoherenceTree) GetCoherencePath(requesterID int, addr uint64) ([]int, error) {
	homeNodeID, err := t.GetHomeNode(addr)
	if err != nil {
		return nil, err
	}

	// 如果 requester 本身就是 Home Node
	if requesterID == homeNodeID {
		return []int{requesterID}, nil
	}

	// 从 requester 向上查找到 Home Node
	path := []int{requesterID}

	// 查找 requester 所在的 Directory
	var requesterDir *CoherenceNode
	for _, dirNode := range t.DirectoryNodes {
		for _, memberID := range dirNode.Domain {
			if memberID == requesterID {
				requesterDir = dirNode
				break
			}
		}
		if requesterDir != nil {
			break
		}
	}

	if requesterDir == nil {
		// requester 可能本身就是一个 Directory
		requesterDir = t.DirectoryNodes[requesterID]
		if requesterDir == nil {
			return nil, fmt.Errorf("无法找到 requester %d 的 Directory", requesterID)
		}
		// 如果 requester 本身就是 Directory，不需要重复添加到路径
	} else {
		// requester 不是 Directory，添加其所属的 Directory 到路径
		path = append(path, requesterDir.NodeID)
	}

	// 检查 requesterDir 是否就是 Home Node
	if requesterDir.NodeID == homeNodeID {
		// requester 的 Directory 就是 Home Node，路径已完整
		return path, nil
	}

	// 向上遍历直到找到 Home Node
	current := requesterDir.Parent
	for current != nil {
		path = append(path, current.NodeID)

		if current.NodeID == homeNodeID {
			return path, nil
		}

		// 向上移动到父节点
		current = current.Parent
	}

	// 如果 Home Node 不在父节点路径上，需要向下查找
	// 这种情况下，需要先到达公共父节点，再向下到 Home Node
	homeNode := t.DirectoryNodes[homeNodeID]
	if homeNode != nil {
		// 找到公共父节点
		commonParent := t.findCommonParent(requesterDir, homeNode)
		if commonParent != nil {
			// 路径：requester → ... → commonParent → ... → homeNode
			pathToParent := t.getPathToAncestor(requesterDir, commonParent)
			pathFromParent := t.getPathFromAncestor(commonParent, homeNode)

			// 合并路径（避免重复 commonParent）
			path = []int{requesterID}
			path = append(path, pathToParent...)
			path = append(path, pathFromParent...)
			return path, nil
		}
	}

	return nil, fmt.Errorf("无法找到从 %d 到 Home Node %d 的路径", requesterID, homeNodeID)
}

// findCommonParent 查找两个节点的最近公共父节点
func (t *CoherenceTree) findCommonParent(node1, node2 *CoherenceNode) *CoherenceNode {
	// 收集 node1 的所有祖先
	ancestors := make(map[int]*CoherenceNode)
	current := node1
	for current != nil {
		ancestors[current.NodeID] = current
		current = current.Parent
	}

	// 从 node2 向上查找，第一个在 ancestors 中的就是公共父节点
	current = node2
	for current != nil {
		if ancestor, exists := ancestors[current.NodeID]; exists {
			return ancestor
		}
		current = current.Parent
	}

	return nil
}

// getPathToAncestor 获取从 node 到 ancestor 的路径（包括 node，包括 ancestor）
func (t *CoherenceTree) getPathToAncestor(node, ancestor *CoherenceNode) []int {
	var path []int
	// 先添加起始节点
	path = append(path, node.NodeID)

	current := node.Parent
	for current != nil {
		path = append(path, current.NodeID)
		if current.NodeID == ancestor.NodeID {
			break
		}
		current = current.Parent
	}
	return path
}

// getPathFromAncestor 获取从 ancestor 到 node 的路径（不包括 ancestor，包括 node）
func (t *CoherenceTree) getPathFromAncestor(ancestor, node *CoherenceNode) []int {
	// 先找到从 node 到 ancestor 的路径
	pathUp := []int{node.NodeID}
	current := node.Parent
	for current != nil && current.NodeID != ancestor.NodeID {
		pathUp = append(pathUp, current.NodeID)
		current = current.Parent
	}

	// 反转路径
	path := make([]int, len(pathUp))
	for i := 0; i < len(pathUp); i++ {
		path[i] = pathUp[len(pathUp)-1-i]
	}

	return path
}

// Validate 验证一致性树的正确性
func (t *CoherenceTree) Validate() error {
	// 检查 1: 每个节点只能属于一个 Domain
	allNodes := make(map[int]int) // nodeID -> dirNodeID
	for dirID, dirNode := range t.DirectoryNodes {
		for _, nodeID := range dirNode.Domain {
			if existingDirID, exists := allNodes[nodeID]; exists {
				return fmt.Errorf("节点 %d 属于多个 Coherence Domain (Dir %d 和 Dir %d)", nodeID, existingDirID, dirID)
			}
			allNodes[nodeID] = dirID
		}
	}

	// 检查 2: 树结构是否有环
	visited := make(map[int]bool)
	var checkCycle func(node *CoherenceNode) error
	checkCycle = func(node *CoherenceNode) error {
		if visited[node.NodeID] {
			return fmt.Errorf("一致性树存在环，节点 %d", node.NodeID)
		}
		visited[node.NodeID] = true

		for _, child := range node.Children {
			if err := checkCycle(child); err != nil {
				return err
			}
		}

		visited[node.NodeID] = false
		return nil
	}

	if t.Root != nil {
		if err := checkCycle(t.Root); err != nil {
			return err
		}
	}

	return nil
}
