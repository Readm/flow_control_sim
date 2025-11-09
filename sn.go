package main

import (
	"sync"

	"github.com/example/flow_sim/queue"
)

// SlaveNode (SN) represents a CHI Slave Node that processes requests and provides data.
// It processes CHI protocol requests in FIFO order and generates appropriate CHI responses.
type SlaveNode struct {
	Node        // embedded Node base class
	ProcessRate int
	queue       *queue.TrackedQueue[*Packet]
	txnMgr      *TransactionManager // for recording packet events

	// stats
	ProcessedCount int
	MaxQueueLength int
	TotalQueueSum  int64 // cumulative length for avg
	Samples        int64

	mu       sync.Mutex
	bindings *NodeCycleBindings
}

func NewSlaveNode(id int, rate int) *SlaveNode {
	sn := &SlaveNode{
		Node: Node{
			ID:   id,
			Type: NodeTypeSN,
		},
		ProcessRate: rate,
		txnMgr:      nil, // will be set by simulator
		bindings:    NewNodeCycleBindings(),
	}
	sn.AddQueue("request_queue", 0, DefaultSlaveQueueCapacity)
	sn.queue = queue.NewTrackedQueue("request_queue", DefaultSlaveQueueCapacity, sn.makeQueueMutator(), queue.QueueHooks[*Packet]{
		OnEnqueue: sn.enqueueHook,
		OnDequeue: sn.dequeueHook,
	})
	return sn
}

func (sn *SlaveNode) makeQueueMutator() queue.MutateFunc {
	return func(length int, capacity int) {
		sn.UpdateQueueState("request_queue", length, capacity)
		if length > sn.MaxQueueLength {
			sn.MaxQueueLength = length
		}
	}
}

func (sn *SlaveNode) enqueueHook(p *Packet, cycle int) {
	if sn.txnMgr == nil || p == nil || p.TransactionID == 0 {
		return
	}
	event := &PacketEvent{
		TransactionID:  p.TransactionID,
		PacketID:       p.ID,
		ParentPacketID: p.ParentPacketID,
		NodeID:         sn.ID,
		EventType:      PacketEnqueued,
		Cycle:          cycle,
		EdgeKey:        nil,
	}
	sn.txnMgr.RecordPacketEvent(event)
}

func (sn *SlaveNode) dequeueHook(p *Packet, cycle int) {
	if sn.txnMgr == nil || p == nil || p.TransactionID == 0 {
		return
	}
	event := &PacketEvent{
		TransactionID:  p.TransactionID,
		PacketID:       p.ID,
		ParentPacketID: p.ParentPacketID,
		NodeID:         sn.ID,
		EventType:      PacketDequeued,
		Cycle:          cycle,
		EdgeKey:        nil,
	}
	sn.txnMgr.RecordPacketEvent(event)
}

// SetTransactionManager sets the transaction manager for event recording
func (sn *SlaveNode) SetTransactionManager(txnMgr *TransactionManager) {
	sn.txnMgr = txnMgr
}

// ConfigureCycleRuntime sets the coordinator bindings for the node.
func (sn *SlaveNode) ConfigureCycleRuntime(componentID string, coord *CycleCoordinator) {
	if sn.bindings == nil {
		sn.bindings = NewNodeCycleBindings()
	}
	sn.bindings.SetComponent(componentID)
	sn.bindings.SetCoordinator(coord)
}

// RegisterIncomingSignal registers the receive-finished signal for an incoming edge.
func (sn *SlaveNode) RegisterIncomingSignal(edge EdgeKey, signal *CycleSignal) {
	if sn.bindings == nil {
		sn.bindings = NewNodeCycleBindings()
	}
	sn.bindings.RegisterIncoming(edge, signal)
}

// RegisterOutgoingSignal registers the send-finished signal for an outgoing edge.
func (sn *SlaveNode) RegisterOutgoingSignal(edge EdgeKey, signal *CycleSignal) {
	if sn.bindings == nil {
		sn.bindings = NewNodeCycleBindings()
	}
	sn.bindings.RegisterOutgoing(edge, signal)
}

// CanReceive checks if the SlaveNode can receive packets from the given edge.
// Checks if the request_queue has capacity.
func (sn *SlaveNode) CanReceive(edgeKey EdgeKey, packetCount int) bool {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	return sn.queue.CanAccept(packetCount)
}

// OnPackets receives packets from the channel and enqueues them.
func (sn *SlaveNode) OnPackets(messages []*InFlightMessage, cycle int) {
	for _, msg := range messages {
		sn.processIncomingMessage(msg, cycle)
	}
}

func (sn *SlaveNode) EnqueueRequest(p *Packet) {
	if p == nil {
		return
	}
	sn.mu.Lock()
	defer sn.mu.Unlock()

	sn.queue.Enqueue(p, p.ReceivedAt)
}

// Tick processes up to ProcessRate requests from the head of queue and returns generated CHI responses.
// For ReadNoSnp transactions, generates CompData responses.
func (sn *SlaveNode) Tick(cycle int, packetIDs *PacketIDAllocator) []*Packet {
	sn.mu.Lock()
	queueLen := sn.queue.Len()
	sn.TotalQueueSum += int64(queueLen)
	sn.Samples++

	if sn.ProcessRate <= 0 || queueLen == 0 {
		sn.mu.Unlock()
		return nil
	}
	n := sn.ProcessRate
	if n > queueLen {
		n = queueLen
	}
	processed := make([]*Packet, 0, n)
	for i := 0; i < n; i++ {
		pkt, ok := sn.queue.PopFront(cycle)
		if !ok {
			break
		}
		processed = append(processed, pkt)
	}
	sn.mu.Unlock()

	responses := make([]*Packet, 0, len(processed))
	for _, req := range processed {
		// Record PacketProcessingStart event
		if sn.txnMgr != nil && req.TransactionID > 0 {
			event := &PacketEvent{
				TransactionID:  req.TransactionID,
				PacketID:       req.ID,
				ParentPacketID: req.ParentPacketID,
				NodeID:         sn.ID,
				EventType:      PacketProcessingStart,
				Cycle:          cycle,
				EdgeKey:        nil, // in-node event
			}
			sn.txnMgr.RecordPacketEvent(event)
		}

		req.CompletedAt = cycle
		sn.ProcessedCount++

		// Generate CHI protocol response
		// For ReadNoSnp, generate CompData (Completion with Data)
		resp := sn.generateCHIResponse(req, cycle, packetIDs)

		// Record PacketProcessingEnd event
		if sn.txnMgr != nil && req.TransactionID > 0 {
			event := &PacketEvent{
				TransactionID:  req.TransactionID,
				PacketID:       req.ID,
				ParentPacketID: req.ParentPacketID,
				NodeID:         sn.ID,
				EventType:      PacketProcessingEnd,
				Cycle:          cycle,
				EdgeKey:        nil, // in-node event
			}
			sn.txnMgr.RecordPacketEvent(event)
		}

		// Record PacketGenerated event for the response
		if sn.txnMgr != nil && resp.TransactionID > 0 {
			event := &PacketEvent{
				TransactionID:  resp.TransactionID,
				PacketID:       resp.ID,
				ParentPacketID: req.ID, // parent is the request packet
				NodeID:         sn.ID,
				EventType:      PacketGenerated,
				Cycle:          cycle,
				EdgeKey:        nil, // in-node event
			}
			sn.txnMgr.RecordPacketEvent(event)
		}

		responses = append(responses, resp)
	}

	sn.mu.Lock()
	sn.ProcessedCount += len(processed)
	sn.mu.Unlock()

	return responses
}

// SlaveNodeRuntime contains dependencies used during runtime execution.
type SlaveNodeRuntime struct {
	PacketAllocator *PacketIDAllocator
	Link            *Link
	RelayID         int
	RelayLatency    int
}

// RunRuntime executes the slave node logic driven by the coordinator.
func (sn *SlaveNode) RunRuntime(ctx *SlaveNodeRuntime) {
	if sn.bindings == nil {
		return
	}

	coord := sn.bindings.Coordinator()
	if coord == nil {
		return
	}
	componentID := sn.bindings.ComponentID()
	for {
		cycle := coord.WaitForCycle(componentID)
		if cycle < 0 {
			return
		}
		sn.bindings.WaitIncoming(cycle)
		sn.bindings.SignalReceive(cycle)
		responses := sn.Tick(cycle, ctx.PacketAllocator)
		if ctx.Link != nil && ctx.RelayID >= 0 {
			for _, resp := range responses {
				ctx.Link.Send(resp, sn.ID, ctx.RelayID, cycle, ctx.RelayLatency)
			}
		}
		sn.bindings.SignalSend(cycle)
		coord.MarkDone(componentID, cycle)
	}
}

// generateCHIResponse creates a CHI protocol response packet based on the request.
// For ReadNoSnp transactions, returns CompData response.
func (sn *SlaveNode) generateCHIResponse(req *Packet, cycle int, packetIDs *PacketIDAllocator) *Packet {
	resp := &Packet{
		ID:             packetIDs.Allocate(),
		Type:           "response", // legacy field
		SrcID:          sn.ID,
		DstID:          req.MasterID, // return to Request Node via Home Node
		SentAt:         cycle,
		RequestID:      req.RequestID,
		MasterID:       req.MasterID,
		Address:        req.Address,       // preserve address from request
		DataSize:       req.DataSize,      // preserve data size from request
		TransactionID:  req.TransactionID, // preserve transaction ID from request
		ParentPacketID: req.ID,            // parent is the request packet
	}

	// Set CHI protocol fields based on transaction type
	if req.TransactionType == CHITxnReadNoSnp {
		resp.TransactionType = CHITxnReadNoSnp
		resp.MessageType = CHIMsgComp
		resp.ResponseType = CHIRespCompData // Completion with Data for read
	} else if req.MessageType == CHIMsgReq {
		// Generic CHI request, generate CompData response
		resp.TransactionType = req.TransactionType
		resp.MessageType = CHIMsgComp
		resp.ResponseType = CHIRespCompData
	} else {
		// Legacy support
		resp.MessageType = CHIMsgResp
	}

	return resp
}

func (sn *SlaveNode) QueueLength() int {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	return sn.queue.Len()
}

// GetQueuePackets returns packet information for the request_queue
func (sn *SlaveNode) GetQueuePackets() []PacketInfo {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	packets := make([]PacketInfo, 0, sn.queue.Len())
	for _, p := range sn.queue.Items() {
		if p == nil {
			continue
		}
		packets = append(packets, PacketInfo{
			ID:              p.ID,
			Type:            p.Type,
			SrcID:           p.SrcID,
			DstID:           p.DstID,
			GeneratedAt:     p.GeneratedAt,
			SentAt:          p.SentAt,
			ReceivedAt:      p.ReceivedAt,
			CompletedAt:     p.CompletedAt,
			MasterID:        p.MasterID,
			RequestID:       p.RequestID,
			TransactionType: p.TransactionType,
			MessageType:     p.MessageType,
			ResponseType:    p.ResponseType,
			Address:         p.Address,
			DataSize:        p.DataSize,
			TransactionID:   p.TransactionID,
		})
	}
	return packets
}

type SlaveNodeStats struct {
	TotalProcessed int
	MaxQueueLength int
	AvgQueueLength float64
}

func (sn *SlaveNode) SnapshotStats() *SlaveNodeStats {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	var avg float64
	if sn.Samples > 0 {
		avg = float64(sn.TotalQueueSum) / float64(sn.Samples)
	}
	return &SlaveNodeStats{
		TotalProcessed: sn.ProcessedCount,
		MaxQueueLength: sn.MaxQueueLength,
		AvgQueueLength: avg,
	}
}

// Legacy type aliases for backward compatibility during transition
// These are kept temporarily for compatibility but should be migrated to SlaveNode
type Slave = SlaveNode
type SlaveStats = SlaveNodeStats

// NewSlave creates a new SlaveNode (legacy compatibility function)
// Deprecated: Use NewSlaveNode instead
func NewSlave(id int, rate int) *Slave {
	return NewSlaveNode(id, rate)
}

func (sn *SlaveNode) processIncomingMessage(msg *InFlightMessage, cycle int) {
	if msg == nil || msg.Packet == nil {
		return
	}
	packet := msg.Packet
	packet.ReceivedAt = cycle

	sn.mu.Lock()
	defer sn.mu.Unlock()

	// Record PacketReceived event
	if sn.txnMgr != nil && packet.TransactionID > 0 {
		event := &PacketEvent{
			TransactionID:  packet.TransactionID,
			PacketID:       packet.ID,
			ParentPacketID: packet.ParentPacketID,
			NodeID:         sn.ID,
			EventType:      PacketReceived,
			Cycle:          cycle,
			EdgeKey:        nil, // in-node event
		}
		sn.txnMgr.RecordPacketEvent(event)
	}

	sn.queue.Enqueue(packet, cycle)
}
