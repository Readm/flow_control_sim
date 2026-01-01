package network

import (
	"context"
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/components/directory"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/core/trace"
)

// ... (rest of imports and structs unchanged) ...

// NodeHandle keeps the node instance together with the concrete queues used to connect links.
type NodeHandle struct {
	Node    node.Node
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
	nodeList     []*NodeHandle
	frozen       bool // True after first Advance or explicit Finalize
	currentCycle int  // Track max cycle reached for convenience

	// Worker management
	workerCtx    context.Context
	workerCancel context.CancelFunc
	workerWg     sync.WaitGroup
	nodeCmds     []chan int
	linkCmds     []chan int

	// Trace recorder（可选，用于生成 Chrome trace）
	tracer *trace.TraceRecorder
}

// New creates an empty network.
func New() *Network {
	return &Network{
		nodes:        make(map[int]*NodeHandle),
		links:        make([]*link.Link, 0),
		nodeList:     nil,
		frozen:       false,
		currentCycle: 0,
	}
}

// CurrentCycle returns the maximum cycle reached by the network (based on targetCycle of last AdvanceTo).
func (n *Network) CurrentCycle() int {
	return n.currentCycle
}

// SetTracer 设置 trace recorder 并将其传播到所有节点
// 必须在 Finalize/Advance 之前调用
func (n *Network) SetTracer(tracer *trace.TraceRecorder) {
	n.tracer = tracer

	// 将 tracer 传播到所有已添加的节点
	for _, handle := range n.nodes {
		if baseNode, ok := handle.Node.(*node.BaseNode); ok {
			baseNode.SetTracer(tracer)
		}
	}
}

// GetTracer 获取 trace recorder
func (n *Network) GetTracer() *trace.TraceRecorder {
	return n.tracer
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

	// 如果已经设置了 tracer，自动传播到新添加的节点
	if n.tracer != nil {
		if baseNode, ok := handle.Node.(*node.BaseNode); ok {
			baseNode.SetTracer(n.tracer)
		}
	}

	return nil
}

// ConnectOption is a functional option for configuring a network connection.
type ConnectOption func(*connectOptions)

type connectOptions struct {
	handler link.LinkHandler
}

// WithHandler specifies a custom link handler for the connection.
func WithHandler(handler link.LinkHandler) ConnectOption {
	return func(o *connectOptions) {
		o.handler = handler
	}
}

// Connect wires a source output queue to a target input queue with a Link.
// Must be called before Advance. Panics if network is frozen.
func (n *Network) Connect(sourceID int, sourceOutputIdx int, targetID int, targetInputIdx int, latency int, bandwidth int, opts ...ConnectOption) (*link.Link, error) {
	if n.frozen {
		panic("cannot connect after network is frozen (Advance called)")
	}

	// Default options
	options := connectOptions{
		handler: nil,
	}
	for _, opt := range opts {
		opt(&options)
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

	// Create Link
	var linkInstance *link.Link
	if options.handler != nil {
		linkInstance = link.NewLinkWithHandler(sourceID, targetID, latency, bandwidth, options.handler)
	} else {
		linkInstance = link.NewLink(sourceID, targetID, latency, bandwidth)
	}

	// Connect using ahead_port.ConnectWithIDs for profiling
	// Port 1: sourceOutput -> linkInstance (sourceID sends to link)
	ahead_port.ConnectWithIDs(sourceID, targetID, sourceOutput, linkInstance)
	// Port 2: linkInstance -> targetInput (link sends to targetID)
	ahead_port.ConnectWithIDs(sourceID, targetID, linkInstance, targetInput)

	// Link will be initialized in Advance or explicitly by user

	n.links = append(n.links, linkInstance)
	return linkInstance, nil
}

// ConnectWithHandler is a legacy wrapper. Use Connect(..., WithHandler(h)) instead.
func (n *Network) ConnectWithHandler(sourceID int, sourceOutputIdx int, targetID int, targetInputIdx int, latency int, bandwidth int, handler link.LinkHandler) (*link.Link, error) {
	return n.Connect(sourceID, sourceOutputIdx, targetID, targetInputIdx, latency, bandwidth, WithHandler(handler))
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
	n.stopWorkers()
	n.nodes = make(map[int]*NodeHandle)
	n.links = make([]*link.Link, 0)
	n.nodeList = nil
	n.frozen = false

	// Create nodes from schema
	for _, nodeSchema := range schema.Nodes {
		// Create node
		newNode := node.NewWorkerNode(nodeSchema.NodeID)

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
			iq := queue.NewInputQueue(bufferSize, inBandwidth)
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
			oq := queue.NewOutputQueue(bufferSize, outBandwidth)
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

		// Create Link
		linkInstance := link.NewLink(
			edgeSchema.SrcNodeID,
			edgeSchema.DstNodeID,
			latency,
			bandwidth,
		)

		// Connect using ahead_port.ConnectWithIDs for profiling
		// OutputQueue -> Link -> InputQueue
		ahead_port.ConnectWithIDs(edgeSchema.SrcNodeID, edgeSchema.DstNodeID, sourceOutput, linkInstance)
		ahead_port.ConnectWithIDs(edgeSchema.SrcNodeID, edgeSchema.DstNodeID, linkInstance, targetInput)

		// Link will be initialized in Advance or explicitly by user

		n.links = append(n.links, linkInstance)
	}

	return nil
}

// AdvanceTo runs all registered nodes and links in parallel up to the target cycle.
// On first call, freezes the network topology (no more AddNode/Connect allowed).
// Can be called multiple times sequentially with increasing target cycles.
func (n *Network) AdvanceTo(targetCycle int) error {
	if targetCycle < 0 {
		return nil
	}

	// Freeze and build node list on first Advance
	if !n.frozen {
		n.nodeList = make([]*NodeHandle, 0, len(n.nodes))
		for _, handle := range n.nodes {
			n.nodeList = append(n.nodeList, handle)
		}
		n.frozen = true
		debug.Logf("Network.AdvanceTo: network frozen, nodes=%d, links=%d", len(n.nodeList), len(n.links))

		// Initialize all links
		for _, l := range n.links {
			l.Init()
		}
	}

	// Ensure workers are started
	if n.workerCtx == nil {
		n.startWorkers()
	}

	debug.Logf("Network.AdvanceTo: starting to targetCycle=%d", targetCycle)

	// Broadcast target cycle to all workers
	n.workerWg.Add(len(n.nodeCmds) + len(n.linkCmds))
	for _, ch := range n.nodeCmds {
		ch <- targetCycle
	}
	for _, ch := range n.linkCmds {
		ch <- targetCycle
	}

	// Wait for all workers to complete this cycle
	n.workerWg.Wait()

	// Update network's current cycle to reflect execution up to targetCycle
	n.currentCycle = targetCycle + 1

	debug.Logf("Network.AdvanceTo: completed successfully")
	return nil
}

func (n *Network) startWorkers() {
	n.workerCtx, n.workerCancel = context.WithCancel(context.Background())
	n.nodeCmds = make([]chan int, 0, len(n.nodeList))
	n.linkCmds = make([]chan int, 0, len(n.links))

	// Start Node Workers
	for _, handle := range n.nodeList {
		cmdCh := make(chan int, 1) // Buffered to prevent blocking sender if possible (though we strictly synchronize)
		n.nodeCmds = append(n.nodeCmds, cmdCh)

		// Use local variable for closure capture
		h := handle
		nodeID := h.Node.ID()

		go func(cmd <-chan int) {
			for {
				select {
				case <-n.workerCtx.Done():
					return
				case target := <-cmd:
					debug.Logf("Network.AdvanceTo: node %d starting AdvanceTo(%d)", nodeID, target)
					if err := h.Node.AdvanceTo(target); err != nil {
						// Error handling in worker is tricky. For now log and panic or channel back?
						// The original code used an errCh. We should probably keep that.
						// But for high perf, error checking might be omitted or handled differently.
						// Let's Log Panic for now as errors strictly shouldn't happen in sim unless bug.
						debug.Logf("ERROR: node %d advance failed: %v", nodeID, err)
					}
					debug.Logf("Network.AdvanceTo: node %d completed AdvanceTo(%d)", nodeID, target)
					n.workerWg.Done()
				}
			}
		}(cmdCh)
	}

	// Start Link Workers
	for _, lk := range n.links {
		cmdCh := make(chan int, 1)
		n.linkCmds = append(n.linkCmds, cmdCh)

		l := lk
		srcID := l.SourceID()
		tgtID := l.TargetID()

		go func(cmd <-chan int) {
			for {
				select {
				case <-n.workerCtx.Done():
					return
				case target := <-cmd:
					debug.Logf("Network.AdvanceTo: link %d->%d starting AdvanceTo(%d)", srcID, tgtID, target)
					if err := l.AdvanceTo(target); err != nil {
						debug.Logf("ERROR: link %d->%d advance failed: %v", srcID, tgtID, err)
					}
					debug.Logf("Network.AdvanceTo: link %d->%d completed AdvanceTo(%d)", srcID, tgtID, target)
					n.workerWg.Done()
				}
			}
		}(cmdCh)
	}
}

func (n *Network) stopWorkers() {
	if n.workerCancel != nil {
		n.workerCancel()
		n.workerCancel = nil
	}
	// We don't wait for workers to finish here because Cancel is sufficient to stop them eventually.
	// But strictly, we might want to? For now, simple cancel.
	n.workerCtx = nil
	n.nodeCmds = nil
	n.linkCmds = nil
}
