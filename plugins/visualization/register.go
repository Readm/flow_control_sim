package visualization

import (
	"fmt"

	"github.com/Readm/flow_sim/hooks"
	"github.com/Readm/flow_sim/visual"
)

// Factory creates a visualizer instance.
type Factory func() (visual.Visualizer, error)

// Options configure visualization plugin registration.
type Options struct {
	Factories     map[string]Factory
	SetVisualizer func(visual.Visualizer)
}

// Register registers visualization plugins for each provided factory.
func Register(reg *hooks.Registry, opts Options) error {
	if reg == nil {
		return fmt.Errorf("registry is nil")
	}
	if opts.SetVisualizer == nil {
		return fmt.Errorf("SetVisualizer callback is required")
	}
	for mode, factory := range opts.Factories {
		if factory == nil {
			continue
		}
		name := pluginName(mode)
		desc := hooks.PluginDescriptor{
			Name:        name,
			Category:    hooks.PluginCategoryVisualization,
			Description: fmt.Sprintf("%s visualization plugin", mode),
		}
		factoryCopy := factory
		if err := reg.RegisterGlobal(name, desc, func(*hooks.PluginBroker) error {
			visualizer, err := factoryCopy()
			if err != nil {
				return err
			}
			opts.SetVisualizer(visualizer)
			return nil
		}); err != nil {
			return err
		}
		reg.Broker().RegisterPluginMetadata(desc)
	}
	return nil
}

func pluginName(mode string) string {
	return "visualization/" + mode
}
