package main

import (
	"fmt"
	"math/rand"
	"time"
)

type Simulator struct {
	Masters []*Master
	Slaves  []*Slave
	Relay   *Relay
	Chan    *Channel

	masterByID map[int]*Master
	slaveByID  map[int]*Slave
	nodeLabels map[int]string
	edges      []EdgeSnapshot

	cfg        *Config
	rng        *rand.Rand
	pktIDs     *PacketIDAllocator
	current    int
	visualizer Visualizer

	isPaused bool
	isRunning bool
}

func NewSimulator(cfg *Config) *Simulator {
	idAlloc := NewNodeIDAllocator()
	pktAlloc := NewPacketIDAllocator()
	ch := NewChannel()

	masters := make([]*Master, cfg.NumMasters)
	masterByID := make(map[int]*Master, cfg.NumMasters)
	for i := 0; i < cfg.NumMasters; i++ {
		id := idAlloc.Allocate()
		m := NewMaster(id, cfg.RequestRate)
		masters[i] = m
		masterByID[id] = m
	}

	slaves := make([]*Slave, cfg.NumSlaves)
	slaveByID := make(map[int]*Slave, cfg.NumSlaves)
	for i := 0; i < cfg.NumSlaves; i++ {
		id := idAlloc.Allocate()
		s := NewSlave(id, cfg.SlaveProcessRate)
		slaves[i] = s
		slaveByID[id] = s
	}

	// single relay in phase one
	relayID := idAlloc.Allocate()
	relay := NewRelay(relayID)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	labels := make(map[int]string, len(masters)+len(slaves)+1)
	for i, m := range masters {
		labels[m.ID] = fmt.Sprintf("RN %d", i) // Request Node
	}
	for i, s := range slaves {
		labels[s.ID] = fmt.Sprintf("SN %d", i) // Slave Node
	}
	labels[relay.ID] = "HN 0" // Home Node

	sim := &Simulator{
		Masters:    masters,
		Slaves:     slaves,
		Relay:      relay,
		Chan:       ch,
		masterByID: masterByID,
		slaveByID:  slaveByID,
		nodeLabels: labels,
		cfg:        cfg,
		rng:        rng,
		pktIDs:     pktAlloc,
		current:    0,
	}

	sim.edges = sim.buildEdges()
	sim.visualizer = sim.initVisualizer()

	return sim
}

func (s *Simulator) initVisualizer() Visualizer {
	mode := s.cfg.VisualMode
	if mode == "" {
		mode = "web"
	}
	if s.cfg.Headless || mode == "none" {
		viz := NewNullVisualizer()
		viz.SetHeadless(true)
		return viz
	}
	viz := NewWebVisualizer()
	viz.SetHeadless(false)
	return viz
}

func (s *Simulator) buildEdges() []EdgeSnapshot {
	edges := make([]EdgeSnapshot, 0, (len(s.Masters)+len(s.Slaves))*2)
	if s.Relay == nil {
		return edges
	}
	homeNodeID := s.Relay.ID
	for _, m := range s.Masters {
		// CHI edges: RN -> HN (Req), HN -> RN (Comp)
		edges = append(edges,
			EdgeSnapshot{Source: m.ID, Target: homeNodeID, Label: "Req", Latency: s.cfg.MasterRelayLatency},
			EdgeSnapshot{Source: homeNodeID, Target: m.ID, Label: "Comp", Latency: s.cfg.RelayMasterLatency},
		)
	}
	for _, sl := range s.Slaves {
		// CHI edges: HN -> SN (Req), SN -> HN (Comp)
		edges = append(edges,
			EdgeSnapshot{Source: homeNodeID, Target: sl.ID, Label: "Req", Latency: s.cfg.RelaySlaveLatency},
			EdgeSnapshot{Source: sl.ID, Target: homeNodeID, Label: "Comp", Latency: s.cfg.SlaveRelayLatency},
		)
	}
	return edges
}

func cloneQueues(qs []QueueInfo) []QueueInfo {
	if len(qs) == 0 {
		return nil
	}
	res := make([]QueueInfo, len(qs))
	copy(res, qs)
	return res
}

// cloneQueuesWithPackets clones queues and fills packet information for specific queues
func cloneQueuesWithPackets(qs []QueueInfo, queueName string, packets []PacketInfo) []QueueInfo {
	if len(qs) == 0 {
		return nil
	}
	res := make([]QueueInfo, len(qs))
	for i, q := range qs {
		res[i] = QueueInfo{
			Name:     q.Name,
			Length:   q.Length,
			Capacity: q.Capacity,
			Packets:  nil,
		}
		if q.Name == queueName {
			res[i].Packets = make([]PacketInfo, len(packets))
			copy(res[i].Packets, packets)
		}
	}
	return res
}

func (s *Simulator) buildFrame(cycle int) *SimulationFrame {
	nodeCount := len(s.Masters) + len(s.Slaves)
	if s.Relay != nil {
		nodeCount++
	}
	nodes := make([]NodeSnapshot, 0, nodeCount)

	for _, m := range s.Masters {
		stats := m.SnapshotStats()
		payload := map[string]any{
			"totalRequests":     stats.TotalRequests,
			"completedRequests": stats.CompletedRequests,
			"avgDelay":          stats.AvgDelay,
			"maxDelay":          stats.MaxDelay,
			"minDelay":          stats.MinDelay,
			"nodeType":          "RN", // CHI Request Node
			"chiProtocol":       true,
		}
		pendingPackets := m.GetPendingRequests()
		nodes = append(nodes, NodeSnapshot{
			ID:      m.ID,
			Type:    m.Type,
			Label:   s.nodeLabels[m.ID],
			Queues:  cloneQueuesWithPackets(m.GetQueueInfo(), "pending_requests", pendingPackets),
			Payload: payload,
		})
	}

	for _, sl := range s.Slaves {
		stats := sl.SnapshotStats()
		payload := map[string]any{
			"totalProcessed": stats.TotalProcessed,
			"maxQueue":       stats.MaxQueueLength,
			"avgQueue":       stats.AvgQueueLength,
			"nodeType":       "SN", // CHI Slave Node
			"chiProtocol":    true,
		}
		queuePackets := sl.GetQueuePackets()
		nodes = append(nodes, NodeSnapshot{
			ID:      sl.ID,
			Type:    sl.Type,
			Label:   s.nodeLabels[sl.ID],
			Queues:  cloneQueuesWithPackets(sl.GetQueueInfo(), "request_queue", queuePackets),
			Payload: payload,
		})
	}

	if s.Relay != nil {
		// Get queue length from queue info
		queueInfo := s.Relay.GetQueueInfo()
		queueLength := 0
		for _, q := range queueInfo {
			if q.Name == "forward_queue" {
				queueLength = q.Length
				break
			}
		}
		payload := map[string]any{
			"queueLength": queueLength,
			"nodeType":    "HN", // CHI Home Node
			"chiProtocol": true,
		}
		queuePackets := s.Relay.GetQueuePackets()
		nodes = append(nodes, NodeSnapshot{
			ID:      s.Relay.ID,
			Type:    s.Relay.Type,
			Label:   s.nodeLabels[s.Relay.ID],
			Queues:  cloneQueuesWithPackets(s.Relay.GetQueueInfo(), "forward_queue", queuePackets),
			Payload: payload,
		})
	}

	stats := s.CollectStats()
	frame := &SimulationFrame{
		Cycle:         cycle,
		Nodes:         nodes,
		Edges:         s.edges,
		InFlightCount: s.Chan.InFlightCount(),
		Stats:         stats,
	}
	return frame
}

func (s *Simulator) Run() {
	s.isRunning = true
	s.isPaused = false

	for s.current < s.cfg.TotalCycles {
		// Check for control commands (except step, handled in paused section)
		stepCommandPending := false
		if s.visualizer != nil {
			cmd, hasCmd := s.visualizer.NextCommand()
			if hasCmd {
				switch cmd.Type {
				case CommandPause:
					s.isPaused = true
				case CommandResume:
					s.isPaused = false
				case CommandReset:
					s.reset(cmd.ConfigOverride)
					continue
				case CommandStep:
					// Step command is handled in paused section below
					// Mark it as pending so we can process it there
					if s.isPaused {
						stepCommandPending = true
					}
					// If not paused, ignore step command (frontend should disable button)
				}
			}
		}

		// Wait if paused
		if s.isPaused {
			// Check for step command while paused
			if stepCommandPending {
				// Execute one cycle in step mode
				// Continue to execute cycle below (break out of pause wait)
				// isPaused remains true after execution
			} else {
				// No step command, wait normally
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}

		cycle := s.current
		s.current++

		arrivals := s.Chan.CollectArrivals(cycle)
		for _, a := range arrivals {
			if s.Relay != nil && a.ToID == s.Relay.ID {
				// CHI message arrives at Home Node
				s.Relay.OnPacket(a.Packet, cycle, s.Chan, s.cfg)
				continue
			}
			if m := s.masterByID[a.ToID]; m != nil {
				// CHI response arrives at Request Node
				// Check for CHI response or legacy response type
				isCHIResponse := a.Packet.MessageType == CHIMsgComp || a.Packet.MessageType == CHIMsgResp
				isLegacyResponse := a.Packet.Type == "response"
				if isCHIResponse || isLegacyResponse {
					m.OnResponse(a.Packet, cycle)
				}
				continue
			}
			if sl := s.slaveByID[a.ToID]; sl != nil {
				// CHI request arrives at Slave Node
				// Check for CHI request or legacy request type
				isCHIRequest := a.Packet.MessageType == CHIMsgReq
				isLegacyRequest := a.Packet.Type == "request"
				if isCHIRequest || isLegacyRequest {
					sl.EnqueueRequest(a.Packet)
				}
				continue
			}
		}

		relayID := -1
		if s.Relay != nil {
			relayID = s.Relay.ID
		}

		for _, m := range s.Masters {
			if relayID < 0 {
				continue
			}
			m.Tick(cycle, s.cfg, relayID, s.Chan, s.rng, s.pktIDs, s.Slaves)
		}

		if s.Relay != nil {
			s.Relay.Tick(cycle, s.Chan, s.cfg)
		}

		for _, sl := range s.Slaves {
			resps := sl.Tick(cycle, s.pktIDs)
			if relayID < 0 {
				continue
			}
			for _, p := range resps {
				s.Chan.Send(p, sl.ID, relayID, cycle, s.cfg.SlaveRelayLatency)
			}
		}

		if s.visualizer != nil && !s.visualizer.IsHeadless() {
			frame := s.buildFrame(cycle)
			s.visualizer.PublishFrame(frame)
		}

		// Small delay to allow visualization updates
		if s.visualizer != nil && !s.visualizer.IsHeadless() {
			time.Sleep(50 * time.Millisecond)
		}
	}

	s.isRunning = false
}

func (s *Simulator) reset(newCfg *Config) {
	if newCfg != nil {
		s.cfg = newCfg
	}

	// Reinitialize simulator with new config
	idAlloc := NewNodeIDAllocator()
	pktAlloc := NewPacketIDAllocator()
	ch := NewChannel()

	masters := make([]*Master, s.cfg.NumMasters)
	masterByID := make(map[int]*Master, s.cfg.NumMasters)
	for i := 0; i < s.cfg.NumMasters; i++ {
		id := idAlloc.Allocate()
		m := NewMaster(id, s.cfg.RequestRate)
		masters[i] = m
		masterByID[id] = m
	}

	slaves := make([]*Slave, s.cfg.NumSlaves)
	slaveByID := make(map[int]*Slave, s.cfg.NumSlaves)
	for i := 0; i < s.cfg.NumSlaves; i++ {
		id := idAlloc.Allocate()
		sl := NewSlave(id, s.cfg.SlaveProcessRate)
		slaves[i] = sl
		slaveByID[id] = sl
	}

	relayID := idAlloc.Allocate()
	relay := NewRelay(relayID)

	labels := make(map[int]string, len(masters)+len(slaves)+1)
	for i, m := range masters {
		labels[m.ID] = fmt.Sprintf("RN %d", i) // Request Node
	}
	for i, s := range slaves {
		labels[s.ID] = fmt.Sprintf("SN %d", i) // Slave Node
	}
	labels[relay.ID] = "HN 0" // Home Node

	s.Masters = masters
	s.Slaves = slaves
	s.Relay = relay
	s.Chan = ch
	s.masterByID = masterByID
	s.slaveByID = slaveByID
	s.nodeLabels = labels
	s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	s.pktIDs = pktAlloc
	s.current = 0
	s.edges = s.buildEdges()
	s.isPaused = false
}

type GlobalStats struct {
	TotalRequests    int
	Completed        int
	CompletionRate   float64
	AvgEndToEndDelay float64
	MaxDelay         int
	MinDelay         int
}

type SimulationStats struct {
	Global    *GlobalStats
	PerMaster []*MasterStats
	PerSlave  []*SlaveStats
}

func (s *Simulator) CollectStats() *SimulationStats {
	ms := make([]*MasterStats, len(s.Masters))
	totalReq := 0
	completed := 0
	var sumDelay int64
	maxDelay := 0
	minDelay := 0
	first := true
	for i, m := range s.Masters {
		st := m.SnapshotStats()
		ms[i] = st
		totalReq += st.TotalRequests
		completed += st.CompletedRequests
		sumDelay += int64(st.AvgDelay * float64(st.CompletedRequests))
		if st.CompletedRequests > 0 {
			if first {
				minDelay = st.MinDelay
				first = false
			}
			if st.MaxDelay > maxDelay {
				maxDelay = st.MaxDelay
			}
			if st.MinDelay < minDelay {
				minDelay = st.MinDelay
			}
		}
	}
	ss := make([]*SlaveStats, len(s.Slaves))
	for i, sl := range s.Slaves {
		ss[i] = sl.SnapshotStats()
	}
	var avg float64
	if completed > 0 {
		avg = float64(sumDelay) / float64(completed)
	}
	if completed == 0 {
		minDelay = 0
	}
	g := &GlobalStats{
		TotalRequests:    totalReq,
		Completed:        completed,
		CompletionRate:   percent(completed, totalReq),
		AvgEndToEndDelay: avg,
		MaxDelay:         maxDelay,
		MinDelay:         minDelay,
	}
	return &SimulationStats{Global: g, PerMaster: ms, PerSlave: ss}
}

func percent(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100.0
}

