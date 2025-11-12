package hooks

import (
	"fmt"
	"sync"
)

// GlobalPluginFactory installs global hooks into the broker.
type GlobalPluginFactory func(broker *PluginBroker) error

// NodePluginFactory installs hooks scoped to a specific node ID.
type NodePluginFactory func(nodeID int, broker *PluginBroker) error

type registryEntry struct {
	desc    PluginDescriptor
	factory GlobalPluginFactory
}

type nodeRegistryEntry struct {
	desc    PluginDescriptor
	factory NodePluginFactory
}

// Registry keeps plugin factories that can be activated via configuration.
type Registry struct {
	mu     sync.RWMutex
	broker *PluginBroker

	global map[string]registryEntry
	node   map[string]nodeRegistryEntry
}

// NewRegistry creates an empty plugin registry bound to a broker.
func NewRegistry(broker *PluginBroker) *Registry {
	if broker == nil {
		broker = NewPluginBroker()
	}
	return &Registry{
		broker: broker,
		global: make(map[string]registryEntry),
		node:   make(map[string]nodeRegistryEntry),
	}
}

// Broker returns the underlying broker associated with the registry.
func (r *Registry) Broker() *PluginBroker {
	if r == nil {
		return nil
	}
	return r.broker
}

// RegisterGlobal registers a global plugin factory.
func (r *Registry) RegisterGlobal(name string, desc PluginDescriptor, factory GlobalPluginFactory) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	if name == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("plugin factory cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.global[name]; exists {
		return fmt.Errorf("global plugin already registered: %s", name)
	}

	r.global[name] = registryEntry{
		desc:    desc,
		factory: factory,
	}
	return nil
}

// RegisterNode registers a node-scoped plugin factory.
func (r *Registry) RegisterNode(name string, desc PluginDescriptor, factory NodePluginFactory) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	if name == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("plugin factory cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.node[name]; exists {
		return fmt.Errorf("node plugin already registered: %s", name)
	}

	r.node[name] = nodeRegistryEntry{
		desc:    desc,
		factory: factory,
	}
	return nil
}

// LoadGlobal activates the requested global plugins.
func (r *Registry) LoadGlobal(names []string) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	for _, name := range names {
		entry, err := r.getGlobal(name)
		if err != nil {
			return err
		}
		if err := entry.factory(r.broker); err != nil {
			return fmt.Errorf("global plugin %s failed: %w", name, err)
		}
		r.broker.RegisterPluginMetadata(entry.desc)
	}
	return nil
}

// LoadForNode activates the requested node-scoped plugins.
func (r *Registry) LoadForNode(nodeID int, names []string) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	for _, name := range names {
		entry, err := r.getNode(name)
		if err != nil {
			return err
		}
		if err := entry.factory(nodeID, r.broker); err != nil {
			return fmt.Errorf("node plugin %s failed: %w", name, err)
		}
		r.broker.RegisterPluginMetadata(entry.desc)
	}
	return nil
}

// Descriptor returns metadata registered under the provided name.
func (r *Registry) Descriptor(name string) (PluginDescriptor, bool) {
	if r == nil {
		return PluginDescriptor{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	if entry, ok := r.global[name]; ok {
		return entry.desc, true
	}
	if entry, ok := r.node[name]; ok {
		return entry.desc, true
	}
	return PluginDescriptor{}, false
}

func (r *Registry) getGlobal(name string) (registryEntry, error) {
	r.mu.RLock()
	entry, ok := r.global[name]
	r.mu.RUnlock()
	if !ok {
		return registryEntry{}, fmt.Errorf("global plugin not found: %s", name)
	}
	return entry, nil
}

func (r *Registry) getNode(name string) (nodeRegistryEntry, error) {
	r.mu.RLock()
	entry, ok := r.node[name]
	r.mu.RUnlock()
	if !ok {
		return nodeRegistryEntry{}, fmt.Errorf("node plugin not found: %s", name)
	}
	return entry, nil
}
