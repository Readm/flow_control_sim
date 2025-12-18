package node

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Readm/flow_sim/internal/core/capability/cache"
	"github.com/Readm/flow_sim/internal/core/capability/directory"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

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

// TickHook allows callers to observe cycle execution.
type TickHook func(cycle uint64)

// NodeHandler defines the interface that specific node implementations must satisfy.
type NodeHandler interface {
	// Process handles data for the current cycle.
	// It receives packets from all input queues, indexed by queue ID.
	// Returns an error if processing fails.
	// Implementations should use BaseNode.GetOutputQueue(i).InjectPackets() to send data.
	//
	// Parameters:
	//   ctx: context for cancellation
	//   cycle: current simulation cycle
	//   inputs: packets received in this cycle from each input queue (inputs[i] comes from InputQueue i)
	Process(ctx context.Context, cycle uint64, inputs [][]packet.Packet) error
}

// BaseNode implements the common logic for all nodes.
// Specific node types should embed BaseNode and implement NodeHandler.
type BaseNode struct {
	id int

	inputs  []InputQueue
	outputs []OutputQueue

	caches      []cache.Cache
	directories []directory.Directory

	// Protocol-specific data storage
	dataMu sync.RWMutex
	data   map[string]interface{}

	// Handler reference (for polymorphic behavior)
	handler NodeHandler

	currentCycle uint64
	tickHookMu   sync.RWMutex
	tickHook     TickHook
}

// NewBaseNode creates a new BaseNode.
func NewBaseNode(id int, handler NodeHandler) *BaseNode {
	return &BaseNode{
		id:           id,
		inputs:       make([]InputQueue, 0),
		outputs:      make([]OutputQueue, 0),
		caches:       make([]cache.Cache, 0),
		directories:  make([]directory.Directory, 0),
		data:         make(map[string]interface{}),
		handler:      handler,
		currentCycle: 0,
	}
}

// ID returns the node identifier.
func (n *BaseNode) ID() int { return n.id }

// AddInputQueue registers an InputQueue.
func (n *BaseNode) AddInputQueue(q InputQueue) error {
	if q == nil {
		return errors.New("input queue cannot be nil")
	}
	n.inputs = append(n.inputs, q)
	return nil
}

// AddOutputQueue registers an OutputQueue.
func (n *BaseNode) AddOutputQueue(q OutputQueue) error {
	if q == nil {
		return errors.New("output queue cannot be nil")
	}
	n.outputs = append(n.outputs, q)
	return nil
}

// InputQueues returns the registered inputs.
func (n *BaseNode) InputQueues() []InputQueue {
	cp := make([]InputQueue, len(n.inputs))
	copy(cp, n.inputs)
	return cp
}

// OutputQueues returns the registered outputs.
func (n *BaseNode) OutputQueues() []OutputQueue {
	cp := make([]OutputQueue, len(n.outputs))
	copy(cp, n.outputs)
	return cp
}

// GetOutputQueue safely retrieves an output queue by index.
func (n *BaseNode) GetOutputQueue(index int) OutputQueue {
	if index < 0 || index >= len(n.outputs) {
		return nil
	}
	return n.outputs[index]
}

// AddCache attaches a cache to the Node.
func (n *BaseNode) AddCache(c cache.Cache) {
	if c != nil {
		n.caches = append(n.caches, c)
	}
}

// Caches returns attached caches.
func (n *BaseNode) Caches() []cache.Cache {
	cp := make([]cache.Cache, len(n.caches))
	copy(cp, n.caches)
	return cp
}

// AddDirectory attaches a directory.
func (n *BaseNode) AddDirectory(d directory.Directory) {
	if d != nil {
		n.directories = append(n.directories, d)
	}
}

// Directories returns attached directories.
func (n *BaseNode) Directories() []directory.Directory {
	cp := make([]directory.Directory, len(n.directories))
	copy(cp, n.directories)
	return cp
}

// SetTickHook registers a callback invoked after each successful Tick.
func (n *BaseNode) SetTickHook(hook TickHook) {
	n.tickHookMu.Lock()
	defer n.tickHookMu.Unlock()
	n.tickHook = hook
}

// Tick executes one cycle of the node's logic.
// Order: Receive (Input) -> Process (Handler) -> Send (Output)
func (n *BaseNode) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
	n.invokeTickHook(cycle)

	// 1. Phase 1: Receive (Input)
	// Input queues wait for upstream and receive data for CURRENT cycle.
	receivedPackets, err := n.tickInputQueues(cycle)
	if err != nil {
		return fmt.Errorf("node %d input tick failed: %w", n.id, err)
	}

	// 2. Phase 2: Process (Handler)
	// Handler logic processes the data we just received.
	if err := n.handler.Process(ctx, cycle, receivedPackets); err != nil {
		return fmt.Errorf("node %d process failed: %w", n.id, err)
	}

	// 3. Phase 3: Send (Output)
	// Output queues send the data just injected to downstream.
	if err := n.tickOutputQueues(cycle); err != nil {
		return fmt.Errorf("node %d output tick failed: %w", n.id, err)
	}

	return nil
}

// tickInputQueues ticks all input queues and collects received packets.
func (n *BaseNode) tickInputQueues(cycle uint64) ([][]packet.Packet, error) {
	allReceived := make([][]packet.Packet, len(n.inputs))

	for i, input := range n.inputs {
		if err := input.Tick(int(cycle)); err != nil {
			return nil, err
		}
		// Collection strategy corresponding to "Tick then Pick"
		pkts := input.Pick()
		allReceived[i] = pkts
	}

	return allReceived, nil
}

// tickOutputQueues ticks all output queues.
func (n *BaseNode) tickOutputQueues(cycle uint64) error {
	for _, output := range n.outputs {
		if err := output.Tick(int(cycle)); err != nil {
			return err
		}
	}
	return nil
}

func (n *BaseNode) invokeTickHook(cycle uint64) {
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
func (n *BaseNode) Advance(cycles int) error {
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
func (n *BaseNode) SetData(key string, value interface{}) {
	n.dataMu.Lock()
	defer n.dataMu.Unlock()
	n.data[key] = value
}

// GetData retrieves protocol-specific data.
// Returns nil if key not found.
func (n *BaseNode) GetData(key string) interface{} {
	n.dataMu.RLock()
	defer n.dataMu.RUnlock()
	return n.data[key]
}

// HasData checks if a key exists.
func (n *BaseNode) HasData(key string) bool {
	n.dataMu.RLock()
	defer n.dataMu.RUnlock()
	_, exists := n.data[key]
	return exists
}

// DeleteData removes protocol-specific data.
func (n *BaseNode) DeleteData(key string) {
	n.dataMu.Lock()
	defer n.dataMu.Unlock()
	delete(n.data, key)
}

// GetAllData returns a copy of all protocol-specific data.
func (n *BaseNode) GetAllData() map[string]interface{} {
	n.dataMu.RLock()
	defer n.dataMu.RUnlock()

	copy := make(map[string]interface{}, len(n.data))
	for k, v := range n.data {
		copy[k] = v
	}
	return copy
}
