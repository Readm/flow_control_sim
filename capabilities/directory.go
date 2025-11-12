package capabilities

import (
	"sync"

	"flow_sim/hooks"
)

// DirectoryCapability provides address-to-node tracking for coherence.
type DirectoryCapability interface {
	NodeCapability
	Directory() DirectoryStore
}

// DirectoryStore exposes operations for managing sharer sets.
type DirectoryStore interface {
	Add(address uint64, nodeID int)
	Remove(address uint64, nodeID int)
	Sharers(address uint64) []int
	Clear(address uint64)
	Reset()
}

type directoryCapability struct {
	name        string
	description string
	store       *directoryStore
}

// NewDirectoryCapability creates a capability maintaining sharer lists per address.
func NewDirectoryCapability(name string) DirectoryCapability {
	return &directoryCapability{
		name:        name,
		description: "directory capability",
		store:       newDirectoryStore(),
	}
}

func (c *directoryCapability) Descriptor() hooks.PluginDescriptor {
	return hooks.PluginDescriptor{
		Name:        c.name,
		Category:    hooks.PluginCategoryCapability,
		Description: c.description,
	}
}

func (c *directoryCapability) Register(broker *hooks.PluginBroker) error {
	if broker == nil {
		return nil
	}
	broker.RegisterPluginMetadata(c.Descriptor())
	return nil
}

func (c *directoryCapability) Directory() DirectoryStore {
	return c.store
}

type directoryStore struct {
	mu      sync.RWMutex
	entries map[uint64]map[int]struct{}
}

func newDirectoryStore() *directoryStore {
	return &directoryStore{
		entries: make(map[uint64]map[int]struct{}),
	}
}

func (s *directoryStore) Add(address uint64, nodeID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[address]; !ok {
		s.entries[address] = make(map[int]struct{})
	}
	s.entries[address][nodeID] = struct{}{}
}

func (s *directoryStore) Remove(address uint64, nodeID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sharers, ok := s.entries[address]; ok {
		delete(sharers, nodeID)
		if len(sharers) == 0 {
			delete(s.entries, address)
		}
	}
}

func (s *directoryStore) Sharers(address uint64) []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sharers, ok := s.entries[address]
	if !ok || len(sharers) == 0 {
		return nil
	}
	out := make([]int, 0, len(sharers))
	for id := range sharers {
		out = append(out, id)
	}
	return out
}

func (s *directoryStore) Clear(address uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, address)
}

func (s *directoryStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.entries)
}
