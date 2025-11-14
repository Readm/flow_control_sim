package capabilities

import (
	"container/list"
	"sync"

	"github.com/Readm/flow_sim/hooks"
)

// CacheEvictor exposes LRU helpers for nodes to manage cache capacity.
type CacheEvictor interface {
	NodeCapability
	Touch(addr uint64)
	Fill(addr uint64)
	Invalidate(addr uint64)
	Capacity() int
	Entries() int
}

// LRUEvictionConfig configures cache eviction capability behaviour.
type LRUEvictionConfig struct {
	Capacity     int
	RequestCache RequestCache
	HomeCache    HomeCache
}

type lruEntry struct {
	addr uint64
}

type lruEvictionCapability struct {
	desc         hooks.PluginDescriptor
	capacity     int
	requestCache RequestCache
	homeCache    HomeCache

	mu      sync.Mutex
	entries map[uint64]*list.Element
	order   *list.List
}

// NewLRUEvictionCapability creates an eviction capability for request/home caches.
func NewLRUEvictionCapability(name string, cfg LRUEvictionConfig) CacheEvictor {
	capacity := cfg.Capacity
	if capacity < 0 {
		capacity = 0
	}
	c := &lruEvictionCapability{
		desc: hooks.PluginDescriptor{
			Name:        name,
			Category:    hooks.PluginCategoryCapability,
			Description: "cache eviction capability (LRU)",
		},
		capacity:     capacity,
		requestCache: cfg.RequestCache,
		homeCache:    cfg.HomeCache,
		entries:      make(map[uint64]*list.Element),
		order:        list.New(),
	}
	return c
}

func (c *lruEvictionCapability) Descriptor() hooks.PluginDescriptor {
	return c.desc
}

func (c *lruEvictionCapability) Register(b *hooks.PluginBroker) error {
	if b == nil {
		return nil
	}
	b.RegisterPluginMetadata(c.desc)
	return nil
}

func (c *lruEvictionCapability) Capacity() int {
	return c.capacity
}

func (c *lruEvictionCapability) Entries() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

func (c *lruEvictionCapability) Touch(addr uint64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.entries[addr]; ok {
		c.order.MoveToFront(elem)
	}
}

func (c *lruEvictionCapability) Fill(addr uint64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.entries[addr]; ok {
		c.order.MoveToFront(elem)
		return
	}
	elem := c.order.PushFront(&lruEntry{addr: addr})
	c.entries[addr] = elem
	if c.capacity > 0 && c.order.Len() > c.capacity {
		c.evictOldestLocked()
	}
}

func (c *lruEvictionCapability) Invalidate(addr uint64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removeLocked(addr)
	c.invalidateBacking(addr)
}

func (c *lruEvictionCapability) evictOldestLocked() {
	elem := c.order.Back()
	if elem == nil {
		return
	}
	entry := elem.Value.(*lruEntry)
	addr := entry.addr
	c.order.Remove(elem)
	delete(c.entries, addr)
	c.invalidateBacking(addr)
}

func (c *lruEvictionCapability) removeLocked(addr uint64) {
	if elem, ok := c.entries[addr]; ok {
		c.order.Remove(elem)
		delete(c.entries, addr)
	}
}

func (c *lruEvictionCapability) invalidateBacking(addr uint64) {
	if c.requestCache != nil {
		c.requestCache.Invalidate(addr)
	}
	if c.homeCache != nil {
		c.homeCache.Invalidate(addr)
	}
}
