package main

import "sync"

// InFlightMessage represents a CHI protocol packet that is currently in transit.
// It carries CHI messages (Req, Resp, Data, Comp) between nodes.
type InFlightMessage struct {
	Packet       *Packet // Contains CHI protocol fields (TransactionType, MessageType, etc.)
	FromID       int
	ToID         int
	ArrivalCycle int
}

// NodeReceiver interface defines methods for nodes to receive packets from links
type NodeReceiver interface {
	CanReceive(edgeKey EdgeKey, packetCount int) bool
	OnPackets(messages []*InFlightMessage, cycle int)
}

// LinkEndpoint exposes the SFC/RFC signals for an edge endpoint.
type LinkEndpoint struct {
	SendFinished    *CycleSignal
	ReceiveFinished *CycleSignal
}

type pipelineSlot struct {
	packets    []*InFlightMessage
	logicCycle int
	capacity   int
}

type pendingPacket struct {
	msg        *InFlightMessage
	logicCycle int
}

type edgePipeline struct {
	key            EdgeKey
	latency        int
	bandwidthLimit int

	link *Link

	mu           sync.Mutex
	slots        []*pipelineSlot
	sendQueue    []pendingPacket
	backpressure map[int]bool

	sendFinished    *CycleSignal
	receiveFinished *CycleSignal

	coordinator *CycleCoordinator
	componentID string
	started     bool
}

// Link implements a pipeline-based link layer with bandwidth limits and backpressure.
// Each edge (fromID->toID) has an independent pipeline with latency stages.
type Link struct {
	bandwidthLimit int
	nodeRegistry   map[int]NodeReceiver // maps node ID to receiver interface
	txnMgr         *TransactionManager  // for recording packet events

	mu          sync.RWMutex
	pipelines   map[EdgeKey]*edgePipeline
	endpoints   map[EdgeKey]*LinkEndpoint
	coordinator *CycleCoordinator
}

// NewLink creates a new link with the given bandwidth limit and node registry.
func NewLink(bandwidthLimit int, nodeRegistry map[int]NodeReceiver) *Link {
	return &Link{
		bandwidthLimit: bandwidthLimit,
		nodeRegistry:   nodeRegistry,
		pipelines:      make(map[EdgeKey]*edgePipeline),
		endpoints:      make(map[EdgeKey]*LinkEndpoint),
	}
}

// SetTransactionManager sets the transaction manager for event recording.
func (c *Link) SetTransactionManager(txnMgr *TransactionManager) {
	c.txnMgr = txnMgr
}

// ConfigureCoordinator provides the cycle coordinator for all pipelines.
func (c *Link) ConfigureCoordinator(coordinator *CycleCoordinator) {
	c.mu.Lock()
	c.coordinator = coordinator
	for _, pipeline := range c.pipelines {
		pipeline.coordinator = coordinator
		pipeline.start()
	}
	c.mu.Unlock()
}

// EnsureEdge creates the pipeline for the given edge if it does not exist.
func (c *Link) EnsureEdge(edgeKey EdgeKey, latency int, componentID string) *LinkEndpoint {
	if latency <= 0 {
		latency = 1
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if endpoint, ok := c.endpoints[edgeKey]; ok {
		if pipeline := c.pipelines[edgeKey]; pipeline != nil {
			pipeline.componentID = componentID
			if c.coordinator != nil {
				pipeline.coordinator = c.coordinator
			}
		}
		return endpoint
	}

	sendSignal := NewCycleSignal(-1)
	receiveSignal := NewCycleSignal(-1)

	pipeline := &edgePipeline{
		key:             edgeKey,
		latency:         latency,
		bandwidthLimit:  c.bandwidthLimit,
		link:            c,
		slots:           make([]*pipelineSlot, latency),
		sendQueue:       make([]pendingPacket, 0),
		backpressure:    make(map[int]bool),
		sendFinished:    sendSignal,
		receiveFinished: receiveSignal,
		coordinator:     c.coordinator,
		componentID:     componentID,
	}
	for i := 0; i < latency; i++ {
		pipeline.slots[i] = &pipelineSlot{
			packets:    make([]*InFlightMessage, 0),
			logicCycle: 0,
			capacity:   c.bandwidthLimit,
		}
	}

	c.pipelines[edgeKey] = pipeline
	endpoint := &LinkEndpoint{
		SendFinished:    sendSignal,
		ReceiveFinished: receiveSignal,
	}
	c.endpoints[edgeKey] = endpoint

	pipeline.start()

	return endpoint
}

// Send enqueues a CHI protocol packet into the pipeline.
// - If latency=0: packet is delivered immediately in the same cycle.
// - If latency>0: packet enters pipeline in the NEXT cycle (cycle N+1), arrives in cycle N+latency.
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

	if latency == 0 {
		c.deliverImmediate(edgeKey, msg, currentCycle)
		return
	}

	c.mu.RLock()
	pipeline := c.pipelines[edgeKey]
	c.mu.RUnlock()
	if pipeline == nil {
		// Default componentID for late binding
		componentID := c.componentIDForEdge(edgeKey)
		c.EnsureEdge(edgeKey, latency, componentID)
		c.mu.RLock()
		pipeline = c.pipelines[edgeKey]
		c.mu.RUnlock()
	}

	pipeline.enqueue(msg, currentCycle+latency)
}

func (c *Link) componentIDForEdge(edgeKey EdgeKey) string {
	return "link-" + itoa(edgeKey.FromID) + "-" + itoa(edgeKey.ToID)
}

func (c *Link) deliverImmediate(edgeKey EdgeKey, msg *InFlightMessage, cycle int) {
	receiver, exists := c.nodeRegistry[edgeKey.ToID]
	if !exists || !receiver.CanReceive(edgeKey, 1) {
		return
	}

	if c.txnMgr != nil && msg.Packet != nil && msg.Packet.TransactionID > 0 {
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
	receiver.OnPackets([]*InFlightMessage{msg}, cycle)
}

func (ep *edgePipeline) enqueue(msg *InFlightMessage, logicCycle int) {
	ep.mu.Lock()
	ep.sendQueue = append(ep.sendQueue, pendingPacket{
		msg:        msg,
		logicCycle: logicCycle,
	})
	ep.mu.Unlock()
}

func (ep *edgePipeline) start() {
	ep.mu.Lock()
	if ep.started || ep.coordinator == nil {
		ep.mu.Unlock()
		return
	}
	ep.started = true
	ep.mu.Unlock()
	go ep.run()
}

func (ep *edgePipeline) run() {
	for {
		if ep.coordinator == nil {
			return
		}
		cycle := ep.coordinator.WaitForCycle(ep.componentID)
		if cycle < 0 {
			return
		}
		ep.processCycle(cycle)
		ep.coordinator.MarkDone(ep.componentID, cycle)
	}
}

func (ep *edgePipeline) processCycle(cycle int) {
	if ep.sendFinished != nil {
		ep.sendFinished.WaitUntil(cycle)
	}

	receiver := ep.link.nodeRegistry[ep.key.ToID]

	ep.mu.Lock()
	frontPackets := append([]*InFlightMessage(nil), ep.slots[0].packets...)
	frontReady := ep.slots[0].logicCycle
	ep.mu.Unlock()

	backpressure := false
	if len(frontPackets) > 0 && receiver != nil && frontReady <= cycle {
		if receiver.CanReceive(ep.key, len(frontPackets)) {
			ep.deliverPackets(cycle, frontPackets)
		} else {
			backpressure = true
		}
	}

	if backpressure {
		metrics.RecordBackpressure()
		ep.mu.Lock()
		ep.bumpSlotCyclesLocked()
		ep.backpressure[cycle] = true
		ep.mu.Unlock()
		if ep.receiveFinished != nil {
			ep.receiveFinished.Update(cycle)
		}
		return
	}

	ep.mu.Lock()
	if len(frontPackets) == 0 {
		// No packets at front, still advance pipeline movement.
		ep.backpressure[cycle] = false
	} else if frontReady <= cycle {
		// Delivery already happened in deliverPackets, clear the slot.
		ep.slots[0].packets = nil
		ep.slots[0].logicCycle = cycle
		ep.backpressure[cycle] = false
	}

	ep.shiftSlotsLocked()
	ep.fillTailLocked(cycle)
	ep.mu.Unlock()

	if ep.receiveFinished != nil {
		ep.receiveFinished.Update(cycle)
	}
}

func (ep *edgePipeline) deliverPackets(cycle int, packets []*InFlightMessage) {
	ep.mu.Lock()
	ep.slots[0].packets = nil
	ep.slots[0].logicCycle = cycle
	ep.mu.Unlock()

	if ep.link.txnMgr != nil {
		for _, msg := range packets {
			if msg.Packet != nil && msg.Packet.TransactionID > 0 {
				event := &PacketEvent{
					TransactionID:  msg.Packet.TransactionID,
					PacketID:       msg.Packet.ID,
					ParentPacketID: msg.Packet.ParentPacketID,
					NodeID:         ep.key.ToID,
					EventType:      PacketInTransitEnd,
					Cycle:          cycle,
					EdgeKey:        &ep.key,
				}
				ep.link.txnMgr.RecordPacketEvent(event)
			}
			msg.ArrivalCycle = cycle
		}
	}

	if receiver, exists := ep.link.nodeRegistry[ep.key.ToID]; exists {
		receiver.OnPackets(packets, cycle)
	}
}

func (ep *edgePipeline) shiftSlotsLocked() {
	if len(ep.slots) == 0 {
		return
	}
	for i := 0; i < len(ep.slots)-1; i++ {
		ep.slots[i].packets = ep.slots[i+1].packets
		ep.slots[i].logicCycle = ep.slots[i+1].logicCycle
	}
	ep.slots[len(ep.slots)-1].packets = nil
	ep.slots[len(ep.slots)-1].logicCycle = 0
}

func (ep *edgePipeline) fillTailLocked(cycle int) {
	if len(ep.slots) == 0 || len(ep.sendQueue) == 0 {
		return
	}
	tail := ep.slots[len(ep.slots)-1]
	remaining := tail.capacity - len(tail.packets)
	if remaining <= 0 {
		return
	}
	toTake := remaining
	if toTake > len(ep.sendQueue) {
		toTake = len(ep.sendQueue)
	}

	for i := 0; i < toTake; i++ {
		pp := ep.sendQueue[i]
		tail.packets = append(tail.packets, pp.msg)
		tail.logicCycle = pp.logicCycle
	}
	ep.sendQueue = ep.sendQueue[toTake:]
}

func (ep *edgePipeline) bumpSlotCyclesLocked() {
	for _, slot := range ep.slots {
		slot.logicCycle++
	}
}

func (ep *edgePipeline) pipelineState() []PipelineStageInfo {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	stages := make([]PipelineStageInfo, len(ep.slots))
	for i, slot := range ep.slots {
		stages[i] = PipelineStageInfo{
			StageIndex:  i,
			PacketCount: len(slot.packets),
			LogicCycle:  slot.logicCycle,
		}
	}
	return stages
}

// InFlightCount returns the total number of packets in all pipelines.
func (c *Link) InFlightCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	total := 0
	for _, pipeline := range c.pipelines {
		pipeline.mu.Lock()
		for _, slot := range pipeline.slots {
			total += len(slot.packets)
		}
		total += len(pipeline.sendQueue)
		pipeline.mu.Unlock()
	}
	return total
}

// GetPipelineState returns the current state of all pipelines for visualization.
func (c *Link) GetPipelineState(cycle int) map[EdgeKey][]PipelineStageInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[EdgeKey][]PipelineStageInfo, len(c.pipelines))
	for edgeKey, pipeline := range c.pipelines {
		result[edgeKey] = pipeline.pipelineState()
	}
	return result
}

// BackpressureHistory returns a copy of the backpressure history for the specified edge.
func (c *Link) BackpressureHistory(edgeKey EdgeKey) map[int]bool {
	c.mu.RLock()
	pipeline := c.pipelines[edgeKey]
	c.mu.RUnlock()
	if pipeline == nil {
		return nil
	}

	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()

	history := make(map[int]bool, len(pipeline.backpressure))
	for cycle, value := range pipeline.backpressure {
		history[cycle] = value
	}
	return history
}

// GetEndpoint returns the LinkEndpoint for a given edge.
func (c *Link) GetEndpoint(edgeKey EdgeKey) *LinkEndpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.endpoints[edgeKey]
}

// Helper: convert int to string without fmt to avoid heavy deps here.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return sign + string(buf[i:])
}
