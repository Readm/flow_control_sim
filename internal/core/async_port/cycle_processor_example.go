package async_port

import (
	"fmt"
	"log"
)

// Example: Custom implementation for a specific Flow type

// FIFOFlowHooks implements CycleProcessorHooks for a FIFO flow.
type FIFOFlowHooks struct {
	*DefaultHooks // Embed default implementations
	flowID        int
}

func NewFIFOFlowHooks(flowID int) *FIFOFlowHooks {
	return &FIFOFlowHooks{
		DefaultHooks: &DefaultHooks{},
		flowID:       flowID,
	}
}

// Override OnCycleStart to add custom logging
func (f *FIFOFlowHooks) OnCycleStart(cycle int) {
	log.Printf("Flow %d: Starting cycle %d", f.flowID, cycle)
}

// Override OnDataReceived to add custom processing
func (f *FIFOFlowHooks) OnDataReceived(pkt PacketWithCycle, cycle int) {
	log.Printf("Flow %d: Received packet at cycle %d, packet cycle %d",
		f.flowID, cycle, pkt.Cycle)
}

// Override OnPacketReceived to add processing
func (f *FIFOFlowHooks) OnPacketReceived(pkt PacketWithCycle, cycle int) PacketWithCycle {
	// Example: Add flow ID to packet payload
	modifiedPkt := pkt
	modifiedPkt.Packet.Payload = fmt.Sprintf("Flow%d: %s", f.flowID, pkt.Packet.Payload)
	return modifiedPkt
}

// Override OnDownstreamReady for custom ready/not-ready logic
func (f *FIFOFlowHooks) OnDownstreamReady(pkt PacketWithCycle, cycle int, ready bool) int {
	if ready {
		log.Printf("Flow %d: Downstream ready for cycle %d", f.flowID, cycle)
		return cycle
	} else {
		log.Printf("Flow %d: Downstream not ready for cycle %d, will increment", f.flowID, cycle)
		return cycle + 1
	}
}

// Example: Another implementation for a Priority Flow

// PriorityFlowHooks implements CycleProcessorHooks for a priority-based flow.
type PriorityFlowHooks struct {
	*DefaultHooks
	flowID    int
	priority  int
	processed []PacketWithCycle
}

func NewPriorityFlowHooks(flowID, priority int) *PriorityFlowHooks {
	return &PriorityFlowHooks{
		DefaultHooks: &DefaultHooks{},
		flowID:       flowID,
		priority:     priority,
		processed:    make([]PacketWithCycle, 0),
	}
}

// Override OnPacketReceived to implement priority logic
func (p *PriorityFlowHooks) OnPacketReceived(pkt PacketWithCycle, cycle int) PacketWithCycle {
	// Priority flow: might reorder packets or add priority metadata
	// For simplicity, just store for later processing
	p.processed = append(p.processed, pkt)
	return pkt
}

// Override OnDownstreamReady to implement priority-aware cycle increment
func (p *PriorityFlowHooks) OnDownstreamReady(pkt PacketWithCycle, cycle int, ready bool) int {
	if ready {
		return cycle
	}
	// High priority flows might have different increment strategy
	if p.priority > 5 {
		// High priority: only increment by 1, wait more
		return cycle + 1
	}
	// Low priority: can increment more aggressively
	return cycle + 2
}

// Example usage:

// func ExampleUsage() {
//     // Create ports
//     upstreamPort := NewPort(8)
//     downstreamPort := NewPort(8)
//
//     // Create hooks for FIFO flow
//     hooks := NewFIFOFlowHooks(1)
//
//     // Create processor with the hooks
//     processor := NewCycleProcessor(upstreamPort, downstreamPort, hooks)
//
//     // Process cycles
//     for cycle := 0; cycle < 10; cycle++ {
//         processor.ProcessCycle(cycle)
//     }
// }
