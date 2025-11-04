package main

// HomeNode (HN) represents a CHI Home Node that manages cache coherence and routes transactions.
// It receives requests from Request Nodes and forwards them to Slave Nodes,
// then routes responses back to the originating Request Node.
type HomeNode struct {
	Node  // embedded Node base class
	queue []*Packet
}

func NewHomeNode(id int) *HomeNode {
	hn := &HomeNode{
		Node: Node{
			ID:   id,
			Type: NodeTypeHN,
		},
		queue: make([]*Packet, 0),
	}
	hn.AddQueue("forward_queue", 0, -1) // unlimited capacity
	return hn
}

// OnPacket enqueues a CHI packet received at the Home Node.
// For ReadNoSnp requests, this will be forwarded to the target Slave Node.
// For responses from Slave Nodes, this will be forwarded back to the Request Node.
func (hn *HomeNode) OnPacket(p *Packet, cycle int, ch *Channel, cfg *Config) {
	if p == nil {
		return
	}
	p.ReceivedAt = cycle
	hn.queue = append(hn.queue, p)
	hn.UpdateQueue("forward_queue", len(hn.queue))
}

// Tick processes the queue and forwards CHI packets according to CHI protocol rules.
// For ReadNoSnp transactions:
//   - Requests from RN: forward to target SN
//   - CompData responses from SN: forward back to originating RN
func (hn *HomeNode) Tick(cycle int, ch *Channel, cfg *Config) int {
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

		p.SentAt = cycle
		ch.Send(p, hn.ID, toID, cycle, latency)
		count++
	}

	// clear the queue after forwarding
	hn.queue = hn.queue[:0]
	hn.UpdateQueue("forward_queue", 0)
	return count
}

// GetQueuePackets returns packet information for the forward_queue
func (hn *HomeNode) GetQueuePackets() []PacketInfo {
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
		})
	}
	return packets
}

// Legacy type alias for backward compatibility during transition
type Relay = HomeNode

func NewRelay(id int) *Relay {
	return NewHomeNode(id)
}


