package network

import (
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/node"
)

// CycleHook receives callbacks after each simulation cycle completes.
type CycleHook interface {
	OnCycleEnd(cycle uint64, nodes []node.Node, links []*link.Link)
}

// SetCycleHook registers a hook that receives cycle snapshots.
func (m *Manager) SetCycleHook(hook CycleHook) {
	m.cycleHook = hook
}
