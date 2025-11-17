package app

import (
	"fmt"

	"github.com/Readm/flow_sim/framework/core"
	"github.com/Readm/flow_sim/framework/hook"
	"github.com/Readm/flow_sim/framework/plugins/policy_manager"
)

// BehaviorContext carries runtime dependencies for a node behavior instance.
type BehaviorContext struct {
	Config             *Config
	Link               *Link
	PluginBroker       *hooks.PluginBroker
	PolicyManager      policy.Manager
	TxFactory          *TxFactory
	PacketAllocator    *PacketIDAllocator
	TransactionManager *TransactionManager
	ResolveLatency     func(fromID, toID int) int
	Meta               NodeMeta
}

// NodeMeta exposes static metadata extracted from the topology graph.
type NodeMeta struct {
	Label        string
	Capabilities []string
	Metadata     map[string]string
	Position     *Position
}

// NodeBehavior describes the runtime hooks a capability-driven node must expose.
type NodeBehavior interface {
	NodeID() int
	NodeType() core.NodeType
	Init(ctx *BehaviorContext) error
	Tick(cycle int)
	CanReceive(edgeKey EdgeKey, packetCount int) bool
	OnPackets(messages []*InFlightMessage, cycle int)
	RegisterIncomingSignal(edge EdgeKey, signal *CycleSignal)
	RegisterOutgoingSignal(edge EdgeKey, signal *CycleSignal)
	Snapshot() NodeSnapshot
}

// BaseSimNode is a thin shim between Simulator orchestration and capability behaviors.
type BaseSimNode struct {
	behavior     NodeBehavior
	label        string
	capabilities []string
	position     *Position
}

// NewBaseSimNode wires a behavior with its metadata and context.
func NewBaseSimNode(behavior NodeBehavior, ctx *BehaviorContext) (*BaseSimNode, error) {
	if behavior == nil {
		return nil, fmt.Errorf("nil node behavior")
	}
	if ctx == nil {
		return nil, fmt.Errorf("nil behavior context")
	}
	if err := behavior.Init(ctx); err != nil {
		return nil, err
	}
	node := &BaseSimNode{
		behavior:     behavior,
		label:        ctx.Meta.Label,
		capabilities: append([]string(nil), ctx.Meta.Capabilities...),
		position:     ctx.Meta.Position,
	}
	return node, nil
}

// ID returns the node identifier.
func (n *BaseSimNode) ID() int {
	if n == nil || n.behavior == nil {
		return 0
	}
	return n.behavior.NodeID()
}

// Type returns the node type reported by the behavior.
func (n *BaseSimNode) Type() core.NodeType {
	if n == nil || n.behavior == nil {
		return ""
	}
	return n.behavior.NodeType()
}

// Tick advances the node state by one cycle.
func (n *BaseSimNode) Tick(cycle int) {
	if n == nil || n.behavior == nil {
		return
	}
	n.behavior.Tick(cycle)
}

// CanReceive delegates to the underlying behavior.
func (n *BaseSimNode) CanReceive(edgeKey EdgeKey, packetCount int) bool {
	if n == nil || n.behavior == nil {
		return false
	}
	return n.behavior.CanReceive(edgeKey, packetCount)
}

// OnPackets forwards received packets to the behavior.
func (n *BaseSimNode) OnPackets(messages []*InFlightMessage, cycle int) {
	if n == nil || n.behavior == nil {
		return
	}
	n.behavior.OnPackets(messages, cycle)
}

// RegisterIncomingSignal registers incoming cycle signals.
func (n *BaseSimNode) RegisterIncomingSignal(edge EdgeKey, signal *CycleSignal) {
	if n == nil || n.behavior == nil {
		return
	}
	n.behavior.RegisterIncomingSignal(edge, signal)
}

// RegisterOutgoingSignal registers outgoing cycle signals.
func (n *BaseSimNode) RegisterOutgoingSignal(edge EdgeKey, signal *CycleSignal) {
	if n == nil || n.behavior == nil {
		return
	}
	n.behavior.RegisterOutgoingSignal(edge, signal)
}

// Snapshot returns a visualization snapshot enriched with metadata.
func (n *BaseSimNode) Snapshot() NodeSnapshot {
	if n == nil || n.behavior == nil {
		return NodeSnapshot{}
	}
	snapshot := n.behavior.Snapshot()
	if snapshot.Label == "" {
		snapshot.Label = n.label
	}
	if len(snapshot.Capabilities) == 0 && len(n.capabilities) > 0 {
		snapshot.Capabilities = append([]string(nil), n.capabilities...)
	}
	if snapshot.ID == 0 {
		snapshot.ID = n.behavior.NodeID()
	}
	if snapshot.Type == "" {
		snapshot.Type = n.behavior.NodeType()
	}
	if snapshot.Payload == nil {
		snapshot.Payload = map[string]any{}
	}
	if n.position != nil {
		if snapshot.Payload == nil {
			snapshot.Payload = map[string]any{}
		}
		snapshot.Payload["position_x"] = n.position.X
		snapshot.Payload["position_y"] = n.position.Y
	}
	return snapshot
}
