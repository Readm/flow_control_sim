package pipeline

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Pipeline defines the contract for moving packets through a node using AheadPort.
// Concrete implementations can apply arbitrary policies while conforming to this API.
type Pipeline interface {
	ID() int
	// Tick processes a single cycle, receiving packets from upstream and sending to downstream.
	Tick(cycle int) error
	// InPort returns the input AheadPort for receiving packets from upstream Link.
	InPort() ahead_port.AheadPort
	// OutPort returns the output AheadPort for sending packets to downstream Link.
	OutPort() ahead_port.AheadPort
	// ProcessedCount returns the number of packets processed lifecycle-wide.
	ProcessedCount() int
	// SetOutPort sets the output port for a downstream Link.
	SetOutPort(port ahead_port.AheadPort)
	// GetProcessedPackets returns packets processed in the last Tick call.
	// Returns an empty slice if no packets were processed or if not supported.
	GetProcessedPackets() []packet.Packet
	// InjectPackets is deprecated. Pipeline no longer manages out_queue.
	// Use OutputQueue component instead for packet transmission.
	InjectPackets(cycle int, packets []packet.Packet) error
}

// PipelinePacketProcessor implements PacketProcessor for Pipeline.
// It handles receiving packets, processing them, and sending to output queue.
type PipelinePacketProcessor struct {
	pipeline *FIFO
}

// NewPipelinePacketProcessor creates a new PipelinePacketProcessor.
func NewPipelinePacketProcessor(pipeline *FIFO) *PipelinePacketProcessor {
	return &PipelinePacketProcessor{
		pipeline: pipeline,
	}
}

// ProcessPackets processes packets for Pipeline: receive from in_queue and process up to Pick().
// Data flow stops at Pick(), processed packets are stored for GetProcessedPackets().
func (p *PipelinePacketProcessor) ProcessPackets(
	receiveChan <-chan ahead_port.PacketWithCycle,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(ahead_port.PacketWithCycle),
	setDone func(int),
	updateUpstreamReady func(cycle int, ready bool),
) {
	// Step 1: Receive packets from inPort and store in in_queue array
	if p.pipeline.inQueue != nil {
		// Directly receive from inPort channel and store in in_queue array
		for {
			select {
			case pkt := <-receiveChan:
				// Store in in_queue array
				slot := p.pipeline.inQueue.findFreeSlot()
				if slot >= 0 {
					p.pipeline.inQueue.arrayMu.Lock()
					p.pipeline.inQueue.slots[slot] = packet.PacketWithCycle(pkt)
					p.pipeline.inQueue.freeBitmap[slot] = false
					p.pipeline.inQueue.blockReasons[slot] = 0
					p.pipeline.inQueue.arrayMu.Unlock()
				}
			default:
				goto drainInQueue
			}
		}
	}

drainInQueue:

	// Step 2: Drain packets from in_queue (using Pick method)
	// Data flow stops here - processed packets are stored for GetProcessedPackets()
	var processedPackets []packet.Packet
	if p.pipeline.inQueue != nil {
		// Pick all available packets from in_queue array (loop until no more packets)
		for {
			pickedPackets := p.pipeline.inQueue.Pick()
			if len(pickedPackets) == 0 {
				break
			}
			for _, pkt := range pickedPackets {
				processedPackets = append(processedPackets, pkt.Packet)
			}
		}
	}

	// Step 3: Record processed packets
	p.pipeline.processed = append(p.pipeline.processed, processedPackets...)
	// Store packets processed in this cycle for GetProcessedPackets()
	p.pipeline.lastCyclePackets = processedPackets

	// Step 4: Notify upstream that we are ready for next cycle
	updateUpstreamReady(cycle+1, true)
}

// FIFO implements Pipeline by draining packets in the order they arrive using AheadPort.
type FIFO struct {
	id               int
	inPort           ahead_port.AheadPort // Input port from upstream Link
	outPort          ahead_port.AheadPort // Output port to downstream Link (deprecated, kept for interface compatibility)
	processor        *ahead_port.CycleProcessor
	packetProc       *PipelinePacketProcessor
	processed        []packet.Packet
	lastCyclePackets []packet.Packet // Packets processed in the last Tick call
	inQueue          *Queue          // Internal in_queue
}

// NewFIFO constructs a FIFO pipeline with the provided identifier.
// Creates an AheadPort for input, internal queues, and CycleProcessor for processing.
func NewFIFO(id int, bufferSize int) *FIFO {
	if bufferSize <= 0 {
		bufferSize = 8
	}

	// Create input port
	inPort := ahead_port.NewAheadPort(bufferSize)

	// Create internal queue
	inQueue := NewQueue(bufferSize, 1, 1, 1) // size, inBandwidth, outBandwidth, bitmapWidth

	// Create pipeline instance
	f := &FIFO{
		id:        id,
		inPort:    inPort,
		outPort:   nil,
		processed: make([]packet.Packet, 0),
		inQueue:   inQueue,
	}

	// Create packet processor
	f.packetProc = NewPipelinePacketProcessor(f)

	// Create cycle processor (downstream port will be set via SetOutPort)
	dummyDownstream := ahead_port.NewAheadPort(bufferSize)
	f.processor = ahead_port.NewCycleProcessor(inPort, dummyDownstream, f.packetProc)

	return f
}

// ID returns the node identifier that owns the pipeline.
func (f *FIFO) ID() int {
	return f.id
}

// Tick processes a single cycle.
func (f *FIFO) Tick(cycle int) error {
	// Recreate processor with dummy downstream (not used in ProcessPackets)
	dummyDownstream := ahead_port.NewAheadPort(8)
	f.processor = ahead_port.NewCycleProcessor(f.inPort, dummyDownstream, f.packetProc)

	return f.processor.Tick(cycle)
}

// InPort returns the input AheadPort.
func (f *FIFO) InPort() ahead_port.AheadPort {
	return f.inPort
}

// OutPort returns the output AheadPort.
func (f *FIFO) OutPort() ahead_port.AheadPort {
	return f.outPort
}

// ProcessedCount returns the number of packets processed.
func (f *FIFO) ProcessedCount() int {
	return len(f.processed)
}

// GetProcessedPackets returns packets processed in the last Tick call.
func (f *FIFO) GetProcessedPackets() []packet.Packet {
	// Return a copy of packets processed in the last cycle
	result := make([]packet.Packet, len(f.lastCyclePackets))
	copy(result, f.lastCyclePackets)
	return result
}

// SetOutPort sets the output port for a downstream Link.
func (f *FIFO) SetOutPort(port ahead_port.AheadPort) {
	f.outPort = port
}

// InjectPackets is deprecated. Pipeline no longer manages out_queue.
// Use OutputQueue component instead for packet transmission.
func (f *FIFO) InjectPackets(cycle int, packets []packet.Packet) error {
	// Pipeline no longer manages out_queue, packets are dropped
	// Use OutputQueue component for packet injection
	return nil
}
