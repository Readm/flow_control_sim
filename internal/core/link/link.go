package link

import (
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow"
)

// LinkCycleProcessor is a custom cycle processor for Link that waits for Done(cycle-latency)
// instead of Done(cycle-1). This allows Link to process packets earlier, taking advantage
// of the latency buffer.
type LinkCycleProcessor struct {
	upstreamPort   ahead_port.AheadPort
	downstreamPort ahead_port.AheadPort
	processor      ahead_port.PacketProcessor
	latency        int
}

// Tick implements the cycle processing workflow with custom wait logic.
func (lcp *LinkCycleProcessor) Tick(cycle int) error {
	if lcp.processor == nil {
		panic("LinkCycleProcessor.processor is nil")
	}

	// Wait for upstream Done >= cycle-latency
	// This allows Link to start processing earlier, utilizing the latency buffer
	targetWaitCycle := cycle - lcp.latency
	lcp.upstreamPort.WaitForDone(targetWaitCycle)

	// Prepare updateUpstreamReady function
	var updateUpstreamReady func(cycle int, ready bool)
	if lcp.upstreamPort != nil {
		if updater, ok := lcp.upstreamPort.(interface{ UpdateReady(int, bool) }); ok && updater != nil {
			updateUpstreamReady = updater.UpdateReady
		} else {
			panic("upstreamPort does not implement UpdateReady interface")
		}
	} else {
		updateUpstreamReady = func(cycle int, ready bool) {}
	}

	// Process all packets
	lcp.processor.ProcessPackets(
		lcp.upstreamPort.ReceiveChan(),
		cycle,
		lcp.downstreamPort.Ready,
		lcp.sendPacket,
		lcp.downstreamPort.SetDone,
		updateUpstreamReady,
	)

	// SetDone after processing all packets
	currentDone := lcp.downstreamPort.GetDone()
	if currentDone < cycle {
		lcp.downstreamPort.SetDone(cycle)
	}

	// Assert that cycle+1 has been configured in upstream port
	if lcp.upstreamPort != nil {
		if checker, ok := lcp.upstreamPort.(interface{ ReadyNonBlocking(int) (bool, bool) }); ok && checker != nil {
			_, configured := checker.ReadyNonBlocking(cycle + 1)
			if !configured {
				panic(fmt.Sprintf("Tick(cycle=%d) completed but cycle+1=%d is not configured in upstream port. Processor must call updateUpstreamReady(cycle+1, ready) in ProcessPackets.", cycle, cycle+1))
			}
		}
	}

	return nil
}

// sendPacket sends a packet to downstream.
func (lcp *LinkCycleProcessor) sendPacket(pkt ahead_port.PacketWithCycle) {
	lcp.downstreamPort.SendChan() <- pkt
}

// Link represents a directed edge in the topology using AheadPort.
// Link receives packets from an upstream AheadPort (which can be a single port or an aggregator)
// and forwards them to a single downstream Pipeline.
// It implements latency and bandwidth constraints.
type Link struct {
	sourceID          int
	targetID          int
	channel           dataflow.Channel
	upstreamPort      ahead_port.AheadPort // Single upstream port (may be an aggregator)
	downstreamPort    ahead_port.AheadPort // Single downstream port to target Pipeline
	processor         *LinkCycleProcessor
	packetProc        *LinkPacketProcessor
	latency           int
	bandwidth         int
	totalBackpressure int
}

// LinkPacketProcessor implements PacketProcessor for Link.
// It handles latency (delaying packets) and bandwidth constraints.
type LinkPacketProcessor struct {
	link           *Link
	pendingPackets []ahead_port.PacketWithCycle
	// Slots for delayed delivery (ring buffer)
	slots [][]ahead_port.PacketWithCycle
}

// NewLinkPacketProcessor creates a new LinkPacketProcessor.
func NewLinkPacketProcessor(link *Link) *LinkPacketProcessor {
	slotCount := link.latency
	slots := make([][]ahead_port.PacketWithCycle, slotCount)
	return &LinkPacketProcessor{
		link:           link,
		slots:          slots,
		pendingPackets: make([]ahead_port.PacketWithCycle, 0),
	}
}

// ProcessPackets processes packets for Link: receive from upstream, apply latency, and send to downstream.
func (l *LinkPacketProcessor) ProcessPackets(
	receiveChan <-chan ahead_port.PacketWithCycle,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(ahead_port.PacketWithCycle),
	setDone func(int),
	updateUpstreamReady func(cycle int, ready bool),
) {
	// Link just transparently forwards the updateUpstreamReady call to the upstream ports.
	// The Ready state for cycle+1 is determined by checking downstream readiness.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		updateUpstreamReady(cycle+1, checkReady(cycle+1))
	}()

	newPendingPackets := make([]ahead_port.PacketWithCycle, 0)

	// Helper to add packet to slot
	addToSlot := func(pkt ahead_port.PacketWithCycle, targetCycle int) {
		// Calculate target slot index with backpressure adjustment.
		// Design guarantee: totalBackpressure will never exceed targetCycle (in practice)
		targetSlotIndex := (targetCycle - l.link.totalBackpressure) % len(l.slots)
		// Check bandwidth limit for the target slot
		if len(l.slots[targetSlotIndex]) >= l.link.bandwidth {
			panic(fmt.Sprintf("Slot is full (bandwidth limit exceeded) for targetSlotIndex %d at cycle %d", targetSlotIndex, cycle))
		} else {
			l.slots[targetSlotIndex] = append(l.slots[targetSlotIndex], pkt)
		}
	}

	// 1. Process pending packets (already have correct TargetCycle)
	for _, pkt := range l.pendingPackets {
		targetCycle := pkt.Cycle
		// Check if it fits in window
		// The ring buffer has 'latency' slots, covering [cycle, cycle + latency - 1].
		// If targetCycle >= cycle + latency, it doesn't fit in current window.
		if targetCycle-cycle >= l.link.latency {
			newPendingPackets = append(newPendingPackets, pkt)
			continue
		}
		addToSlot(pkt, targetCycle)
	}

	// 2. Receive and process new packets from channel
	for {
		select {
		case pkt := <-receiveChan:
			sourceCycle := pkt.Cycle
			targetCycle := sourceCycle + l.link.latency

			// Create packet with target cycle
			delayedPkt := ahead_port.PacketWithCycle{
				Cycle:  targetCycle,
				Packet: pkt.Packet,
			}

			// Check if it fits in window
			if targetCycle-cycle >= l.link.latency {
				newPendingPackets = append(newPendingPackets, delayedPkt)
				continue
			}
			addToSlot(delayedPkt, targetCycle)

		default:
			goto doneProcessing
		}
	}

doneProcessing:
	// Update pending packets
	l.pendingPackets = newPendingPackets

	// If the downstream is ready, send the packets from the slots.
	if checkReady(cycle) {
		// Calculate slot index with backpressure adjustment.
		slotIndex := (cycle - l.link.totalBackpressure) % len(l.slots)
		for _, pkt := range l.slots[slotIndex] {
			pkt.Cycle = cycle
			sendPacket(pkt)
		}
		l.slots[slotIndex] = nil // Clear the slot
	} else {
		// Downstream not ready: increment backpressure counter to delay slot access
		l.link.totalBackpressure = l.link.totalBackpressure + 1
	}

	// Set Done
	setDone(cycle)

	// Notify upstream that we are ready for next cycle using waitGroup to wait for completion
	// TODO: This may remove to further optimization
	wg.Wait()
}

// NewLink creates a link with the specified upstream port and downstream port.
// - sourceID: ID of the source node
// - targetID: ID of the target node
// - upstreamPort: AheadPort from source Flows (can be a single port or an aggregator)
// - downstreamPort: AheadPort to target Pipeline (single)
// - latency: number of cycles for packet delivery (defaults to 1 if 0)
// - bandwidth: maximum packets per cycle (defaults to 1 if 0)
func NewLink(sourceID int, targetID int, upstreamPort ahead_port.AheadPort, downstreamPort ahead_port.AheadPort, latency int, bandwidth int) *Link {
	if latency < 0 {
		panic("latency must not be negative")
	}
	if bandwidth <= 0 {
		panic("bandwidth must be positive")
	}
	if upstreamPort == nil {
		panic("Link requires an upstream port")
	}

	link := &Link{
		sourceID:          sourceID,
		targetID:          targetID,
		channel:           dataflow.ChannelREQ,
		upstreamPort:      upstreamPort,
		downstreamPort:    downstreamPort,
		latency:           latency,
		bandwidth:         bandwidth,
		totalBackpressure: 0,
	}

	// Create packet processor
	link.packetProc = NewLinkPacketProcessor(link)

	// Create custom cycle processor with latency-aware wait logic
	link.processor = &LinkCycleProcessor{
		upstreamPort:   upstreamPort,
		downstreamPort: downstreamPort,
		processor:      link.packetProc,
		latency:        latency,
	}

	return link
}

// SourceID returns the ID of the upstream node.
func (l *Link) SourceID() int {
	return l.sourceID
}

// TargetID returns the ID of the downstream node.
func (l *Link) TargetID() int {
	return l.targetID
}

// Channel returns the channel type carried by this link.
func (l *Link) Channel() dataflow.Channel {
	return l.channel
}

// SetChannel sets the channel type carried by this link.
func (l *Link) SetChannel(ch dataflow.Channel) {
	l.channel = ch
}

// Latency returns the configured delay in cycles.
func (l *Link) Latency() int {
	return l.latency
}

// Bandwidth returns the maximum packets per cycle.
func (l *Link) Bandwidth() int {
	return l.bandwidth
}

// Tick processes a single cycle.
func (l *Link) Tick(cycle int) error {
	return l.processor.Tick(cycle)
}

// UpstreamPort returns the upstream port.
func (l *Link) UpstreamPort() ahead_port.AheadPort {
	return l.upstreamPort
}

// DownstreamPort returns the downstream port.
func (l *Link) DownstreamPort() ahead_port.AheadPort {
	return l.downstreamPort
}

// SnapshotOccupancy reports the pending packet count per slot.
func (l *Link) SnapshotOccupancy() []int {
	if l.packetProc == nil {
		return nil
	}
	occupancy := make([]int, len(l.packetProc.slots))
	for i, slot := range l.packetProc.slots {
		occupancy[i] = len(slot)
	}
	return occupancy
}
