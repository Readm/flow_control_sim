package node

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/components/directory"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Node defines the public interface for all simulation nodes.
// Node defines the public interface for all simulation nodes.
type Node interface {
	ID() int
	Tick(cycle uint64, duration time.Duration) error
	AddInputQueue(q InputQueue) error
	AddOutputQueue(q OutputQueue) error
	InputQueues() []InputQueue
	OutputQueues() []OutputQueue

	AddCache(c cache.Cache)
	Caches() []cache.Cache
	AddDirectory(d directory.Directory)
	Directories() []directory.Directory

	InjectPacket(pkt packet.Packet) error
	AdvanceTo(targetCycle int) error
	CurrentCycle() int

	SetData(key string, value interface{})
	GetData(key string) interface{}
	HasData(key string) bool
	DeleteData(key string)
	UpdateData(modifier func(map[string]interface{}))
	UpdateKeyData(key string, modifier func(interface{}) interface{})
}

// Tickable is an interface for components that can be ticked.
type Tickable interface {
	Tick(cycle uint64, duration time.Duration) error
}

// CreatePacket is a helper to create a packet (compatibility alias).
func CreatePacket(src, dst int, payload string) packet.Packet {
	return packet.Packet{
		SourceID: src,
		TargetID: dst,
		Payload:  payload,
	}
}

// InputQueue describes the behaviors Node needs from an input buffer.
type InputQueue interface {
	Pick() []packet.Packet
	PeekPickTo(out []queue.PacketRef) int
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
	OutBandwidth() int
}

// NodeHandler defines the interface that specific node implementations must satisfy.
type NodeHandler interface {
	// Process handles data for the current cycle.
	// It receives packets from all input queues, indexed by queue ID.
	// Returns an error if processing fails.
	// Implementations should use BaseNode.GetOutputQueue(i).InjectPackets() to send data.
	//
	// Parameters:
	//   cycle: current simulation cycle
	//   inputs: packets received in this cycle from each input queue (inputs[i] comes from InputQueue i)
	Process(cycle uint64, inputs [][]queue.PacketRef) error
}

// BaseNode implements the common logic for all nodes.
// Specific node types should embed BaseNode and implement NodeHandler.
type BaseNode struct {
	id int

	inputs  []InputQueue
	outputs []OutputQueue

	// Zero-allocation input buffers
	// inputBuffer is the slice of slices passed to Process
	inputBuffer [][]queue.PacketRef
	// inputValues is the backing array for all packet refs
	inputValues [][]queue.PacketRef

	caches      []cache.Cache
	directories []directory.Directory

	// Protocol-specific data storage
	dataMu sync.RWMutex
	data   map[string]interface{}

	// Handler reference (for polymorphic behavior)
	handler NodeHandler

	currentCycle     uint64
	advanceTarget    uint64   // Target cycle for current AdvanceTo
	outputQueueAhead []uint64 // Next cycle to tick for each output queue
}

// NewBaseNode creates a new BaseNode.
func NewBaseNode(id int, handler NodeHandler) *BaseNode {
	return &BaseNode{
		id:               id,
		inputs:           make([]InputQueue, 0),
		outputs:          make([]OutputQueue, 0),
		inputBuffer:      make([][]queue.PacketRef, 0),
		inputValues:      make([][]queue.PacketRef, 0),
		caches:           make([]cache.Cache, 0),
		directories:      make([]directory.Directory, 0),
		data:             make(map[string]interface{}),
		handler:          handler,
		currentCycle:     0,
		outputQueueAhead: make([]uint64, 0),
	}
}

// CurrentCycle returns the current cycle of the node.
func (n *BaseNode) CurrentCycle() int {
	return int(n.currentCycle)
}

// ID returns the node identifier.
func (n *BaseNode) ID() int { return n.id }

// AddInputQueue registers an InputQueue.
func (n *BaseNode) AddInputQueue(q InputQueue) error {
	if q == nil {
		return errors.New("input queue cannot be nil")
	}
	n.inputs = append(n.inputs, q)

	// Expand zero-allocation buffers
	// Create a new slice for this queue with capacity equal to queue capacity
	// This ensures we have enough space for PeekPickTo
	newBuffer := make([]queue.PacketRef, q.Capacity())
	n.inputValues = append(n.inputValues, newBuffer)

	// Expand the container slice (it will be populated in Tick)
	n.inputBuffer = append(n.inputBuffer, nil)

	return nil
}

// UpdateData atomically updates the entire protocol-specific data map.
func (n *BaseNode) UpdateData(modifier func(map[string]interface{})) {
	n.dataMu.Lock()
	defer n.dataMu.Unlock()
	modifier(n.data)
}

// UpdateKeyData atomically updates a specific key in protocol-specific data.
func (n *BaseNode) UpdateKeyData(key string, modifier func(interface{}) interface{}) {
	n.dataMu.Lock()
	defer n.dataMu.Unlock()

	val := n.data[key]
	newVal := modifier(val)
	if newVal == nil {
		delete(n.data, key)
	} else {
		n.data[key] = newVal
	}
}

// AddOutputQueue registers an OutputQueue.
func (n *BaseNode) AddOutputQueue(q OutputQueue) error {
	if q == nil {
		return errors.New("output queue cannot be nil")
	}
	n.outputs = append(n.outputs, q)
	n.outputQueueAhead = append(n.outputQueueAhead, 0) // Initialize with 0 (will be corrected to currentCycle on use)
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

// InjectPacket is a helper to inject a packet into the first output queue.
func (n *BaseNode) InjectPacket(pkt packet.Packet) error {
	if len(n.outputs) == 0 {
		return errors.New("node has no output queues")
	}
	// Use current cycle for injection
	return n.outputs[0].InjectPackets(int(n.currentCycle), []packet.Packet{pkt})
}

// Tick executes one cycle of the node's logic.
// Order: Receive (Input) -> Process (Handler) -> Send (Output)
func (n *BaseNode) Tick(cycle uint64, _ time.Duration) error {

	// 1. Phase 1: Receive (Input)
	// Input queues wait for upstream and receive data for CURRENT cycle.
	// Returns the number of packets processed, or error
	if err := n.tickInputQueues(cycle); err != nil {
		return fmt.Errorf("node %d input tick failed: %w", n.id, err)
	}

	// 2. Phase 2: Process (Handler)
	// Handler logic processes the data we just received.
	// Packets are available in n.inputBuffer (which points to n.inputValues)
	if err := n.handler.Process(cycle, n.inputBuffer); err != nil {
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
// It populates n.inputBuffer with slices pointing to n.inputValues.
func (n *BaseNode) tickInputQueues(cycle uint64) error {
	for i, input := range n.inputs {
		if err := input.Tick(int(cycle)); err != nil {
			return err
		}

		// Zero-Alloc Receive Strategy:
		// 1. Get the pre-allocated backing array for this input
		buf := n.inputValues[i] // This has len=cap=Capacity

		// 2. Peek packets directly into this buffer
		count := input.PeekPickTo(buf)

		// 3. Update the slice header in inputBuffer to point to valid data
		// This does NOT allocate new memory, just updates the slice length
		n.inputBuffer[i] = buf[:count]
	}

	return nil
}

// tickOutputQueues ticks all output queues.
func (n *BaseNode) tickOutputQueues(cycle uint64) error {
	for i, output := range n.outputs {
		// Optimization: Tick ahead if possible
		// We can tick ahead if:
		// 1. We are within the AdvanceTo window (cycle <= advanceTarget)
		// 2. We have packets to send AND sufficient volume to justify it (Length >= Bandwidth)
		// OR
		// 3. It is the CURRENT cycle (we must always tick the current cycle to ensure progress)

		// Determine the start cycle for this queue.
		// It should be at least the current global cycle, but might be further ahead if we already ticked it.
		startCycle := cycle
		if i < len(n.outputQueueAhead) && n.outputQueueAhead[i] > startCycle {
			startCycle = n.outputQueueAhead[i]
		}

		// Iterate from startCycle up to advanceTarget
		// We use a loop to potentially tick multiple cycles in one go.
		curr := startCycle
		for curr <= n.advanceTarget {
			isFuture := curr > cycle
			// Policy:
			// - If curr == cycle (Current Real Cycle): MUST Tick.
			// - If curr > cycle (Future):
			//   - ONLY Tick if Queue.Length >= OutBandwidth.
			//   - Rationale: If Length < BW, we might produce fragments. Better to wait for new packets
			//     that might arrive in future Process() calls to fill the bandwidth.
			if isFuture {
				if output.Length() < output.OutBandwidth() {
					break // Stop optimization
				}
			}

			if err := output.Tick(int(curr)); err != nil {
				return err
			}

			curr++
			// Update the ahead tracker
			if i < len(n.outputQueueAhead) {
				n.outputQueueAhead[i] = curr
			}
		}
	}
	return nil
}

// AdvanceTo executes the configured cycles sequentially using Background context until the node reaches the target cycle.
func (n *BaseNode) AdvanceTo(targetCycle int) error {
	if targetCycle < int(n.currentCycle) {
		return nil
	}

	debug.Logf("Node.AdvanceTo: node=%d, target=%d, starting from cycle=%d", n.id, targetCycle, n.currentCycle)

	debug.Logf("Node.AdvanceTo: node=%d, target=%d, starting from cycle=%d", n.id, targetCycle, n.currentCycle)

	// Execute logic for each cycle from current up to target (inclusive? check plan)
	// Plan said: "Link.AdvanceTo: loop from current to target (inclusive)"
	// Implementation in Link was: for cycle := l.currentCycle; cycle <= targetCycle; cycle++
	// So Node should do the same.

	n.advanceTarget = uint64(targetCycle)

	for cycle := n.currentCycle; int(cycle) <= targetCycle; cycle++ {
		debug.Logf("Node.AdvanceTo: node=%d, executing cycle=%d", n.id, cycle)
		if err := n.Tick(cycle, 0); err != nil {
			debug.Logf("Node.AdvanceTo: node=%d, cycle=%d failed: %v", n.id, cycle, err)
			return err
		}
		// Update currentCycle locally or via loop?
		// Node structure has currentCycle.
		n.currentCycle = cycle + 1

		debug.Logf("Node.AdvanceTo: node=%d, cycle=%d completed", n.id, cycle)
	}
	debug.Logf("Node.AdvanceTo: node=%d, reached cycle=%d (next=%d)", n.id, targetCycle, n.currentCycle)
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
