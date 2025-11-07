package main

// SlaveNode (SN) represents a CHI Slave Node that processes requests and provides data.
// It processes CHI protocol requests in FIFO order and generates appropriate CHI responses.
type SlaveNode struct {
	Node        // embedded Node base class
	ProcessRate int
	queue       []*Packet

	// stats
	ProcessedCount int
	MaxQueueLength int
	TotalQueueSum  int64 // cumulative length for avg
	Samples        int64
}

func NewSlaveNode(id int, rate int) *SlaveNode {
	sn := &SlaveNode{
		Node: Node{
			ID:   id,
			Type: NodeTypeSN,
		},
		ProcessRate: rate,
		queue:       make([]*Packet, 0),
	}
	sn.AddQueue("request_queue", 0, DefaultSlaveQueueCapacity)
	return sn
}

// CanReceive checks if the SlaveNode can receive packets from the given edge.
// Checks if the request_queue has capacity.
func (sn *SlaveNode) CanReceive(edgeKey EdgeKey, packetCount int) bool {
	return len(sn.queue)+packetCount <= DefaultSlaveQueueCapacity
}

// OnPackets receives packets from the channel and enqueues them.
func (sn *SlaveNode) OnPackets(messages []*InFlightMessage, cycle int) {
	for _, msg := range messages {
		if msg.Packet != nil {
			msg.Packet.ReceivedAt = cycle
			sn.EnqueueRequest(msg.Packet)
		}
	}
}

func (sn *SlaveNode) EnqueueRequest(p *Packet) {
	// Note: ReceivedAt will be set by the simulator when the packet arrives
	sn.queue = append(sn.queue, p)
	if len(sn.queue) > sn.MaxQueueLength {
		sn.MaxQueueLength = len(sn.queue)
	}
	sn.UpdateQueue("request_queue", len(sn.queue))
}

// Tick processes up to ProcessRate requests from the head of queue and returns generated CHI responses.
// For ReadNoSnp transactions, generates CompData responses.
func (sn *SlaveNode) Tick(cycle int, packetIDs *PacketIDAllocator) []*Packet {
	sn.TotalQueueSum += int64(len(sn.queue))
	sn.Samples++

	if sn.ProcessRate <= 0 || len(sn.queue) == 0 {
		sn.UpdateQueue("request_queue", len(sn.queue))
		return nil
	}
	n := sn.ProcessRate
	if n > len(sn.queue) {
		n = len(sn.queue)
	}
	processed := sn.queue[:n]
	sn.queue = sn.queue[n:]

	responses := make([]*Packet, 0, n)
	for _, req := range processed {
		req.CompletedAt = cycle
		sn.ProcessedCount++

		// Generate CHI protocol response
		// For ReadNoSnp, generate CompData (Completion with Data)
		resp := sn.generateCHIResponse(req, cycle, packetIDs)
		responses = append(responses, resp)
	}
	sn.UpdateQueue("request_queue", len(sn.queue))
	return responses
}

// generateCHIResponse creates a CHI protocol response packet based on the request.
// For ReadNoSnp transactions, returns CompData response.
func (sn *SlaveNode) generateCHIResponse(req *Packet, cycle int, packetIDs *PacketIDAllocator) *Packet {
	resp := &Packet{
		ID:            packetIDs.Allocate(),
		Type:          "response", // legacy field
		SrcID:         sn.ID,
		DstID:         req.MasterID, // return to Request Node via Home Node
		SentAt:        cycle,
		RequestID:     req.RequestID,
		MasterID:      req.MasterID,
		Address:       req.Address,  // preserve address from request
		DataSize:      req.DataSize, // preserve data size from request
		TransactionID: req.TransactionID, // preserve transaction ID from request
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

func (sn *SlaveNode) QueueLength() int { return len(sn.queue) }

// GetQueuePackets returns packet information for the request_queue
func (sn *SlaveNode) GetQueuePackets() []PacketInfo {
	packets := make([]PacketInfo, 0, len(sn.queue))
	for _, p := range sn.queue {
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
