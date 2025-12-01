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

// PortSchema represents a port in the OpenAPI schema.
type PortSchema struct {
	PortID       *int  `json:"port_id,omitempty"`
	PacketTypes  []int `json:"packet_types,omitempty"`
	BufferSize   int   `json:"buffer_size"`
	InBandwidth  int   `json:"in_bandwidth"`
	OutBandwidth int   `json:"out_bandwidth"`
}

// NodeSchema represents a node in the OpenAPI schema.
type NodeSchema struct {
	NodeID   int          `json:"node_id"`
	NodeName string       `json:"node_name,omitempty"`
	InPorts  []PortSchema `json:"in_ports,omitempty"`
	OutPorts []PortSchema `json:"out_ports,omitempty"`
}

// EdgeSchema represents an edge in the OpenAPI schema.
type EdgeSchema struct {
	EdgeID      int   `json:"edge_id"`
	SrcNodeID   int   `json:"src_node_id"`
	SrcPortID   int   `json:"src_port_id"`
	DstNodeID   int   `json:"dst_node_id"`
	DstPortID   int   `json:"dst_port_id"`
	PacketTypes []int `json:"packet_types,omitempty"`
}

// NetworkSchema represents the network topology in the OpenAPI schema.
type NetworkSchema struct {
	Version string       `json:"version,omitempty"`
	Nodes   []NodeSchema `json:"nodes"`
	Edges   []EdgeSchema `json:"edges"`
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

// Reset clears the network and rebuilds it from the provided schema.
func (n *Network) Reset(schema *NetworkSchema) error {
	if schema == nil {
		return fmt.Errorf("schema cannot be nil")
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	// Clear existing nodes and links
	n.nodes = make(map[int]*NodeHandle)
	n.links = make([]*link.Link, 0)

	// Create nodes from schema
	for _, nodeSchema := range schema.Nodes {
		// Create node
		newNode := node.New(nodeSchema.NodeID)

		// Create input queues
		inputs := make([]*queue.InputQueue, 0, len(nodeSchema.InPorts))
		for i, portSchema := range nodeSchema.InPorts {
			bufferSize := portSchema.BufferSize
			if bufferSize <= 0 {
				bufferSize = 8 // Default bufferSize
			}
			inBandwidth := portSchema.InBandwidth
			if inBandwidth <= 0 {
				return fmt.Errorf("node %d input port %d: inBandwidth must be positive, got %d", nodeSchema.NodeID, i, inBandwidth)
			}
			outBandwidth := portSchema.OutBandwidth
			if outBandwidth <= 0 {
				return fmt.Errorf("node %d input port %d: outBandwidth must be positive, got %d", nodeSchema.NodeID, i, outBandwidth)
			}
			iq := queue.NewInputQueue(bufferSize, inBandwidth, outBandwidth)
			inputs = append(inputs, iq)
			if err := newNode.AddInputQueue(iq); err != nil {
				return fmt.Errorf("failed to add input queue to node %d port %d: %w", nodeSchema.NodeID, i, err)
			}
		}

		// Create output queues
		outputs := make([]*queue.OutputQueue, 0, len(nodeSchema.OutPorts))
		for i, portSchema := range nodeSchema.OutPorts {
			bufferSize := portSchema.BufferSize
			if bufferSize <= 0 {
				bufferSize = 8 // Default bufferSize
			}
			inBandwidth := portSchema.InBandwidth
			if inBandwidth <= 0 {
				return fmt.Errorf("node %d output port %d: inBandwidth must be positive, got %d", nodeSchema.NodeID, i, inBandwidth)
			}
			outBandwidth := portSchema.OutBandwidth
			if outBandwidth <= 0 {
				return fmt.Errorf("node %d output port %d: outBandwidth must be positive, got %d", nodeSchema.NodeID, i, outBandwidth)
			}
			oq := queue.NewOutputQueue(bufferSize, inBandwidth, outBandwidth)
			outputs = append(outputs, oq)
			if err := newNode.AddOutputQueue(oq); err != nil {
				return fmt.Errorf("failed to add output queue to node %d port %d: %w", nodeSchema.NodeID, i, err)
			}
		}

		// Create node handle
		handle := &NodeHandle{
			Node:    newNode,
			Inputs:  inputs,
			Outputs: outputs,
		}

		// Add to network
		n.nodes[nodeSchema.NodeID] = handle
	}

	// Create links from edges
	for _, edgeSchema := range schema.Edges {
		// Validate source node
		source, ok := n.nodes[edgeSchema.SrcNodeID]
		if !ok {
			return fmt.Errorf("source node %d not found for edge %d", edgeSchema.SrcNodeID, edgeSchema.EdgeID)
		}
		// Validate target node
		target, ok := n.nodes[edgeSchema.DstNodeID]
		if !ok {
			return fmt.Errorf("target node %d not found for edge %d", edgeSchema.DstNodeID, edgeSchema.EdgeID)
		}

		// Validate port indices
		if edgeSchema.SrcPortID < 0 || edgeSchema.SrcPortID >= len(source.Outputs) {
			return fmt.Errorf("source node %d output index %d invalid for edge %d", edgeSchema.SrcNodeID, edgeSchema.SrcPortID, edgeSchema.EdgeID)
		}
		if edgeSchema.DstPortID < 0 || edgeSchema.DstPortID >= len(target.Inputs) {
			return fmt.Errorf("target node %d input index %d invalid for edge %d", edgeSchema.DstNodeID, edgeSchema.DstPortID, edgeSchema.EdgeID)
		}

		sourceOutput := source.Outputs[edgeSchema.SrcPortID]
		targetInput := target.Inputs[edgeSchema.DstPortID]
		if sourceOutput == nil || targetInput == nil {
			return fmt.Errorf("queues for edge %d (%d->%d) must not be nil", edgeSchema.EdgeID, edgeSchema.SrcNodeID, edgeSchema.DstNodeID)
		}

		// Use default values: latency=1, bandwidth=1
		latency := 1
		bandwidth := 1

		upstreamPort := ahead_port.NewAheadPort(bandwidth)
		sourceOutput.SetOutPort(upstreamPort)

		linkInstance := link.NewLink(
			edgeSchema.SrcNodeID,
			edgeSchema.DstNodeID,
			upstreamPort,
			targetInput.InPort(),
			latency,
			bandwidth,
		)

		n.links = append(n.links, linkInstance)
	}

	return nil
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
