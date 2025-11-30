package network

import (
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
)

// NodeHandle keeps the node instance together with the concrete queues used to connect links.
type NodeHandle struct {
	Node    *node.Node
	Inputs  []*queue.InputQueue
	Outputs []*queue.OutputQueue
}

type Network struct {
	mu    sync.RWMutex
	nodes map[int]*NodeHandle
	links []*link.Link
}

// New creates an empty network.
func New() *Network {
	return &Network{
		nodes: make(map[int]*NodeHandle),
		links: make([]*link.Link, 0),
	}
}

// AddNode registers a node handle in the network.
func (n *Network) AddNode(handle *NodeHandle) error {
	if handle == nil || handle.Node == nil {
		return fmt.Errorf("node handle cannot be nil")
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	id := handle.Node.ID()
	if _, exists := n.nodes[id]; exists {
		return fmt.Errorf("node %d already exists", id)
	}

	n.nodes[id] = handle
	return nil
}

// Connect wires a source output queue to a target input queue with a Link.
func (n *Network) Connect(sourceID int, sourceOutputIdx int, targetID int, targetInputIdx int, latency int, bandwidth int) (*link.Link, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	source, ok := n.nodes[sourceID]
	if !ok {
		return nil, fmt.Errorf("source node %d not found", sourceID)
	}
	target, ok := n.nodes[targetID]
	if !ok {
		return nil, fmt.Errorf("target node %d not found", targetID)
	}

	if sourceOutputIdx < 0 || sourceOutputIdx >= len(source.Outputs) {
		return nil, fmt.Errorf("source node %d output index %d invalid", sourceID, sourceOutputIdx)
	}
	if targetInputIdx < 0 || targetInputIdx >= len(target.Inputs) {
		return nil, fmt.Errorf("target node %d input index %d invalid", targetID, targetInputIdx)
	}

	sourceOutput := source.Outputs[sourceOutputIdx]
	targetInput := target.Inputs[targetInputIdx]
	if sourceOutput == nil || targetInput == nil {
		return nil, fmt.Errorf("queues for connection %d->%d must not be nil", sourceID, targetID)
	}

	upstreamPort := ahead_port.NewAheadPort(bandwidth)
	sourceOutput.SetOutPort(upstreamPort)

	linkInstance := link.NewLink(
		sourceID,
		targetID,
		upstreamPort,
		targetInput.InPort(),
		latency,
		bandwidth,
	)

	n.links = append(n.links, linkInstance)
	return linkInstance, nil
}

// Advance runs all registered nodes and links in parallel for the given number of cycles.
func (n *Network) Advance(cycles int) error {
	if cycles <= 0 {
		return nil
	}

	n.mu.RLock()
	nodes := make([]*NodeHandle, 0, len(n.nodes))
	for _, handle := range n.nodes {
		nodes = append(nodes, handle)
	}
	links := append([]*link.Link(nil), n.links...)
	n.mu.RUnlock()

	var wg sync.WaitGroup
	errCh := make(chan error, len(nodes)+len(links))

	for _, handle := range nodes {
		wg.Add(1)
		go func(h *NodeHandle) {
			defer wg.Done()
			if err := h.Node.Advance(cycles); err != nil {
				errCh <- fmt.Errorf("node %d advance failed: %w", h.Node.ID(), err)
			}
		}(handle)
	}

	for _, lk := range links {
		wg.Add(1)
		go func(l *link.Link) {
			defer wg.Done()
			if err := l.Advance(cycles); err != nil {
				errCh <- fmt.Errorf("link %d->%d advance failed: %w", l.SourceID(), l.TargetID(), err)
			}
		}(lk)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}
