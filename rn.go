package main

import (
	"math/rand"
)

// RequestNode (RN) represents a CHI Request Node that initiates transactions.
// It generates CHI protocol requests and collects responses.
type RequestNode struct {
	Node        // embedded Node base class
	generator   RequestGenerator
	masterIndex int  // index of this master in the masters array (for generator)

	// track request generation times by request id
	generatedAtByReq map[int64]int

	// stats
	TotalRequests  int
	CompletedCount int
	TotalDelay     int64
	MaxDelay       int
	MinDelay       int

	// address generator for CHI transactions
	nextAddress uint64
}

func NewRequestNode(id int, masterIndex int, generator RequestGenerator) *RequestNode {
	rn := &RequestNode{
		Node: Node{
			ID:   id,
			Type: NodeTypeRN,
		},
		generator:        generator,
		masterIndex:     masterIndex,
		generatedAtByReq: make(map[int64]int),
		MinDelay:         int(^uint(0) >> 1), // max int
		nextAddress:      DefaultAddressBase,
	}
	rn.AddQueue("pending_requests", 0, DefaultRequestQueueCapacity)
	return rn
}

// GenerateReadNoSnpRequest creates a CHI ReadNoSnp transaction request packet.
// ReadNoSnp is a simple read request that does not require snoop operations.
func (rn *RequestNode) GenerateReadNoSnpRequest(reqID int64, cycle int, dstSNID int, homeNodeID int) *Packet {
	address := rn.nextAddress
	rn.nextAddress += DefaultCacheLineSize

	return &Packet{
		ID:              reqID,
		Type:            "request", // legacy field for compatibility
		SrcID:           rn.ID,
		DstID:           dstSNID, // final destination is Slave Node
		GeneratedAt:     cycle,
		SentAt:          cycle,
		MasterID:        rn.ID, // legacy field
		RequestID:       reqID,
		TransactionType: CHITxnReadNoSnp,
		MessageType:     CHIMsgReq,
		Address:         address,
		DataSize:        DefaultCacheLineSize,
	}
}

// Tick may generate request(s) per cycle based on the configured RequestGenerator.
// Supports generating multiple requests in the same cycle.
func (rn *RequestNode) Tick(cycle int, cfg *Config, homeNodeID int, ch *Link, packetIDs *PacketIDAllocator, slaves []*SlaveNode) {
	if homeNodeID < 0 {
		return
	}
	if rn.generator == nil {
		return
	}
	
	// Query generator for requests to generate
	results := rn.generator.ShouldGenerate(cycle, rn.masterIndex, len(slaves))
	if len(results) == 0 {
		return
	}
	
	// Generate all requested packets
	for _, result := range results {
		if !result.ShouldGenerate {
			continue
		}
		if result.SlaveIndex < 0 || result.SlaveIndex >= len(slaves) {
			continue
		}
		
		// Get actual slave node ID from the slaves array
		dstSNID := slaves[result.SlaveIndex].ID
		
		reqID := packetIDs.Allocate()
		
		// Determine address and data size
		address := result.Address
		if address == 0 {
			address = rn.nextAddress
			rn.nextAddress += DefaultCacheLineSize
		}
		
		dataSize := result.DataSize
		if dataSize == 0 {
			dataSize = DefaultCacheLineSize
		}
		
		// Determine transaction type
		txnType := result.TransactionType
		if txnType == "" {
			txnType = CHITxnReadNoSnp
		}
		
		// Generate request packet
		p := &Packet{
			ID:              reqID,
			Type:            "request", // legacy field
			SrcID:           rn.ID,
			DstID:           dstSNID,
			GeneratedAt:     cycle,
			SentAt:          cycle,
			MasterID:        rn.ID,
			RequestID:       reqID,
			TransactionType: txnType,
			MessageType:     CHIMsgReq,
			Address:         address,
			DataSize:        dataSize,
		}
		
		rn.TotalRequests++
		rn.generatedAtByReq[reqID] = cycle
		rn.UpdateQueue("pending_requests", len(rn.generatedAtByReq))
		
		// Send request to Home Node
		ch.Send(p, rn.ID, homeNodeID, cycle, cfg.MasterRelayLatency)
	}
}

// CanReceive checks if the RequestNode can receive packets from the given edge.
// RequestNode always can receive (unlimited capacity for pending_requests).
func (rn *RequestNode) CanReceive(edgeKey EdgeKey, packetCount int) bool {
	// pending_requests queue has unlimited capacity (-1)
	return true
}

// OnPackets receives packets from the channel and processes them as responses.
func (rn *RequestNode) OnPackets(messages []*InFlightMessage, cycle int) {
	for _, msg := range messages {
		if msg.Packet != nil {
			rn.OnResponse(msg.Packet, cycle)
		}
	}
}

// OnResponse processes a CHI response arriving to the request node at given cycle.
// Handles CompData responses for ReadNoSnp transactions.
func (rn *RequestNode) OnResponse(p *Packet, cycle int) {
	if p == nil {
		return
	}
	// Check for CHI response or legacy response type
	isCHIResponse := p.MessageType == CHIMsgComp || p.MessageType == CHIMsgResp
	isLegacyResponse := p.Type == "response"
	if !isCHIResponse && !isLegacyResponse {
		return
	}
	gen, ok := rn.generatedAtByReq[p.RequestID]
	if !ok {
		return
	}
	delay := cycle - gen
	rn.CompletedCount++
	rn.TotalDelay += int64(delay)
	if delay > rn.MaxDelay {
		rn.MaxDelay = delay
	}
	if delay < rn.MinDelay {
		rn.MinDelay = delay
	}
	delete(rn.generatedAtByReq, p.RequestID)
	rn.UpdateQueue("pending_requests", len(rn.generatedAtByReq))
}

type RequestNodeStats struct {
	TotalRequests     int
	CompletedRequests int
	AvgDelay          float64
	MaxDelay          int
	MinDelay          int
}

func (rn *RequestNode) SnapshotStats() *RequestNodeStats {
	var avg float64
	if rn.CompletedCount > 0 {
		avg = float64(rn.TotalDelay) / float64(rn.CompletedCount)
	}
	min := rn.MinDelay
	if rn.CompletedCount == 0 {
		min = 0
	}
	return &RequestNodeStats{
		TotalRequests:     rn.TotalRequests,
		CompletedRequests: rn.CompletedCount,
		AvgDelay:          avg,
		MaxDelay:          rn.MaxDelay,
		MinDelay:          min,
	}
}

// GetPendingRequests returns packet information for pending requests
// Since RequestNode doesn't store actual Packet objects, we return simplified info
// based on generatedAtByReq map
func (rn *RequestNode) GetPendingRequests() []PacketInfo {
	packets := make([]PacketInfo, 0, len(rn.generatedAtByReq))
	for reqID, genCycle := range rn.generatedAtByReq {
		packets = append(packets, PacketInfo{
			ID:              reqID,
			RequestID:       reqID,
			Type:            "request",
			SrcID:           rn.ID,
			MasterID:        rn.ID,
			GeneratedAt:     genCycle,
			TransactionType: CHITxnReadNoSnp,
			MessageType:     CHIMsgReq,
			// Other fields are not available since Packet is not stored
		})
	}
	return packets
}

// Legacy type aliases for backward compatibility during transition
// These are kept temporarily for compatibility but should be migrated to RequestNode
type Master = RequestNode
type MasterStats = RequestNodeStats

// NewMaster creates a new RequestNode (legacy compatibility function)
// Deprecated: Use NewRequestNode instead
// Note: This function is kept for backward compatibility but requires a generator
// For legacy code, create a ProbabilityGenerator first
func NewMaster(id int, masterIndex int, generator RequestGenerator) *Master {
	return NewRequestNode(id, masterIndex, generator)
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
