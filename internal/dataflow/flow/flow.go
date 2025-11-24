package flow

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Flow defines the contract for moving packets through a node using AheadPort.
// Concrete implementations can apply arbitrary policies while conforming to this API.
type Flow interface {
	ID() int
	// ProcessCycle processes a single cycle, receiving packets from upstream and sending to downstream.
	ProcessCycle(cycle int) error
	// InPort returns the input AheadPort for receiving packets from upstream Link.
	InPort() ahead_port.AheadPort
	// OutPorts returns all output AheadPorts for sending packets to downstream Links.
	OutPorts() []ahead_port.AheadPort
	// ProcessedCount returns the number of packets processed lifecycle-wide.
	ProcessedCount() int
	// SetRouterHook sets the routing hook function for determining which outPort to use.
	SetRouterHook(hook RouterHook)
	// AddOutPort adds a new output port for a downstream Link.
	AddOutPort(port ahead_port.AheadPort)
	// Emit sends packets to output ports. Packets will be routed based on router hook.
	Emit(pkts ...packet.Packet)
}

// FlowPacketProcessor implements PacketProcessor for Flow.
// It handles receiving packets, processing them, and routing to multiple output ports.
type FlowPacketProcessor struct {
	flow           *FIFO
	pendingPackets []ahead_port.PacketWithCycle
}

// NewFlowPacketProcessor creates a new FlowPacketProcessor.
func NewFlowPacketProcessor(flow *FIFO) *FlowPacketProcessor {
	return &FlowPacketProcessor{
		flow:           flow,
		pendingPackets: make([]ahead_port.PacketWithCycle, 0),
	}
}

// ProcessPackets processes packets for Flow: receive, process, and route to output ports.
// Note: checkReady and sendPacket are not used here since we route to multiple output ports.
func (f *FlowPacketProcessor) ProcessPackets(
	receiveChan <-chan ahead_port.PacketWithCycle,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(ahead_port.PacketWithCycle),
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
	newPendingPackets := make([]ahead_port.PacketWithCycle, 0)
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
		env := ahead_port.PacketWithCycle{
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

// FIFO implements Flow by draining packets in the order they arrive using AheadPort.
type FIFO struct {
	id         int
	inPort     ahead_port.AheadPort   // Input port from upstream Link
	outPorts   []ahead_port.AheadPort // Output ports to downstream Links
	processor  *ahead_port.CycleProcessor
	packetProc *FlowPacketProcessor
	processed  []packet.Packet
	routerHook RouterHook
	topology   interface{}     // Network topology information for Router Hook
	emitQueue  []packet.Packet // Queue for packets to be emitted (from Emit method)
}

// NewFIFO constructs a FIFO flow with the provided identifier.
// Creates an AheadPort for input and CycleProcessor for processing.
func NewFIFO(id int, bufferSize int) *FIFO {
	if bufferSize <= 0 {
		bufferSize = 8
	}

	// Create input port
	inPort := ahead_port.NewAheadPort(bufferSize)

	// Create flow instance
	f := &FIFO{
		id:         id,
		inPort:     inPort,
		outPorts:   make([]ahead_port.AheadPort, 0),
		routerHook: DefaultRouterHook,
		processed:  make([]packet.Packet, 0),
		emitQueue:  make([]packet.Packet, 0),
	}

	// Create packet processor
	f.packetProc = NewFlowPacketProcessor(f)

	// Create cycle processor (downstream port will be set per packet via router)
	// For now, we'll use a dummy downstream port that will be replaced in ProcessPackets
	dummyDownstream := ahead_port.NewAheadPort(bufferSize)
	f.processor = ahead_port.NewCycleProcessor(inPort, dummyDownstream, f.packetProc)

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
	dummyDownstream := ahead_port.NewAheadPort(8)
	f.processor = ahead_port.NewCycleProcessor(f.inPort, dummyDownstream, f.packetProc)

	return f.processor.ProcessCycle(cycle)
}

// InPort returns the input AheadPort.
func (f *FIFO) InPort() ahead_port.AheadPort {
	return f.inPort
}

// OutPorts returns all output AheadPorts.
func (f *FIFO) OutPorts() []ahead_port.AheadPort {
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
func (f *FIFO) AddOutPort(port ahead_port.AheadPort) {
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
