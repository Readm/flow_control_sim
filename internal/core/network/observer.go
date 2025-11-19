package network

import (
	"context"
	"time"

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

// GetNodes returns a copy of the node order slice.
func (m *Manager) GetNodes() []node.Node {
	return append([]node.Node(nil), m.order...)
}

// GetLinks returns a copy of the links slice.
func (m *Manager) GetLinks() []*link.Link {
	return append([]*link.Link(nil), m.links...)
}

// GetOutgoing returns a copy of the outgoing edges map.
func (m *Manager) GetOutgoing() map[int][]*link.Link {
	result := make(map[int][]*link.Link, len(m.outgoing))
	for k, v := range m.outgoing {
		result[k] = append([]*link.Link(nil), v...)
	}
	return result
}

// RunFrom executes cycles starting from startCycle for the specified number of cycles.
func (m *Manager) RunFrom(ctx context.Context, startCycle, cycles uint64) error {
	if cycles == 0 {
		return nil
	}

	endCycle := startCycle + cycles
	for cycle := startCycle; cycle < endCycle; cycle++ {
		m.advanceLinks(cycle)

		var delay time.Duration
		if mockDelayEnabled {
			delay = mockDelay
		}
		if err := m.dispatchCycle(ctx, cycle, delay); err != nil {
			return err
		}
		if m.cycleHook != nil {
			m.cycleHook.OnCycleEnd(cycle, m.order, m.links)
		}
		if mockDelayEnabled && cycle+1 < endCycle {
			if err := waitForLink(ctx, delay); err != nil {
				return err
			}
		}
	}

	return ctx.Err()
}
