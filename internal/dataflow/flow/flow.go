package flow

import (
	"github.com/Readm/flow_sim/internal/core/cycle_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Flow defines the contract for moving packets through a node using CyclePort.
// Concrete implementations can apply arbitrary policies while conforming to this API.
type Flow interface {
	ID() int
	// ProcessCycle processes a single cycle, receiving packets from upstream and sending to downstream.
	ProcessCycle(cycle int) error
	// InPort returns the input CyclePort for receiving packets from upstream Link.
	InPort() cycle_port.CyclePort
	// OutPorts returns all output CyclePorts for sending packets to downstream Links.
	OutPorts() []cycle_port.CyclePort
	// ProcessedCount returns the number of packets processed lifecycle-wide.
	ProcessedCount() int
	// SetRouterHook sets the routing hook function for determining which outPort to use.
	SetRouterHook(hook RouterHook)
	// AddOutPort adds a new output port for a downstream Link.
	AddOutPort(port cycle_port.CyclePort)
	// Emit sends packets to output ports. Packets will be routed based on router hook.
	Emit(pkts ...packet.Packet)
}

// FlowPacketProcessor implements PacketProcessor for Flow.
// It handles receiving packets, processing them, and routing to multiple output ports.
type FlowPacketProcessor struct {
	flow           *FIFO
	pendingPackets []cycle_port.PacketWithCycle
}

// NewFlowPacketProcessor creates a new FlowPacketProcessor.
func NewFlowPacketProcessor(flow *FIFO) *FlowPacketProcessor {
	return &FlowPacketProcessor{
		flow:           flow,
		pendingPackets: make([]cycle_port.PacketWithCycle, 0),
	}
}

// ProcessPackets processes packets for Flow: receive, process, and route to output ports.
// Note: checkReady and sendPacket are not used here since we route to multiple output ports.
func (f *FlowPacketProcessor) ProcessPackets(
	receiveChan <-chan cycle_port.PacketWithCycle,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(cycle_port.PacketWithCycle),
	setDone func(int),
	updateUpstreamReady func(cycle int, ready bool),
) {
	// Collect all incoming packets
	var incomingPackets []packet.Packet

	// Process pending packets first
	for _, pkt := range f.pendingPackets {
		// Process the packet (simple FIFO: just add to processed list)
		incomingPackets = append(incomingPackets, pkt.Packet)
	}

	// Receive all available packets from channel (non-blocking, drain all)
	for {
		select {
		case pkt := <-receiveChan:
			incomingPackets = append(incomingPackets, pkt.Packet)
		default:
			goto process
		}
	}

process:
	// Add packets from emit queue (from Emit method)
	incomingPackets = append(incomingPackets, f.flow.emitQueue...)
	f.flow.emitQueue = nil // Clear emit queue

	// Process all incoming packets (simple FIFO processing)
	f.flow.processed = append(f.flow.processed, incomingPackets...)

	// Route processed packets to output ports
	newPendingPackets := make([]cycle_port.PacketWithCycle, 0)
	// Convert outPorts to []interface{} for router hook
	outPortsInterface := make([]interface{}, len(f.flow.outPorts))
	for i, port := range f.flow.outPorts {
		outPortsInterface[i] = port
	}
	for _, pkt := range incomingPackets {
		// Use router hook to determine which output port to use
		portIndex := f.flow.routerHook(pkt, outPortsInterface, f.flow.topology)
		if portIndex < 0 || portIndex >= len(f.flow.outPorts) {
			// Invalid port index, discard packet
			continue
		}

		outPort := f.flow.outPorts[portIndex]
		pktCycle := cycle
		env := cycle_port.PacketWithCycle{
			Cycle:  pktCycle,
			Packet: pkt,
		}

		// Check if downstream is ready for this cycle
		if outPort.Ready(pktCycle) {
			// Ready: send immediately
			outPort.Chan() <- env
		} else {
			// Not ready: keep in pending
			newPendingPackets = append(newPendingPackets, env)
		}
	}

	// Update pending packets
	f.pendingPackets = newPendingPackets

	// Set Done for all output ports
	for _, outPort := range f.flow.outPorts {
		currentDone := outPort.GetDone()
		if currentDone < cycle {
			outPort.SetDone(cycle)
		}
	}

	// Notify upstream that we are ready for next cycle
	updateUpstreamReady(cycle+1, true)
}

// FIFO implements Flow by draining packets in the order they arrive using CyclePort.
type FIFO struct {
	id         int
	inPort     cycle_port.CyclePort   // Input port from upstream Link
	outPorts   []cycle_port.CyclePort // Output ports to downstream Links
	processor  *cycle_port.CycleProcessor
	packetProc *FlowPacketProcessor
	processed  []packet.Packet
	routerHook RouterHook
	topology   interface{}     // Network topology information for Router Hook
	emitQueue  []packet.Packet // Queue for packets to be emitted (from Emit method)
}

// NewFIFO constructs a FIFO flow with the provided identifier.
// Creates a CyclePort for input and CycleProcessor for processing.
func NewFIFO(id int, bufferSize int) *FIFO {
	if bufferSize <= 0 {
		bufferSize = 8
	}

	// Create input port
	inPort := cycle_port.NewCyclePort(bufferSize)

	// Create flow instance
	f := &FIFO{
		id:         id,
		inPort:     inPort,
		outPorts:   make([]cycle_port.CyclePort, 0),
		routerHook: DefaultRouterHook,
		processed:  make([]packet.Packet, 0),
		emitQueue:  make([]packet.Packet, 0),
	}

	// Create packet processor
	f.packetProc = NewFlowPacketProcessor(f)

	// Create cycle processor (downstream port will be set per packet via router)
	// For now, we'll use a dummy downstream port that will be replaced in ProcessPackets
	dummyDownstream := cycle_port.NewCyclePort(bufferSize)
	f.processor = cycle_port.NewCycleProcessor(inPort, dummyDownstream, f.packetProc)

	return f
}

// ID returns the node identifier that owns the flow.
func (f *FIFO) ID() int {
	return f.id
}

// ProcessCycle processes a single cycle.
func (f *FIFO) ProcessCycle(cycle int) error {
	// CycleProcessor needs a downstream port, but Flow has multiple output ports.
	// We create a dummy downstream port that won't be used (FlowPacketProcessor routes to outPorts directly).
	// Recreate processor if outPorts changed (for simplicity, always recreate with dummy downstream)
	dummyDownstream := cycle_port.NewCyclePort(8)
	f.processor = cycle_port.NewCycleProcessor(f.inPort, dummyDownstream, f.packetProc)

	return f.processor.ProcessCycle(cycle)
}

// InPort returns the input CyclePort.
func (f *FIFO) InPort() cycle_port.CyclePort {
	return f.inPort
}

// OutPorts returns all output CyclePorts.
func (f *FIFO) OutPorts() []cycle_port.CyclePort {
	return f.outPorts
}

// ProcessedCount returns the number of packets processed.
func (f *FIFO) ProcessedCount() int {
	return len(f.processed)
}

// SetRouterHook sets the routing hook function.
func (f *FIFO) SetRouterHook(hook RouterHook) {
	if hook == nil {
		hook = DefaultRouterHook
	}
	f.routerHook = hook
}

// AddOutPort adds a new output port for a downstream Link.
func (f *FIFO) AddOutPort(port cycle_port.CyclePort) {
	f.outPorts = append(f.outPorts, port)
}

// Emit sends packets to output ports. Packets will be routed based on router hook.
// Packets are queued and will be sent in the next ProcessCycle.
func (f *FIFO) Emit(pkts ...packet.Packet) {
	if len(pkts) == 0 {
		return
	}
	f.emitQueue = append(f.emitQueue, pkts...)
}
