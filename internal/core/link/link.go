package link

import (
	"sync"

	"github.com/Readm/flow_sim/internal/core/cycle_port"
)

// Link represents a directed edge in the topology using CyclePort.
// Link can aggregate multiple upstream Flow output ports and forward to a single downstream Flow.
// It implements latency and bandwidth constraints.
type Link struct {
	sourceID          int
	targetID          int
	upstreamPorts     []cycle_port.CyclePort // Multiple upstream ports from source Flows
	downstreamPort    cycle_port.CyclePort   // Single downstream port to target Flow
	processor         *cycle_port.CycleProcessor
	packetProc        *LinkPacketProcessor
	latency           uint64
	bandwidth         uint64
	totalBackpressure uint64
}

// LinkPacketProcessor implements PacketProcessor for Link.
// It handles latency (delaying packets) and bandwidth constraints.
type LinkPacketProcessor struct {
	link           *Link
	pendingPackets []cycle_port.PacketWithCycle
	// Slots for delayed delivery (ring buffer)
	slots [][]cycle_port.PacketWithCycle
}

// NewLinkPacketProcessor creates a new LinkPacketProcessor.
func NewLinkPacketProcessor(link *Link) *LinkPacketProcessor {
	slotCount := link.latency
	if slotCount == 0 {
		slotCount = 1
	}
	slots := make([][]cycle_port.PacketWithCycle, slotCount)
	return &LinkPacketProcessor{
		link:           link,
		slots:          slots,
		pendingPackets: make([]cycle_port.PacketWithCycle, 0),
	}
}

// ProcessPackets processes packets for Link: receive from upstream, apply latency, and send to downstream.
func (l *LinkPacketProcessor) ProcessPackets(
	receiveChan <-chan cycle_port.PacketWithCycle,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(cycle_port.PacketWithCycle),
	setDoneUntil func(int),
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
	// Collect all incoming packets
	var incomingPackets []cycle_port.PacketWithCycle

	// Process pending packets first
	incomingPackets = append(incomingPackets, l.pendingPackets...)

	// Receive all available packets from channel (non-blocking, drain all)
	for {
		select {
		case pkt := <-receiveChan:
			incomingPackets = append(incomingPackets, pkt)
		default:
			goto process
		}
	}

process:
	// Process incoming packets: apply latency and bandwidth constraints
	newPendingPackets := make([]cycle_port.PacketWithCycle, 0)

	// Process new incoming packets: add latency and put into slots
	for _, pkt := range incomingPackets {
		sourceCycle := int(pkt.Cycle)
		targetCycle := sourceCycle + int(l.link.latency)
		if cycle < targetCycle {
			panic("Past cycle detected in link processing")
		}

		// Create packet with target cycle
		delayedPkt := cycle_port.PacketWithCycle{
			Cycle:  uint64(targetCycle),
			Packet: pkt.Packet,
		}

		// Future cycle: put into slot
		// If the target cycle is more than or equal to one full loop ahead, treat as after wraparound and put into pendingPackets.
		if targetCycle-cycle >= int(l.link.latency)-1 {
			newPendingPackets = append(newPendingPackets, delayedPkt)
			continue
		}
		// Calculate target slot index with backpressure adjustment.
		// Design guarantee: totalBackpressure will never exceed targetCycle because:
		// 1. totalBackpressure only increases when downstream is not ready for the current cycle
		// 2. targetCycle = sourceCycle + latency, and we only process packets where cycle >= targetCycle
		// 3. Therefore, targetCycle >= cycle >= totalBackpressure (in practice)
		// This ensures the subtraction (targetCycle - totalBackpressure) is always non-negative.
		targetSlotIndex := (targetCycle - int(l.link.totalBackpressure)) % len(l.slots)
		// Check bandwidth limit for the target slot
		// Design constraint: slot capacity = bandwidth. If slot is full, panic to enforce bandwidth limit.
		// Callers must ensure packets don't exceed bandwidth per cycle.
		if len(l.slots[targetSlotIndex]) >= int(l.link.bandwidth) {
			panic("Slot is full (bandwidth limit exceeded)")
		} else {
			l.slots[targetSlotIndex] = append(l.slots[targetSlotIndex], delayedPkt)
		}
	}

	// Update pending packets
	l.pendingPackets = newPendingPackets

	// If the downstream is ready, send the packets from the slots.
	if checkReady(cycle) {
		// Calculate slot index with backpressure adjustment.
		// Design guarantee: totalBackpressure will never exceed cycle because:
		// 1. totalBackpressure only increases when downstream is not ready
		// 2. We only process cycle N when upstream DoneUntil >= N
		// 3. totalBackpressure tracks how many cycles we've been blocked
		// 4. In practice, totalBackpressure <= cycle (blocked cycles <= current cycle)
		// This ensures the subtraction (cycle - totalBackpressure) is always non-negative.
		slotIndex := int(cycle-int(l.link.totalBackpressure)) % len(l.slots)
		for _, pkt := range l.slots[slotIndex] {
			pkt.Cycle = uint64(cycle)
			sendPacket(pkt)
		}
		l.slots[slotIndex] = nil // Clear the slot
	} else {
		// Downstream not ready: increment backpressure counter to delay slot access
		l.link.totalBackpressure = l.link.totalBackpressure + 1
	}

	// Set DoneUntil
	setDoneUntil(cycle + 1)

	// Notify upstream that we are ready for next cycle using waitGroup to wait for completion

	wg.Wait()
}

// NewLink creates a link with the specified upstream ports and downstream port.
// - sourceID: ID of the source node
// - targetID: ID of the target node
// - upstreamPorts: list of CyclePorts from source Flows (can be multiple)
// - downstreamPort: CyclePort to target Flow (single)
// - latency: number of cycles for packet delivery (defaults to 1 if 0)
// - bandwidth: maximum packets per cycle (defaults to 1 if 0)
func NewLink(sourceID int, targetID int, upstreamPorts []cycle_port.CyclePort, downstreamPort cycle_port.CyclePort, latency uint64, bandwidth uint64) *Link {
	if latency == 0 {
		latency = 1
	}
	if bandwidth == 0 {
		bandwidth = 1
	}
	if len(upstreamPorts) == 0 {
		panic("Link requires at least one upstream port")
	}

	link := &Link{
		sourceID:          sourceID,
		targetID:          targetID,
		upstreamPorts:     upstreamPorts,
		downstreamPort:    downstreamPort,
		latency:           latency,
		bandwidth:         bandwidth,
		totalBackpressure: 0,
	}

	// Create packet processor
	link.packetProc = NewLinkPacketProcessor(link)

	// Create multi-upstream port if multiple upstream ports
	var upstreamPort cycle_port.CyclePort
	if len(upstreamPorts) == 1 {
		upstreamPort = upstreamPorts[0]
	} else {
		upstreamPort = cycle_port.NewMultiUpstreamPort(upstreamPorts)
	}

	// Create cycle processor
	link.processor = cycle_port.NewCycleProcessor(upstreamPort, downstreamPort, link.packetProc)

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

// Latency returns the configured delay in cycles.
func (l *Link) Latency() uint64 {
	return l.latency
}

// Bandwidth returns the maximum packets per cycle.
func (l *Link) Bandwidth() uint64 {
	return l.bandwidth
}

// ProcessCycle processes a single cycle.
func (l *Link) ProcessCycle(cycle int) error {
	return l.processor.ProcessCycle(cycle)
}

// UpstreamPorts returns all upstream ports.
func (l *Link) UpstreamPorts() []cycle_port.CyclePort {
	return l.upstreamPorts
}

// DownstreamPort returns the downstream port.
func (l *Link) DownstreamPort() cycle_port.CyclePort {
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
