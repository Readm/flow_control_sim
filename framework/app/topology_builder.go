package app

import (
	"fmt"
	"math/rand"
	"strings"
)

// TopologyArtifacts aggregates instantiated nodes, link, and lookup tables.
type TopologyArtifacts struct {
	Masters       []*RequestNode
	Slaves        []*SlaveNode
	Relay         *HomeNode
	Routers       []*RingRouterNode
	Link          *Link
	MasterByID    map[int]*RequestNode
	SlaveByID     map[int]*SlaveNode
	RouterByID    map[int]*RingRouterNode
	Labels        map[int]string
	GraphNodeIDs  map[string]int
	Edges         []EdgeSnapshot
	EdgeLatencies map[EdgeKey]int
	NodeProfiles  map[int]nodeCapabilityProfile
}

type graphTopologyBuilder struct {
	cfg             *Config
	rng             *rand.Rand
	idAlloc         *NodeIDAllocator
	nodeRegistry    map[int]NodeReceiver
	labels          map[int]string
	graphIDToNodeID map[string]int
	artifacts       *TopologyArtifacts
}

func newGraphTopologyBuilder(cfg *Config, rng *rand.Rand) *graphTopologyBuilder {
	return &graphTopologyBuilder{
		cfg:             cfg,
		rng:             rng,
		idAlloc:         NewNodeIDAllocator(),
		nodeRegistry:    make(map[int]NodeReceiver),
		labels:          make(map[int]string),
		graphIDToNodeID: make(map[string]int),
		artifacts: &TopologyArtifacts{
			MasterByID:   make(map[int]*RequestNode),
			SlaveByID:    make(map[int]*SlaveNode),
			RouterByID:   make(map[int]*RingRouterNode),
			NodeProfiles: make(map[int]nodeCapabilityProfile),
		},
	}
}

func initializeSimulatorComponents(cfg *Config, rng *rand.Rand) (*TopologyArtifacts, error) {
	builder := newGraphTopologyBuilder(cfg, rng)
	return builder.build()
}

func (b *graphTopologyBuilder) build() (*TopologyArtifacts, error) {
	if b.cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if b.cfg.Graph == nil {
		return nil, fmt.Errorf("graph configuration is required")
	}
	if len(b.cfg.Graph.Nodes) == 0 {
		return nil, fmt.Errorf("graph must contain at least one node")
	}
	if err := b.instantiatePrimaryNodes(); err != nil {
		return nil, err
	}
	bandwidth := b.cfg.BandwidthLimit
	if bandwidth <= 0 {
		bandwidth = DefaultBandwidthLimit
	}
	b.artifacts.Link = NewLink(bandwidth, b.nodeRegistry)
	if err := b.buildEdges(); err != nil {
		return nil, err
	}
	b.artifacts.Labels = b.labels
	b.artifacts.GraphNodeIDs = b.graphIDToNodeID
	return b.artifacts, nil
}

func (b *graphTopologyBuilder) instantiatePrimaryNodes() error {
	mastersExpected, slavesExpected, homesExpected := countGraphRoles(b.cfg.Graph)
	if mastersExpected == 0 {
		return fmt.Errorf("graph must include at least one requester node")
	}
	if slavesExpected == 0 {
		return fmt.Errorf("graph must include at least one slave_target node")
	}
	if homesExpected != 1 {
		return fmt.Errorf("graph must include exactly one home_directory node (found %d)", homesExpected)
	}

	b.cfg.NumMasters = mastersExpected
	b.cfg.NumSlaves = slavesExpected
	b.cfg.NumRelays = homesExpected
	b.ensureSlaveWeights(slavesExpected)

	masterIndex := 0
	for _, node := range b.cfg.Graph.Nodes {
		role := classifyGraphRole(node.Capabilities)
		switch role {
		case graphRoleRequester:
			generator := b.selectRequestGenerator(masterIndex)
			reqNode := NewRequestNode(b.idAlloc.Allocate(), masterIndex, generator)
			reqNode.SetCacheCapacity(b.cfg.RequestCacheCapacity)
			b.registerNode(node, reqNode.ID, reqNode, role)
			b.artifacts.Masters = append(b.artifacts.Masters, reqNode)
			b.artifacts.MasterByID[reqNode.ID] = reqNode
			masterIndex++
		case graphRoleHome:
			if b.artifacts.Relay != nil {
				return fmt.Errorf("multiple home_directory nodes are not supported")
			}
			homeNode := NewHomeNode(b.idAlloc.Allocate())
			homeNode.SetCacheCapacity(b.cfg.HomeCacheCapacity)
			b.registerNode(node, homeNode.ID, homeNode, role)
			b.artifacts.Relay = homeNode
		case graphRoleSlave:
			processRate := b.cfg.SlaveProcessRate
			if processRate < 0 {
				processRate = 1
			}
			slaveNode := NewSlaveNode(b.idAlloc.Allocate(), processRate)
			b.registerNode(node, slaveNode.ID, slaveNode, role)
			b.artifacts.Slaves = append(b.artifacts.Slaves, slaveNode)
			b.artifacts.SlaveByID[slaveNode.ID] = slaveNode
		case graphRoleRouter:
			router := NewRingRouterNode(b.idAlloc.Allocate())
			b.registerNode(node, router.ID, router, role)
			b.artifacts.Routers = append(b.artifacts.Routers, router)
			b.artifacts.RouterByID[router.ID] = router
		default:
			return fmt.Errorf("node %q has unsupported capabilities %v", node.ID, node.Capabilities)
		}
	}

	if b.artifacts.Relay == nil {
		return fmt.Errorf("graph must include one home_directory node")
	}
	if len(b.artifacts.Masters) != mastersExpected {
		return fmt.Errorf("expected %d requester nodes, got %d", mastersExpected, len(b.artifacts.Masters))
	}
	if len(b.artifacts.Slaves) != slavesExpected {
		return fmt.Errorf("expected %d slave nodes, got %d", slavesExpected, len(b.artifacts.Slaves))
	}
	return nil
}

func (b *graphTopologyBuilder) registerNode(node GraphNode, id int, receiver NodeReceiver, role graphNodeRole) {
	label := node.Label
	if strings.TrimSpace(label) == "" {
		label = node.ID
	}
	b.nodeRegistry[id] = receiver
	b.labels[id] = label
	if node.ID != "" {
		b.graphIDToNodeID[node.ID] = id
	}
	b.recordProfile(id, role, node.Metadata)
	applyGraphPosition(receiver, node.Position)
}

func (b *graphTopologyBuilder) recordProfile(nodeID int, role graphNodeRole, metadata map[string]string) {
	if b.artifacts.NodeProfiles == nil {
		b.artifacts.NodeProfiles = make(map[int]nodeCapabilityProfile)
	}
	params := extractParams(metadata)
	b.artifacts.NodeProfiles[nodeID] = nodeCapabilityProfile{
		role:   role,
		params: params,
	}
}

func (b *graphTopologyBuilder) selectRequestGenerator(index int) RequestGenerator {
	if len(b.cfg.RequestGenerators) > index && b.cfg.RequestGenerators[index] != nil {
		return b.cfg.RequestGenerators[index]
	}
	if b.cfg.RequestGenerator != nil {
		return b.cfg.RequestGenerator
	}
	if len(b.cfg.ScheduleConfig) > 0 {
		return NewScheduleGenerator(b.cfg.ScheduleConfig)
	}
	rate := b.cfg.RequestRateConfig
	if rate <= 0 {
		rate = 0.5
	}
	slaveWeights := b.cfg.SlaveWeights
	if len(slaveWeights) == 0 {
		slaveWeights = make([]int, len(b.artifacts.Slaves))
		for i := range slaveWeights {
			slaveWeights[i] = 1
		}
		b.cfg.SlaveWeights = slaveWeights
	}
	return NewProbabilityGenerator(rate, slaveWeights, b.rng)
}

func (b *graphTopologyBuilder) ensureSlaveWeights(expected int) {
	if expected <= 0 {
		b.cfg.SlaveWeights = nil
		return
	}
	if len(b.cfg.SlaveWeights) == expected {
		return
	}
	weights := make([]int, expected)
	for i := range weights {
		if i < len(b.cfg.SlaveWeights) && b.cfg.SlaveWeights[i] > 0 {
			weights[i] = b.cfg.SlaveWeights[i]
		} else {
			weights[i] = 1
		}
	}
	b.cfg.SlaveWeights = weights
}

func (b *graphTopologyBuilder) buildEdges() error {
	if len(b.cfg.Graph.Edges) == 0 {
		return fmt.Errorf("graph must define at least one link")
	}
	edges := make([]EdgeSnapshot, 0, len(b.cfg.Graph.Edges))
	latencies := make(map[EdgeKey]int, len(b.cfg.Graph.Edges))
	for _, edge := range b.cfg.Graph.Edges {
		fromID, ok := b.graphIDToNodeID[edge.From]
		if !ok {
			return fmt.Errorf("link references unknown from node %q", edge.From)
		}
		toID, ok := b.graphIDToNodeID[edge.To]
		if !ok {
			return fmt.Errorf("link references unknown to node %q", edge.To)
		}
		latency := positiveOrDefault(edge.Latency, 1)
		label := ""
		if edge.Metadata != nil {
			label = edge.Metadata["label"]
			if label == "" {
				label = edge.Metadata["channel"]
			}
		}
		if label == "" {
			label = fmt.Sprintf("%dcy", latency)
		}
		bandwidth := edge.Bandwidth
		if bandwidth <= 0 {
			bandwidth = b.cfg.BandwidthLimit
			if bandwidth <= 0 {
				bandwidth = DefaultBandwidthLimit
			}
		}
		snapshot := EdgeSnapshot{
			Source:         fromID,
			Target:         toID,
			Label:          label,
			Latency:        latency,
			BandwidthLimit: bandwidth,
		}
		edges = append(edges, snapshot)
		latencies[EdgeKey{FromID: fromID, ToID: toID}] = latency
	}
	b.artifacts.Edges = edges
	b.artifacts.EdgeLatencies = latencies
	return nil
}
