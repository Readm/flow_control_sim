package main

// Slave processes incoming request packets in FIFO order with a fixed rate.
type Slave struct {
	Node  // embedded Node base class
	ProcessRate    int
	queue          []*Packet

	// stats
	ProcessedCount int
	MaxQueueLength int
	TotalQueueSum  int64 // cumulative length for avg
	Samples        int64
}

func NewSlave(id int, rate int) *Slave {
	s := &Slave{
		Node: Node{
			ID:   id,
			Type: NodeTypeSlave,
		},
		ProcessRate: rate,
		queue:       make([]*Packet, 0),
	}
	s.AddQueue("request_queue", 0, -1) // unlimited capacity
	return s
}

func (s *Slave) EnqueueRequest(p *Packet) {
	s.queue = append(s.queue, p)
	if len(s.queue) > s.MaxQueueLength {
		s.MaxQueueLength = len(s.queue)
	}
	s.UpdateQueue("request_queue", len(s.queue))
}

// Tick processes up to ProcessRate requests from the head of queue and returns generated responses.
func (s *Slave) Tick(cycle int, packetIDs *PacketIDAllocator) []*Packet {
	s.TotalQueueSum += int64(len(s.queue))
	s.Samples++

	if s.ProcessRate <= 0 || len(s.queue) == 0 {
		s.UpdateQueue("request_queue", len(s.queue))
		return nil
	}
	n := s.ProcessRate
	if n > len(s.queue) {
		n = len(s.queue)
	}
	processed := s.queue[:n]
	s.queue = s.queue[n:]

	responses := make([]*Packet, 0, n)
	for _, req := range processed {
		req.CompletedAt = cycle
		s.ProcessedCount++

		// generate response packet
		resp := &Packet{
			ID:        packetIDs.Allocate(),
			Type:      "response",
			SrcID:     s.ID,
			DstID:     req.MasterID, // symmetric return to master
			SentAt:    cycle,        // will be updated by sender if needed
			RequestID: req.ID,
			MasterID:  req.MasterID,
		}
		responses = append(responses, resp)
	}
	s.UpdateQueue("request_queue", len(s.queue))
	return responses
}

func (s *Slave) QueueLength() int { return len(s.queue) }

type SlaveStats struct {
    TotalProcessed  int
    MaxQueueLength  int
    AvgQueueLength  float64
}

func (s *Slave) SnapshotStats() *SlaveStats {
    var avg float64
    if s.Samples > 0 {
        avg = float64(s.TotalQueueSum) / float64(s.Samples)
    }
    return &SlaveStats{
        TotalProcessed: s.ProcessedCount,
        MaxQueueLength: s.MaxQueueLength,
        AvgQueueLength: avg,
    }
}


