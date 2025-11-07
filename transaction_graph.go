package main

// TransactionGraph represents the graph structure for visualization
// Built from TransactionManager for visualization purposes
type TransactionGraph struct {
	Nodes []TransactionNode `json:"nodes"`
	Edges []TransactionEdge  `json:"edges"`
}

// TransactionNode represents a transaction node in the graph
type TransactionNode struct {
	ID              int64             `json:"id"`
	TransactionType CHITransactionType `json:"transactionType"`
	Address         uint64            `json:"address"`
	State           TransactionState  `json:"state"`
	InitiatedAt     int               `json:"initiatedAt"`
	CompletedAt     int               `json:"completedAt"`
	CacheHit        *bool             `json:"cacheHit,omitempty"` // nil if not checked
	// Additional visualization fields
	SameAddrOrderingCount int `json:"sameAddrOrderingCount"` // Number of same-address ordering dependencies
	GlobalOrderingCount   int `json:"globalOrderingCount"`   // Number of global ordering dependencies
	GeneratedCount        int `json:"generatedCount"`         // Number of generated child transactions
}

// TransactionEdge represents a dependency edge in the graph
type TransactionEdge struct {
	FromID         int64          `json:"fromID"`
	ToID           int64          `json:"toID"`
	DependencyType DependencyType `json:"dependencyType"`
	Reason         string         `json:"reason"`
	Cycle          int            `json:"cycle"`
}

// BuildTransactionGraph builds a TransactionGraph from TransactionManager
// Optionally queries state providers for additional information
func BuildTransactionGraph(
	tm *TransactionManager,
	cacheProvider CacheStateProvider,
	directoryProvider DirectoryStateProvider,
	orderingProvider OrderingConstraintProvider,
) *TransactionGraph {
	allTxns := tm.GetAllTransactions()
	allDeps := tm.GetAllDependencies()

	nodes := make([]TransactionNode, 0, len(allTxns))
	edges := make([]TransactionEdge, 0)

	// Build nodes
	for txnID, txn := range allTxns {
		ctx := txn.Context

		node := TransactionNode{
			ID:                   txnID,
			TransactionType:      ctx.TransactionType,
			Address:              ctx.Address,
			State:                ctx.State,
			InitiatedAt:          ctx.InitiatedAt,
			CompletedAt:          ctx.CompletedAt,
			SameAddrOrderingCount: len(ctx.SameAddrOrdering),
			GlobalOrderingCount:   len(ctx.GlobalOrdering),
			GeneratedCount:       len(ctx.GeneratedTransactions),
		}

		// Query cache state if provider is available
		if cacheProvider != nil {
			cacheHit := cacheProvider.GetCacheHit(txnID)
			node.CacheHit = cacheHit
		}

		nodes = append(nodes, node)
	}

	// Build edges
	for fromID, deps := range allDeps {
		for _, dep := range deps {
			edge := TransactionEdge{
				FromID:         fromID,
				ToID:           dep.ToTransactionID,
				DependencyType: dep.DependencyType,
				Reason:         dep.Reason,
				Cycle:          dep.Cycle,
			}
			edges = append(edges, edge)
		}
	}

	return &TransactionGraph{
		Nodes: nodes,
		Edges: edges,
	}
}

// GetTransactionGraph is a convenience method on TransactionManager
func (tm *TransactionManager) GetTransactionGraph(
	cacheProvider CacheStateProvider,
	directoryProvider DirectoryStateProvider,
	orderingProvider OrderingConstraintProvider,
) *TransactionGraph {
	return BuildTransactionGraph(tm, cacheProvider, directoryProvider, orderingProvider)
}

