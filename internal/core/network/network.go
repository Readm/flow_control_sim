package network

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/node"
)

// Manager coordinates nodes and links inside a Network cycle.
type Manager struct {
	nodes    map[int]node.Node
	order    []node.Node
	outgoing map[int][]*link.Link
	links    []*link.Link
}

// NewManager builds a Manager with the provided nodes和图结构。graph 使用 node ID 作为 key，value 为从该节点出发的 Link。
func NewManager(nodes []node.Node, edges map[int][]*link.Link) (*Manager, error) {
	if len(nodes) == 0 {
		return nil, errors.New("network manager requires at least one node")
	}
	if edges == nil {
		edges = make(map[int][]*link.Link)
	}

	nodeMap := make(map[int]node.Node, len(nodes))
	order := make([]node.Node, 0, len(nodes))
	for i, n := range nodes {
		if n == nil {
			return nil, fmt.Errorf("node at index %d is nil", i)
		}
		id := n.ID()
		if _, exists := nodeMap[id]; exists {
			return nil, fmt.Errorf("duplicate node id %d", id)
		}
		nodeMap[id] = n
		order = append(order, n)
	}

	allLinks := make([]*link.Link, 0)
	for from, ls := range edges {
		if _, ok := nodeMap[from]; !ok {
			return nil, fmt.Errorf("edge defined for unknown node %d", from)
		}
		for _, l := range ls {
			if l == nil {
				return nil, fmt.Errorf("link from node %d is nil", from)
			}
			if l.SourceID() != from {
				return nil, fmt.Errorf("link source mismatch: edge %d vs link %d", from, l.SourceID())
			}
			if _, ok := nodeMap[l.TargetID()]; !ok {
				return nil, fmt.Errorf("link target %d not found", l.TargetID())
			}
			allLinks = append(allLinks, l)
		}
	}

	return &Manager{
		nodes:    nodeMap,
		order:    order,
		outgoing: edges,
		links:    allLinks,
	}, nil
}

// Run executes the requested number of cycles. Each cycle first advances all
// links (delivering packets whose latency expired), then runs nodes in parallel
// and finally waits for the time-based link delay before moving to the next
// cycle. Context cancellation stops the loop immediately.
func (m *Manager) Run(ctx context.Context, cycles uint64) error {
	if cycles == 0 {
		return nil
	}

	for cycle := uint64(0); cycle < cycles; cycle++ {
		m.advanceLinks(cycle)

		var delay time.Duration
		if mockDelayEnabled {
			delay = mockDelay
		}
		if err := m.dispatchCycle(ctx, cycle, delay); err != nil {
			return err
		}
		if mockDelayEnabled && cycle+1 < cycles {
			if err := waitForLink(ctx, delay); err != nil {
				return err
			}
		}
	}

	return ctx.Err()
}

func (m *Manager) advanceLinks(cycle uint64) {
	for _, l := range m.links {
		l.Advance(cycle)
	}
}

func (m *Manager) dispatchCycle(ctx context.Context, cycle uint64, linkDelay time.Duration) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(m.order))

	for _, n := range m.order {
		wg.Add(1)
		go func(n node.Node) {
			defer wg.Done()
			if err := n.Tick(ctx, cycle, linkDelay); err != nil {
				select {
				case errCh <- fmt.Errorf("node %d: %w", n.ID(), err):
				default:
				}
			}
		}(n)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}

	return m.routePackets(cycle)
}

func (m *Manager) routePackets(cycle uint64) error {
	for _, n := range m.order {
		flow := n.Flow()
		if flow == nil {
			continue
		}
		packets := flow.DrainOutgoing()
		if len(packets) == 0 {
			continue
		}

		links := m.outgoing[n.ID()]
		for _, pkt := range packets {
			if pkt.SourceID != n.ID() {
				return fmt.Errorf("packet source mismatch: node %d vs packet %d", n.ID(), pkt.SourceID)
			}
			delivered := false
			for _, l := range links {
				if l.TargetID() == pkt.TargetID {
					l.Transmit(cycle, pkt)
					delivered = true
					break
				}
			}
			if !delivered {
				return fmt.Errorf("no link from node %d to target %d", n.ID(), pkt.TargetID)
			}
		}
	}
	return nil
}

func waitForLink(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
