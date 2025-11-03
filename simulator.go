package main

import (
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

	cfg        *Config
	rng        *rand.Rand
	pktIDs     *PacketIDAllocator
	current    int
	visualizer Visualizer
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

	sim := &Simulator{
		Masters:    masters,
		Slaves:     slaves,
		Relay:      relay,
		Chan:       ch,
		masterByID: masterByID,
		slaveByID:  slaveByID,
		cfg:        cfg,
		rng:        rng,
		pktIDs:     pktAlloc,
		current:    0,
	}

	// Initialize visualizer based on config
	if !cfg.Headless && cfg.VisualMode == "gui" {
		viz := NewFyneVisualizer()
		viz.SetHeadless(false)
		viz.Initialize()

		// Set visualizer for all nodes
		for _, m := range masters {
			m.SetVisualizer(viz)
			viz.UpdateNode(&m.Node)
		}
		for _, s := range slaves {
			s.SetVisualizer(viz)
			viz.UpdateNode(&s.Node)
		}
		relay.SetVisualizer(viz)
		viz.UpdateNode(&relay.Node)

		sim.visualizer = viz
	} else {
		// Headless mode - use nil visualizer
		viz := NewFyneVisualizer()
		viz.SetHeadless(true)
		sim.visualizer = viz
	}

	return sim
}

func (s *Simulator) Run() {
	for cycle := 0; cycle < s.cfg.TotalCycles; cycle++ {
		s.current = cycle

		// Handle pause/resume if visualizer is active
		if s.visualizer != nil && !s.visualizer.IsHeadless() {
			if fviz, ok := s.visualizer.(*FyneVisualizer); ok {
				// Check for pause
				for fviz.IsPaused() {
					if fviz.WaitForResume() {
						break
					}
					// Small sleep to avoid busy waiting
					time.Sleep(10 * time.Millisecond)
				}
				fviz.UpdateCycle(cycle)
			}
		}

		// 1) deliver arrivals to their destination
		arrivals := s.Chan.CollectArrivals(cycle)
		for _, a := range arrivals {
			if a.ToID == s.Relay.ID {
				s.Relay.OnPacket(a.Packet, cycle, s.Chan, s.cfg)
				continue
			}
			if m := s.masterByID[a.ToID]; m != nil {
				if a.Packet.Type == "response" {
					m.OnResponse(a.Packet, cycle)
				}
				continue
			}
			if sl := s.slaveByID[a.ToID]; sl != nil {
				if a.Packet.Type == "request" {
					sl.EnqueueRequest(a.Packet)
				}
				continue
			}
		}

		// 2) node ticks
		// masters may generate new requests to relay
		for _, m := range s.Masters {
			m.Tick(cycle, s.cfg, s.Relay.ID, s.Chan, s.rng, s.pktIDs)
		}

		// relay processes queue and forwards packets
		s.Relay.Tick(cycle, s.Chan, s.cfg)

		// slaves process queue and generate responses to relay
		for _, sl := range s.Slaves {
			resps := sl.Tick(cycle, s.pktIDs)
			for _, p := range resps {
				s.Chan.Send(p, sl.ID, s.Relay.ID, cycle, s.cfg.SlaveRelayLatency)
			}
		}

		// Update visualization after all nodes have been updated
		if s.visualizer != nil && !s.visualizer.IsHeadless() {
			for _, m := range s.Masters {
				m.UpdateVisualization()
			}
			for _, sl := range s.Slaves {
				sl.UpdateVisualization()
			}
			s.Relay.UpdateVisualization()

			if fviz, ok := s.visualizer.(*FyneVisualizer); ok {
				fviz.Render()
			}
		}
	}
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
    Global *GlobalStats
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
            if st.MaxDelay > maxDelay { maxDelay = st.MaxDelay }
            if st.MinDelay < minDelay { minDelay = st.MinDelay }
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
    if b == 0 { return 0 }
    return float64(a) / float64(b) * 100.0
}


