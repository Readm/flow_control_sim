package queue

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Pipeline defines the contract for moving packets through a node using AheadPort.
// Concrete implementations can apply arbitrary policies while conforming to this API.
type Pipeline interface {
	ID() int
	Tick(cycle int) error
	InPort() ahead_port.AheadPort
	OutPort() ahead_port.AheadPort
	ProcessedCount() int
	SetOutPort(port ahead_port.AheadPort)
	GetProcessedPackets() []packet.Packet
	InjectPackets(cycle int, packets []packet.Packet) error
}

type PipelinePacketProcessor struct {
	pipeline *FIFO
}

func NewPipelinePacketProcessor(pipeline *FIFO) *PipelinePacketProcessor {
	return &PipelinePacketProcessor{
		pipeline: pipeline,
	}
}

func (p *PipelinePacketProcessor) ProcessPackets(
	receiveChan <-chan ahead_port.PacketWithCycle,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(ahead_port.PacketWithCycle),
	setDone func(int),
	updateUpstreamReady func(cycle int, ready bool),
) {
	if p.pipeline.inQueue != nil {
		for {
			select {
			case pkt := <-receiveChan:
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

	var processedPackets []packet.Packet
	if p.pipeline.inQueue != nil {
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

	p.pipeline.processed = append(p.pipeline.processed, processedPackets...)
	p.pipeline.lastCyclePackets = processedPackets

	updateUpstreamReady(cycle+1, true)
}

type FIFO struct {
	id               int
	inPort           ahead_port.AheadPort
	outPort          ahead_port.AheadPort
	processor        *ahead_port.CycleProcessor
	packetProc       *PipelinePacketProcessor
	processed        []packet.Packet
	lastCyclePackets []packet.Packet
	inQueue          *Queue
	outQueue         *Queue
}

func NewFIFO(id int, bufferSize int) *FIFO {
	if bufferSize <= 0 {
		bufferSize = 8
	}

	inPort := ahead_port.NewAheadPort(bufferSize)
	inQueue := NewQueue(bufferSize, 1, 1, 1)
	outQueue := NewQueue(bufferSize, 1, 1, 1)

	f := &FIFO{
		id:        id,
		inPort:    inPort,
		outPort:   nil,
		processed: make([]packet.Packet, 0),
		inQueue:   inQueue,
		outQueue:  outQueue,
	}

	f.packetProc = NewPipelinePacketProcessor(f)
	dummyDownstream := ahead_port.NewAheadPort(bufferSize)
	f.processor = ahead_port.NewCycleProcessor(inPort, dummyDownstream, f.packetProc)

	return f
}

func (f *FIFO) ID() int {
	return f.id
}

func (f *FIFO) Tick(cycle int) error {
	dummyDownstream := ahead_port.NewAheadPort(8)
	f.processor = ahead_port.NewCycleProcessor(f.inPort, dummyDownstream, f.packetProc)

	return f.processor.Tick(cycle)
}

func (f *FIFO) InPort() ahead_port.AheadPort {
	return f.inPort
}

func (f *FIFO) OutPort() ahead_port.AheadPort {
	return f.outPort
}

func (f *FIFO) ProcessedCount() int {
	return len(f.processed)
}

func (f *FIFO) GetProcessedPackets() []packet.Packet {
	result := make([]packet.Packet, len(f.lastCyclePackets))
	copy(result, f.lastCyclePackets)
	return result
}

func (f *FIFO) SetOutPort(port ahead_port.AheadPort) {
	f.outPort = port
}

func (f *FIFO) InjectPackets(cycle int, packets []packet.Packet) error {
	if f.outQueue == nil {
		return nil
	}

	for _, pkt := range packets {
		env := packet.PacketWithCycle{
			Cycle:  cycle + 1,
			Packet: pkt,
		}
		slot := f.outQueue.findFreeSlot()
		if slot >= 0 {
			f.outQueue.arrayMu.Lock()
			f.outQueue.slots[slot] = env
			f.outQueue.freeBitmap[slot] = false
			f.outQueue.blockReasons[slot] = 0
			f.outQueue.arrayMu.Unlock()
		}
	}
	return nil
}


