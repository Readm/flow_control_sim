package async_port

// PacketSource indicates where a packet comes from
type PacketSource int

const (
	PacketSourceReceived PacketSource = iota // Packet received in current cycle
	PacketSourcePending                      // Packet from previous cycle (pending)
)

// PacketWithSource represents a packet with its source information
type PacketWithSource struct {
	Packet PacketWithCycle
	Source PacketSource
}

// CycleProcessorHooks defines the customizable steps in the cycle processing workflow.
// Implementations can provide custom logic for each hook.
type CycleProcessorHooks interface {
	// OnCycleStart is called at the beginning of each cycle.
	// cycle: the current cycle number
	OnCycleStart(cycle int)

	// ProcessPackets processes all packets (both received from channel and pending).
	// This method should handle receiving packets from channel and the loop processing logic.
	// pendingPackets is a static variable internal to the hook implementation (stored as a struct field).
	// receiveChan: channel to receive packets from upstream (non-blocking, drain all available)
	// cycle: the current cycle number
	// checkReady: function to check if downstream is ready for a given cycle
	// sendPacket: function to send a packet to downstream (called when packet is ready)
	// setDoneUntil: function to set downstream DoneUntil
	ProcessPackets(receiveChan <-chan PacketWithCycle, cycle int, checkReady func(int) bool, sendPacket func(PacketWithCycle), setDoneUntil func(int))

	// OnCycleEnd is called at the end of each cycle.
	// cycle: the completed cycle number
	OnCycleEnd(cycle int)
}

// CycleProcessor provides the base workflow for processing cycles.
// It implements the template method pattern, where the overall flow is fixed,
// but specific steps can be customized through hooks.
type CycleProcessor struct {
	upstreamPort   ASyncPort // Port for receiving packets from upstream
	downstreamPort ASyncPort // Port for sending packets to downstream
	hooks          CycleProcessorHooks
}

// NewCycleProcessor creates a new cycle processor with the given ports and hooks.
// Both ports must implement ASyncPort interface.
// If hooks is nil, DefaultHooks will be used.
func NewCycleProcessor(upstreamPort ASyncPort, downstreamPort ASyncPort, hooks CycleProcessorHooks) *CycleProcessor {
	if hooks == nil {
		hooks = &DefaultHooks{}
	}
	return &CycleProcessor{
		upstreamPort:   upstreamPort,
		downstreamPort: downstreamPort,
		hooks:          hooks,
	}
}

// ProcessCycle implements the complete cycle processing workflow as defined in the documentation.
// This is the "template method" that defines the overall flow.
func (cp *CycleProcessor) ProcessCycle(cycle int) error {
	// Ensure hooks is not nil (should never happen if NewCycleProcessor is used correctly)
	if cp.hooks == nil {
		panic("CycleProcessor.hooks is nil, this should never happen. Use NewCycleProcessor to create CycleProcessor.")
	}

	// A: Start Cycle N
	cp.hooks.OnCycleStart(cycle)

	// Wait for upstream DoneUntil >= cycle
	// Uses condition variable to avoid busy waiting - goroutine will block until
	// SetDoneUntil is called and condition is satisfied
	cp.upstreamPort.WaitForDoneUntil(cycle)

	// H: Process all packets using hook
	// The hook is responsible for receiving packets from channel, processing them, and sending ready packets
	// pendingPackets is a static variable internal to the hook implementation
	cp.hooks.ProcessPackets(
		cp.upstreamPort.ReceiveChan(),
		cycle,
		cp.downstreamPort.Ready,
		cp.sendPacket,
		cp.downstreamPort.SetDoneUntil,
	)

	// F: SetDoneUntil after processing all packets
	// Only set downstream DoneUntil to cycle + 1 (current cycle completed) if it's not already larger
	// This ensures DoneUntil is monotonically increasing and doesn't decrease if Hook already set a larger value
	// Upstream DoneUntil should be set by upstream itself, not by this processor
	currentDoneUntil := cp.downstreamPort.GetDoneUntil()
	if currentDoneUntil < cycle+1 {
		cp.downstreamPort.SetDoneUntil(cycle + 1)
	}

	// P: N++ (handled by caller)
	// OnCycleEnd hook
	cp.hooks.OnCycleEnd(cycle)

	return nil
}

// sendPacket sends a packet to downstream.
// The packet's cycle should already be set correctly before calling this method.
func (cp *CycleProcessor) sendPacket(pkt PacketWithCycle) {
	cp.downstreamPort.Chan() <- pkt
}

// DefaultHooks provides default implementations for all hooks.
// Implementations can embed this and override only the hooks they need.
type DefaultHooks struct {
	// pendingPackets is a static variable internal to ProcessPackets
	// It stores packets that were not sent due to downstream not ready
	pendingPackets []PacketWithCycle
}

func (d *DefaultHooks) OnCycleStart(cycle int) {}

func (d *DefaultHooks) OnCycleEnd(cycle int) {}
func (d *DefaultHooks) ProcessPackets(receiveChan <-chan PacketWithCycle, cycle int, checkReady func(int) bool, sendPacket func(PacketWithCycle), setDoneUntil func(int)) {
	// pendingPackets is a static variable (struct field) - access directly
	newPendingPackets := make([]PacketWithCycle, 0)

	// Helper function to process a single packet
	processPacket := func(pkt PacketWithCycle) {
		pktCycle := int(pkt.Cycle)
		isReady := checkReady(pktCycle)
		if isReady {
			// Ready: send the packet immediately
			pkt.Cycle = uint64(pktCycle)
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
	// F: SetDoneUntil after processing all packets
	// Only set downstream DoneUntil to cycle + 1 (current cycle completed)
	setDoneUntil(cycle + 1)

	// Update pending packets (static variable - struct field)
	d.pendingPackets = newPendingPackets
}
