package main

import (
	"fmt"
	"log"
	"math/rand"
	"time"
)

type Simulator struct {
	Masters []*RequestNode
	Slaves  []*SlaveNode
	Relay   *HomeNode
	Chan    *Link

	masterByID map[int]*RequestNode
	slaveByID  map[int]*SlaveNode
	nodeLabels map[int]string
	edges      []EdgeSnapshot

	cfg        *Config
	rng        *rand.Rand
	pktIDs     *PacketIDAllocator
	txnMgr     *TransactionManager // Transaction manager for tracking transaction relationships
	current    int
	visualizer Visualizer

	isPaused  bool
	isRunning bool
}

// initializeSimulatorComponents creates and initializes all simulator components from config
func initializeSimulatorComponents(cfg *Config, rng *rand.Rand) (
	masters []*RequestNode,
	slaves []*SlaveNode,
	relay *HomeNode,
	ch *Link,
	masterByID map[int]*RequestNode,
	slaveByID map[int]*SlaveNode,
	labels map[int]string,
) {
	idAlloc := NewNodeIDAllocator()

	// Create request generators for each master
	// If RequestGenerators is provided, use it; otherwise use RequestGenerator for all
	// If no generator is set, create ProbabilityGenerator from RequestRateConfig
	generators := make([]RequestGenerator, cfg.NumMasters)

	// Prepare slave weights
	slaveWeights := cfg.SlaveWeights
	if len(slaveWeights) != cfg.NumSlaves {
		slaveWeights = make([]int, cfg.NumSlaves)
		for j := range slaveWeights {
			slaveWeights[j] = 1
		}
	}

	// Create default generator if needed
	if cfg.RequestGenerator == nil {
		if cfg.ScheduleConfig != nil && len(cfg.ScheduleConfig) > 0 {
			// Create ScheduleGenerator from ScheduleConfig
			cfg.RequestGenerator = NewScheduleGenerator(cfg.ScheduleConfig)
		} else if cfg.RequestRateConfig > 0 {
			// Create ProbabilityGenerator from RequestRateConfig
			cfg.RequestGenerator = NewProbabilityGenerator(cfg.RequestRateConfig, slaveWeights, rng)
		}
	}

	if cfg.RequestGenerators != nil && len(cfg.RequestGenerators) > 0 {
		// Use per-master generators
		for i := 0; i < cfg.NumMasters; i++ {
			if i < len(cfg.RequestGenerators) && cfg.RequestGenerators[i] != nil {
				generators[i] = cfg.RequestGenerators[i]
			} else if cfg.RequestGenerator != nil {
				generators[i] = cfg.RequestGenerator
			} else {
				// Fallback: create ProbabilityGenerator with RequestRateConfig or default
				rate := cfg.RequestRateConfig
				if rate <= 0 {
					rate = 0.5 // default
				}
				generators[i] = NewProbabilityGenerator(rate, slaveWeights, rng)
			}
		}
	} else {
		// Use default generator for all masters
		if cfg.RequestGenerator != nil {
			for i := 0; i < cfg.NumMasters; i++ {
				generators[i] = cfg.RequestGenerator
			}
		} else {
			// Fallback: create ProbabilityGenerator with RequestRateConfig or default
			rate := cfg.RequestRateConfig
			if rate <= 0 {
				rate = 0.5 // default
			}
			defaultGen := NewProbabilityGenerator(rate, slaveWeights, rng)
			for i := 0; i < cfg.NumMasters; i++ {
				generators[i] = defaultGen
			}
		}
	}

	masters = make([]*RequestNode, cfg.NumMasters)
	masterByID = make(map[int]*RequestNode, cfg.NumMasters)
	// Note: TransactionManager will be set after Simulator creation
	for i := 0; i < cfg.NumMasters; i++ {
		id := idAlloc.Allocate()
		m := NewRequestNode(id, i, generators[i])
		masters[i] = m
		masterByID[id] = m
	}

	slaves = make([]*SlaveNode, cfg.NumSlaves)
	slaveByID = make(map[int]*SlaveNode, cfg.NumSlaves)
	for i := 0; i < cfg.NumSlaves; i++ {
		id := idAlloc.Allocate()
		s := NewSlaveNode(id, cfg.SlaveProcessRate)
		slaves[i] = s
		slaveByID[id] = s
	}

	// single relay in phase one
	relayID := idAlloc.Allocate()
	relay = NewHomeNode(relayID)

	// Create node registry for link
	nodeRegistry := make(map[int]NodeReceiver)
	for _, m := range masters {
		nodeRegistry[m.ID] = m
	}
	for _, s := range slaves {
		nodeRegistry[s.ID] = s
	}
	if relay != nil {
		nodeRegistry[relay.ID] = relay
	}

	// Create link with bandwidth limit and node registry
	bandwidthLimit := cfg.BandwidthLimit
	if bandwidthLimit <= 0 {
		bandwidthLimit = DefaultBandwidthLimit
	}
	ch = NewLink(bandwidthLimit, nodeRegistry)

	// Create node labels
	labels = make(map[int]string, len(masters)+len(slaves)+1)
	for i, m := range masters {
		labels[m.ID] = fmt.Sprintf("RN %d", i) // Request Node
	}
	for i, s := range slaves {
		labels[s.ID] = fmt.Sprintf("SN %d", i) // Slave Node
	}
	labels[relay.ID] = "HN 0" // Home Node

	return masters, slaves, relay, ch, masterByID, slaveByID, labels
}

func NewSimulator(cfg *Config) *Simulator {
	pktAlloc := NewPacketIDAllocator()
	txnMgr := NewTransactionManager()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	masters, slaves, relay, ch, masterByID, slaveByID, labels := initializeSimulatorComponents(cfg, rng)

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
		txnMgr:     txnMgr,
		current:    0,
	}

	sim.edges = sim.buildEdges()
	sim.visualizer = sim.initVisualizer()
	
	// Set TransactionManager for all components
	for _, m := range sim.Masters {
		m.SetTransactionManager(sim.txnMgr)
	}
	for _, s := range sim.Slaves {
		s.SetTransactionManager(sim.txnMgr)
	}
	if sim.Relay != nil {
		sim.Relay.SetTransactionManager(sim.txnMgr)
	}
	if sim.Chan != nil {
		sim.Chan.SetTransactionManager(sim.txnMgr)
	}
	
	// Set node labels in TransactionManager
	sim.txnMgr.SetNodeLabels(sim.nodeLabels)
	
	// Configure packet history tracking
	// Default: enabled (true) if not explicitly disabled
	// Since bool zero value is false, we check if any history config is set
	// If MaxPacketHistorySize, HistoryOverflowMode, or MaxTransactionHistory are set,
	// we assume user wants to configure history, so we respect EnablePacketHistory
	// Otherwise, we default to enabled
	enableHistory := true
	if cfg.EnablePacketHistory == false {
		// Check if user explicitly configured history (any non-zero config)
		if cfg.MaxPacketHistorySize != 0 || cfg.HistoryOverflowMode != "" || cfg.MaxTransactionHistory != 0 {
			// User explicitly configured, respect the false setting
			enableHistory = false
		} else {
			// No explicit config, default to enabled
			enableHistory = true
		}
	}
	
	historyConfig := &PacketHistoryConfig{
		EnablePacketHistory:  enableHistory,
		MaxPacketHistorySize:  cfg.MaxPacketHistorySize,
		HistoryOverflowMode:   cfg.HistoryOverflowMode,
		MaxTransactionHistory: cfg.MaxTransactionHistory,
	}
	if historyConfig.HistoryOverflowMode == "" {
		historyConfig.HistoryOverflowMode = "circular"
	}
	if historyConfig.MaxTransactionHistory == 0 {
		historyConfig.MaxTransactionHistory = 1000
	}
	sim.txnMgr.SetHistoryConfig(historyConfig)

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
	viz := NewWebVisualizer(s.txnMgr)
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
	
	// Debug: log node counts
	if cycle == 0 {
		log.Printf("[DEBUG] buildFrame cycle 0: %d Masters, %d Slaves, Relay=%v", len(s.Masters), len(s.Slaves), s.Relay != nil)
	}

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
		// Get packet info for both queues separately
		stimulusPackets := m.GetStimulusQueuePackets()
		dispatchPackets := m.GetDispatchQueuePackets()
		// Clone queues and add packet info for both stimulus_queue and dispatch_queue
		queueInfo := m.GetQueueInfo()
		clonedQueues := cloneQueues(queueInfo)
		// Add packets to respective queues
		for i := range clonedQueues {
			if clonedQueues[i].Name == "stimulus_queue" {
				clonedQueues[i].Packets = stimulusPackets
			} else if clonedQueues[i].Name == "dispatch_queue" {
				clonedQueues[i].Packets = dispatchPackets
			}
		}
		// Get label with fallback
		label, ok := s.nodeLabels[m.ID]
		if !ok {
			label = fmt.Sprintf("RN %d", m.ID)
		}
		nodes = append(nodes, NodeSnapshot{
			ID:      m.ID,
			Type:    m.Type,
			Label:   label,
			Queues:  clonedQueues,
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
		// Get label with fallback
		label, ok := s.nodeLabels[sl.ID]
		if !ok {
			label = fmt.Sprintf("SN %d", sl.ID)
		}
		nodes = append(nodes, NodeSnapshot{
			ID:      sl.ID,
			Type:    sl.Type,
			Label:   label,
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
		// Get label with fallback
		label, ok := s.nodeLabels[s.Relay.ID]
		if !ok {
			label = "HN 0"
		}
		nodes = append(nodes, NodeSnapshot{
			ID:      s.Relay.ID,
			Type:    s.Relay.Type,
			Label:   label,
			Queues:  cloneQueuesWithPackets(s.Relay.GetQueueInfo(), "forward_queue", queuePackets),
			Payload: payload,
		})
	}

	// Get pipeline state from link
	pipelineState := s.Chan.GetPipelineState(cycle)

	// Update edges with pipeline stages
	edges := make([]EdgeSnapshot, len(s.edges))
	copy(edges, s.edges)
	for i := range edges {
		edgeKey := EdgeKey{FromID: edges[i].Source, ToID: edges[i].Target}
		edges[i].BandwidthLimit = s.cfg.BandwidthLimit
		if stages, exists := pipelineState[edgeKey]; exists {
			edges[i].PipelineStages = stages
		}
	}

	stats := s.CollectStats()
	configHash := computeConfigHash(s.cfg)
	
	// Build transaction graph (state providers are nil for now, will be implemented by other modules)
	var txnGraph *TransactionGraph
	if s.txnMgr != nil {
		txnGraph = s.txnMgr.GetTransactionGraph(nil, nil, nil)
	}
	
	frame := &SimulationFrame{
		Cycle:         cycle,
		Nodes:         nodes,
		Edges:         edges,
		InFlightCount: s.Chan.InFlightCount(),
		Stats:         stats,
		ConfigHash:    configHash,
		TransactionGraph: txnGraph,
	}
	return frame
}

func (s *Simulator) Run() {
	s.isRunning = true

	// If web frontend is available, start paused at cycle 0
	// Otherwise (headless mode), start running immediately
	if s.visualizer != nil && !s.visualizer.IsHeadless() {
		s.isPaused = true
		// Publish initial frame at cycle 0
		frame := s.buildFrame(0)
		s.visualizer.PublishFrame(frame)
	} else {
		s.isPaused = false
	}

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
					if cmd.ConfigOverride != nil {
						log.Printf("[DEBUG] Simulator received reset command with config: NumMasters=%d, NumSlaves=%d, TotalCycles=%d",
							cmd.ConfigOverride.NumMasters, cmd.ConfigOverride.NumSlaves, cmd.ConfigOverride.TotalCycles)
					} else {
						log.Printf("[DEBUG] Simulator received reset command without config override")
					}
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

		// Process link pipeline (handles packet movement and backpressure)
		s.Chan.Tick(cycle)

		relayID := -1
		if s.Relay != nil {
			relayID = s.Relay.ID
		}

		for _, m := range s.Masters {
			if relayID < 0 {
				continue
			}
			m.Tick(cycle, s.cfg, relayID, s.Chan, s.pktIDs, s.Slaves, s.txnMgr)
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

		// Periodic history cleanup (every 100 cycles)
		if cycle%100 == 0 && s.txnMgr != nil {
			s.txnMgr.CleanupHistory()
		}

		// Small delay to allow visualization updates
		if s.visualizer != nil && !s.visualizer.IsHeadless() {
			time.Sleep(DefaultVisualizationDelay)
		}
	}

	s.isRunning = false
}

func (s *Simulator) reset(newCfg *Config) {
	if newCfg != nil {
		log.Printf("[DEBUG] Simulator.reset: Applying new config: NumMasters=%d, NumSlaves=%d, TotalCycles=%d",
			newCfg.NumMasters, newCfg.NumSlaves, newCfg.TotalCycles)
		s.cfg = newCfg
	} else {
		log.Printf("[DEBUG] Simulator.reset: No new config provided, using existing config")
	}

	// Reinitialize simulator with new config
	pktAlloc := NewPacketIDAllocator()
	txnMgr := NewTransactionManager()
	masters, slaves, relay, ch, masterByID, slaveByID, labels := initializeSimulatorComponents(s.cfg, s.rng)

	s.Masters = masters
	s.Slaves = slaves
	s.Relay = relay
	s.Chan = ch
	s.masterByID = masterByID
	s.slaveByID = slaveByID
	s.nodeLabels = labels
	s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	s.pktIDs = pktAlloc
	s.txnMgr = txnMgr
	s.current = 0
	s.edges = s.buildEdges()
	
	// Set TransactionManager for all components
	for _, m := range s.Masters {
		m.SetTransactionManager(s.txnMgr)
	}
	for _, s := range s.Slaves {
		s.SetTransactionManager(s.txnMgr)
	}
	if s.Relay != nil {
		s.Relay.SetTransactionManager(s.txnMgr)
	}
	if s.Chan != nil {
		s.Chan.SetTransactionManager(s.txnMgr)
	}
	
	// Set node labels in TransactionManager
	s.txnMgr.SetNodeLabels(s.nodeLabels)
	
	// Configure packet history tracking
	// Default: enabled (true) if not explicitly disabled
	// Since bool zero value is false, we check if any history config is set
	// If MaxPacketHistorySize, HistoryOverflowMode, or MaxTransactionHistory are set,
	// we assume user wants to configure history, so we respect EnablePacketHistory
	// Otherwise, we default to enabled
	enableHistory := true
	if s.cfg.EnablePacketHistory == false {
		// Check if user explicitly configured history (any non-zero config)
		if s.cfg.MaxPacketHistorySize != 0 || s.cfg.HistoryOverflowMode != "" || s.cfg.MaxTransactionHistory != 0 {
			// User explicitly configured, respect the false setting
			enableHistory = false
		} else {
			// No explicit config, default to enabled
			enableHistory = true
		}
	}
	
	historyConfig := &PacketHistoryConfig{
		EnablePacketHistory:  enableHistory,
		MaxPacketHistorySize:  s.cfg.MaxPacketHistorySize,
		HistoryOverflowMode:   s.cfg.HistoryOverflowMode,
		MaxTransactionHistory: s.cfg.MaxTransactionHistory,
	}
	if historyConfig.HistoryOverflowMode == "" {
		historyConfig.HistoryOverflowMode = "circular"
	}
	if historyConfig.MaxTransactionHistory == 0 {
		historyConfig.MaxTransactionHistory = 1000
	}
	s.txnMgr.SetHistoryConfig(historyConfig)

	// If web frontend is available, pause at cycle 0 after reset
	// Otherwise (headless mode), continue running
	if s.visualizer != nil && !s.visualizer.IsHeadless() {
		s.isPaused = true
		// Publish frame at cycle 0 after reset
		frame := s.buildFrame(0)
		s.visualizer.PublishFrame(frame)
	} else {
		s.isPaused = false
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
	Global    *GlobalStats
	PerMaster []*RequestNodeStats
	PerSlave  []*SlaveNodeStats
}

func (s *Simulator) CollectStats() *SimulationStats {
	ms := make([]*RequestNodeStats, len(s.Masters))
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
	ss := make([]*SlaveNodeStats, len(s.Slaves))
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
