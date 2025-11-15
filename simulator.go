package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/Readm/flow_sim/capabilities"
	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
	incentiveplugin "github.com/Readm/flow_sim/plugins/incentives"
	visualplugin "github.com/Readm/flow_sim/plugins/visualization"
	"github.com/Readm/flow_sim/policy"
	"github.com/Readm/flow_sim/router"
	simruntime "github.com/Readm/flow_sim/simulator"
	"github.com/Readm/flow_sim/visual"
)

const runCommandChunkSize = 1000

type Simulator struct {
	Masters []*RequestNode
	Slaves  []*SlaveNode
	Relay   *HomeNode
	Routers []*RingRouterNode
	Chan    *Link

	masterByID    map[int]*RequestNode
	slaveByID     map[int]*SlaveNode
	routerByID    map[int]*RingRouterNode
	routerForNode map[int]int
	nodeLabels    map[int]string
	edges         []EdgeSnapshot
	edgeLatencies map[EdgeKey]int
	ringOrder     []int

	cfg         *Config
	rng         *rand.Rand
	pktIDs      *PacketIDAllocator
	txnMgr      *TransactionManager // Transaction manager for tracking transaction relationships
	current     int
	visualizer  visual.Visualizer
	commandLoop *simruntime.CommandLoop[visual.ControlCommand]
	runner      *simruntime.Runner[visual.ControlCommand, *SimulationFrame]

	isPaused  bool
	isRunning bool

	pendingReset      *Config
	runBudget         int
	runChunkRemaining int
	runTarget         int

	coordinator  *CycleCoordinator
	componentIDs []string
	runtimeWG    sync.WaitGroup

	pluginBroker        *hooks.PluginBroker
	pluginReg           *hooks.Registry
	vizRegistered       bool
	incentiveRegistered bool
	txFactory           *TxFactory
	policyMgr           policy.Manager
}

type cycleSignaler interface {
	RegisterIncomingSignal(edge EdgeKey, signal *CycleSignal)
	RegisterOutgoingSignal(edge EdgeKey, signal *CycleSignal)
}

type visualizerCommandSource struct {
	visualizer visual.Visualizer
}

func (s *visualizerCommandSource) NextCommand() (visual.ControlCommand, bool) {
	if s == nil || s.visualizer == nil {
		return visual.ControlCommand{Type: visual.CommandNone}, false
	}
	return s.visualizer.NextCommand()
}

func (s *visualizerCommandSource) WaitCommand(ctx context.Context) (visual.ControlCommand, bool) {
	if s == nil || s.visualizer == nil {
		return visual.ControlCommand{Type: visual.CommandNone}, false
	}
	return s.visualizer.WaitCommand(ctx)
}

// initializeSimulatorComponents creates and initializes all simulator components from config
func initializeSimulatorComponents(cfg *Config, rng *rand.Rand) (
	masters []*RequestNode,
	slaves []*SlaveNode,
	relay *HomeNode,
	routers []*RingRouterNode,
	ch *Link,
	masterByID map[int]*RequestNode,
	slaveByID map[int]*SlaveNode,
	routerByID map[int]*RingRouterNode,
	routerForNode map[int]*RingRouterNode,
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
		if len(cfg.ScheduleConfig) > 0 {
			// Create ScheduleGenerator from ScheduleConfig
			cfg.RequestGenerator = NewScheduleGenerator(cfg.ScheduleConfig)
		} else if cfg.RequestRateConfig > 0 {
			// Create ProbabilityGenerator from RequestRateConfig
			cfg.RequestGenerator = NewProbabilityGenerator(cfg.RequestRateConfig, slaveWeights, rng)
		}
	}

	if len(cfg.RequestGenerators) > 0 {
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
		m.SetCacheCapacity(cfg.RequestCacheCapacity)
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
	relay.SetCacheCapacity(cfg.HomeCacheCapacity)

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

	labels = make(map[int]string, len(masters)+len(slaves)+1)
	for i, m := range masters {
		labels[m.ID] = fmt.Sprintf("RN %d", i)
	}
	for i, s := range slaves {
		labels[s.ID] = fmt.Sprintf("SN %d", i)
	}
	if relay != nil {
		labels[relay.ID] = "HN 0"
	}

	routerByID = make(map[int]*RingRouterNode)
	routerForNode = make(map[int]*RingRouterNode)
	if cfg.RingEnabled {
		routerCount := len(masters) + len(slaves)
		if relay != nil {
			routerCount++
		}
		routers = make([]*RingRouterNode, 0, routerCount)

		addRouter := func(nodeID int, label string) {
			routerID := idAlloc.Allocate()
			router := NewRingRouterNode(routerID)
			routers = append(routers, router)
			routerByID[routerID] = router
			routerForNode[nodeID] = router
			nodeRegistry[routerID] = router
			labels[routerID] = label
		}

		for i, m := range masters {
			addRouter(m.ID, fmt.Sprintf("RT RN %d", i))
			m.SetRouterID(routerForNode[m.ID].ID)
		}
		if relay != nil {
			addRouter(relay.ID, "RT HN 0")
			relay.SetRouterID(routerForNode[relay.ID].ID)
		}
		for i, s := range slaves {
			addRouter(s.ID, fmt.Sprintf("RT SN %d", i))
			s.SetRouterID(routerForNode[s.ID].ID)
		}
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
	return masters, slaves, relay, routers, ch, masterByID, slaveByID, routerByID, routerForNode, labels
}

func buildRingOrder(masters []*RequestNode, relay *HomeNode, slaves []*SlaveNode, routerForNode map[int]*RingRouterNode) []int {
	order := make([]int, 0, len(masters)+len(slaves)+1)
	if len(masters) > 0 {
		order = append(order, masters[0].ID)
	}
	if relay != nil {
		order = append(order, relay.ID)
	}
	for i := 1; i < len(masters); i++ {
		order = append(order, masters[i].ID)
	}
	for _, s := range slaves {
		order = append(order, s.ID)
	}
	seen := make(map[int]struct{}, len(order))
	unique := make([]int, 0, len(order))
	for _, id := range order {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if router, ok := routerForNode[id]; ok {
			unique = append(unique, router.ID)
		} else {
			unique = append(unique, id)
		}
	}
	return unique
}

func (s *Simulator) rebuildRuntimeHelpers() {
	var visualBridge *simruntime.VisualBridge[*SimulationFrame]
	if s.visualizer != nil {
		visualBridge = simruntime.NewVisualBridge[*SimulationFrame](s.visualizer.IsHeadless(), func(frame *SimulationFrame) {
			s.visualizer.PublishFrame(frame)
		})
	}

	var commandLoop *simruntime.CommandLoop[visual.ControlCommand]
	if s.visualizer != nil {
		commandLoop = simruntime.NewCommandLoop[visual.ControlCommand](
			&visualizerCommandSource{visualizer: s.visualizer},
			simruntime.CommandHandlerFunc[visual.ControlCommand](s.handleCommand),
		)
	} else {
		commandLoop = simruntime.NewCommandLoop[visual.ControlCommand](nil, simruntime.CommandHandlerFunc[visual.ControlCommand](s.handleCommand))
	}

	s.commandLoop = commandLoop
	if s.runner == nil {
		s.runner = simruntime.NewRunner[visual.ControlCommand, *SimulationFrame](commandLoop, visualBridge)
	} else {
		s.runner.UpdateCommandLoop(commandLoop)
		s.runner.UpdateVisualBridge(visualBridge)
	}
}

func (s *Simulator) visualEnabled() bool {
	return s.runner != nil && s.runner.VisualEnabled()
}

func (s *Simulator) setVisualizer(v visual.Visualizer) {
	s.visualizer = v
}

func (s *Simulator) configureVisualizer() {
	if s.pluginReg == nil {
		if s.visualizer == nil {
			v := visual.NewNullVisualizer()
			v.SetHeadless(true)
			s.setVisualizer(v)
		}
		s.rebuildRuntimeHelpers()
		return
	}

	if !s.vizRegistered {
		factories := map[string]visualplugin.Factory{
			"web": func() (visual.Visualizer, error) {
				v := NewWebVisualizer(s.txnMgr)
				v.SetHeadless(false)
				v.SetTransactionManager(s.txnMgr)
				v.SetPluginRegistry(s.pluginReg)
				return v, nil
			},
			"none": func() (visual.Visualizer, error) {
				v := visual.NewNullVisualizer()
				v.SetHeadless(true)
				return v, nil
			},
		}

		if err := visualplugin.Register(s.pluginReg, visualplugin.Options{
			Factories:     factories,
			SetVisualizer: s.setVisualizer,
		}); err != nil {
			GetLogger().Warnf("visualization plugin registration failed: %v", err)
			v := visual.NewNullVisualizer()
			v.SetHeadless(true)
			s.setVisualizer(v)
			s.rebuildRuntimeHelpers()
			return
		}
		s.vizRegistered = true
	}

	mode := s.cfg.VisualMode
	if mode == "" {
		mode = "web"
	}
	if s.cfg.Headless || mode == "none" {
		mode = "none"
	}

	if err := s.pluginReg.LoadGlobal([]string{"visualization/" + mode}); err != nil {
		GetLogger().Warnf("visualization plugin load failed: %v", err)
		v := visual.NewNullVisualizer()
		v.SetHeadless(true)
		s.setVisualizer(v)
	}

	if s.visualizer == nil {
		v := visual.NewNullVisualizer()
		v.SetHeadless(true)
		s.setVisualizer(v)
	}

	if viz, ok := s.visualizer.(interface{ SetTransactionManager(*TransactionManager) }); ok {
		viz.SetTransactionManager(s.txnMgr)
	}

	s.rebuildRuntimeHelpers()
}

func (s *Simulator) configureIncentives() {
	if s.pluginReg == nil {
		return
	}
	if !s.incentiveRegistered {
		factories := map[string]incentiveplugin.Factory{
			"random": s.randomIncentiveFactory(),
			"noop": func(b *hooks.PluginBroker) error {
				desc := hooks.PluginDescriptor{
					Name:        "incentive/noop",
					Category:    hooks.PluginCategoryInstrumentation,
					Description: "no-op incentive plugin",
				}
				b.RegisterPluginMetadata(desc)
				return nil
			},
		}
		if err := incentiveplugin.Register(s.pluginReg, incentiveplugin.Options{
			Factories: factories,
		}); err != nil {
			GetLogger().Warnf("incentive plugin registration failed: %v", err)
			return
		}
		s.incentiveRegistered = true
	}
	if len(s.cfg.Plugins.Incentives) == 0 {
		return
	}
	names := make([]string, 0, len(s.cfg.Plugins.Incentives))
	for _, name := range s.cfg.Plugins.Incentives {
		names = append(names, "incentive/"+name)
	}
	if err := s.pluginReg.LoadGlobal(names); err != nil {
		GetLogger().Warnf("incentive plugin load failed: %v", err)
	}
}

func (s *Simulator) randomIncentiveFactory() incentiveplugin.Factory {
	return func(b *hooks.PluginBroker) error {
		desc := hooks.PluginDescriptor{
			Name:        "incentive/random",
			Category:    hooks.PluginCategoryInstrumentation,
			Description: "random incentive sampler",
		}
		bundle := hooks.HookBundle{
			AfterProcess: []hooks.AfterProcessHook{
				func(ctx *hooks.ProcessContext) error {
					if ctx == nil || ctx.Packet == nil || s.rng == nil {
						return nil
					}
					if s.rng.Intn(100) < 5 {
						GetLogger().Debugf("[incentive] reward triggered node=%d packet=%d", ctx.NodeID, ctx.Packet.ID)
					}
					return nil
				},
			},
		}
		b.RegisterBundle(desc, bundle)
		return nil
	}
}

func NewSimulator(cfg *Config) *Simulator {
	if err := ValidateConfig(cfg); err != nil {
		GetLogger().Errorf("NewSimulator: invalid config: %v", err)
		return nil
	}
	factory := NewConfigGeneratorFactory()
	defaultGen := factory.BuildDefault(cfg)
	cfg.RequestGenerator = defaultGen
	cfg.RequestGenerators = factory.BuildPerMaster(cfg, defaultGen)

	pktAlloc := NewPacketIDAllocator()
	txnMgr := NewTransactionManager()
	broker := hooks.NewPluginBroker()
	registry := hooks.NewRegistry(broker)
	txFactory := NewTxFactory(broker, txnMgr, pktAlloc)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	masters, slaves, relay, routers, ch, masterByID, slaveByID, routerByID, routerForNode, labels := initializeSimulatorComponents(cfg, rng)

	sim := &Simulator{
		Masters:       masters,
		Slaves:        slaves,
		Relay:         relay,
		Routers:       routers,
		Chan:          ch,
		masterByID:    masterByID,
		slaveByID:     slaveByID,
		routerByID:    routerByID,
		routerForNode: make(map[int]int, len(routerForNode)),
		nodeLabels:    labels,
		cfg:           cfg,
		rng:           rng,
		pktIDs:        pktAlloc,
		txnMgr:        txnMgr,
		current:       0,
		pluginBroker:  broker,
		txFactory:     txFactory,
		policyMgr:     policy.NewDefaultManager(),
		pluginReg:     registry,
		edgeLatencies: make(map[EdgeKey]int),
	}
	fallbackRouter := router.NewDefaultRouter()
	for nodeID, router := range routerForNode {
		sim.routerForNode[nodeID] = router.ID
		fallbackRouter.AddDirectedEdge(nodeID, router.ID, 1)
		fallbackRouter.AddDirectedEdge(router.ID, nodeID, 1)
	}

	routerLocal := make(map[int][]int)
	for nodeID, routerID := range sim.routerForNode {
		routerLocal[routerID] = append(routerLocal[routerID], nodeID)
	}

	routerCaps := make(map[int]capabilities.RingRouterCapability)
	for _, router := range sim.Routers {
		cap := capabilities.NewRingRouterCapability(
			fmt.Sprintf("ring-router-forward-%d", router.ID),
			capabilities.RingRouterConfig{RouterID: router.ID},
		)
		router.RegisterCapability(cap)
		routerCaps[router.ID] = cap
	}

	if cfg.RingEnabled {
		interleaver := NewRingAddressInterleaver(slaves, cfg.RingInterleaveStride)
		for _, m := range masters {
			m.SetAddressMapper(interleaver)
		}
		sim.ringOrder = buildRingOrder(masters, relay, slaves, routerForNode)
		if len(sim.ringOrder) > 0 {
			for idx, routerID := range sim.ringOrder {
				nextID := sim.ringOrder[(idx+1)%len(sim.ringOrder)]
				local := routerLocal[routerID]
				routerCaps[routerID].Update(capabilities.RingRouterConfig{
					RouterID:     routerID,
					NextRouterID: nextID,
					LocalNodeIDs: local,
				})
				fallbackRouter.AddDirectedEdge(routerID, nextID, cfg.MasterRelayLatency)
			}
		}
	}

	if relay != nil {
		for _, m := range masters {
			fallbackRouter.AddDirectedEdge(m.ID, relay.ID, cfg.MasterRelayLatency)
			fallbackRouter.AddDirectedEdge(relay.ID, m.ID, cfg.RelayMasterLatency)
		}
		for _, s := range slaves {
			fallbackRouter.AddDirectedEdge(relay.ID, s.ID, cfg.RelaySlaveLatency)
			fallbackRouter.AddDirectedEdge(s.ID, relay.ID, cfg.SlaveRelayLatency)
		}
	}

	sim.policyMgr = policy.WithFallbackRouter(sim.policyMgr, fallbackRouter.PolicyRouter())

	for _, m := range sim.Masters {
		m.SetTxFactory(txFactory)
		m.SetPluginBroker(broker)
		m.SetPolicyManager(sim.policyMgr)
		m.SetPacketIDAllocator(sim.pktIDs)
	}
	for _, sl := range sim.Slaves {
		sl.SetPluginBroker(broker)
	}
	if sim.Relay != nil {
		sim.Relay.SetPluginBroker(broker)
		sim.Relay.SetPolicyManager(sim.policyMgr)
		sim.Relay.SetPacketIDAllocator(sim.pktIDs)
	}
	for _, router := range sim.Routers {
		router.SetPluginBroker(broker)
		router.SetTransactionManager(sim.txnMgr)
		router.SetPacketIDAllocator(sim.pktIDs)
	}

	sim.edges = sim.buildEdges()
	sim.configureVisualizer()
	sim.configureIncentives()

	if sim.visualEnabled() {
		sim.isPaused = true
		frame := sim.buildFrame(0)
		sim.runner.PublishFrame(frame)
	} else {
		sim.isPaused = false
	}

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
		EnablePacketHistory:   enableHistory,
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

	sim.initializeCycleRuntime()

	return sim
}

func (s *Simulator) initializeCycleRuntime() {
	componentIDs := make([]string, 0, len(s.Masters)+len(s.Slaves)+len(s.Routers)+len(s.edges)+1)
	for _, m := range s.Masters {
		componentIDs = append(componentIDs, fmt.Sprintf("node-%d", m.ID))
	}
	if s.Relay != nil {
		componentIDs = append(componentIDs, fmt.Sprintf("node-%d", s.Relay.ID))
	}
	for _, sl := range s.Slaves {
		componentIDs = append(componentIDs, fmt.Sprintf("node-%d", sl.ID))
	}
	for _, rt := range s.Routers {
		componentIDs = append(componentIDs, fmt.Sprintf("node-%d", rt.ID))
	}
	for _, edge := range s.edges {
		componentIDs = append(componentIDs, fmt.Sprintf("link-%d-%d", edge.Source, edge.Target))
	}

	s.componentIDs = componentIDs
	s.coordinator = NewCycleCoordinator(componentIDs)

	for _, m := range s.Masters {
		m.ConfigureCycleRuntime(fmt.Sprintf("node-%d", m.ID), s.coordinator)
	}
	if s.Relay != nil {
		s.Relay.ConfigureCycleRuntime(fmt.Sprintf("node-%d", s.Relay.ID), s.coordinator)
	}
	for _, sl := range s.Slaves {
		sl.ConfigureCycleRuntime(fmt.Sprintf("node-%d", sl.ID), s.coordinator)
	}
	for _, rt := range s.Routers {
		rt.ConfigureCycleRuntime(fmt.Sprintf("node-%d", rt.ID), s.coordinator)
	}

	s.Chan.ConfigureCoordinator(s.coordinator)
	for _, edge := range s.edges {
		edgeKey := EdgeKey{FromID: edge.Source, ToID: edge.Target}
		componentID := fmt.Sprintf("link-%d-%d", edge.Source, edge.Target)
		endpoint := s.Chan.EnsureEdge(edgeKey, edge.Latency, componentID)
		s.bindEdgeSignals(edgeKey, endpoint)
	}

	s.updateCoordinatorLimit()
}

func (s *Simulator) bindEdgeSignals(edgeKey EdgeKey, endpoint *LinkEndpoint) {
	if endpoint == nil {
		return
	}
	if sender := s.getCycleSignaler(edgeKey.FromID); sender != nil {
		sender.RegisterOutgoingSignal(edgeKey, endpoint.SendFinished)
	}
	if receiver := s.getCycleSignaler(edgeKey.ToID); receiver != nil {
		receiver.RegisterIncomingSignal(edgeKey, endpoint.ReceiveFinished)
	}
}

func (s *Simulator) getCycleSignaler(id int) cycleSignaler {
	if node, ok := s.masterByID[id]; ok {
		return node
	}
	if node, ok := s.slaveByID[id]; ok {
		return node
	}
	if s.Relay != nil && s.Relay.ID == id {
		return s.Relay
	}
	if node, ok := s.routerByID[id]; ok {
		return node
	}
	return nil
}

func (s *Simulator) startRuntimes() {
	s.runtimeWG = sync.WaitGroup{}

	relayID := -1
	if s.Relay != nil {
		relayID = s.Relay.ID
		ctx := &HomeNodeRuntime{
			Config: s.cfg,
			Link:   s.Chan,
		}
		s.runtimeWG.Add(1)
		go func(hn *HomeNode) {
			defer s.runtimeWG.Done()
			hn.RunRuntime(ctx)
		}(s.Relay)
	}

	for _, rn := range s.Masters {
		ctx := &RequestNodeRuntime{
			Config:             s.cfg,
			Link:               s.Chan,
			PacketAllocator:    s.pktIDs,
			Slaves:             s.Slaves,
			TransactionManager: s.txnMgr,
			HomeNodeID:         relayID,
		}
		s.runtimeWG.Add(1)
		go func(node *RequestNode) {
			defer s.runtimeWG.Done()
			node.RunRuntime(ctx)
		}(rn)
	}

	resolveLatency := func(fromID, toID int) int {
		if latency, ok := s.edgeLatencies[EdgeKey{FromID: fromID, ToID: toID}]; ok {
			return latency
		}
		return 1
	}

	for _, sl := range s.Slaves {
		routerID := s.routerForNode[sl.ID]
		routerLatency := resolveLatency(sl.ID, routerID)
		ctx := &SlaveNodeRuntime{
			PacketAllocator: s.pktIDs,
			Link:            s.Chan,
			RelayID:         relayID,
			RelayLatency:    s.cfg.SlaveRelayLatency,
			RouterID:        routerID,
			RouterLatency:   routerLatency,
		}
		s.runtimeWG.Add(1)
		go func(node *SlaveNode) {
			defer s.runtimeWG.Done()
			node.RunRuntime(ctx)
		}(sl)
	}

	for _, rt := range s.Routers {
		ctx := &RingRouterRuntime{
			Link: s.Chan,
			ResolveLatency: func(fromID, toID int) int {
				return resolveLatency(fromID, toID)
			},
			TransactionMgr:  s.txnMgr,
			PacketAllocator: s.pktIDs,
		}
		s.runtimeWG.Add(1)
		go func(router *RingRouterNode) {
			defer s.runtimeWG.Done()
			router.RunRuntime(ctx)
		}(rt)
	}

	s.updateCoordinatorLimit()
}

func (s *Simulator) updateCoordinatorLimit() {
	if s.coordinator == nil {
		return
	}

	maxLimit := s.cfg.TotalCycles - 1
	if maxLimit < -1 {
		maxLimit = -1
	}

	limit := maxLimit

	if s.runChunkRemaining > 0 {
		target := s.runTarget - 1
		if target > maxLimit {
			target = maxLimit
		}
		if target < s.current {
			target = s.current
		}
		if target < -1 {
			target = -1
		}
		limit = target
	} else if s.isPaused {
		target := s.current - 1
		if target > maxLimit {
			target = maxLimit
		}
		if target < -1 {
			target = -1
		}
		limit = target
	}

	s.coordinator.SetMaxTarget(limit)
}

func (s *Simulator) clearRunRequests() {
	s.runBudget = 0
	s.runChunkRemaining = 0
	s.runTarget = s.current
	s.isPaused = true
	s.updateCoordinatorLimit()
}

func (s *Simulator) scheduleNextRunChunk() {
	if s.cfg == nil {
		s.clearRunRequests()
		return
	}

	remainingTotal := s.cfg.TotalCycles - s.current
	if remainingTotal <= 0 {
		s.clearRunRequests()
		return
	}

	if s.runBudget <= 0 {
		s.runChunkRemaining = 0
		s.runTarget = s.current
		s.isPaused = true
		s.updateCoordinatorLimit()
		return
	}

	chunk := runCommandChunkSize
	if s.runBudget < chunk {
		chunk = s.runBudget
	}
	if remainingTotal < chunk {
		chunk = remainingTotal
	}
	if chunk <= 0 {
		s.clearRunRequests()
		return
	}

	s.runBudget -= chunk
	s.runChunkRemaining = chunk
	s.runTarget = s.current + chunk
	if s.runTarget > s.cfg.TotalCycles {
		s.runTarget = s.cfg.TotalCycles
	}
	s.isPaused = false
	s.updateCoordinatorLimit()
}

func (s *Simulator) onCycleAdvanced() {
	if s.runChunkRemaining > 0 {
		s.runChunkRemaining--
		if s.runChunkRemaining == 0 {
			s.scheduleNextRunChunk()
		}
	}
}

func (s *Simulator) buildEdges() []EdgeSnapshot {
	s.edgeLatencies = make(map[EdgeKey]int)
	if s.cfg != nil && s.cfg.RingEnabled && len(s.ringOrder) > 1 {
		edges := make([]EdgeSnapshot, 0, len(s.ringOrder))
		latency := s.cfg.MasterRelayLatency
		if latency <= 0 {
			latency = 1
		}
		for i, src := range s.ringOrder {
			dst := s.ringOrder[(i+1)%len(s.ringOrder)]
			edge := EdgeSnapshot{
				Source:  src,
				Target:  dst,
				Label:   fmt.Sprintf("%dcy", latency),
				Latency: latency,
			}
			edges = append(edges, edge)
			s.edgeLatencies[EdgeKey{FromID: src, ToID: dst}] = latency
		}

		localLatency := 1
		for nodeID, routerID := range s.routerForNode {
			if routerID == 0 {
				continue
			}
			localLabel := fmt.Sprintf("%dcy", localLatency)
			rtToNode := EdgeSnapshot{
				Source:  routerID,
				Target:  nodeID,
				Label:   localLabel,
				Latency: localLatency,
			}
			nodeToRt := EdgeSnapshot{
				Source:  nodeID,
				Target:  routerID,
				Label:   localLabel,
				Latency: localLatency,
			}
			edges = append(edges, nodeToRt, rtToNode)
			s.edgeLatencies[EdgeKey{FromID: routerID, ToID: nodeID}] = localLatency
			s.edgeLatencies[EdgeKey{FromID: nodeID, ToID: routerID}] = localLatency
		}
		return edges
	}

	edges := make([]EdgeSnapshot, 0, (len(s.Masters)+len(s.Slaves))*2)
	if s.Relay == nil {
		return edges
	}
	homeNodeID := s.Relay.ID
	for _, m := range s.Masters {
		reqEdge := EdgeSnapshot{Source: m.ID, Target: homeNodeID, Label: "Req", Latency: s.cfg.MasterRelayLatency}
		compEdge := EdgeSnapshot{Source: homeNodeID, Target: m.ID, Label: "Comp", Latency: s.cfg.RelayMasterLatency}
		edges = append(edges, reqEdge, compEdge)
		s.edgeLatencies[EdgeKey{FromID: reqEdge.Source, ToID: reqEdge.Target}] = reqEdge.Latency
		s.edgeLatencies[EdgeKey{FromID: compEdge.Source, ToID: compEdge.Target}] = compEdge.Latency
	}
	for _, sl := range s.Slaves {
		reqEdge := EdgeSnapshot{Source: homeNodeID, Target: sl.ID, Label: "Req", Latency: s.cfg.RelaySlaveLatency}
		compEdge := EdgeSnapshot{Source: sl.ID, Target: homeNodeID, Label: "Comp", Latency: s.cfg.SlaveRelayLatency}
		edges = append(edges, reqEdge, compEdge)
		s.edgeLatencies[EdgeKey{FromID: reqEdge.Source, ToID: reqEdge.Target}] = reqEdge.Latency
		s.edgeLatencies[EdgeKey{FromID: compEdge.Source, ToID: compEdge.Target}] = compEdge.Latency
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
	nodeCount := len(s.Masters) + len(s.Slaves) + len(s.Routers)
	if s.Relay != nil {
		nodeCount++
	}
	nodes := make([]NodeSnapshot, 0, nodeCount)

	// Debug: log node counts
	if cycle == 0 {
		GetLogger().Debugf("buildFrame cycle 0: %d Masters, %d Slaves, Relay=%v", len(s.Masters), len(s.Slaves), s.Relay != nil)
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
		queuePackets := m.GetQueuePackets()
		queueInfo := m.GetQueueInfo()
		clonedQueues := cloneQueues(queueInfo)
		packetsByQueue := make(map[string][]core.PacketInfo)
		for _, pkt := range queuePackets {
			stageName := pkt.Metadata["node_queue_stage"]
			packetsByQueue[stageName] = append(packetsByQueue[stageName], pkt)
		}
		for i := range clonedQueues {
			if packets, ok := packetsByQueue[clonedQueues[i].Name]; ok {
				clonedQueues[i].Packets = packets
			}
		}
		// Get label with fallback
		label, ok := s.nodeLabels[m.ID]
		if !ok {
			label = fmt.Sprintf("RN %d", m.ID)
		}
		nodes = append(nodes, NodeSnapshot{
			ID:           m.ID,
			Type:         m.Type,
			Label:        label,
			Queues:       clonedQueues,
			Capabilities: m.CapabilityNames(),
			Payload:      payload,
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
		queueInfo := sl.GetQueueInfo()
		clonedQueues := cloneQueues(queueInfo)
		packetsByQueue := make(map[string][]core.PacketInfo)
		for _, pkt := range queuePackets {
			stageName := pkt.Metadata["node_queue_stage"]
			packetsByQueue[stageName] = append(packetsByQueue[stageName], pkt)
		}
		for i := range clonedQueues {
			if packets, ok := packetsByQueue[clonedQueues[i].Name]; ok {
				clonedQueues[i].Packets = packets
			}
		}
		// Get label with fallback
		label, ok := s.nodeLabels[sl.ID]
		if !ok {
			label = fmt.Sprintf("SN %d", sl.ID)
		}
		nodes = append(nodes, NodeSnapshot{
			ID:           sl.ID,
			Type:         sl.Type,
			Label:        label,
			Queues:       clonedQueues,
			Capabilities: sl.CapabilityNames(),
			Payload:      payload,
		})
	}

	for _, rt := range s.Routers {
		queuePackets := rt.GetQueuePackets()
		queueInfo := rt.GetQueueInfo()
		clonedQueues := cloneQueues(queueInfo)
		packetsByQueue := make(map[string][]core.PacketInfo)
		for _, pkt := range queuePackets {
			stageName := pkt.Metadata["node_queue_stage"]
			packetsByQueue[stageName] = append(packetsByQueue[stageName], pkt)
		}
		for i := range clonedQueues {
			if packets, ok := packetsByQueue[clonedQueues[i].Name]; ok {
				clonedQueues[i].Packets = packets
			}
		}
		label, ok := s.nodeLabels[rt.ID]
		if !ok {
			label = fmt.Sprintf("RT %d", rt.ID)
		}
		payload := map[string]any{
			"nodeType":    "RT",
			"chiProtocol": true,
		}
		nodes = append(nodes, NodeSnapshot{
			ID:           rt.ID,
			Type:         rt.Type,
			Label:        label,
			Queues:       clonedQueues,
			Capabilities: rt.CapabilityNames(),
			Payload:      payload,
		})
	}

	if s.Relay != nil {
		queueInfo := s.Relay.GetQueueInfo()
		queueLength := 0
		for _, q := range queueInfo {
			if q.Name == string(pipelineStageProcess) {
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
		queueInfoCloned := cloneQueues(queueInfo)
		packetsByQueue := make(map[string][]core.PacketInfo)
		for _, pkt := range queuePackets {
			stageName := pkt.Metadata["node_queue_stage"]
			packetsByQueue[stageName] = append(packetsByQueue[stageName], pkt)
		}
		for i := range queueInfoCloned {
			if packets, ok := packetsByQueue[queueInfoCloned[i].Name]; ok {
				queueInfoCloned[i].Packets = packets
			}
		}
		label, ok := s.nodeLabels[s.Relay.ID]
		if !ok {
			label = "HN 0"
		}
		nodes = append(nodes, NodeSnapshot{
			ID:           s.Relay.ID,
			Type:         s.Relay.Type,
			Label:        label,
			Queues:       queueInfoCloned,
			Capabilities: s.Relay.CapabilityNames(),
			Payload:      payload,
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
		Cycle:            cycle,
		Paused:           s.isPaused && s.runChunkRemaining == 0,
		Nodes:            nodes,
		Edges:            edges,
		InFlightCount:    s.Chan.InFlightCount(),
		Stats:            stats,
		ConfigHash:       configHash,
		TransactionGraph: txnGraph,
	}
	return frame
}

// GetTransactionManager returns the TransactionManager instance (for testing)
func (s *Simulator) GetTransactionManager() *TransactionManager {
	return s.txnMgr
}

func (s *Simulator) Run() {
	s.isRunning = true
	defer func() {
		s.isRunning = false
	}()

	for {
		resetRequested := s.runCycles()
		if resetRequested {
			s.isPaused = false
			cfg := s.pendingReset
			s.pendingReset = nil
			if cfg == nil {
				cfg = s.cfg
			}
			s.reset(cfg)
			continue
		}

		if s.visualEnabled() {
			cfg := s.waitForResetCommand()
			s.isPaused = false
			s.reset(cfg)
			continue
		}

		break
	}
}

func (s *Simulator) reset(newCfg *Config) {
	if newCfg != nil {
		if err := ValidateConfig(newCfg); err != nil {
			GetLogger().Errorf("Simulator.reset: invalid config: %v", err)
			newCfg = nil
		}
	}

	if newCfg != nil {
		GetLogger().Debugf("Simulator.reset: Applying new config: NumMasters=%d, NumSlaves=%d, TotalCycles=%d",
			newCfg.NumMasters, newCfg.NumSlaves, newCfg.TotalCycles)
		s.cfg = newCfg
	} else {
		GetLogger().Debugf("Simulator.reset: No new config provided, using existing config")
	}

	// Prepare generators based on new config
	factory := NewConfigGeneratorFactory()
	defaultGen := factory.BuildDefault(s.cfg)
	s.cfg.RequestGenerator = defaultGen
	s.cfg.RequestGenerators = factory.BuildPerMaster(s.cfg, defaultGen)

	// Reinitialize simulator with new config
	pktAlloc := NewPacketIDAllocator()
	txnMgr := NewTransactionManager()
	broker := hooks.NewPluginBroker()
	txFactory := NewTxFactory(broker, txnMgr, pktAlloc)
	masters, slaves, relay, routers, ch, masterByID, slaveByID, routerByID, routerForNode, labels := initializeSimulatorComponents(s.cfg, s.rng)

	s.Masters = masters
	s.Slaves = slaves
	s.Relay = relay
	s.Routers = routers
	s.Chan = ch
	s.masterByID = masterByID
	s.slaveByID = slaveByID
	s.routerByID = routerByID
	s.routerForNode = make(map[int]int, len(routerForNode))
	fallbackRouter := router.NewDefaultRouter()
	for nodeID, router := range routerForNode {
		s.routerForNode[nodeID] = router.ID
		fallbackRouter.AddDirectedEdge(nodeID, router.ID, 1)
		fallbackRouter.AddDirectedEdge(router.ID, nodeID, 1)
	}
	s.ringOrder = nil
	routerLocal := make(map[int][]int)
	for nodeID, routerID := range s.routerForNode {
		routerLocal[routerID] = append(routerLocal[routerID], nodeID)
	}
	routerCaps := make(map[int]capabilities.RingRouterCapability)
	for _, router := range s.Routers {
		cap := capabilities.NewRingRouterCapability(
			fmt.Sprintf("ring-router-forward-%d", router.ID),
			capabilities.RingRouterConfig{RouterID: router.ID},
		)
		router.RegisterCapability(cap)
		routerCaps[router.ID] = cap
	}
	if s.cfg.RingEnabled {
		interleaver := NewRingAddressInterleaver(slaves, s.cfg.RingInterleaveStride)
		for _, m := range masters {
			m.SetAddressMapper(interleaver)
		}
		s.ringOrder = buildRingOrder(masters, relay, slaves, routerForNode)
		if len(s.ringOrder) > 0 {
			for idx, routerID := range s.ringOrder {
				nextID := s.ringOrder[(idx+1)%len(s.ringOrder)]
				local := routerLocal[routerID]
				routerCaps[routerID].Update(capabilities.RingRouterConfig{
					RouterID:     routerID,
					NextRouterID: nextID,
					LocalNodeIDs: local,
				})
				fallbackRouter.AddDirectedEdge(routerID, nextID, s.cfg.MasterRelayLatency)
			}
		}
	}
	if s.Relay != nil {
		for _, m := range s.Masters {
			fallbackRouter.AddDirectedEdge(m.ID, s.Relay.ID, s.cfg.MasterRelayLatency)
			fallbackRouter.AddDirectedEdge(s.Relay.ID, m.ID, s.cfg.RelayMasterLatency)
		}
		for _, sl := range s.Slaves {
			fallbackRouter.AddDirectedEdge(s.Relay.ID, sl.ID, s.cfg.RelaySlaveLatency)
			fallbackRouter.AddDirectedEdge(sl.ID, s.Relay.ID, s.cfg.SlaveRelayLatency)
		}
	}
	baseMgr := policy.NewDefaultManager()
	s.policyMgr = policy.WithFallbackRouter(baseMgr, fallbackRouter.PolicyRouter())
	s.nodeLabels = labels
	// s.ringOrder already assigned if ring enabled
	s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	s.pktIDs = pktAlloc
	s.txnMgr = txnMgr
	s.current = 0
	s.edges = s.buildEdges()
	s.pluginBroker = broker
	s.txFactory = txFactory
	// s.policyMgr already configured with fallback router

	for _, m := range s.Masters {
		m.SetTxFactory(txFactory)
		m.SetPluginBroker(broker)
		m.SetPolicyManager(s.policyMgr)
		m.SetPacketIDAllocator(s.pktIDs)
	}
	for _, sl := range s.Slaves {
		sl.SetPluginBroker(broker)
	}
	if s.Relay != nil {
		s.Relay.SetPluginBroker(broker)
		s.Relay.SetPolicyManager(s.policyMgr)
		s.Relay.SetPacketIDAllocator(s.pktIDs)
	}
	for _, router := range s.Routers {
		router.SetPluginBroker(broker)
		router.SetTransactionManager(s.txnMgr)
		router.SetPacketIDAllocator(s.pktIDs)
	}

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
		EnablePacketHistory:   enableHistory,
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

	s.rebuildRuntimeHelpers()

	// If web frontend is available, pause at cycle 0 after reset
	// Otherwise (headless mode), continue running
	if s.visualEnabled() {
		s.isPaused = true
		// Publish frame at cycle 0 after reset
		frame := s.buildFrame(0)
		s.runner.PublishFrame(frame)
	} else {
		s.isPaused = false
	}
	s.runBudget = 0
	s.runChunkRemaining = 0
	s.runTarget = s.current

	s.initializeCycleRuntime()
	s.updateCoordinatorLimit()
}

func (s *Simulator) stopRuntimes() {
	if s.coordinator != nil {
		s.coordinator.Stop()
	}
	s.runtimeWG.Wait()
	s.coordinator = nil
}

func (s *Simulator) handleCommand(cmd visual.ControlCommand) bool {
	if cmd.Type == visual.CommandNone {
		return true
	}

	switch cmd.Type {
	case visual.CommandPause:
		s.clearRunRequests()
	case visual.CommandRun:
		if cmd.Cycles <= 0 {
			break
		}
		s.runBudget += cmd.Cycles
		if s.runChunkRemaining == 0 {
			s.scheduleNextRunChunk()
		}
	case visual.CommandReset:
		if s.pendingReset == nil {
			var cfg *Config
			if provided, ok := cmd.ConfigOverride.(*Config); ok && provided != nil {
				cfgCopy := *provided
				cfg = &cfgCopy
			} else {
				cfgCopy := *s.cfg
				cfg = &cfgCopy
			}
			s.pendingReset = cfg
			s.clearRunRequests()
			s.stopRuntimes()
		}
		return false
	}

	return true
}

func (s *Simulator) processCommands() bool {
	if s.runner == nil {
		return true
	}
	return s.runner.DrainPendingCommands()
}

func (s *Simulator) runCycles() bool {
	s.rebuildRuntimeHelpers()

	totalCycles := s.cfg.TotalCycles
	if totalCycles < 0 {
		totalCycles = 0
	}

	if totalCycles == 0 {
		if s.visualEnabled() {
			frame := s.buildFrame(0)
			s.runner.PublishFrame(frame)
		}
		return false
	}

	if s.coordinator == nil {
		s.initializeCycleRuntime()
	}

	s.startRuntimes()

	for cycle := s.current; cycle < totalCycles; {
		if !s.processCommands() {
			return true
		}

		if s.runChunkRemaining == 0 {
			if !s.isPaused && s.runBudget > 0 {
				s.scheduleNextRunChunk()
				continue
			}
			if s.isPaused {
				if s.runner != nil {
					if !s.runner.WaitForCommand(context.Background()) {
						return true
					}
				} else {
					time.Sleep(1 * time.Millisecond)
				}
				continue
			}
		}

		for {
			if s.coordinator.TargetCycle() > cycle {
				break
			}
			if !s.processCommands() {
				return true
			}
			time.Sleep(1 * time.Millisecond)
		}

		if s.visualEnabled() {
			frame := s.buildFrame(cycle)
			s.runner.PublishFrame(frame)
		}

		if s.txnMgr != nil && cycle%100 == 0 {
			s.txnMgr.CleanupHistory()
		}

		cycle++
		s.current = cycle
		metrics.RecordCycles(1)
		s.onCycleAdvanced()

		if s.visualEnabled() {
			frame := s.buildFrame(s.current)
			s.runner.PublishFrame(frame)
		}
	}

	s.clearRunRequests()
	s.stopRuntimes()
	s.current = totalCycles
	return false
}

func (s *Simulator) waitForResetCommand() *Config {
	for {
		if !s.processCommands() {
			break
		}
		if s.runner != nil {
			if !s.runner.WaitForCommand(context.Background()) {
				break
			}
			continue
		}
		time.Sleep(50 * time.Millisecond)
	}
	cfg := s.pendingReset
	s.pendingReset = nil
	return cfg
}

func cloneRingOrder(order []int) []int {
	if len(order) == 0 {
		return nil
	}
	cloned := make([]int, len(order))
	copy(cloned, order)
	return cloned
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
