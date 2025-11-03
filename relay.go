package main

// Relay forwards packets according to DstID with fixed per-link latencies.
// Relay now supports buffering packets in a queue.
type Relay struct {
	Node  // embedded Node base class
	queue []*Packet
}

func NewRelay(id int) *Relay {
	r := &Relay{
		Node: Node{
			ID:   id,
			Type: NodeTypeRelay,
		},
		queue: make([]*Packet, 0),
	}
	r.AddQueue("forward_queue", 0, -1) // unlimited capacity
	return r
}

// OnPacket enqueues a packet instead of forwarding immediately.
func (r *Relay) OnPacket(p *Packet, cycle int, ch *Channel, cfg *Config) {
	if p == nil {
		return
	}
	r.queue = append(r.queue, p)
	r.UpdateQueue("forward_queue", len(r.queue))
}

// Tick processes the queue and forwards packets with appropriate latencies.
// Returns the number of packets forwarded.
func (r *Relay) Tick(cycle int, ch *Channel, cfg *Config) int {
	if len(r.queue) == 0 {
		return 0
	}

	count := 0
	for _, p := range r.queue {
		var latency int
		var toID int
		switch p.Type {
		case "request":
			toID = p.DstID // target slave id
			latency = cfg.RelaySlaveLatency
		case "response":
			toID = p.DstID // target master id
			latency = cfg.RelayMasterLatency
		default:
			continue
		}
		p.SentAt = cycle
		ch.Send(p, r.ID, toID, cycle, latency)
		count++
	}

	// clear the queue after forwarding
	r.queue = r.queue[:0]
	r.UpdateQueue("forward_queue", 0)
	return count
}


