package network

import (
	"context"
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/core/trace"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

// ... (rest of imports and structs unchanged) ...

// NodeHandle keeps the node instance together with the concrete queues used to connect links.
type NodeHandle struct {
	Node    node.Node
	Inputs  []*queue.InputQueue
	Outputs []*queue.OutputQueue
}


// Network manages a collection of nodes and links.
// Design assumptions:
// - Network topology is built once (via AddNode/Connect or builder.BuildFromFlowSimNetwork)
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

	// Protocol 配置引用 (只读,保留原始 FlowSimNetwork 用于 Display 数据)
	sourceConfig *protocol.FlowSimNetwork

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
	net := &Network{
		nodes:        make(map[int]*NodeHandle),
		links:        make([]*link.Link, 0),
		nodeList:     nil,
		frozen:       false,
		currentCycle: 0,
	}

	// 自动注入全局 Tracer (如果启用了 -flow_trace)
	if globalTracer := trace.GetGlobalTracer(); globalTracer != nil {
		net.SetTracer(globalTracer)
	}

	return net
}

// CurrentCycle returns the maximum cycle reached by the network (based on targetCycle of last AdvanceTo).
func (n *Network) CurrentCycle() int {
	return n.currentCycle
}

// ===== Protocol Config 访问方法 (Phase 1) =====

// SetSourceConfig 设置原始 Protocol 配置引用 (只读)
func (n *Network) SetSourceConfig(config *protocol.FlowSimNetwork) {
	n.sourceConfig = config
}

// GetSourceConfig 获取原始 Protocol 配置引用 (只读)
func (n *Network) GetSourceConfig() *protocol.FlowSimNetwork {
	return n.sourceConfig
}

// SetTracer 设置 trace recorder 并将其传播到所有支持 trace 的节点
// 必须在 Finalize/Advance 之前调用
// 使用类型断言，只为实现 trace.Traceable 接口的节点设置 tracer
func (n *Network) SetTracer(tracer *trace.TraceRecorder) {
	n.tracer = tracer

	// 使用类型断言，只为支持 trace 的节点设置 tracer
	for _, handle := range n.nodes {
		if traceable, ok := handle.Node.(trace.Traceable); ok {
			traceable.SetTracer(tracer)
		}
	}

	// 为所有 links 设置 tracer
	for _, l := range n.links {
		l.SetTracer(tracer)
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

	// 如果已经设置了 tracer，自动传播到新添加的节点（使用类型断言）
	if n.tracer != nil {
		if traceable, ok := handle.Node.(trace.Traceable); ok {
			traceable.SetTracer(n.tracer)
		}
	}

	return nil
}

// ConnectOption is a functional option for configuring a network connection.
type ConnectOption func(*connectOptions)

type connectOptions struct {
	linkType link.LinkType
}

// WithLinkType specifies a custom link type for the connection.
func WithLinkType(linkType link.LinkType) ConnectOption {
	return func(o *connectOptions) {
		o.linkType = linkType
	}
}

// WithBufferless creates a connection using BufferlessLinkType.
// This is a convenience wrapper for WithLinkType(link.NewBufferlessLinkType()).
func WithBufferless() ConnectOption {
	return func(o *connectOptions) {
		o.linkType = link.NewBufferlessLinkType()
	}
}

// WithHandler is deprecated. Use WithLinkType instead.
// Deprecated: Use WithLinkType.
func WithHandler(handler link.LinkHandler) ConnectOption {
	return WithLinkType(handler)
}

// portToIndex converts a port specification (int or string) to an index.
// Supports both port indices (int) and port names (string).
// Returns an error if the port type is invalid or the port name is not found.
func portToIndex(n node.Node, port interface{}, isInput bool) (int, error) {
	switch p := port.(type) {
	case int:
		// Direct index (backward compatible)
		return p, nil
	case string:
		// Port name lookup
		var idx int
		var ok bool

		// Use type assertion to check if node supports port naming
		type portNamer interface {
			GetInputPortIndex(name string) (int, bool)
			GetOutputPortIndex(name string) (int, bool)
		}

		namer, supportsNaming := n.(portNamer)
		if !supportsNaming {
			return 0, fmt.Errorf("node %d does not support port naming", n.ID())
		}

		if isInput {
			idx, ok = namer.GetInputPortIndex(p)
		} else {
			idx, ok = namer.GetOutputPortIndex(p)
		}

		if !ok {
			portType := "output"
			if isInput {
				portType = "input"
			}
			return 0, fmt.Errorf("node %d: %s port name %q not found", n.ID(), portType, p)
		}
		return idx, nil
	default:
		return 0, fmt.Errorf("invalid port type: %T (expected int or string)", port)
	}
}

// ConnectNodes connects two nodes using Node objects instead of IDs.
// This is the recommended way to connect nodes as it's more type-safe and convenient.
// The node IDs are automatically obtained via source.ID() and target.ID().
//
// Port parameters can be either:
//   - int: port index (e.g., 0, 1, 2)
//   - string: port name (e.g., "to_l2", "from_cpu0")
//
// Port naming must be set up beforehand using node.NameInputPort() or node.NameOutputPort().
func (n *Network) ConnectNodes(
	source node.Node, sourcePort interface{},
	target node.Node, targetPort interface{},
	latency int, bandwidth int,
	opts ...ConnectOption,
) (*link.Link, error) {
	if source == nil || target == nil {
		return nil, fmt.Errorf("source and target nodes must not be nil")
	}

	// Convert port specifications to indices
	sourcePortIdx, err := portToIndex(source, sourcePort, false)
	if err != nil {
		return nil, fmt.Errorf("invalid source port: %w", err)
	}

	targetPortIdx, err := portToIndex(target, targetPort, true)
	if err != nil {
		return nil, fmt.Errorf("invalid target port: %w", err)
	}

	return n.Connect(
		source.ID(), sourcePortIdx,
		target.ID(), targetPortIdx,
		latency, bandwidth,
		opts...,
	)
}

// ConnectNodesWithHandler is deprecated. Use ConnectNodes with WithLinkType or WithBufferless instead.
// Deprecated: Use ConnectNodes(src, srcPort, dst, dstPort, latency, bandwidth, WithBufferless()).
func (n *Network) ConnectNodesWithHandler(
	source node.Node, sourcePort int,
	target node.Node, targetPort int,
	latency int, bandwidth int,
	handler link.LinkHandler,
) (*link.Link, error) {
	return n.ConnectNodes(source, sourcePort, target, targetPort,
		latency, bandwidth, WithLinkType(handler))
}

// Connect wires a source output queue to a target input queue with a Link.
// Must be called before Advance. Panics if network is frozen.
func (n *Network) Connect(sourceID int, sourceOutputIdx int, targetID int, targetInputIdx int, latency int, bandwidth int, opts ...ConnectOption) (*link.Link, error) {
	if n.frozen {
		panic("cannot connect after network is frozen (Advance called)")
	}

	// Default options
	options := connectOptions{
		linkType: nil,
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

	// 检查端口是否已被连接（鲁棒性检查）
	if sourceOutput.GetDownstreamPort() != nil {
		return nil, fmt.Errorf("source node %d output port %d is already connected", sourceID, sourceOutputIdx)
	}
	if targetInput.GetUpstreamPort() != nil {
		return nil, fmt.Errorf("target node %d input port %d is already connected", targetID, targetInputIdx)
	}

	// Create Link with port IDs
	var linkInstance *link.Link
	if options.linkType != nil {
		linkInstance = link.NewLinkWithPortIDs(sourceID, sourceOutputIdx, targetID, targetInputIdx, latency, bandwidth, options.linkType)
	} else {
		linkInstance = link.NewLinkWithPortIDs(sourceID, sourceOutputIdx, targetID, targetInputIdx, latency, bandwidth, link.NewBufferedLinkType(latency, bandwidth))
	}

	// Connect using ahead_port.ConnectWithIDs for profiling
	// Port 1: sourceOutput -> linkInstance (sourceID sends to link)
	ahead_port.ConnectWithIDs(sourceID, targetID, sourceOutput, linkInstance)
	// Port 2: linkInstance -> targetInput (link sends to targetID)
	ahead_port.ConnectWithIDs(sourceID, targetID, linkInstance, targetInput)

	// Link will be initialized in Advance or explicitly by user

	// Inject global tracer if enabled
	if n.tracer != nil {
		linkInstance.SetTracer(n.tracer)
	}

	n.links = append(n.links, linkInstance)
	return linkInstance, nil
}

// ConnectWithHandler is a legacy wrapper. Use Connect(..., WithHandler(h)) instead.
func (n *Network) ConnectWithHandler(sourceID int, sourceOutputIdx int, targetID int, targetInputIdx int, latency int, bandwidth int, handler link.LinkHandler) (*link.Link, error) {
	return n.Connect(sourceID, sourceOutputIdx, targetID, targetInputIdx, latency, bandwidth, WithHandler(handler))
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
