package network

import (
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/internal/core/capability/cache"
	"github.com/Readm/flow_sim/internal/core/capability/directory"
	"github.com/Readm/flow_sim/internal/core/debug"
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

// CacheConfigSchema represents cache configuration in the OpenAPI schema.
type CacheConfigSchema struct {
	Capacity          int    `json:"capacity"`
	NumSets           int    `json:"num_sets"`
	ReplacementPolicy string `json:"replacement_policy"`
	States            string `json:"states"`
}

// DirectoryConfigSchema represents directory configuration in the OpenAPI schema.
type DirectoryConfigSchema struct {
	Capacity          int    `json:"capacity"`
	NumSets           int    `json:"num_sets"`
	ReplacementPolicy string `json:"replacement_policy"`
	States            string `json:"states"`
}

// NodeSchema represents a node in the OpenAPI schema.
type NodeSchema struct {
	NodeID            int                    `json:"node_id"`
	NodeName          string                 `json:"node_name,omitempty"`
	NodeFeatures      []string               `json:"node_features,omitempty"`
	Cache             *CacheConfigSchema     `json:"cache,omitempty"`
	Directory         *DirectoryConfigSchema `json:"directory,omitempty"`
	CoherenceDomainID *int                   `json:"coherence_domain_id,omitempty"`
	InPorts           []PortSchema           `json:"in_ports,omitempty"`
	OutPorts          []PortSchema           `json:"out_ports,omitempty"`
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

// Network manages a collection of nodes and links.
// Design assumptions:
// - Network topology is built once (via AddNode/Connect or FromSchema)
// - After construction, topology is immutable during Advance
// - No concurrent modifications during Advance (single-threaded execution model)
// - Advance can be called multiple times sequentially
type Network struct {
	nodes map[int]*NodeHandle
	links []*link.Link

	// Cached slices for Advance (built once during first Advance or explicit finalization)
	nodeList []*NodeHandle
	frozen   bool // True after first Advance or explicit Finalize
}

// New creates an empty network.
func New() *Network {
	return &Network{
		nodes:    make(map[int]*NodeHandle),
		links:    make([]*link.Link, 0),
		nodeList: nil,
		frozen:   false,
	}
}

// AddNode registers a node handle in the network.
// Must be called before Advance. Panics if network is frozen.
func (n *Network) AddNode(handle *NodeHandle) error {
	if n.frozen {
		panic("cannot add node after network is frozen (Advance called)")
	}
	if handle == nil || handle.Node == nil {
		return fmt.Errorf("node handle cannot be nil")
	}

	id := handle.Node.ID()
	if _, exists := n.nodes[id]; exists {
		return fmt.Errorf("node %d already exists", id)
	}

	n.nodes[id] = handle
	return nil
}

// Connect wires a source output queue to a target input queue with a Link.
// Must be called before Advance. Panics if network is frozen.
func (n *Network) Connect(sourceID int, sourceOutputIdx int, targetID int, targetInputIdx int, latency int, bandwidth int) (*link.Link, error) {
	if n.frozen {
		panic("cannot connect after network is frozen (Advance called)")
	}

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

	// Create Link with new API (returns 3 values)
	linkInstance, linkIn, linkOut := link.NewLink(
		sourceID,
		targetID,
		latency,
		bandwidth,
	)

	// Connect using Plug pattern
	// Link.InPort (receives) plugs OutputQueue's internal Queue.OutPort (sends)
	// Link.OutPort (sends) plugs InputQueue (receives via its InPort)
	linkIn.Plug(sourceOutput.QueueOutPort())
	linkOut.Plug(targetInput.AsInPort())

	n.links = append(n.links, linkInstance)
	return linkInstance, nil
}

// Reset clears the network and rebuilds it from the provided schema.
// Must be called before any Advance. Panics if network is frozen.
func (n *Network) Reset(schema *NetworkSchema) error {
	if n.frozen {
		panic("cannot reset after network is frozen (Advance called)")
	}
	if schema == nil {
		return fmt.Errorf("schema cannot be nil")
	}

	// Clear existing nodes and links
	n.nodes = make(map[int]*NodeHandle)
	n.links = make([]*link.Link, 0)
	n.nodeList = nil
	n.frozen = false

	// Create nodes from schema
	for _, nodeSchema := range schema.Nodes {
		// Create node
		newNode := node.New(nodeSchema.NodeID)

		// Create cache if configured
		// TODO: adapt cache configs
		if nodeSchema.Cache != nil {
			cacheInstance := cache.NewFullyAssociativeCache(nodeSchema.Cache.Capacity)
			newNode.AddCache(cacheInstance)
		}

		// Create directory if configured
		// TODO: adapt cache configs
		if nodeSchema.Directory != nil {
			directoryInstance := directory.NewFullyAssociativeDirectory(nodeSchema.Directory.Capacity)
			newNode.AddDirectory(directoryInstance)
		}

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

		// Create Link with new API (returns 3 values)
		linkInstance, linkIn, linkOut := link.NewLink(
			edgeSchema.SrcNodeID,
			edgeSchema.DstNodeID,
			latency,
			bandwidth,
		)

		// Connect using Plug pattern
		linkIn.Plug(sourceOutput.QueueOutPort())
		linkOut.Plug(targetInput.AsInPort())

		n.links = append(n.links, linkInstance)
	}

	return nil
}

// Advance runs all registered nodes and links in parallel for the given number of cycles.
// On first call, freezes the network topology (no more AddNode/Connect allowed).
// Can be called multiple times sequentially.
func (n *Network) Advance(cycles int) error {
	if cycles <= 0 {
		return nil
	}

	// Freeze and build node list on first Advance
	if !n.frozen {
		n.nodeList = make([]*NodeHandle, 0, len(n.nodes))
		for _, handle := range n.nodes {
			n.nodeList = append(n.nodeList, handle)
		}
		n.frozen = true
		debug.Logf("Network.Advance: network frozen, nodes=%d, links=%d", len(n.nodeList), len(n.links))
	}

	debug.Logf("Network.Advance: starting cycles=%d", cycles)

	// Run all nodes and links in parallel
	var wg sync.WaitGroup
	errCh := make(chan error, len(n.nodeList)+len(n.links))

	for _, handle := range n.nodeList {
		wg.Add(1)
		nodeID := handle.Node.ID()
		go func(h *NodeHandle) {
			defer wg.Done()
			debug.Logf("Network.Advance: node %d starting Advance(%d)", nodeID, cycles)
			if err := h.Node.Advance(cycles); err != nil {
				errCh <- fmt.Errorf("node %d advance failed: %w", nodeID, err)
			}
			debug.Logf("Network.Advance: node %d completed Advance(%d)", nodeID, cycles)
		}(handle)
	}

	for _, lk := range n.links {
		wg.Add(1)
		srcID := lk.SourceID()
		tgtID := lk.TargetID()
		go func(l *link.Link) {
			defer wg.Done()
			debug.Logf("Network.Advance: link %d->%d starting Advance(%d)", srcID, tgtID, cycles)
			if err := l.Advance(cycles); err != nil {
				errCh <- fmt.Errorf("link %d->%d advance failed: %w", srcID, tgtID, err)
			}
			debug.Logf("Network.Advance: link %d->%d completed Advance(%d)", srcID, tgtID, cycles)
		}(lk)
	}

	debug.Logf("Network.Advance: waiting for all components to complete")
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			debug.Logf("Network.Advance: error occurred: %v", err)
			return err
		}
	}
	debug.Logf("Network.Advance: completed successfully")
	return nil
}
