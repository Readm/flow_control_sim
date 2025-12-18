package node

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// RoutingStrategy defines how a router decides output ports for a packet.
type RoutingStrategy interface {
	// Route returns the indices of output queues the packet should be sent to.
	// outputCount is the number of available output queues.
	Route(pkt packet.Packet, outputCount int) ([]int, error)
}

// StaticRoutingTable implements a simple map-based routing.
type StaticRoutingTable struct {
	Routes map[int]int // TargetID -> OutputIndex
}

func NewStaticRoutingTable() *StaticRoutingTable {
	return &StaticRoutingTable{
		Routes: make(map[int]int),
	}
}

func (s *StaticRoutingTable) AddRoute(targetID, outputIndex int) {
	s.Routes[targetID] = outputIndex
}

func (s *StaticRoutingTable) Route(pkt packet.Packet, outputCount int) ([]int, error) {
	outIdx, ok := s.Routes[pkt.TargetID]
	if !ok {
		return nil, nil // Drop or error? For now drop (empty list)
	}
	if outIdx < 0 || outIdx >= outputCount {
		return nil, fmt.Errorf("invalid output index %d for target %d (max %d)", outIdx, pkt.TargetID, outputCount)
	}
	return []int{outIdx}, nil
}

// BroadcastRouting sends to all outputs.
type BroadcastRouting struct{}

func (b *BroadcastRouting) Route(pkt packet.Packet, outputCount int) ([]int, error) {
	routes := make([]int, outputCount)
	for i := 0; i < outputCount; i++ {
		routes[i] = i
	}
	return routes, nil
}

// RoundRobinRouting sends to outputs in a round-robin fashion.
type RoundRobinRouting struct {
	counter int
}

func (r *RoundRobinRouting) Route(pkt packet.Packet, outputCount int) ([]int, error) {
	if outputCount == 0 {
		return nil, nil
	}
	idx := r.counter % outputCount
	r.counter++
	return []int{idx}, nil
}
