package async_port

import (
	"log"
)

// Example: Custom implementation for a specific Flow type

// FIFOFlowProcessor implements PacketProcessor for a FIFO flow.
type FIFOFlowProcessor struct {
	*DefaultProcessor // Embed default implementation
	flowID            int
}

func NewFIFOFlowProcessor(flowID int) *FIFOFlowProcessor {
	return &FIFOFlowProcessor{
		DefaultProcessor: &DefaultProcessor{},
		flowID:           flowID,
	}
}

// Override ProcessPackets to add custom processing
func (f *FIFOFlowProcessor) ProcessPackets(receiveChan <-chan PacketWithCycle, cycle int, checkReady func(int) bool, sendPacket func(PacketWithCycle), setDoneUntil func(int), updateUpstreamReady func(cycle int, ready bool)) {
	// Use default processing logic
	f.DefaultProcessor.ProcessPackets(receiveChan, cycle, checkReady, sendPacket, setDoneUntil, updateUpstreamReady)

	// Add custom logging
	log.Printf("Flow %d: Completed processing cycle %d", f.flowID, cycle)
}

// Example: Another implementation for a Priority Flow

// PriorityFlowProcessor implements PacketProcessor for a priority-based flow.
type PriorityFlowProcessor struct {
	*DefaultProcessor
	flowID   int
	priority int
}

func NewPriorityFlowProcessor(flowID, priority int) *PriorityFlowProcessor {
	return &PriorityFlowProcessor{
		DefaultProcessor: &DefaultProcessor{},
		flowID:           flowID,
		priority:         priority,
	}
}

// Override ProcessPackets to implement priority logic
func (p *PriorityFlowProcessor) ProcessPackets(receiveChan <-chan PacketWithCycle, cycle int, checkReady func(int) bool, sendPacket func(PacketWithCycle), setDoneUntil func(int), updateUpstreamReady func(cycle int, ready bool)) {
	// Collect all packets first
	allPackets := make([]PacketWithCycle, 0)

	// Process pending packets
	for _, pkt := range p.pendingPackets {
		allPackets = append(allPackets, pkt)
	}

	// Receive new packets
	for {
		select {
		case pkt := <-receiveChan:
			allPackets = append(allPackets, pkt)
		default:
			goto process
		}
	}

process:
	// Priority-based processing: high priority packets first
	// For simplicity, use default logic
	p.DefaultProcessor.ProcessPackets(receiveChan, cycle, checkReady, sendPacket, setDoneUntil, updateUpstreamReady)
}

// Example usage:

// func ExampleUsage() {
//     // Create ports
//     upstreamPort := NewCyclePort(8)
//     downstreamPort := NewCyclePort(8)
//
//     // Create processor for FIFO flow
//     proc := NewFIFOFlowProcessor(1)
//
//     // Create cycle processor with the packet processor
//     processor := NewCycleProcessor(upstreamPort, downstreamPort, proc)
//
//     // Process cycles
//     for cycle := 0; cycle < 10; cycle++ {
//         processor.ProcessCycle(cycle)
//     }
// }
