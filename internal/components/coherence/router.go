package coherence

import "fmt"

// CoherenceRouter 基于一致性树的路由器
type CoherenceRouter struct {
	myNodeID int
	tree     *CoherenceTree
}

// NewCoherenceRouter 创建一致性路由器
func NewCoherenceRouter(myNodeID int, tree *CoherenceTree) *CoherenceRouter {
	return &CoherenceRouter{
		myNodeID: myNodeID,
		tree:     tree,
	}
}

// RouteForCoherence 一致性请求路由
// 返回：下一跳节点 ID
// 如果返回 myNodeID，表示本地处理（我就是 Home Node）
func (r *CoherenceRouter) RouteForCoherence(addr uint64) (int, error) {
	// 1. 找到该地址的 Home Node
	homeNodeID, err := r.tree.GetHomeNode(addr)
	if err != nil {
		return -1, fmt.Errorf("无法找到 Home Node: %v", err)
	}

	// 2. 如果我就是 Home Node，本地处理
	if homeNodeID == r.myNodeID {
		return r.myNodeID, nil
	}

	// 3. 获取从当前节点到 Home Node 的路径
	path, err := r.tree.GetCoherencePath(r.myNodeID, addr)
	if err != nil {
		return -1, fmt.Errorf("无法找到路径: %v", err)
	}

	// 4. 返回下一跳（路径的第二个节点）
	if len(path) < 2 {
		return -1, fmt.Errorf("路径长度不足: %v", path)
	}

	return path[1], nil
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

// GetMyDirectory 获取管理当前节点的 Directory
func (r *CoherenceRouter) GetMyDirectory() (int, error) {
	// 查找当前节点所属的 Directory
	for dirID, dirNode := range r.tree.DirectoryNodes {
		for _, memberID := range dirNode.Domain {
			if memberID == r.myNodeID {
				return dirID, nil
			}
		}
	}

	// 如果当前节点本身就是 Directory
	if r.tree.DirectoryNodes[r.myNodeID] != nil {
		return r.myNodeID, nil
	}

	return -1, fmt.Errorf("节点 %d 不属于任何 Directory Domain", r.myNodeID)
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
