package async_port

// CycleProcessorHooks defines the customizable steps in the cycle processing workflow.
// Implementations can provide custom logic for each hook.
type CycleProcessorHooks interface {
	// OnCycleStart is called at the beginning of each cycle.
	// cycle: the current cycle number
	OnCycleStart(cycle int)

	// OnDataReceived is called after receiving data from upstream.
	// pkt: the received packet
	// cycle: the cycle at which the packet was received
	OnDataReceived(pkt PacketWithCycle, cycle int)

	// OnDownstreamBackpressureIndependentLogic is called for logic that doesn't depend on downstream backpressure.
	// This corresponds to step "I" in the documentation.
	// pkt: the packet to process
	// cycle: the current cycle
	// Returns: processed data (can be the same packet or modified)
	OnDownstreamBackpressureIndependentLogic(pkt PacketWithCycle, cycle int) PacketWithCycle

	// OnDownstreamReady is called when downstream is ready for the current cycle.
	// This corresponds to step "C" in the documentation.
	// pkt: the packet to send
	// cycle: the current cycle
	OnDownstreamReady(pkt PacketWithCycle, cycle int)

	// OnDownstreamNotReady is called when downstream is not ready for the current cycle.
	// This corresponds to step "D" in the documentation.
	// pkt: the packet to send
	// cycle: the current cycle
	// Returns: the actual cycle to use (may be incremented)
	OnDownstreamNotReady(pkt PacketWithCycle, cycle int) int

	// OnCycleEnd is called at the end of each cycle.
	// cycle: the completed cycle number
	OnCycleEnd(cycle int)
}

// CycleProcessor provides the base workflow for processing cycles.
// It implements the template method pattern, where the overall flow is fixed,
// but specific steps can be customized through hooks.
type CycleProcessor struct {
	upstreamPort   *Port // Use concrete type to access GetDoneUntil and ReceiveChan
	downstreamPort ASyncPort
	hooks          CycleProcessorHooks
}

// NewCycleProcessor creates a new cycle processor with the given ports and hooks.
// upstreamPort must be a *Port to access GetDoneUntil and ReceiveChan methods.
func NewCycleProcessor(upstreamPort *Port, downstreamPort ASyncPort, hooks CycleProcessorHooks) *CycleProcessor {
	return &CycleProcessor{
		upstreamPort:   upstreamPort,
		downstreamPort: downstreamPort,
		hooks:          hooks,
	}
}

// ProcessCycle implements the complete cycle processing workflow as defined in the documentation.
// This is the "template method" that defines the overall flow.
func (cp *CycleProcessor) ProcessCycle(cycle int) error {
	// A: Start Cycle N
	if cp.hooks != nil {
		cp.hooks.OnCycleStart(cycle)
	}

	// Wait for upstream DoneUntil >= cycle
	// Uses condition variable to avoid busy waiting - goroutine will block until
	// SetDoneUntil is called and condition is satisfied
	cp.upstreamPort.WaitForDoneUntil(cycle)

	// H: Get data: Chan() -> in_queue
	select {
	case pkt := <-cp.upstreamPort.ReceiveChan():
		// OnDataReceived hook
		if cp.hooks != nil {
			cp.hooks.OnDataReceived(pkt, cycle)
		}

		// I: Downstream backpressure-independent logic simulation
		var processedPkt PacketWithCycle
		if cp.hooks != nil {
			processedPkt = cp.hooks.OnDownstreamBackpressureIndependentLogic(pkt, cycle)
		} else {
			processedPkt = pkt
		}

		// B: Check downstream.CheckReady(cycle)
		// If not ready, increment cycle until ready (this is the core logic)
		actualCycle := cp.incrementCycleUntilReady(cycle, processedPkt)

		// C: Downstream ready logic (now we know downstream is ready for actualCycle)
		processedPkt.Cycle = uint64(actualCycle)
		if cp.hooks != nil {
			cp.hooks.OnDownstreamReady(processedPkt, actualCycle)
		}

		// E: Send data (with potentially incremented cycle)
		cp.sendPacket(processedPkt, actualCycle)

		// F: SetDoneUntil(N+1)
		cp.upstreamPort.SetDoneUntil(cycle + 1)
		cp.downstreamPort.SetDoneUntil(actualCycle + 1)

	default:
		// No packet received, but still need to update DoneUntil
		cp.upstreamPort.SetDoneUntil(cycle + 1)
	}

	// P: N++ (handled by caller)
	// OnCycleEnd hook
	if cp.hooks != nil {
		cp.hooks.OnCycleEnd(cycle)
	}

	return nil
}

// incrementCycleUntilReady increments the cycle until downstream is ready.
// This implements the core logic: if downstream is not ready for cycle N,
// increment to N+1, N+2, etc. until ready.
// Returns the actual cycle to use for sending.
func (cp *CycleProcessor) incrementCycleUntilReady(originalCycle int, pkt PacketWithCycle) int {
	currentCycle := originalCycle
	cycleIncrement := 0

	// Keep checking and incrementing until downstream is ready
	for !cp.downstreamPort.Ready(currentCycle) {
		// Call hook to notify about cycle increment (optional, for logging/monitoring)
		if cp.hooks != nil {
			cp.hooks.OnDownstreamNotReady(pkt, currentCycle)
		}

		cycleIncrement++
		currentCycle++

		// Safety: prevent infinite loop
		if cycleIncrement > 1000 {
			// Error: downstream not ready for too many cycles
			// Return the last checked cycle (could be improved with error handling)
			return currentCycle
		}
	}

	// Return the actual cycle (may be same as original if ready, or incremented)
	return currentCycle
}

// sendPacket sends a packet to downstream.
// The packet's cycle should already be set correctly before calling this method.
func (cp *CycleProcessor) sendPacket(pkt PacketWithCycle, cycle int) {
	cp.downstreamPort.Chan() <- pkt
}

// DefaultHooks provides default implementations for all hooks.
// Implementations can embed this and override only the hooks they need.
type DefaultHooks struct{}

func (d *DefaultHooks) OnCycleStart(cycle int)                        {}
func (d *DefaultHooks) OnDataReceived(pkt PacketWithCycle, cycle int) {}
func (d *DefaultHooks) OnDownstreamBackpressureIndependentLogic(pkt PacketWithCycle, cycle int) PacketWithCycle {
	return pkt // Default: no modification
}
func (d *DefaultHooks) OnDownstreamReady(pkt PacketWithCycle, cycle int) {}
func (d *DefaultHooks) OnDownstreamNotReady(pkt PacketWithCycle, cycle int) int {
	return cycle + 1 // Default: increment by 1
}
func (d *DefaultHooks) OnCycleEnd(cycle int) {}
