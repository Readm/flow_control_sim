package main

// EdgeKey represents a unique edge in the network (fromID -> toID)
type EdgeKey struct {
	FromID int `json:"fromID"`
	ToID   int `json:"toID"`
}

// InFlightMessage represents a CHI protocol packet that is currently in transit.
// It carries CHI messages (Req, Resp, Data, Comp) between nodes.
type InFlightMessage struct {
	Packet       *Packet // Contains CHI protocol fields (TransactionType, MessageType, etc.)
	FromID       int
	ToID         int
	ArrivalCycle int
}

// Slot represents a single stage in the pipeline with a capacity limit
type Slot struct {
	packets  []*InFlightMessage
	capacity int // equals bandwidthLimit
}

// pendingPacket represents a packet waiting in the send queue
type pendingPacket struct {
	msg   *InFlightMessage
	cycle int
}

// Pipeline represents a pipeline for a specific edge with latency stages
type Pipeline struct {
	latency   int
	slots     []*Slot         // fixed size: latency slots
	sendQueue []pendingPacket // packets waiting when bandwidth limit is reached
}

// NodeReceiver interface defines methods for nodes to receive packets from links
type NodeReceiver interface {
	CanReceive(edgeKey EdgeKey, packetCount int) bool
	OnPackets(messages []*InFlightMessage, cycle int)
}

// Link implements a pipeline-based link layer with bandwidth limits and backpressure.
// Each edge (fromID->toID) has an independent pipeline with latency stages.
type Link struct {
	bandwidthLimit int
	pipelines      map[EdgeKey]*Pipeline
	nodeRegistry   map[int]NodeReceiver // maps node ID to receiver interface
	txnMgr         *TransactionManager  // for recording packet events
}

// NewLink creates a new link with the given bandwidth limit and node registry
func NewLink(bandwidthLimit int, nodeRegistry map[int]NodeReceiver) *Link {
	return &Link{
		bandwidthLimit: bandwidthLimit,
		pipelines:      make(map[EdgeKey]*Pipeline),
		nodeRegistry:   nodeRegistry,
		txnMgr:         nil, // will be set by simulator
	}
}

// SetTransactionManager sets the transaction manager for event recording
func (c *Link) SetTransactionManager(txnMgr *TransactionManager) {
	c.txnMgr = txnMgr
}

// getOrCreatePipeline gets or creates a pipeline for the given edge key
func (c *Link) getOrCreatePipeline(edgeKey EdgeKey, latency int) *Pipeline {
	if pipeline, exists := c.pipelines[edgeKey]; exists {
		return pipeline
	}

	// Create new pipeline with latency slots
	slots := make([]*Slot, latency)
	for i := range slots {
		slots[i] = &Slot{
			packets:  make([]*InFlightMessage, 0),
			capacity: c.bandwidthLimit,
		}
	}

	pipeline := &Pipeline{
		latency:   latency,
		slots:     slots,
		sendQueue: make([]pendingPacket, 0),
	}
	c.pipelines[edgeKey] = pipeline
	return pipeline
}

// advanceSlots moves all packets forward in the pipeline (Slot[i+1] -> Slot[i])
func (p *Pipeline) advanceSlots() {
	if len(p.slots) == 0 {
		return
	}
	// Move from last to first (avoid overwriting before reading)
	for i := 0; i < len(p.slots)-1; i++ {
		p.slots[i].packets = p.slots[i+1].packets
	}
	// Clear the last slot after moving
	p.slots[len(p.slots)-1].packets = nil
}

// fillFromQueue fills the last slot (Slot[latency-1]) from the send queue
func (p *Pipeline) fillFromQueue(cycle int) {
	if len(p.slots) == 0 {
		return
	}
	lastSlot := p.slots[len(p.slots)-1]

	// Fill from queue up to capacity
	remaining := lastSlot.capacity - len(lastSlot.packets)
	if remaining <= 0 || len(p.sendQueue) == 0 {
		return
	}

	// Take up to remaining packets from queue
	toTake := remaining
	if toTake > len(p.sendQueue) {
		toTake = len(p.sendQueue)
	}

	for i := 0; i < toTake; i++ {
		lastSlot.packets = append(lastSlot.packets, p.sendQueue[i].msg)
	}
	p.sendQueue = p.sendQueue[toTake:]
}

// Send enqueues a CHI protocol packet into the pipeline.
// The packet enters the last slot (Slot[latency-1]) if bandwidth allows, otherwise queues.
// IMPORTANT:
// - If latency=0: packet is delivered immediately in the same cycle
// - If latency>0: packet enters pipeline in the NEXT cycle (cycle N+1), arrives in cycle N+latency
// This ensures packets take the full configured latency (latency cycles from send to receive).
func (c *Link) Send(packet *Packet, fromID, toID, currentCycle, latency int) {
	edgeKey := EdgeKey{FromID: fromID, ToID: toID}

	msg := &InFlightMessage{
		Packet:       packet,
		FromID:       fromID,
		ToID:         toID,
		ArrivalCycle: currentCycle + latency,
	}

	// Record PacketSent event
	if c.txnMgr != nil && packet != nil && packet.TransactionID > 0 {
		event := &PacketEvent{
			TransactionID:  packet.TransactionID,
			PacketID:       packet.ID,
			ParentPacketID: packet.ParentPacketID,
			NodeID:         fromID,
			EventType:      PacketSent,
			Cycle:          currentCycle,
			EdgeKey:        &edgeKey,
		}
		c.txnMgr.RecordPacketEvent(event)
	}

	// Handle latency=0 case: deliver immediately
	if latency == 0 {
		receiver, exists := c.nodeRegistry[toID]
		if exists && receiver.CanReceive(edgeKey, 1) {
			// Record PacketInTransitEnd event (packet leaving pipeline immediately)
			if c.txnMgr != nil && packet != nil && packet.TransactionID > 0 {
				event := &PacketEvent{
					TransactionID:  packet.TransactionID,
					PacketID:       packet.ID,
					ParentPacketID: packet.ParentPacketID,
					NodeID:         toID,
					EventType:      PacketInTransitEnd,
					Cycle:          currentCycle,
					EdgeKey:        &edgeKey,
				}
				c.txnMgr.RecordPacketEvent(event)
			}
			receiver.OnPackets([]*InFlightMessage{msg}, currentCycle)
		}
		return
	}

	// For latency > 0: add to send queue, will enter pipeline in next cycle
	pipeline := c.getOrCreatePipeline(edgeKey, latency)
	pipeline.sendQueue = append(pipeline.sendQueue, pendingPacket{
		msg:   msg,
		cycle: currentCycle,
	})
}

// Tick processes one cycle of the link, handling backpressure and packet movement
func (c *Link) Tick(cycle int) {
	for edgeKey, pipeline := range c.pipelines {
		// Check if Slot[0] has packets ready to arrive
		if len(pipeline.slots[0].packets) > 0 {
			// Try to receive at destination node
			receiver, exists := c.nodeRegistry[edgeKey.ToID]
			if exists && receiver.CanReceive(edgeKey, len(pipeline.slots[0].packets)) {
				// Successfully received: move slots and fill from queue
				arrivals := pipeline.slots[0].packets

				// Record PacketInTransitEnd events (packet leaving pipeline)
				if c.txnMgr != nil {
					for _, msg := range arrivals {
						if msg.Packet != nil && msg.Packet.TransactionID > 0 {
							event := &PacketEvent{
								TransactionID:  msg.Packet.TransactionID,
								PacketID:       msg.Packet.ID,
								ParentPacketID: msg.Packet.ParentPacketID,
								NodeID:         edgeKey.ToID,
								EventType:      PacketInTransitEnd,
								Cycle:          cycle,
								EdgeKey:        &edgeKey,
							}
							c.txnMgr.RecordPacketEvent(event)
						}
					}
				}

				pipeline.slots[0].packets = nil
				receiver.OnPackets(arrivals, cycle)
				pipeline.advanceSlots()
				pipeline.fillFromQueue(cycle)
			}
			// Backpressure: slots remain unchanged, no new packets accepted
		} else {
			// Slot[0] is empty, normal advancement
			pipeline.advanceSlots()
			pipeline.fillFromQueue(cycle)
		}
	}
}

// InFlightCount returns the total number of packets in all pipelines
func (c *Link) InFlightCount() int {
	total := 0
	for _, pipeline := range c.pipelines {
		for _, slot := range pipeline.slots {
			total += len(slot.packets)
		}
		total += len(pipeline.sendQueue)
	}
	return total
}

// GetPipelineState returns the current state of all pipelines for visualization
func (c *Link) GetPipelineState(cycle int) map[EdgeKey][]PipelineStageInfo {
	result := make(map[EdgeKey][]PipelineStageInfo)
	for edgeKey, pipeline := range c.pipelines {
		stages := make([]PipelineStageInfo, pipeline.latency)
		for i, slot := range pipeline.slots {
			stages[i] = PipelineStageInfo{
				StageIndex:  i,
				PacketCount: len(slot.packets),
			}
		}
		result[edgeKey] = stages
	}
	return result
}
