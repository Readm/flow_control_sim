package loader

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	app "github.com/Readm/flow_sim/framework/app"
)

var errMissingMetaName = errors.New("meta.name is required")

// ToAppConfig converts the topology document into an app.Config instance.
func (doc *TopologyDocument) ToAppConfig() (*app.Config, error) {
	builder, err := newConfigBuilder(doc)
	if err != nil {
		return nil, err
	}
	return builder.buildConfig()
}

type configBuilder struct {
	doc       *TopologyDocument
	nodeByID  map[string]*NodeDocument
	roleIndex roleIndex
}

type roleIndex struct {
	masters      []string
	slaves       []string
	homes        []string
	masterLookup map[string]int
	slaveLookup  map[string]int
}

func newConfigBuilder(doc *TopologyDocument) (*configBuilder, error) {
	if doc == nil {
		return nil, errors.New("topology document is nil")
	}
	if strings.TrimSpace(doc.Meta.Name) == "" {
		return nil, errMissingMetaName
	}
	builder := &configBuilder{
		doc:      doc,
		nodeByID: make(map[string]*NodeDocument, len(doc.Nodes)),
	}
	for i := range doc.Nodes {
		node := &doc.Nodes[i]
		nodeID := strings.TrimSpace(node.ID)
		if nodeID == "" {
			return nil, fmt.Errorf("node at index %d missing id", i)
		}
		if _, exists := builder.nodeByID[nodeID]; exists {
			return nil, fmt.Errorf("duplicate node id %q", nodeID)
		}
		node.ID = nodeID
		builder.nodeByID[nodeID] = node
		builder.consumeNode(node)
	}
	if len(builder.roleIndex.masters) == 0 {
		return nil, errors.New("no node tagged with capability 'requester'")
	}
	if len(builder.roleIndex.slaves) == 0 {
		return nil, errors.New("no node tagged with capability 'slave_target'")
	}
	if len(builder.roleIndex.homes) == 0 {
		return nil, errors.New("no node tagged with capability 'home_directory'")
	}
	if len(builder.roleIndex.homes) > 1 {
		return nil, fmt.Errorf("current simulator supports a single home_directory node, found %d", len(builder.roleIndex.homes))
	}
	builder.roleIndex.masterLookup = make(map[string]int, len(builder.roleIndex.masters))
	for idx, id := range builder.roleIndex.masters {
		builder.roleIndex.masterLookup[id] = idx
	}
	builder.roleIndex.slaveLookup = make(map[string]int, len(builder.roleIndex.slaves))
	for idx, id := range builder.roleIndex.slaves {
		builder.roleIndex.slaveLookup[id] = idx
	}
	if err := builder.validateLinks(); err != nil {
		return nil, err
	}
	return builder, nil
}

func (b *configBuilder) consumeNode(node *NodeDocument) {
	for _, raw := range node.Capabilities {
		switch classifyCapability(raw) {
		case capabilityRequester:
			b.roleIndex.masters = append(b.roleIndex.masters, node.ID)
		case capabilitySlave:
			b.roleIndex.slaves = append(b.roleIndex.slaves, node.ID)
		case capabilityHome:
			b.roleIndex.homes = append(b.roleIndex.homes, node.ID)
		default:
			// ignored for now (relay/cache/etc.)
		}
	}
}

type capabilityType string

const (
	capabilityRequester capabilityType = "requester"
	capabilitySlave     capabilityType = "slave_target"
	capabilityHome      capabilityType = "home_directory"
)

func classifyCapability(raw string) capabilityType {
	token := strings.ToLower(strings.TrimSpace(raw))
	if token == "" {
		return ""
	}
	if idx := strings.IndexRune(token, ':'); idx > 0 {
		token = token[:idx]
	}
	switch token {
	case "requester", "rn":
		return capabilityRequester
	case "slave_target", "sn", "target":
		return capabilitySlave
	case "home_directory", "hn", "home":
		return capabilityHome
	default:
		return ""
	}
}

func (b *configBuilder) validateLinks() error {
	if len(b.doc.Links) == 0 {
		return nil
	}
	for idx, link := range b.doc.Links {
		link.From = strings.TrimSpace(link.From)
		link.To = strings.TrimSpace(link.To)
		if link.From == "" || link.To == "" {
			return fmt.Errorf("link[%d] missing from/to id", idx)
		}
		if _, ok := b.nodeByID[link.From]; !ok {
			return fmt.Errorf("link[%d] references unknown from node %q", idx, link.From)
		}
		if _, ok := b.nodeByID[link.To]; !ok {
			return fmt.Errorf("link[%d] references unknown to node %q", idx, link.To)
		}
		if link.Latency <= 0 {
			return fmt.Errorf("link[%d] latency must be positive", idx)
		}
		if link.Bandwidth == 0 {
			return fmt.Errorf("link[%d] bandwidth must be non-zero", idx)
		}
	}
	return nil
}

func (b *configBuilder) buildConfig() (*app.Config, error) {
	defaults := b.doc.Defaults
	cfg := &app.Config{
		TotalCycles:           choosePositive(defaults.TotalCycles, 1000),
		RequestRateConfig:     clamp01(defaults.RequestRate),
		BandwidthLimit:        defaults.BandwidthLimit,
		DispatchQueueCapacity: defaults.DispatchQueueCapacity,
		RequestCacheCapacity:  defaults.RequestCacheCapacity,
		HomeCacheCapacity:     defaults.HomeCacheCapacity,
		VisualMode:            strings.TrimSpace(defaults.VisualMode),
		Plugins:               defaults.Plugins,
		MaxPacketHistorySize:  defaults.MaxPacketHistorySize,
		HistoryOverflowMode:   defaults.HistoryOverflowMode,
		MaxTransactionHistory: defaults.MaxTransactionHistory,
	}
	if defaults.Headless != nil {
		cfg.Headless = *defaults.Headless
	}
	if defaults.EnablePacketHistory != nil {
		cfg.EnablePacketHistory = *defaults.EnablePacketHistory
	} else {
		cfg.EnablePacketHistory = true
	}

	schedule, err := b.buildNodeSchedules()
	if err != nil {
		return nil, err
	}
	if len(schedule) > 0 {
		cfg.NodeSchedules = schedule
	}

	initialState, err := b.buildInitialCacheState()
	if err != nil {
		return nil, err
	}
	if len(initialState) > 0 {
		cfg.InitialCacheState = initialState
	}

	if graph := b.buildGraph(); graph != nil {
		cfg.Graph = graph
	}

	return cfg, nil
}

func (b *configBuilder) buildGraph() *app.GraphConfig {
	if len(b.doc.Nodes) == 0 {
		return nil
	}
	graph := &app.GraphConfig{
		Nodes: make([]app.GraphNode, 0, len(b.doc.Nodes)),
		Edges: make([]app.GraphEdge, 0, len(b.doc.Links)*2),
	}
	for _, node := range b.doc.Nodes {
		graphNode := app.GraphNode{
			ID:           node.ID,
			Label:        node.Label,
			Capabilities: cloneStrings(node.Capabilities),
			Metadata:     copyStringMap(node.Metadata),
		}
		if node.Params != nil {
			if graphNode.Metadata == nil {
				graphNode.Metadata = make(map[string]string, len(node.Params))
			}
			for key, value := range node.Params {
				graphNode.Metadata["param."+key] = fmt.Sprint(value)
			}
		}
		if node.Position != nil {
			pos := app.Position{X: node.Position.X, Y: node.Position.Y}
			graphNode.Position = &pos
		}
		graph.Nodes = append(graph.Nodes, graphNode)
	}
	for _, link := range b.doc.Links {
		edge := app.GraphEdge{
			From:      link.From,
			To:        link.To,
			Latency:   link.Latency,
			Bandwidth: link.Bandwidth,
			Metadata:  copyStringMap(link.Metadata),
		}
		graph.Edges = append(graph.Edges, edge)
		if link.Bidirectional {
			rev := edge
			rev.From = link.To
			rev.To = link.From
			graph.Edges = append(graph.Edges, rev)
		}
	}
	return graph
}

func (b *configBuilder) buildSlaveWeights() []int {
	count := len(b.roleIndex.slaves)
	weights := make([]int, count)
	if len(b.doc.Defaults.SlaveWeights) == count {
		copy(weights, b.doc.Defaults.SlaveWeights)
	} else {
		for i := range weights {
			weights[i] = 1
		}
	}
	for idx, nodeID := range b.roleIndex.slaves {
		if node := b.nodeByID[nodeID]; node != nil {
			if value, ok := readInt(node.Params, "weight"); ok && value > 0 {
				weights[idx] = value
			}
		}
		if weights[idx] <= 0 {
			weights[idx] = 1
		}
	}
	return weights
}

func (b *configBuilder) buildNodeSchedules() (map[string]map[int][]app.ScheduleItem, error) {
	if len(b.doc.Schedules) == 0 {
		return nil, nil
	}
	result := make(map[string]map[int][]app.ScheduleItem)
	for idx, sched := range b.doc.Schedules {
		if sched.Tick < 0 {
			return nil, fmt.Errorf("schedule[%d] tick must be non-negative", idx)
		}
		sourceID := strings.TrimSpace(sched.Source)
		if _, ok := b.roleIndex.masterLookup[sourceID]; !ok {
			return nil, fmt.Errorf("schedule[%d] references unknown source %q", idx, sched.Source)
		}
		if len(sched.Transactions) == 0 {
			continue
		}
		if _, exists := result[sourceID]; !exists {
			result[sourceID] = make(map[int][]app.ScheduleItem)
		}
		for txnIdx, txn := range sched.Transactions {
			item, err := b.buildScheduleItem(txn)
			if err != nil {
				return nil, fmt.Errorf("schedule[%d].transactions[%d]: %w", idx, txnIdx, err)
			}
			targetID := strings.TrimSpace(txn.Target)
			if targetID != "" {
				slaveIdx, ok := b.roleIndex.slaveLookup[targetID]
				if !ok {
					return nil, fmt.Errorf("schedule[%d].transactions[%d]: unknown target %q", idx, txnIdx, txn.Target)
				}
				item.SlaveIndex = slaveIdx
				item.Target = targetID
			}
			result[sourceID][sched.Tick] = append(result[sourceID][sched.Tick], item)
		}
	}
	return result, nil
}

func (b *configBuilder) buildScheduleItem(txn TransactionDocument) (app.ScheduleItem, error) {
	item := app.ScheduleItem{
		SlaveIndex: 0,
		DataSize:   txn.DataSize,
	}
	txnType, err := parseTransactionType(txn.Type)
	if err != nil {
		return item, err
	}
	item.TransactionType = txnType
	if strings.TrimSpace(txn.Address) != "" {
		address, err := parseAddress(txn.Address)
		if err != nil {
			return item, err
		}
		item.Address = address
	}
	return item, nil
}

func parseTransactionType(kind string) (app.CHITransactionType, error) {
	token := strings.ToLower(strings.TrimSpace(kind))
	switch token {
	case "", "read", "readnosnp", "read_no_snp":
		return app.CHITxnReadNoSnp, nil
	case "write", "writenosnp", "write_no_snp":
		return app.CHITxnWriteNoSnp, nil
	case "readonce", "read_once":
		return app.CHITxnReadOnce, nil
	case "writeunique", "write_unique":
		return app.CHITxnWriteUnique, nil
	default:
		return app.CHITransactionType(""), fmt.Errorf("unsupported transaction type %q", kind)
	}
}

func parseAddress(addr string) (uint64, error) {
	value := strings.TrimSpace(addr)
	if value == "" {
		return 0, nil
	}
	base := 10
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		base = 0
	}
	parsed, err := strconv.ParseUint(value, base, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid address %q: %w", addr, err)
	}
	return parsed, nil
}

func (b *configBuilder) buildInitialCacheState() (map[int]map[uint64]app.CacheState, error) {
	if len(b.doc.InitialStates) == 0 {
		return nil, nil
	}
	nodeMapping := b.runtimeNodeMapping()
	result := make(map[int]map[uint64]app.CacheState, len(b.doc.InitialStates))
	for nodeRef, entries := range b.doc.InitialStates {
		nodeID := strings.TrimSpace(nodeRef)
		runtimeID, ok := nodeMapping[nodeID]
		if !ok {
			return nil, fmt.Errorf("initial_states references unknown node %q", nodeRef)
		}
		if len(entries) == 0 {
			continue
		}
		target := make(map[uint64]app.CacheState, len(entries))
		for addrStr, stateStr := range entries {
			addr, err := parseAddress(addrStr)
			if err != nil {
				return nil, fmt.Errorf("initial_states[%s]: %w", nodeRef, err)
			}
			state := app.CacheState(strings.TrimSpace(stateStr))
			if state == "" {
				state = app.CacheState("Invalid")
			}
			target[addr] = state
		}
		result[runtimeID] = target
	}
	return result, nil
}

func (b *configBuilder) runtimeNodeMapping() map[string]int {
	mapping := make(map[string]int, len(b.roleIndex.masters)+len(b.roleIndex.slaves)+len(b.roleIndex.homes))
	nextID := 0
	nextID = appendMapping(mapping, b.roleIndex.masters, nextID)
	nextID = appendMapping(mapping, b.roleIndex.slaves, nextID)
	appendMapping(mapping, b.roleIndex.homes, nextID)
	return mapping
}

func appendMapping(target map[string]int, ids []string, start int) int {
	next := start
	for _, id := range ids {
		target[id] = next
		next++
	}
	return next
}

func readInt(params map[string]any, key string) (int, bool) {
	if params == nil {
		return 0, false
	}
	value, exists := params[key]
	if !exists {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return int(math.Round(v)), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func choosePositive(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cp := make([]string, len(values))
	copy(cp, values)
	return cp
}

func copyStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	cp := make(map[string]string, len(src))
	for k, v := range src {
		cp[k] = v
	}
	return cp
}
