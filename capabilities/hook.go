package capabilities

import "flow_sim/hooks"

type hookCapability struct {
	desc   hooks.PluginDescriptor
	bundle hooks.HookBundle
}

// NewHookCapability builds a capability from an arbitrary set of hook handlers.
func NewHookCapability(name string, category hooks.PluginCategory, description string, bundle hooks.HookBundle) NodeCapability {
	return &hookCapability{
		desc: hooks.PluginDescriptor{
			Name:        name,
			Category:    category,
			Description: description,
		},
		bundle: bundle,
	}
}

func (c *hookCapability) Descriptor() hooks.PluginDescriptor {
	return c.desc
}

func (c *hookCapability) Register(broker *hooks.PluginBroker) error {
	if broker == nil {
		return nil
	}
	broker.RegisterBundle(c.desc, c.bundle)
	return nil
}
