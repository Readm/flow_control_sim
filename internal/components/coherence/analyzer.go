package coherence

import (
	"fmt"
	"sort"
)

// Topology 拓扑接口（简化版，用于分析）
type Topology struct {
	Nodes       map[int]*NodeDescriptor
	Connections map[int][]int // nodeID -> [neighborIDs]
}

// AnalysisResult 分析结果
type AnalysisResult struct {
	// 节点分类
	ComputeNodes   []int
	CacheNodes     map[int][]int // level -> [nodeIDs]
	DirectoryNodes []int
	MemoryNodes    []int

	// 一致性树
	CoherenceTree *CoherenceTree

	// 诊断信息
	Warnings []string
	Errors   []string
}

// TopologyAnalyzer 拓扑分析器
type TopologyAnalyzer struct {
	topology *Topology
	config   AddressMappingConfig
}

// NewTopologyAnalyzer 创建拓扑分析器
func NewTopologyAnalyzer(topology *Topology, config AddressMappingConfig) *TopologyAnalyzer {
	return &TopologyAnalyzer{
		topology: topology,
		config:   config,
	}
}

// Analyze 分析拓扑，自动推断一致性树
func (a *TopologyAnalyzer) Analyze() (*AnalysisResult, error) {
	result := &AnalysisResult{
		CacheNodes: make(map[int][]int),
	}

	// Step 1: 节点分类
	a.classifyNodes(result)

	// Step 2: 尝试自动推断一致性树
	tree, warnings, err := a.inferCoherenceTree(result)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	result.CoherenceTree = tree
	result.Warnings = warnings

	// Step 3: 验证一致性树
	if tree != nil {
		if err := tree.Validate(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("一致性树验证失败: %v", err))
			return result, err
		}
	}

	return result, nil
}

// classifyNodes 节点分类
func (a *TopologyAnalyzer) classifyNodes(result *AnalysisResult) {
	for nodeID, desc := range a.topology.Nodes {
		switch desc.Capability.Role {
		case RoleCompute:
			result.ComputeNodes = append(result.ComputeNodes, nodeID)

		case RoleCache:
			level := desc.Capability.CacheLevel
			result.CacheNodes[level] = append(result.CacheNodes[level], nodeID)
			if desc.Capability.HasDirectory {
				result.DirectoryNodes = append(result.DirectoryNodes, nodeID)
			}

		case RoleMemoryCtrl:
			if desc.Capability.HasDirectory {
				result.DirectoryNodes = append(result.DirectoryNodes, nodeID)
			}
			result.MemoryNodes = append(result.MemoryNodes, nodeID)

		case RoleMemory:
			result.MemoryNodes = append(result.MemoryNodes, nodeID)
		}
	}
}

// inferCoherenceTree 自动推断一致性树
func (a *TopologyAnalyzer) inferCoherenceTree(result *AnalysisResult) (*CoherenceTree, []string, error) {
	var warnings []string

	// 如果没有 Directory 节点，无法构建一致性树
	if len(result.DirectoryNodes) == 0 {
		return nil, warnings, fmt.Errorf("⚠️ 没有找到任何 Directory 节点，无法构建一致性树")
	}

	tree := &CoherenceTree{
		DirectoryNodes:       make(map[int]*CoherenceNode),
		AddressMappingConfig: a.config,
	}

	// Step 1: 为每个 Directory 创建 CoherenceNode
	for _, dirID := range result.DirectoryNodes {
		desc := a.topology.Nodes[dirID]
		tree.DirectoryNodes[dirID] = &CoherenceNode{
			NodeID: dirID,
			Role:   desc.Capability.Role,
		}
	}

	// Step 2: 推断层次关系
	if err := a.inferHierarchy(tree, result); err != nil {
		warnings = append(warnings, err.Error())
		return nil, warnings, fmt.Errorf("⚠️ 无法自动推断一致性层次: %v", err)
	}

	// Step 3: 为每个 Directory 分配 Domain
	a.assignDomains(tree, result)

	// Step 4: 分配地址责任（如果有多个同层 Directory）
	a.assignAddressResponsibility(tree, result)

	return tree, warnings, nil
}

// inferHierarchy 推断层次关系
func (a *TopologyAnalyzer) inferHierarchy(tree *CoherenceTree, result *AnalysisResult) error {
	// 策略：根据节点类型和 Cache 层级推断
	// 规则：
	// 1. MemoryCtrl (HA) 是 Cache 的父节点
	// 2. 高层级 Cache 是低层级 Cache 的父节点
	// 3. 如果有多个同级 Directory，它们是并列的（无父子关系）

	dirNodes := tree.DirectoryNodes

	// 找出所有 Directory 节点的层级
	type dirInfo struct {
		nodeID int
		role   NodeRole
		level  int // Cache level, MemoryCtrl 为 999
	}

	var dirs []dirInfo
	for dirID := range dirNodes {
		desc := a.topology.Nodes[dirID]
		level := 999 // MemoryCtrl 视为最高层
		if desc.Capability.Role == RoleCache {
			level = desc.Capability.CacheLevel
		}
		dirs = append(dirs, dirInfo{
			nodeID: dirID,
			role:   desc.Capability.Role,
			level:  level,
		})
	}

	// 按层级排序（从低到高）
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].level < dirs[j].level
	})

	// 推断父子关系
	for i := 0; i < len(dirs); i++ {
		for j := i + 1; j < len(dirs); j++ {
			// 如果 j 的层级更高，j 可能是 i 的父节点
			if dirs[j].level > dirs[i].level {
				// 检查它们是否在同一个连通路径上
				if a.isConnected(dirs[i].nodeID, dirs[j].nodeID) {
					child := tree.DirectoryNodes[dirs[i].nodeID]
					parent := tree.DirectoryNodes[dirs[j].nodeID]

					// 检查是否已经有父节点
					if child.Parent != nil {
						// 选择层级更接近的作为父节点
						existingParentLevel := a.getNodeLevel(child.Parent.NodeID)
						newParentLevel := dirs[j].level
						if newParentLevel < existingParentLevel {
							// 新的父节点层级更低（更接近），替换
							// 先从旧父节点移除
							a.removeChild(child.Parent, child.NodeID)
							child.Parent = parent
							parent.Children = append(parent.Children, child)
						}
					} else {
						child.Parent = parent
						parent.Children = append(parent.Children, child)
					}
				}
			}
		}
	}

	// 找出根节点
	var roots []*CoherenceNode
	for _, node := range tree.DirectoryNodes {
		if node.Parent == nil {
			roots = append(roots, node)
		}
	}

	if len(roots) == 0 {
		return fmt.Errorf("没有找到根节点（可能存在环）")
	}

	if len(roots) == 1 {
		tree.Root = roots[0]
	} else {
		// 多个根节点
		if len(roots) > 1 {
			// 检查是否都是同一层级（如多个 L3 Slice）
			firstLevel := a.getNodeLevel(roots[0].NodeID)
			allSameLevel := true
			for _, root := range roots[1:] {
				if a.getNodeLevel(root.NodeID) != firstLevel {
					allSameLevel = false
					break
				}
			}

			if allSameLevel {
				// 多个同层级的根节点（如多个 L3 Slice），这是合法的
				tree.Root = nil
				return nil
			}

			// 不同层级的多个根节点，无法自动推断
			return fmt.Errorf("发现多个不同层级的根节点，无法自动推断（需要用户显式指定）")
		}
	}

	return nil
}

// getNodeLevel 获取节点的层级
func (a *TopologyAnalyzer) getNodeLevel(nodeID int) int {
	desc := a.topology.Nodes[nodeID]
	if desc.Capability.Role == RoleMemoryCtrl {
		return 999
	}
	return desc.Capability.CacheLevel
}

// removeChild 从父节点移除子节点
func (a *TopologyAnalyzer) removeChild(parent *CoherenceNode, childID int) {
	newChildren := make([]*CoherenceNode, 0, len(parent.Children))
	for _, child := range parent.Children {
		if child.NodeID != childID {
			newChildren = append(newChildren, child)
		}
	}
	parent.Children = newChildren
}

// isConnected 检查两个节点是否在同一连通路径上（BFS）
func (a *TopologyAnalyzer) isConnected(nodeA, nodeB int) bool {
	visited := make(map[int]bool)
	queue := []int{nodeA}
	visited[nodeA] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current == nodeB {
			return true
		}

		for _, neighbor := range a.topology.Connections[current] {
			if !visited[neighbor] {
				visited[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
	}

	return false
}

// assignDomains 为每个 Directory 分配 Domain
func (a *TopologyAnalyzer) assignDomains(tree *CoherenceTree, result *AnalysisResult) {
	// 策略：BFS 从每个 Compute 节点向下游查找，直到遇到第一个 Directory
	for _, cpuID := range result.ComputeNodes {
		dirID := a.findNearestDirectory(cpuID, tree)
		if dirID != -1 {
			dirNode := tree.DirectoryNodes[dirID]
			dirNode.Domain = append(dirNode.Domain, cpuID)
		}
	}

	// 同样为 Cache 节点（无 Directory 的）分配到最近的 Directory
	for level, cacheIDs := range result.CacheNodes {
		for _, cacheID := range cacheIDs {
			desc := a.topology.Nodes[cacheID]
			if !desc.Capability.HasDirectory {
				dirID := a.findNearestDirectory(cacheID, tree)
				if dirID != -1 {
					dirNode := tree.DirectoryNodes[dirID]
					// 避免重复添加
					if !contains(dirNode.Domain, cacheID) {
						dirNode.Domain = append(dirNode.Domain, cacheID)
					}
				}
			}
		}
		_ = level
	}
}

// findNearestDirectory BFS 查找最近的 Directory 节点
func (a *TopologyAnalyzer) findNearestDirectory(startNode int, tree *CoherenceTree) int {
	visited := make(map[int]bool)
	queue := []int{startNode}
	visited[startNode] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// 检查当前节点是否是 Directory
		if tree.DirectoryNodes[current] != nil {
			return current
		}

		// 继续向邻居搜索
		for _, neighbor := range a.topology.Connections[current] {
			if !visited[neighbor] {
				visited[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
	}

	return -1
}

// assignAddressResponsibility 分配地址责任
func (a *TopologyAnalyzer) assignAddressResponsibility(tree *CoherenceTree, result *AnalysisResult) {
	// 如果有多个根节点（同层级），分配地址范围
	var roots []*CoherenceNode
	for _, node := range tree.DirectoryNodes {
		if node.Parent == nil {
			roots = append(roots, node)
		}
	}

	if len(roots) > 1 {
		// 均匀分配地址空间（简化版）
		// 实际使用时，会根据 AddressMapper 动态决定
		// 这里只是标记，表示需要地址映射
		for _, root := range roots {
			root.AddressResponsibility = nil // nil 表示由 AddressMapper 决定
		}
	}
}

// contains 检查 slice 是否包含元素
func contains(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}
