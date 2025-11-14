package capabilities

import (
	"strconv"
	"sync"

	"github.com/Readm/flow_sim/hooks"
)

// RingFinalTargetMetadataKey stores the ultimate destination for ring forwarding.
const RingFinalTargetMetadataKey = "ring_final_target"

// RingRouterConfig describes attachment information for a ring routing node.
type RingRouterConfig struct {
	RouterID     int
	NextRouterID int
	LocalNodeIDs []int
}

// RingRouterCapability exposes hook-based forwarding logic for ring routers.
type RingRouterCapability interface {
	NodeCapability
	Update(cfg RingRouterConfig)
}

type ringRouterCapability struct {
	desc hooks.PluginDescriptor

	mu           sync.RWMutex
	routerID     int
	nextRouterID int
	localNodes   map[int]struct{}
}

// NewRingRouterCapability creates a capability that forwards packets around a ring.
func NewRingRouterCapability(name string, cfg RingRouterConfig) RingRouterCapability {
	cap := &ringRouterCapability{
		desc: hooks.PluginDescriptor{
			Name:        name,
			Category:    hooks.PluginCategoryCapability,
			Description: "ring router forwarding capability",
		},
		localNodes: make(map[int]struct{}),
	}
	cap.Update(cfg)
	return cap
}

func (c *ringRouterCapability) Descriptor() hooks.PluginDescriptor {
	return c.desc
}

func (c *ringRouterCapability) Register(b *hooks.PluginBroker) error {
	if b == nil {
		return nil
	}
	b.RegisterBundle(c.desc, hooks.HookBundle{
		BeforeRoute: []hooks.BeforeRouteHook{c.beforeRoute},
	})
	return nil
}

func (c *ringRouterCapability) Update(cfg RingRouterConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.routerID = cfg.RouterID
	c.nextRouterID = cfg.NextRouterID
	c.localNodes = make(map[int]struct{}, len(cfg.LocalNodeIDs))
	for _, id := range cfg.LocalNodeIDs {
		c.localNodes[id] = struct{}{}
	}
}

func (c *ringRouterCapability) beforeRoute(ctx *hooks.RouteContext) error {
	if ctx == nil || ctx.Packet == nil {
		return nil
	}
	c.mu.RLock()
	routerID := c.routerID
	nextRouterID := c.nextRouterID
	local := c.localNodes
	c.mu.RUnlock()

	if routerID == 0 || ctx.SourceNodeID != routerID {
		return nil
	}

	finalTarget := readFinalTarget(ctx)
	if _, exists := local[finalTarget]; exists {
		ctx.TargetID = finalTarget
		return nil
	}

	if nextRouterID != 0 {
		ctx.TargetID = nextRouterID
	}

	return nil
}

func readFinalTarget(ctx *hooks.RouteContext) int {
	if ctx == nil || ctx.Packet == nil {
		return ctx.DefaultTarget
	}
	if value, ok := ctx.Packet.GetMetadata(RingFinalTargetMetadataKey); ok {
		if id, err := strconv.Atoi(value); err == nil {
			return id
		}
	}
	if ctx.Packet.DstID != 0 {
		return ctx.Packet.DstID
	}
	return ctx.DefaultTarget
}
