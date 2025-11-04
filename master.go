package main

import (
	"math/rand"
)

// Master generates requests and collects responses.
type Master struct {
	Node        // embedded Node base class
	RequestRate float64

	// track request generation times by request id
	generatedAtByReq map[int64]int

	// stats
	TotalRequests  int
	CompletedCount int
	TotalDelay     int64
	MaxDelay       int
	MinDelay       int
}

func NewMaster(id int, requestRate float64) *Master {
	m := &Master{
		Node: Node{
			ID:   id,
			Type: NodeTypeMaster,
		},
		RequestRate:      requestRate,
		generatedAtByReq: make(map[int64]int),
		MinDelay:         int(^uint(0) >> 1), // max int
	}
	m.AddQueue("pending_requests", 0, -1) // unlimited capacity
	return m
}

// Tick may generate at most one request per cycle with probability RequestRate.
func (m *Master) Tick(cycle int, cfg *Config, relayID int, ch *Channel, rng *rand.Rand, packetIDs *PacketIDAllocator) {
	if relayID < 0 {
		return
	}
	if rng.Float64() >= m.RequestRate {
		return
	}
	// choose destination slave by weights
	dst := weightedChoose(rng, cfg.SlaveWeights)

	reqID := packetIDs.Allocate()
	p := &Packet{
		ID:          reqID,
		Type:        "request",
		SrcID:       m.ID,
		DstID:       dst,
		GeneratedAt: cycle,
		SentAt:      cycle,
		MasterID:    m.ID,
		RequestID:   reqID,
	}
	m.TotalRequests++
	m.generatedAtByReq[reqID] = cycle
	m.UpdateQueue("pending_requests", len(m.generatedAtByReq))

	ch.Send(p, m.ID, relayID, cycle, cfg.MasterRelayLatency)
}

// OnResponse processes a response arriving to the master at given cycle.
func (m *Master) OnResponse(p *Packet, cycle int) {
	if p == nil || p.Type != "response" {
		return
	}
	gen, ok := m.generatedAtByReq[p.RequestID]
	if !ok {
		return
	}
	delay := cycle - gen
	m.CompletedCount++
	m.TotalDelay += int64(delay)
	if delay > m.MaxDelay {
		m.MaxDelay = delay
	}
	if delay < m.MinDelay {
		m.MinDelay = delay
	}
	delete(m.generatedAtByReq, p.RequestID)
	m.UpdateQueue("pending_requests", len(m.generatedAtByReq))
}

type MasterStats struct {
	TotalRequests     int
	CompletedRequests int
	AvgDelay          float64
	MaxDelay          int
	MinDelay          int
}

func (m *Master) SnapshotStats() *MasterStats {
	var avg float64
	if m.CompletedCount > 0 {
		avg = float64(m.TotalDelay) / float64(m.CompletedCount)
	}
	min := m.MinDelay
	if m.CompletedCount == 0 {
		min = 0
	}
	return &MasterStats{
		TotalRequests:     m.TotalRequests,
		CompletedRequests: m.CompletedCount,
		AvgDelay:          avg,
		MaxDelay:          m.MaxDelay,
		MinDelay:          min,
	}
}

// weightedChoose returns an index in [0,len(weights)) with probability proportional to weights.
func weightedChoose(rng *rand.Rand, weights []int) int {
	if len(weights) == 0 {
		return 0
	}
	var sum int
	for _, w := range weights {
		if w > 0 {
			sum += w
		}
	}
	if sum <= 0 {
		// default to uniform among indices
		return rng.Intn(len(weights))
	}
	x := rng.Intn(sum)
	acc := 0
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		acc += w
		if x < acc {
			return i
		}
	}
	return len(weights) - 1
}
