package queue

import (
	"fmt"
	"sync"
)

// BlockIndex identifies a specific blocking reason bit assigned by the registry.
type BlockIndex uint16

const (
	// InvalidBlockIndex indicates registration failure.
	InvalidBlockIndex BlockIndex = ^BlockIndex(0)
)

// BlockDescriptor describes a registered blocking reason.
type BlockDescriptor struct {
	Name string
}

// BlockRegistry allocates unique BlockIndex values for hooks/capabilities.
type BlockRegistry struct {
	mu          sync.Mutex
	limit       uint16
	next        BlockIndex
	nameToIndex map[string]BlockIndex
	descriptors []BlockDescriptor
}

// NewBlockRegistry creates a registry with an optional bit limit.
// When limit is 0 it is treated as unlimited.
func NewBlockRegistry(limit uint16) *BlockRegistry {
	return &BlockRegistry{
		limit:       limit,
		nameToIndex: make(map[string]BlockIndex),
	}
}

// Register assigns a new bit index for the given name. Registration is idempotent.
func (r *BlockRegistry) Register(name string) (BlockIndex, error) {
	if name == "" {
		return InvalidBlockIndex, fmt.Errorf("block reason name must not be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if idx, ok := r.nameToIndex[name]; ok {
		return idx, nil
	}

	if r.limit > 0 && r.next >= BlockIndex(r.limit) {
		return InvalidBlockIndex, fmt.Errorf("block registry limit reached (%d)", r.limit)
	}

	idx := r.next
	r.next++

	r.nameToIndex[name] = idx
	r.descriptors = append(r.descriptors, BlockDescriptor{
		Name: name,
	})

	return idx, nil
}

// Descriptor returns the descriptor for the given index.
func (r *BlockRegistry) Descriptor(index BlockIndex) (BlockDescriptor, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := int(index)
	if i < 0 || i >= len(r.descriptors) {
		return BlockDescriptor{}, false
	}
	return r.descriptors[i], true
}

// Count returns the number of registered descriptors.
func (r *BlockRegistry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.descriptors)
}

// Limit returns the maximum number of indices supported (0 for unlimited).
func (r *BlockRegistry) Limit() uint16 {
	return r.limit
}


