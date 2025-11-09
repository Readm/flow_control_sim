package main

import "sync"

// HomeNode (HN) represents a CHI Home Node that manages cache coherence and routes transactions.
// It receives requests from Request Nodes and forwards them to Slave Nodes,
// then routes responses back to the originating Request Node.
type HomeNode struct {
	Node   // embedded Node base class
	queue  []*Packet
	txnMgr *TransactionManager // for recording packet events

	mu       sync.Mutex
	bindings *NodeCycleBindings
}

func NewHomeNode(id int) *HomeNode {
	hn := &HomeNode{
		Node: Node{
			ID:   id,
			Type: NodeTypeHN,
		},
		queue:    make([]*Packet, 0),
		txnMgr:   nil, // will be set by simulator
		bindings: NewNodeCycleBindings(),
	}
	hn.AddQueue("forward_queue", 0, DefaultForwardQueueCapacity)
	return hn
}

// SetTransactionManager sets the transaction manager for event recording
func (hn *HomeNode) SetTransactionManager(txnMgr *TransactionManager) {
	hn.txnMgr = txnMgr
}

// ConfigureCycleRuntime sets the coordinator bindings for the node.
func (hn *HomeNode) ConfigureCycleRuntime(componentID string, coord *CycleCoordinator) {
	if hn.bindings == nil {
		hn.bindings = NewNodeCycleBindings()
	}
	hn.bindings.SetComponent(componentID)
	hn.bindings.SetCoordinator(coord)
}

// RegisterIncomingSignal registers the receive-finished signal for an incoming edge.
func (hn *HomeNode) RegisterIncomingSignal(edge EdgeKey, signal *CycleSignal) {
	if hn.bindings == nil {
		hn.bindings = NewNodeCycleBindings()
	}
	hn.bindings.RegisterIncoming(edge, signal)
}

// RegisterOutgoingSignal registers the send-finished signal for an outgoing edge.
func (hn *HomeNode) RegisterOutgoingSignal(edge EdgeKey, signal *CycleSignal) {
	if hn.bindings == nil {
		hn.bindings = NewNodeCycleBindings()
	}
	hn.bindings.RegisterOutgoing(edge, signal)
}

// CanReceive checks if the HomeNode can receive packets from the given edge.
// HomeNode always can receive (unlimited capacity for forward_queue).
func (hn *HomeNode) CanReceive(edgeKey EdgeKey, packetCount int) bool {
	// forward_queue has unlimited capacity (-1)
	return true
}

// OnPackets receives packets from the channel and enqueues them.
func (hn *HomeNode) OnPackets(messages []*InFlightMessage, cycle int) {
	for _, msg := range messages {
		hn.processIncomingMessage(msg, cycle)
	}
}

// OnPacket enqueues a CHI packet received at the Home Node.
// For ReadNoSnp requests, this will be forwarded to the target Slave Node.
// For responses from Slave Nodes, this will be forwarded back to the Request Node.
// This method is kept for backward compatibility but is now called by OnPackets.
func (hn *HomeNode) OnPacket(p *Packet, cycle int, ch *Link, cfg *Config) {
	if p == nil {
		return
	}
	p.ReceivedAt = cycle
	hn.mu.Lock()
	hn.queue = append(hn.queue, p)
	hn.UpdateQueue("forward_queue", len(hn.queue))
	hn.mu.Unlock()
}

// Tick processes the queue and forwards CHI packets according to CHI protocol rules.
// For ReadNoSnp transactions:
//   - Requests from RN: forward to target SN
//   - CompData responses from SN: forward back to originating RN
func (hn *HomeNode) Tick(cycle int, ch *Link, cfg *Config) int {
	hn.mu.Lock()
	defer hn.mu.Unlock()

	if len(hn.queue) == 0 {
		return 0
	}

	count := 0
	for _, p := range hn.queue {
		var latency int
		var toID int

		// CHI protocol routing logic
		if p.MessageType == CHIMsgReq {
			// This is a request from Request Node, forward to Slave Node
			// For ReadNoSnp, the DstID already points to the target Slave Node
			toID = p.DstID
			latency = cfg.RelaySlaveLatency
		} else if p.MessageType == CHIMsgComp || p.MessageType == CHIMsgResp {
			// This is a response from Slave Node, forward back to Request Node
			// The MasterID field contains the original Request Node ID
			toID = p.MasterID
			latency = cfg.RelayMasterLatency
		} else if p.Type == "request" {
			// Legacy support: treat as request, forward to Slave Node
			toID = p.DstID
			latency = cfg.RelaySlaveLatency
		} else if p.Type == "response" {
			// Legacy support: treat as response, forward to Request Node
			toID = p.DstID
			latency = cfg.RelayMasterLatency
		} else {
			continue
		}

		// Record PacketDequeued event
		if hn.txnMgr != nil && p.TransactionID > 0 {
			event := &PacketEvent{
				TransactionID:  p.TransactionID,
				PacketID:       p.ID,
				ParentPacketID: p.ParentPacketID,
				NodeID:         hn.ID,
				EventType:      PacketDequeued,
				Cycle:          cycle,
				EdgeKey:        nil, // in-node event
			}
			hn.txnMgr.RecordPacketEvent(event)
		}

		p.SentAt = cycle
		ch.Send(p, hn.ID, toID, cycle, latency)
		count++
	}

	// clear the queue after forwarding
	hn.queue = hn.queue[:0]
	hn.UpdateQueue("forward_queue", 0)
	return count
}

// HomeNodeRuntime contains dependencies required during runtime execution.
type HomeNodeRuntime struct {
	Config *Config
	Link   *Link
}

// RunRuntime executes the home node logic driven by the coordinator.
func (hn *HomeNode) RunRuntime(ctx *HomeNodeRuntime) {
	if hn.bindings == nil {
		return
	}
	coord := hn.bindings.Coordinator()
	if coord == nil {
		return
	}
	componentID := hn.bindings.ComponentID()
	for {
		cycle := coord.WaitForCycle(componentID)
		if cycle < 0 {
			return
		}
		hn.bindings.WaitIncoming(cycle)
		hn.bindings.SignalReceive(cycle)
		hn.Tick(cycle, ctx.Link, ctx.Config)
		hn.bindings.SignalSend(cycle)
		coord.MarkDone(componentID, cycle)
	}
}

// GetQueuePackets returns packet information for the forward_queue
func (hn *HomeNode) GetQueuePackets() []PacketInfo {
	hn.mu.Lock()
	defer hn.mu.Unlock()
	packets := make([]PacketInfo, 0, len(hn.queue))
	for _, p := range hn.queue {
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

// Legacy type alias for backward compatibility during transition
// This is kept temporarily for compatibility but should be migrated to HomeNode
type Relay = HomeNode

// NewRelay creates a new HomeNode (legacy compatibility function)
// Deprecated: Use NewHomeNode instead
func NewRelay(id int) *Relay {
	return NewHomeNode(id)
}

func (hn *HomeNode) processIncomingMessage(msg *InFlightMessage, cycle int) {
	if msg == nil || msg.Packet == nil {
		return
	}

	packet := msg.Packet
	packet.ReceivedAt = cycle

	hn.mu.Lock()
	defer hn.mu.Unlock()

	// Record PacketReceived event
	if hn.txnMgr != nil && packet.TransactionID > 0 {
		event := &PacketEvent{
			TransactionID:  packet.TransactionID,
			PacketID:       packet.ID,
			ParentPacketID: packet.ParentPacketID,
			NodeID:         hn.ID,
			EventType:      PacketReceived,
			Cycle:          cycle,
			EdgeKey:        nil, // in-node event
		}
		hn.txnMgr.RecordPacketEvent(event)
	}

	// Record PacketEnqueued event
	if hn.txnMgr != nil && packet.TransactionID > 0 {
		event := &PacketEvent{
			TransactionID:  packet.TransactionID,
			PacketID:       packet.ID,
			ParentPacketID: packet.ParentPacketID,
			NodeID:         hn.ID,
			EventType:      PacketEnqueued,
			Cycle:          cycle,
			EdgeKey:        nil, // in-node event
		}
		hn.txnMgr.RecordPacketEvent(event)
	}

	hn.queue = append(hn.queue, packet)
	hn.UpdateQueue("forward_queue", len(hn.queue))
}
