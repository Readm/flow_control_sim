package capabilities

import (
	"github.com/Readm/flow_sim/hooks"
)

// RingRoutingCapability routes packets along a logical ring by picking the next hop.
type ringRoutingCapability struct {
	desc   hooks.PluginDescriptor
	order  []int
	index  map[int]int
	length int
}

// NewRingRoutingCapability builds a capability that advances packets along the provided order.
func NewRingRoutingCapability(name string, order []int) NodeCapability {
	index := make(map[int]int, len(order))
	for i, id := range order {
		if _, ok := index[id]; !ok {
			index[id] = i
		}
	}
	return &ringRoutingCapability{
		desc: hooks.PluginDescriptor{
			Name:        name,
			Category:    hooks.PluginCategoryCapability,
			Description: "ring routing capability",
		},
		order:  order,
		index:  index,
		length: len(order),
	}
}

func (c *ringRoutingCapability) Descriptor() hooks.PluginDescriptor {
	return c.desc
}

func (c *ringRoutingCapability) Register(b *hooks.PluginBroker) error {
	if b == nil || c.length == 0 {
		return nil
	}
	b.RegisterBundle(c.desc, hooks.HookBundle{
		BeforeRoute: []hooks.BeforeRouteHook{c.beforeRoute},
	})
	return nil
}

func (c *ringRoutingCapability) beforeRoute(ctx *hooks.RouteContext) error {
	if ctx == nil {
		return nil
	}
	if ctx.SourceNodeID == ctx.TargetID {
		return nil
	}
	next := c.nextHop(ctx.SourceNodeID, ctx.TargetID)
	ctx.TargetID = next
	return nil
}

func (c *ringRoutingCapability) nextHop(source, target int) int {
	if c.length == 0 {
		return target
	}
	srcIdx, ok := c.index[source]
	if !ok {
		return target
	}
	if _, ok := c.index[target]; !ok {
		return target
	}
	nextIdx := (srcIdx + 1) % c.length
	return c.order[nextIdx]
}
