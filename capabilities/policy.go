package capabilities

import (
	"flow_sim/hooks"
	"flow_sim/policy"
)

type policyRoutingCapability struct {
	name   string
	policy policy.Manager
}

type policyFlowCapability struct {
	name   string
	policy policy.Manager
}

// NewRoutingCapability returns a capability that resolves routes using policy.Manager.
func NewRoutingCapability(name string, mgr policy.Manager) NodeCapability {
	return &policyRoutingCapability{
		name:   name,
		policy: mgr,
	}
}

// NewFlowControlCapability returns a capability that performs flow control using policy.Manager.
func NewFlowControlCapability(name string, mgr policy.Manager) NodeCapability {
	return &policyFlowCapability{
		name:   name,
		policy: mgr,
	}
}

func (c *policyRoutingCapability) Descriptor() hooks.PluginDescriptor {
	return hooks.PluginDescriptor{
		Name:        c.name,
		Category:    hooks.PluginCategoryPolicy,
		Description: "default routing capability",
	}
}

func (c *policyRoutingCapability) Register(broker *hooks.PluginBroker) error {
	if broker == nil {
		return nil
	}
	broker.RegisterBundle(c.Descriptor(), hooks.HookBundle{
		BeforeRoute: []hooks.BeforeRouteHook{c.beforeRoute},
	})
	return nil
}

func (c *policyRoutingCapability) beforeRoute(ctx *hooks.RouteContext) error {
	if c.policy == nil || ctx == nil || ctx.Packet == nil {
		return nil
	}
	defaultTarget := ctx.TargetID
	if defaultTarget == 0 {
		defaultTarget = ctx.DefaultTarget
	}
	resolved, err := c.policy.ResolveRoute(ctx.Packet, ctx.SourceNodeID, defaultTarget)
	if err != nil {
		return err
	}
	ctx.TargetID = resolved
	return nil
}

func (c *policyFlowCapability) Descriptor() hooks.PluginDescriptor {
	return hooks.PluginDescriptor{
		Name:        c.name,
		Category:    hooks.PluginCategoryPolicy,
		Description: "default flow control capability",
	}
}

func (c *policyFlowCapability) Register(broker *hooks.PluginBroker) error {
	if broker == nil {
		return nil
	}
	broker.RegisterBundle(c.Descriptor(), hooks.HookBundle{
		BeforeSend: []hooks.BeforeSendHook{c.beforeSend},
	})
	return nil
}

func (c *policyFlowCapability) beforeSend(ctx *hooks.MessageContext) error {
	if c.policy == nil || ctx == nil || ctx.Packet == nil {
		return nil
	}
	target := ctx.TargetID
	if target == 0 {
		target = ctx.Packet.DstID
	}
	return c.policy.CheckFlowControl(ctx.Packet, ctx.NodeID, target)
}
