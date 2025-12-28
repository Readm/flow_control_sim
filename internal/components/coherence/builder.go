package coherence

import "fmt"

// CoherenceTreeBuilder 一致性树构建器（用户显式指定）
type CoherenceTreeBuilder struct {
	tree   *CoherenceTree
	nodes  map[int]*CoherenceNode
	config AddressMappingConfig
}

// NewCoherenceTreeBuilder 创建构建器
func NewCoherenceTreeBuilder(config AddressMappingConfig) *CoherenceTreeBuilder {
	return &CoherenceTreeBuilder{
		tree: &CoherenceTree{
			DirectoryNodes:       make(map[int]*CoherenceNode),
			AddressMappingConfig: config,
		},
		nodes:  make(map[int]*CoherenceNode),
		config: config,
	}
}

// CoherenceDomain 一致性域配置
type CoherenceDomain struct {
	ManagedNodes []int         // 管理的节点 IDs
	AddressRange *AddressRange // 地址范围（可选）
}

// AddDirectory 添加 Directory 节点
func (b *CoherenceTreeBuilder) AddDirectory(nodeID int, role NodeRole, domain CoherenceDomain) *CoherenceTreeBuilder {
	node := &CoherenceNode{
		NodeID:                nodeID,
		Role:                  role,
		Domain:                domain.ManagedNodes,
		AddressResponsibility: domain.AddressRange,
		Children:              []*CoherenceNode{},
	}

	b.nodes[nodeID] = node
	b.tree.DirectoryNodes[nodeID] = node

	return b
}

// SetParent 设置父子关系
func (b *CoherenceTreeBuilder) SetParent(childID, parentID int) *CoherenceTreeBuilder {
	child, childExists := b.nodes[childID]
	parent, parentExists := b.nodes[parentID]

	if !childExists {
		panic(fmt.Sprintf("子节点 %d 不存在", childID))
	}
	if !parentExists {
		panic(fmt.Sprintf("父节点 %d 不存在", parentID))
	}

	child.Parent = parent
	parent.Children = append(parent.Children, child)

	return b
}

// SetRoot 显式设置根节点
func (b *CoherenceTreeBuilder) SetRoot(rootID int) *CoherenceTreeBuilder {
	root, exists := b.nodes[rootID]
	if !exists {
		panic(fmt.Sprintf("根节点 %d 不存在", rootID))
	}

	b.tree.Root = root
	return b
}

// Build 构建一致性树
func (b *CoherenceTreeBuilder) Build() (*CoherenceTree, error) {
	// 自动查找根节点（如果用户没有显式设置）
	if b.tree.Root == nil {
		var roots []*CoherenceNode
		for _, node := range b.nodes {
			if node.Parent == nil {
				roots = append(roots, node)
			}
		}

		if len(roots) == 1 {
			b.tree.Root = roots[0]
		} else if len(roots) == 0 {
			return nil, fmt.Errorf("没有找到根节点（可能存在环）")
		}
		// len(roots) > 1: 多个根节点，Root 保持为 nil
	}

	// 验证一致性树
	if err := b.tree.Validate(); err != nil {
		return nil, fmt.Errorf("一致性树验证失败: %v", err)
	}

	// 验证地址映射配置
	if err := b.config.Validate(); err != nil {
		return nil, fmt.Errorf("地址映射配置验证失败: %v", err)
	}

	return b.tree, nil
}

// BuildCoherenceTree 便捷函数：尝试自动推断，失败时返回错误
func BuildCoherenceTree(
	topology *Topology,
	config AddressMappingConfig,
	explicitTree *CoherenceTree,
) (*CoherenceTree, error) {
	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %v", err)
	}

	// Step 1: 如果用户提供了显式的树，直接使用
	if explicitTree != nil {
		if err := explicitTree.Validate(); err != nil {
			return nil, fmt.Errorf("用户提供的一致性树验证失败: %v", err)
		}
		return explicitTree, nil
	}

	// Step 2: 尝试自动推断
	analyzer := NewTopologyAnalyzer(topology, config)
	result, err := analyzer.Analyze()

	if err != nil {
		// 自动推断失败，返回详细错误信息
		return nil, fmt.Errorf("⚠️ 自动推断一致性树失败: %v\n请使用 CoherenceTreeBuilder 显式指定一致性树", err)
	}

	if len(result.Warnings) > 0 {
		// 有警告但成功
		fmt.Printf("⚠️ 一致性树构建完成，但有以下警告:\n")
		for _, warning := range result.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}

	return result.CoherenceTree, nil
}
