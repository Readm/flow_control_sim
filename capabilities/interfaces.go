package capabilities

import "github.com/Readm/flow_sim/hooks"

// NodeCapability represents a self-contained behaviour that can attach hooks to the broker.
type NodeCapability interface {
	Descriptor() hooks.PluginDescriptor
	Register(broker *hooks.PluginBroker) error
}
