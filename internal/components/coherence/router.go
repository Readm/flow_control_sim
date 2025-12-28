package coherence

import "fmt"

// CoherenceRouter 基于一致性树的路由器
type CoherenceRouter struct {
	myNodeID int
	tree     *CoherenceTree

	// 路由表缓存（初始化时预计算）
	// routingTable[dstDirectoryID] = nextHop
	routingTable map[int]int

	// 我所属的 Directory（缓存）
	myDirectoryID int
}

// NewCoherenceRouter 创建一致性路由器
func NewCoherenceRouter(myNodeID int, tree *CoherenceTree) *CoherenceRouter {
	router := &CoherenceRouter{
		myNodeID:     myNodeID,
		tree:         tree,
		routingTable: make(map[int]int),
	}

	// 初始化时预计算路由表
	router.precomputeRoutingTable()

	return router
}

// precomputeRoutingTable 预计算路由表
// 为每个可能的目标 Directory 计算下一跳
func (r *CoherenceRouter) precomputeRoutingTable() {
	// 1. 找到我所属的 Directory
	myDirID := r.findMyDirectory()
	r.myDirectoryID = myDirID

	// 2. 为每个 Directory 计算下一跳
	for dirID := range r.tree.DirectoryNodes {
		nextHop := r.computeNextHop(myDirID, dirID)
		if nextHop != -1 {
			r.routingTable[dirID] = nextHop
		}
	}
}

// findMyDirectory 查找我所属的 Directory
func (r *CoherenceRouter) findMyDirectory() int {
	// 查找包含当前节点的 Directory
	for dirID, dirNode := range r.tree.DirectoryNodes {
		for _, memberID := range dirNode.Domain {
			if memberID == r.myNodeID {
				return dirID
			}
		}
	}

	// 如果当前节点本身就是 Directory
	if r.tree.DirectoryNodes[r.myNodeID] != nil {
		return r.myNodeID
	}

	// 未找到（不应该发生）
	return -1
}

// computeNextHop 计算从当前节点到目标 Directory 的下一跳
func (r *CoherenceRouter) computeNextHop(myDirID, dstDirID int) int {
	// 如果当前节点不是 Directory，第一跳一定是它的 Directory
	if r.myNodeID != myDirID {
		return myDirID
	}

	// 如果当前节点是 Directory
	// 并且目标就是自己，下一跳就是自己
	if myDirID == dstDirID {
		return myDirID
	}

	// 获取从我的 Directory 到目标 Directory 的路径
	path := r.computeDirectoryPath(myDirID, dstDirID)
	if len(path) < 2 {
		return -1
	}

	// 返回路径的第二个节点（下一跳）
	return path[1]
}

// computeDirectoryPath 计算从源 Directory 到目标 Directory 的路径
func (r *CoherenceRouter) computeDirectoryPath(srcDirID, dstDirID int) []int {
	srcDir := r.tree.DirectoryNodes[srcDirID]
	dstDir := r.tree.DirectoryNodes[dstDirID]

	if srcDir == nil || dstDir == nil {
		return nil
	}

	// 如果源和目标相同
	if srcDirID == dstDirID {
		return []int{srcDirID}
	}

	// 向上遍历找到公共父节点
	commonParent := r.tree.findCommonParent(srcDir, dstDir)
	if commonParent == nil {
		return nil
	}

	// 路径：src -> ... -> commonParent -> ... -> dst
	pathToParent := r.tree.getPathToAncestor(srcDir, commonParent)
	pathFromParent := r.tree.getPathFromAncestor(commonParent, dstDir)

	// 合并路径（避免重复 commonParent）
	path := pathToParent
	path = append(path, pathFromParent...)

	return path
}

// RouteForCoherence 一致性请求路由（优化版：使用预计算的路由表）
// 返回：下一跳节点 ID
// 如果返回 myNodeID，表示本地处理（我就是 Home Node）
func (r *CoherenceRouter) RouteForCoherence(addr uint64) (int, error) {
	// 1. 找到该地址的 Home Node（对于交错映射，这是 O(1) 操作）
	homeNodeID, err := r.tree.GetHomeNode(addr)
	if err != nil {
		return -1, fmt.Errorf("无法找到 Home Node: %v", err)
	}

	// 2. 如果我就是 Home Node，本地处理
	if homeNodeID == r.myNodeID {
		return r.myNodeID, nil
	}

	// 3. 从路由表查找下一跳（O(1) 查表，无需遍历树）
	nextHop, exists := r.routingTable[homeNodeID]
	if !exists {
		return -1, fmt.Errorf("路由表中没有到 Home Node %d 的路由", homeNodeID)
	}

	return nextHop, nil
}

// IsHomeNode 检查当前节点是否是某个地址的 Home Node
func (r *CoherenceRouter) IsHomeNode(addr uint64) bool {
	homeNodeID, err := r.tree.GetHomeNode(addr)
	if err != nil {
		return false
	}
	return homeNodeID == r.myNodeID
}

// GetHomeNode 获取某个地址的 Home Node
func (r *CoherenceRouter) GetHomeNode(addr uint64) (int, error) {
	return r.tree.GetHomeNode(addr)
}

// GetMyDirectory 获取管理当前节点的 Directory（使用缓存值）
func (r *CoherenceRouter) GetMyDirectory() (int, error) {
	// 直接返回初始化时缓存的值
	if r.myDirectoryID == -1 {
		return -1, fmt.Errorf("节点 %d 不属于任何 Directory Domain", r.myNodeID)
	}
	return r.myDirectoryID, nil
}

// RouteForMiss Cache miss 路由（向下游查找）
// 这是数据请求路由，可能与一致性路由不同
// 但根据 Q2 的决策，我们使用相同的层次路由（经过公共父节点）
func (r *CoherenceRouter) RouteForMiss(addr uint64) (int, error) {
	// 对于简单的层次结构，miss 路由就是向下游的下一跳
	// 查找当前节点所在的 Directory
	myDirID, err := r.GetMyDirectory()
	if err != nil {
		return -1, err
	}

	myDir := r.tree.DirectoryNodes[myDirID]
	if myDir == nil {
		return -1, fmt.Errorf("Directory %d 不存在", myDirID)
	}

	// 如果当前节点就是 Directory，向父节点路由
	if myDirID == r.myNodeID {
		if myDir.Parent != nil {
			return myDir.Parent.NodeID, nil
		}
		// 已经是根节点，无法继续向上
		return -1, fmt.Errorf("节点 %d 已经是根 Directory，无法继续路由", r.myNodeID)
	}

	// 如果当前节点不是 Directory，向管理它的 Directory 路由
	return myDirID, nil
}

// PrintRoutingInfo 打印路由信息（调试用）
func (r *CoherenceRouter) PrintRoutingInfo() {
	fmt.Printf("========== Routing Info for Node %d ==========\n", r.myNodeID)

	myDirID, _ := r.GetMyDirectory()
	fmt.Printf("My Directory: %d\n", myDirID)

	fmt.Printf("Directory Nodes: %v\n", getDirNodeIDs(r.tree))

	if r.tree.Root != nil {
		fmt.Printf("Root Node: %d\n", r.tree.Root.NodeID)
	} else {
		fmt.Printf("Root Node: <multiple roots>\n")
	}

	fmt.Println("==============================================")
}

// getDirNodeIDs 获取所有 Directory 节点 IDs
func getDirNodeIDs(tree *CoherenceTree) []int {
	var ids []int
	for id := range tree.DirectoryNodes {
		ids = append(ids, id)
	}
	return ids
}
