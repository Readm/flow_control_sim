package node

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Readm/flow_sim/internal/core/capability/cache"
	"github.com/Readm/flow_sim/internal/core/capability/directory"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// ProcessHook allows callers to implement custom processing logic.
type ProcessHook func(ctx context.Context, cycle uint64, buffer []packet.Packet) ([]packet.Packet, error)

// InputQueue describes the behaviors Node needs from an input buffer.
type InputQueue interface {
	Pick() []packet.Packet
	Tick(cycle int) error
	Length() int
	Capacity() int
	IsFull() bool
}

// OutputQueue describes the behaviors Node needs from an output buffer.
type OutputQueue interface {
	Tick(cycle int) error
	Length() int
	Capacity() int
	IsFull() bool
	InjectPackets(cycle int, packets []packet.Packet) error
}

// Node is a schedulable processing element that aggregates multiple InputQueues,
// OutputQueues, optional cache/directory capabilities, and a pluggable process hook.
type Node struct {
	id int

	inputs  []InputQueue
	outputs []OutputQueue

	caches      []cache.Cache
	directories []directory.Directory

	// Protocol-specific data storage (e.g., "CHI_Role", "AXI_Config")
	dataMu sync.RWMutex
	data   map[string]interface{}

	bufferMu      sync.Mutex
	processBuffer []packet.Packet
	processHook   ProcessHook
	currentCycle  uint64
	tickHookMu    sync.RWMutex
	tickHook      func(cycle uint64)
}

// New creates a Node with the provided identifier.
func New(id int) *Node {
	return &Node{
		id:            id,
		inputs:        make([]InputQueue, 0),
		outputs:       make([]OutputQueue, 0),
		caches:        make([]cache.Cache, 0),
		directories:   make([]directory.Directory, 0),
		data:          make(map[string]interface{}),
		processBuffer: make([]packet.Packet, 0),
		currentCycle:  0,
	}
}

// ID returns the immutable identifier assigned to the Node.
func (n *Node) ID() int { return n.id }

// AddInputQueue registers an InputQueue.
func (n *Node) AddInputQueue(q InputQueue) error {
	if q == nil {
		return errors.New("input queue cannot be nil")
	}
	n.inputs = append(n.inputs, q)
	return nil
}

// AddOutputQueue registers an OutputQueue.
func (n *Node) AddOutputQueue(q OutputQueue) error {
	if q == nil {
		return errors.New("output queue cannot be nil")
	}
	n.outputs = append(n.outputs, q)
	return nil
}

// InputQueues returns the registered inputs.
func (n *Node) InputQueues() []InputQueue {
	cp := make([]InputQueue, len(n.inputs))
	copy(cp, n.inputs)
	return cp
}

// OutputQueues returns the registered outputs.
func (n *Node) OutputQueues() []OutputQueue {
	cp := make([]OutputQueue, len(n.outputs))
	copy(cp, n.outputs)
	return cp
}

// AddCache attaches a cache to the Node.
func (n *Node) AddCache(c cache.Cache) {
	if c != nil {
		n.caches = append(n.caches, c)
	}
}

// Caches returns attached caches.
func (n *Node) Caches() []cache.Cache {
	cp := make([]cache.Cache, len(n.caches))
	copy(cp, n.caches)
	return cp
}

// AddDirectory attaches a directory.
func (n *Node) AddDirectory(d directory.Directory) {
	if d != nil {
		n.directories = append(n.directories, d)
	}
}

// Directories returns attached directories.
func (n *Node) Directories() []directory.Directory {
	cp := make([]directory.Directory, len(n.directories))
	copy(cp, n.directories)
	return cp
}

// SetProcessHook configures the hook invoked after packets are collected.
func (n *Node) SetProcessHook(hook ProcessHook) {
	n.bufferMu.Lock()
	defer n.bufferMu.Unlock()
	n.processHook = hook
}

// SetTickHook registers a callback invoked after each successful Tick.
func (n *Node) SetTickHook(hook func(cycle uint64)) {
	n.tickHookMu.Lock()
	defer n.tickHookMu.Unlock()
	n.tickHook = hook
}

// ProcessBuffer returns the last stored buffer.
func (n *Node) ProcessBuffer() []packet.Packet {
	n.bufferMu.Lock()
	defer n.bufferMu.Unlock()
	buf := make([]packet.Packet, len(n.processBuffer))
	copy(buf, n.processBuffer)
	return buf
}

// Tick executes a cycle of computation on the Node.
func (n *Node) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
	buffer := n.collectPackets()

	if hook := n.getProcessHook(); hook != nil {
		processed, err := hook(ctx, cycle, buffer)
		if err != nil {
			return err
		}
		if processed != nil {
			buffer = processed
		}
	}

	n.storeProcessBuffer(buffer)
	if err := n.tickQueuesConcurrently(int(cycle)); err != nil {
		return err
	}
	n.invokeTickHook(cycle)
	return nil
}

func (n *Node) collectPackets() []packet.Packet {
	collected := make([]packet.Packet, 0)
	for _, input := range n.inputs {
		if packets := input.Pick(); len(packets) > 0 {
			collected = append(collected, packets...)
		}
	}
	return collected
}

// defaultProcessHook is a no-op hook that returns the input buffer unchanged.
func defaultProcessHook(_ context.Context, _ uint64, buffer []packet.Packet) ([]packet.Packet, error) {
	return buffer, nil
}

func (n *Node) getProcessHook() ProcessHook {
	n.bufferMu.Lock()
	defer n.bufferMu.Unlock()
	if n.processHook == nil {
		return defaultProcessHook
	}
	return n.processHook
}

func (n *Node) storeProcessBuffer(buffer []packet.Packet) {
	n.bufferMu.Lock()
	defer n.bufferMu.Unlock()
	n.processBuffer = append(n.processBuffer[:0], buffer...)
}

func (n *Node) invokeTickHook(cycle uint64) {
	n.tickHookMu.RLock()
	defer n.tickHookMu.RUnlock()
	if n.tickHook != nil {
		n.tickHook(cycle)
	}
}

// tickQueuesConcurrently executes queue Tick operations.
// The implementation is controlled by build tags:
// - Default (no tags): synchronous/serial execution (defined in node_queues.go)
// - With -tags async: concurrent execution with goroutines (defined in node_queues_async.go)

// Advance executes the configured number of cycles sequentially using Background context.
func (n *Node) Advance(cycles int) error {
	if cycles <= 0 {
		return nil
	}

	debug.Logf("Node.Advance: node=%d, cycles=%d, starting from cycle=%d", n.id, cycles, n.currentCycle)

	ctx := context.Background()
	for i := 0; i < cycles; i++ {
		cycle := n.currentCycle
		debug.Logf("Node.Advance: node=%d, executing cycle=%d (%d/%d)", n.id, cycle, i+1, cycles)
		if err := n.Tick(ctx, cycle, 0); err != nil {
			debug.Logf("Node.Advance: node=%d, cycle=%d failed: %v", n.id, cycle, err)
			return err
		}
		n.currentCycle++
		debug.Logf("Node.Advance: node=%d, cycle=%d completed", n.id, cycle)
	}
	debug.Logf("Node.Advance: node=%d, all cycles completed", n.id)
	return nil
}

// SetData stores protocol-specific data.
// Key format recommendation: "{Protocol}_{Key}", e.g., "CHI_Role", "AXI_Config"
func (n *Node) SetData(key string, value interface{}) {
	n.dataMu.Lock()
	defer n.dataMu.Unlock()
	n.data[key] = value
}

// GetData retrieves protocol-specific data.
// Returns nil if key not found.
func (n *Node) GetData(key string) interface{} {
	n.dataMu.RLock()
	defer n.dataMu.RUnlock()
	return n.data[key]
}

// HasData checks if a key exists.
func (n *Node) HasData(key string) bool {
	n.dataMu.RLock()
	defer n.dataMu.RUnlock()
	_, exists := n.data[key]
	return exists
}

// DeleteData removes protocol-specific data.
func (n *Node) DeleteData(key string) {
	n.dataMu.Lock()
	defer n.dataMu.Unlock()
	delete(n.data, key)
}

// GetAllData returns a copy of all protocol-specific data.
func (n *Node) GetAllData() map[string]interface{} {
	n.dataMu.RLock()
	defer n.dataMu.RUnlock()

	copy := make(map[string]interface{}, len(n.data))
	for k, v := range n.data {
		copy[k] = v
	}
	return copy
}
