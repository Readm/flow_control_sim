package node

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/components/directory"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/monitor"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/core/trace"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
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

	// Name returns the human-readable name of the node.
	Name() string
	// SetName sets the human-readable name of the node.
	SetName(name string)

	// Protocol 配置访问 (Phase 1)
	SetConfigRef(config *protocol.Node)
	GetConfigRef() *protocol.Node

	// 配置信息访问
	SetFeature(feature string, config map[string]interface{})
	GetFeature(feature string) (map[string]interface{}, bool)
	SetCoherenceDomainID(id int)
	GetCoherenceDomainID() *int

	// 显示信息访问
	SetDisplayData(key string, value interface{})
	GetDisplayData(key string) (interface{}, bool)
	GetAllDisplayData() map[string]interface{}
	SetAllDisplayData(data map[string]interface{})
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

// StatsExporter is an optional interface that NodeHandlers can implement
// to export runtime statistics and configuration to the visualization layer.
// This enables automatic integration with OpenAPI Schema (CPUConfig, MemoryConfig, etc).
type StatsExporter interface {
	// ExportStats returns runtime statistics that should be included in NodeState.Stats.
	// For CPU nodes, this includes IPC, instruction count, ROB occupancy, etc.
	// For DRAM nodes, this includes request counts, latency, row buffer hits, etc.
	// Keys should match the field names in OpenAPI Schema (snake_case).
	ExportStats() map[string]interface{}
}

// BaseNode implements the common logic for all nodes.
// Specific node types should embed BaseNode and implement NodeHandler.
type BaseNode struct {
	id   int
	name string

	inputs  []InputQueue
	outputs []OutputQueue

	// Port naming (optional feature for better readability)
	inputPortNames  map[string]int // port name -> index
	outputPortNames map[string]int // port name -> index

	// Zero-allocation input buffers
	// inputBuffer is the slice of slices passed to Process
	inputBuffer [][]queue.PacketRef
	// inputValues is the backing array for all packet refs
	inputValues [][]queue.PacketRef

	caches      []cache.Cache
	directories []directory.Directory

	// Protocol 配置引用 (只读,直接引用 protocol.Node)
	configRef *protocol.Node

	// Protocol-specific data storage
	dataMu sync.RWMutex
	data   map[string]interface{}

	// Handler reference (for polymorphic behavior)
	handler NodeHandler

	currentCycle     uint64
	advanceTarget    uint64   // Target cycle for current AdvanceTo
	outputQueueAhead []uint64 // Next cycle to tick for each output queue

	// Monitor for Profiling and Tracing
	monitor *monitor.NodeMonitor
}

// NewBaseNode creates a new BaseNode.
func NewBaseNode(id int, handler NodeHandler) *BaseNode {
	return &BaseNode{
		id:               id,
		name:             fmt.Sprintf("Node%d", id), // Default name
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
		monitor:          monitor.NewNodeMonitor(id),
	}
}

// CurrentCycle returns the current cycle of the node.
func (n *BaseNode) CurrentCycle() int {
	return int(n.currentCycle)
}

// ID returns the node identifier.
func (n *BaseNode) ID() int { return n.id }

// Name returns the node name.
func (n *BaseNode) Name() string { return n.name }

// SetName sets the node name.
func (n *BaseNode) SetName(name string) { n.name = name }

// ===== Profiling Getters (Delegated to Monitor) =====

func (n *BaseNode) TotalProcessCycles() uint64 {
	return n.monitor.TotalProcessCycles()
}

func (n *BaseNode) ProcessCount() uint64 {
	return n.monitor.ProcessCount()
}

func (n *BaseNode) AvgProcessCycles() uint64 {
	return n.monitor.AvgProcessCycles()
}

func (n *BaseNode) ReceiveCycles() uint64 {
	return n.monitor.ReceiveCycles()
}

func (n *BaseNode) ProcessCycles() uint64 {
	return n.monitor.ProcessCycles()
}

func (n *BaseNode) SendCycles() uint64 {
	return n.monitor.SendCycles()
}

func (n *BaseNode) AvgReceiveCycles() uint64 {
	return n.monitor.AvgReceiveCycles()
}

func (n *BaseNode) AvgProcessCyclesDetailed() uint64 {
	return n.monitor.AvgProcessCyclesDetailed()
}

func (n *BaseNode) AvgSendCycles() uint64 {
	return n.monitor.AvgSendCycles()
}

// GetProcessProfile 获取 Process 执行的 profiling 数据
func (n *BaseNode) GetProcessProfile() (totalTime, count uint64) {
	return n.monitor.GetProcessProfile()
}

// GetAvgProcessExecTime 获取平均 Process 执行时间
func (n *BaseNode) GetAvgProcessExecTime() uint64 {
	return n.monitor.GetAvgProcessExecTime()
}

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

// ===== Port Naming Methods =====

// NameInputPort assigns a name to an input port at the given index.
// Returns an error if the index is out of range or if the name is already in use.
func (n *BaseNode) NameInputPort(index int, name string) error {
	if index < 0 || index >= len(n.inputs) {
		return fmt.Errorf("input port index %d out of range [0, %d)", index, len(n.inputs))
	}
	if name == "" {
		return fmt.Errorf("port name cannot be empty")
	}

	// Initialize map if needed
	if n.inputPortNames == nil {
		n.inputPortNames = make(map[string]int)
	}

	// Check for duplicate name (禁止重复)
	if existingIdx, exists := n.inputPortNames[name]; exists {
		if existingIdx != index {
			return fmt.Errorf("input port name %q already assigned to index %d", name, existingIdx)
		}
		// Same index, allow re-naming (idempotent)
		return nil
	}

	n.inputPortNames[name] = index
	return nil
}

// NameOutputPort assigns a name to an output port at the given index.
// Returns an error if the index is out of range or if the name is already in use.
func (n *BaseNode) NameOutputPort(index int, name string) error {
	if index < 0 || index >= len(n.outputs) {
		return fmt.Errorf("output port index %d out of range [0, %d)", index, len(n.outputs))
	}
	if name == "" {
		return fmt.Errorf("port name cannot be empty")
	}

	// Initialize map if needed
	if n.outputPortNames == nil {
		n.outputPortNames = make(map[string]int)
	}

	// Check for duplicate name (禁止重复)
	if existingIdx, exists := n.outputPortNames[name]; exists {
		if existingIdx != index {
			return fmt.Errorf("output port name %q already assigned to index %d", name, existingIdx)
		}
		// Same index, allow re-naming (idempotent)
		return nil
	}

	n.outputPortNames[name] = index
	return nil
}

// GetInputPortIndex returns the index of a named input port.
// Returns (index, true) if found, (0, false) if not found.
func (n *BaseNode) GetInputPortIndex(name string) (int, bool) {
	if n.inputPortNames == nil {
		return 0, false
	}
	idx, ok := n.inputPortNames[name]
	return idx, ok
}

// GetOutputPortIndex returns the index of a named output port.
// Returns (index, true) if found, (0, false) if not found.
func (n *BaseNode) GetOutputPortIndex(name string) (int, bool) {
	if n.outputPortNames == nil {
		return 0, false
	}
	idx, ok := n.outputPortNames[name]
	return idx, ok
}

// NameInputPorts assigns names to input ports in order.
// Empty strings are skipped. Returns an error if too many names are provided.
func (n *BaseNode) NameInputPorts(names ...string) error {
	if len(names) > len(n.inputs) {
		return fmt.Errorf("too many names: got %d, have %d input ports", len(names), len(n.inputs))
	}
	for i, name := range names {
		if name != "" {
			if err := n.NameInputPort(i, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// NameOutputPorts assigns names to output ports in order.
// Empty strings are skipped. Returns an error if too many names are provided.
func (n *BaseNode) NameOutputPorts(names ...string) error {
	if len(names) > len(n.outputs) {
		return fmt.Errorf("too many names: got %d, have %d output ports", len(names), len(n.outputs))
	}
	for i, name := range names {
		if name != "" {
			if err := n.NameOutputPort(i, name); err != nil {
				return err
			}
		}
	}
	return nil
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
	// ===== Phase 1: Receive (Input) =====
	recvToken := n.monitor.OnReceiveStart()

	if err := n.tickInputQueues(cycle); err != nil {
		return fmt.Errorf("node %d input tick failed: %w", n.id, err)
	}

	packetCount := 0
	for _, input := range n.inputBuffer {
		packetCount += len(input)
	}
	n.monitor.OnReceiveEnd(recvToken, cycle, packetCount)

	// ===== Phase 2: Process (Handler) =====
	procToken := n.monitor.OnProcessStart()

	if err := n.handler.Process(cycle, n.inputBuffer); err != nil {
		return fmt.Errorf("node %d process failed: %w", n.id, err)
	}

	n.monitor.OnProcessEnd(procToken, cycle)

	// ===== Phase 3: Send (Output) =====
	sendToken := n.monitor.OnSendStart()

	if err := n.tickOutputQueues(cycle); err != nil {
		return fmt.Errorf("node %d output tick failed: %w", n.id, err)
	}

	sentCount := 0
	for _, output := range n.outputs {
		sentCount += output.Length()
	}
	n.monitor.OnSendEnd(sendToken, cycle, sentCount)

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

// ===== Tracer Methods (Delegated to Monitor) =====

// SetTracer 设置 trace recorder（用于 Chrome trace）
func (n *BaseNode) SetTracer(tracer *trace.TraceRecorder) {
	n.monitor.SetTracer(tracer)
	// Register ourselves as source if tracer is set
	if tracer != nil && tracer.IsNodeTraced(n.id) {
		tracer.RegisterSource(n)
	}
}

// GetTracer 获取 trace recorder
func (n *BaseNode) GetTracer() *trace.TraceRecorder {
	// n.monitor.tracer is private, this logic was only present in BaseNode before.
	// Since GetTracer is mostly internal or debug, we might remove it or expose via Monitor?
	// The original Node interface didn't have GetTracer, only BaseNode struct had.
	// We can't access m.tracer directly.
	// For now, let's omit or if needed add GetTracer to NodeMonitor.
	// But actually, we don't really need to expose it if everything is delegated.
	return nil
}

// GetTraceEvents implements trace.TraceSource.
func (n *BaseNode) GetTraceEvents() []trace.TraceEvent {
	return n.monitor.GetTraceEvents()
}

// ========== Protocol Config 访问方法 (Phase 1) ==========

// SetConfigRef 设置 Protocol 配置引用 (只读)
func (n *BaseNode) SetConfigRef(config *protocol.Node) {
	n.configRef = config
}

// SetHandler 设置节点 Handler (用于多态行为和统计导出)
// Phase 6: Builder 需要设置 handler 引用，以便 ExportState 可以调用 ExportStats
func (n *BaseNode) SetHandler(handler NodeHandler) {
	n.handler = handler
}

// GetConfigRef 获取 Protocol 配置引用 (只读)
func (n *BaseNode) GetConfigRef() *protocol.Node {
	return n.configRef
}

// ========== Features 和 DisplayData 访问方法 ==========

// SetFeature 设置节点feature配置 (Phase 2: 已废弃, 空实现)
// Deprecated: Config 数据现在通过 configRef 管理, 此方法仅保持接口兼容
func (n *BaseNode) SetFeature(feature string, config map[string]interface{}) {
	// Phase 2: 空实现, 保持接口兼容
}

// GetFeature 获取节点feature配置 (Phase 2: 从 configRef 读取)
func (n *BaseNode) GetFeature(feature string) (map[string]interface{}, bool) {
	// Phase 2: 从 configRef 读取
	if n.configRef != nil {
		switch feature {
		case "cache":
			if n.configRef.Cache != nil {
				return map[string]interface{}{
					"capacity":           n.configRef.Cache.Capacity,
					"num_sets":           n.configRef.Cache.NumSets,
					"replacement_policy": string(n.configRef.Cache.ReplacementPolicy),
					"states":             n.configRef.Cache.States,
				}, true
			}
		case "directory":
			if n.configRef.Directory != nil {
				return map[string]interface{}{
					"capacity":           n.configRef.Directory.Capacity,
					"num_sets":           n.configRef.Directory.NumSets,
					"replacement_policy": n.configRef.Directory.ReplacementPolicy,
					"states":             n.configRef.Directory.States,
				}, true
			}
		}
	}
	return nil, false
}

// SetCoherenceDomainID 设置一致性域ID (Phase 2: 已废弃, 空实现)
// Deprecated: Config 数据现在通过 configRef 管理, 此方法仅保持接口兼容
func (n *BaseNode) SetCoherenceDomainID(id int) {
	// Phase 2: 空实现, 保持接口兼容
}

// GetCoherenceDomainID 获取一致性域ID (Phase 2: 从 configRef 读取)
func (n *BaseNode) GetCoherenceDomainID() *int {
	// Phase 2: 从 configRef 读取
	if n.configRef != nil {
		return n.configRef.CoherenceDomainId
	}
	return nil
}

// SetDisplayData 设置显示数据的某个键值 (Phase 2: 已废弃, 空实现)
// Deprecated: Display 数据现在通过 configRef 管理, 此方法仅保持接口兼容
func (n *BaseNode) SetDisplayData(key string, value interface{}) {
	// Phase 2: 空实现, 保持接口兼容
}

// GetDisplayData 获取显示数据的某个键值 (Phase 2: 从 configRef 读取)
func (n *BaseNode) GetDisplayData(key string) (interface{}, bool) {
	// Phase 2: 从 configRef 读取
	if n.configRef != nil {
		switch key {
		case "position":
			return n.configRef.Position, true
		case "data":
			return n.configRef.Data, true
		case "style":
			if n.configRef.Style != nil {
				return *n.configRef.Style, true
			}
		}
	}
	return nil, false
}

// GetAllDisplayData 获取所有显示数据 (Phase 2: 从 configRef 读取)
func (n *BaseNode) GetAllDisplayData() map[string]interface{} {
	// Phase 2: 从 configRef 读取
	result := make(map[string]interface{})
	if n.configRef != nil {
		result["position"] = n.configRef.Position
		result["data"] = n.configRef.Data
		if n.configRef.Style != nil {
			result["style"] = *n.configRef.Style
		}
	}
	return result
}

// SetAllDisplayData 设置所有显示数据 (Phase 2: 已废弃, 空实现)
// Deprecated: Display 数据现在通过 configRef 管理, 此方法仅保持接口兼容
func (n *BaseNode) SetAllDisplayData(data map[string]interface{}) {
	// Phase 2: 空实现, 保持接口兼容
}
