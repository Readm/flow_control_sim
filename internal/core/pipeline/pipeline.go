package pipeline

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Pipeline defines the contract for moving packets through a node using AheadPort.
// Concrete implementations can apply arbitrary policies while conforming to this API.
type Pipeline interface {
	ID() int
	// ProcessCycle processes a single cycle, receiving packets from upstream and sending to downstream.
	ProcessCycle(cycle int) error
	// InPort returns the input AheadPort for receiving packets from upstream Link.
	InPort() ahead_port.AheadPort
	// OutPort returns the output AheadPort for sending packets to downstream Link.
	OutPort() ahead_port.AheadPort
	// ProcessedCount returns the number of packets processed lifecycle-wide.
	ProcessedCount() int
	// SetOutPort sets the output port for a downstream Link.
	SetOutPort(port ahead_port.AheadPort)
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

// ProcessPackets processes packets for Pipeline: receive from in_queue, process, and send to out_queue.
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

	// Step 4: Store processed packets directly in out_queue array
	if p.pipeline.outQueue != nil {
		for _, pkt := range processedPackets {
			env := packet.PacketWithCycle{
				Cycle:  cycle,
				Packet: pkt,
			}
			// Store directly in out_queue array
			slot := p.pipeline.outQueue.findFreeSlot()
			if slot >= 0 {
				p.pipeline.outQueue.arrayMu.Lock()
				p.pipeline.outQueue.slots[slot] = env
				p.pipeline.outQueue.freeBitmap[slot] = false
				p.pipeline.outQueue.blockReasons[slot] = 0
				p.pipeline.outQueue.arrayMu.Unlock()
			}
			// If no free slot, packet is dropped
		}
	}

	// Step 5: Process out_queue to send packets to outPort (with bandwidth limit and ready check)
	if p.pipeline.outQueue != nil && p.pipeline.outPort != nil {
		// Process up to outBandwidth packets per cycle
		sentCount := 0
		for sentCount < p.pipeline.outQueue.outBandwidth {
			pickedPackets := p.pipeline.outQueue.Pick()
			if len(pickedPackets) == 0 {
				break // No more packets in queue
			}
			// Try to send each packet if outPort is ready
			for _, pkt := range pickedPackets {
				if sentCount >= p.pipeline.outQueue.outBandwidth {
					// Reached bandwidth limit, put remaining packets back
					slot := p.pipeline.outQueue.findFreeSlot()
					if slot >= 0 {
						p.pipeline.outQueue.arrayMu.Lock()
						p.pipeline.outQueue.slots[slot] = pkt
						p.pipeline.outQueue.freeBitmap[slot] = false
						p.pipeline.outQueue.blockReasons[slot] = 0
						p.pipeline.outQueue.arrayMu.Unlock()
					}
					continue
				}
				if p.pipeline.outPort.Ready(pkt.Cycle) {
					// Send to outPort
					env := ahead_port.PacketWithCycle{
						Cycle:  pkt.Cycle,
						Packet: pkt.Packet,
					}
					select {
					case p.pipeline.outPort.SendChan() <- env:
						// Successfully sent
						sentCount++
					default:
						// outPort channel is full, put back to out_queue
						slot := p.pipeline.outQueue.findFreeSlot()
						if slot >= 0 {
							p.pipeline.outQueue.arrayMu.Lock()
							p.pipeline.outQueue.slots[slot] = pkt
							p.pipeline.outQueue.freeBitmap[slot] = false
							p.pipeline.outQueue.blockReasons[slot] = 0
							p.pipeline.outQueue.arrayMu.Unlock()
						}
					}
				} else {
					// outPort not ready, put back to out_queue
					slot := p.pipeline.outQueue.findFreeSlot()
					if slot >= 0 {
						p.pipeline.outQueue.arrayMu.Lock()
						p.pipeline.outQueue.slots[slot] = pkt
						p.pipeline.outQueue.freeBitmap[slot] = false
						p.pipeline.outQueue.blockReasons[slot] = 0
						p.pipeline.outQueue.arrayMu.Unlock()
					}
				}
			}
		}
	}

	// Step 6: Set Done for output port
	if p.pipeline.outPort != nil {
		currentDone := p.pipeline.outPort.GetDone()
		if currentDone < cycle {
			p.pipeline.outPort.SetDone(cycle)
		}
	}

	// Step 7: Notify upstream that we are ready for next cycle
	updateUpstreamReady(cycle+1, true)
}

// FIFO implements Pipeline by draining packets in the order they arrive using AheadPort.
type FIFO struct {
	id         int
	inPort     ahead_port.AheadPort // Input port from upstream Link
	outPort    ahead_port.AheadPort // Output port to downstream Link
	processor  *ahead_port.CycleProcessor
	packetProc *PipelinePacketProcessor
	processed  []packet.Packet
	inQueue    *Queue // Internal in_queue
	outQueue   *Queue // Internal out_queue
}

// NewFIFO constructs a FIFO pipeline with the provided identifier.
// Creates an AheadPort for input, internal queues, and CycleProcessor for processing.
func NewFIFO(id int, bufferSize int) *FIFO {
	if bufferSize <= 0 {
		bufferSize = 8
	}

	// Create input port
	inPort := ahead_port.NewAheadPort(bufferSize)

	// Create internal queues
	inQueue := NewQueue(bufferSize, 1, 1, 1)           // size, inBandwidth, outBandwidth, bitmapWidth
	outQueue := NewQueue(bufferSize, 1, bufferSize, 1) // size, inBandwidth, outBandwidth (use bufferSize for higher bandwidth), bitmapWidth

	// Create pipeline instance
	f := &FIFO{
		id:        id,
		inPort:    inPort,
		outPort:   nil,
		processed: make([]packet.Packet, 0),
		inQueue:   inQueue,
		outQueue:  outQueue,
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

// ProcessCycle processes a single cycle.
func (f *FIFO) ProcessCycle(cycle int) error {
	// Recreate processor with dummy downstream (not used in ProcessPackets)
	dummyDownstream := ahead_port.NewAheadPort(8)
	f.processor = ahead_port.NewCycleProcessor(f.inPort, dummyDownstream, f.packetProc)

	return f.processor.ProcessCycle(cycle)
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

// SetOutPort sets the output port for a downstream Link.
func (f *FIFO) SetOutPort(port ahead_port.AheadPort) {
	f.outPort = port
}
