package incentives

import (
	"fmt"

	"flow_sim/hooks"
)

// Factory installs incentive hooks into the broker.
type Factory func(broker *hooks.PluginBroker) error

// Options configure incentive plugin registration.
type Options struct {
	Factories map[string]Factory
}

// Register registers incentive plugins for each provided factory.
func Register(reg *hooks.Registry, opts Options) error {
	if reg == nil {
		return fmt.Errorf("registry is nil")
	}
	for name, factory := range opts.Factories {
		if factory == nil {
			continue
		}
		desc := hooks.PluginDescriptor{
			Name:        pluginName(name),
			Category:    hooks.PluginCategoryCapability,
			Description: fmt.Sprintf("%s incentive plugin", name),
		}
		factoryCopy := factory
		if err := reg.RegisterGlobal(desc.Name, desc, func(b *hooks.PluginBroker) error {
			if b == nil {
				return fmt.Errorf("plugin broker is nil")
			}
			if err := factoryCopy(b); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return err
		}
		reg.Broker().RegisterPluginMetadata(desc)
	}
	return nil
}

func pluginName(name string) string {
	return "incentive/" + name
}
