package ahead_port

import (
	"fmt"
)

// PacketProcessor defines the interface for processing packets in each cycle.
type PacketProcessor interface {
	// ProcessPackets processes all packets (both received from channel and pending).
	// This method should handle receiving packets from channel and the loop processing logic.
	// pendingPackets is a static variable internal to the processor implementation (stored as a struct field).
	// receiveChan: channel to receive packets from upstream (non-blocking, drain all available)
	// cycle: the current cycle number
	// checkReady: function to check if downstream is ready for a given cycle
	// sendPacket: function to send a packet to downstream (called when packet is ready)
	// setDone: function to set downstream Done
	// updateUpstreamReady: function to notify upstream about readiness for next cycle (Q node in flowchart)
	// Called by processor to indicate readiness for cycle N+1 after completing cycle N
	ProcessPackets(receiveChan <-chan PacketWithCycle, cycle int, checkReady func(int) bool, sendPacket func(PacketWithCycle), setDone func(int), updateUpstreamReady func(cycle int, ready bool))
}

// CycleProcessor provides the base workflow for processing cycles.
type CycleProcessor struct {
	upstreamPort   AheadPort       // Port for receiving packets from upstream
	downstreamPort AheadPort       // Port for sending packets to downstream
	processor      PacketProcessor // Processor for handling packets
}

// NewCycleProcessor creates a new cycle processor with the given ports and processor.
// Both ports must implement AheadPort interface.
// If processor is nil, DefaultProcessor will be used.
func NewCycleProcessor(upstreamPort AheadPort, downstreamPort AheadPort, processor PacketProcessor) *CycleProcessor {
	if processor == nil {
		processor = &DefaultProcessor{}
	}
	return &CycleProcessor{
		upstreamPort:   upstreamPort,
		downstreamPort: downstreamPort,
		processor:      processor,
	}
}

// ProcessCycle implements the complete cycle processing workflow.
func (cp *CycleProcessor) ProcessCycle(cycle int) error {
	// Ensure processor is not nil (should never happen if NewCycleProcessor is used correctly)
	if cp.processor == nil {
		panic("CycleProcessor.processor is nil, this should never happen. Use NewCycleProcessor to create CycleProcessor.")
	}

	// Wait for upstream Done >= cycle-1
	// Uses condition variable to avoid busy waiting - goroutine will block until
	// SetDone is called and condition is satisfied
	cp.upstreamPort.WaitForDone(cycle - 1)

	// Prepare updateUpstreamReady function
	// UpdateReady is an internal implementation detail, accessed via type assertion
	var updateUpstreamReady func(cycle int, ready bool)
	if upstreamPort, ok := cp.upstreamPort.(*SinglePort); ok {
		updateUpstreamReady = upstreamPort.UpdateReady
	} else if faninPort, ok := cp.upstreamPort.(*FaninPort); ok {
		updateUpstreamReady = faninPort.UpdateReady
	} else {
		// If upstreamPort is not a *SinglePort or *FaninPort (e.g., a mock), provide a no-op function
		updateUpstreamReady = func(cycle int, ready bool) {}
	}

	// Process all packets
	// The processor is responsible for receiving packets from channel, processing them, and sending ready packets
	// pendingPackets is a static variable internal to the processor implementation
	// The processor should call updateUpstreamReady(cycle+1, ready) to notify upstream about readiness
	cp.processor.ProcessPackets(
		cp.upstreamPort.ReceiveChan(),
		cycle,
		cp.downstreamPort.Ready,
		cp.sendPacket,
		cp.downstreamPort.SetDone,
		updateUpstreamReady,
	)

	// SetDone after processing all packets
	// Only set downstream Done to cycle (current cycle completed) if it's not already larger
	// This ensures Done is monotonically increasing and doesn't decrease if processor already set a larger value
	// Upstream Done should be set by upstream itself, not by this processor
	currentDone := cp.downstreamPort.GetDone()
	if currentDone < cycle {
		cp.downstreamPort.SetDone(cycle)
	}

	// Assert that cycle+1 has been configured in upstream port
	// Either readyMap contains cycle+1, or readyUntil > cycle+1
	// This ensures that upstream can check Ready(cycle+1) without blocking
	if upstreamPort, ok := cp.upstreamPort.(*SinglePort); ok {
		_, configured := upstreamPort.ReadyNonBlocking(cycle + 1)
		if !configured {
			panic(fmt.Sprintf("ProcessCycle(cycle=%d) completed but cycle+1=%d is not configured in upstream port. Processor must call updateUpstreamReady(cycle+1, ready) in ProcessPackets.", cycle, cycle+1))
		}
	} else if faninPort, ok := cp.upstreamPort.(*FaninPort); ok {
		_, configured := faninPort.ReadyNonBlocking(cycle + 1)
		if !configured {
			panic(fmt.Sprintf("ProcessCycle(cycle=%d) completed but cycle+1=%d is not configured in all upstream ports. Processor must call updateUpstreamReady(cycle+1, ready) in ProcessPackets.", cycle, cycle+1))
		}
	}

	return nil
}

// sendPacket sends a packet to downstream.
// The packet's cycle should already be set correctly before calling this method.
func (cp *CycleProcessor) sendPacket(pkt PacketWithCycle) {
	cp.downstreamPort.Chan() <- pkt
}

// DefaultProcessor provides default implementation for packet processing.
// Implementations can embed this and override ProcessPackets if needed.
type DefaultProcessor struct {
	// pendingPackets is a static variable internal to ProcessPackets
	// It stores packets that were not sent due to downstream not ready
	pendingPackets []PacketWithCycle
}

func (d *DefaultProcessor) ProcessPackets(receiveChan <-chan PacketWithCycle, cycle int, checkReady func(int) bool, sendPacket func(PacketWithCycle), setDone func(int), updateUpstreamReady func(cycle int, ready bool)) {
	// pendingPackets is a static variable (struct field) - access directly
	newPendingPackets := make([]PacketWithCycle, 0)

		// Helper function to process a single packet
		processPacket := func(pkt PacketWithCycle) {
			pktCycle := int(pkt.Cycle)
			isReady := checkReady(pktCycle)
			if isReady {
				// Ready: send the packet immediately
				pkt.Cycle = int(pktCycle)
				sendPacket(pkt)
			} else {
				// Not ready: keep in pending
				newPendingPackets = append(newPendingPackets, pkt)
			}
		}

	// Process pending packets first (from static variable)
	for _, pkt := range d.pendingPackets {
		processPacket(pkt)
	}

	// H: Receive and process all available packets from channel (non-blocking, drain all available)
	// Similar to Flow.Tick behavior - receive and process all packets available in this cycle
	for {
		select {
		case pkt := <-receiveChan:
			processPacket(pkt)
		default:
			// No more packets available
			goto done
		}
	}

done:
	// F: SetDone after processing all packets
	// Set downstream Done to cycle (current cycle completed)
	setDone(cycle)

	// Q: Calculate if cycle N+1 is ready and notify upstream
	// After completing cycle N, notify upstream that we are ready for cycle N+1
	// This corresponds to the "Q" node in the documentation flowchart
	// Default behavior: if cycle N is completed, cycle N+1 is ready by default
	updateUpstreamReady(cycle+1, true)

	// Update pending packets (static variable - struct field)
	d.pendingPackets = newPendingPackets
}
