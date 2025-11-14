package router

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/policy"
)

type DefaultRouter struct {
	graph *Graph
	mu    sync.RWMutex
	// map[source]map[target]nextHop
	nextHop map[int]map[int]int
}

const ringFinalTargetKey = "ring_final_target"

func NewDefaultRouter() *DefaultRouter {
	return &DefaultRouter{
		graph:   NewGraph(),
		nextHop: make(map[int]map[int]int),
	}
}

func (r *DefaultRouter) AddDirectedEdge(fromID, toID, latency int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.graph.AddEdge(fromID, toID, latency)
}

func (r *DefaultRouter) ResolveRoute(packet *core.Packet, sourceID int, defaultTarget int) (int, error) {
	if packet == nil {
		if defaultTarget != 0 {
			return defaultTarget, nil
		}
		return 0, fmt.Errorf("default router: packet nil and default target missing")
	}
	targetID := packet.DstID
	if targetID == 0 {
		targetID = defaultTarget
		if targetID == 0 {
			if value, ok := packet.GetMetadata(ringFinalTargetKey); ok {
				if v, err := strconv.Atoi(value); err == nil {
					targetID = v
				}
			}
		}
	}
	if targetID == 0 {
		return defaultTarget, nil
	}

	if sourceID == targetID {
		return targetID, nil
	}

	r.mu.RLock()
	if hopMap, ok := r.nextHop[sourceID]; ok {
		if hop, exists := hopMap[targetID]; exists {
			r.mu.RUnlock()
			return hop, nil
		}
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.nextHop[sourceID]; !ok {
		r.nextHop[sourceID] = make(map[int]int)
	}
	if hop, exists := r.nextHop[sourceID][targetID]; exists {
		return hop, nil
	}

	path, ok := r.graph.ShortestPath(sourceID, targetID)
	if !ok || len(path) < 2 {
		if defaultTarget != 0 {
			return defaultTarget, nil
		}
		return 0, fmt.Errorf("default router: path not found")
	}
	next := path[1]
	r.nextHop[sourceID][targetID] = next
	return next, nil
}

func (r *DefaultRouter) PolicyRouter() policy.Router {
	return policy.RouterFunc(r.ResolveRoute)
}
