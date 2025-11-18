package configs

import (
	app "github.com/Readm/flow_sim/framework/app"

	"github.com/Readm/flow_sim/configs/loader"
)

// Register allows configuration Go files to add descriptors during init.
func Register(desc app.ConfigDescriptor) {
	if desc.Name == "" || desc.Config == nil {
		return
	}
	registry = append(registry, app.ConfigDescriptor{
		Name:        desc.Name,
		Description: desc.Description,
		Config:      cloneConfig(desc.Config),
	})
}

// RegisterJSON loads a JSON topology file and registers it as a config descriptor.
func RegisterJSON(path string) error {
	doc, err := loader.LoadFile(path)
	if err != nil {
		return err
	}
	cfg, err := doc.ToAppConfig()
	if err != nil {
		return err
	}
	Register(app.ConfigDescriptor{
		Name:        doc.Meta.Name,
		Description: doc.Meta.Description,
		Config:      cfg,
	})
	return nil
}

// Provider returns a ConfigProvider implementation backed by the registry.
func Provider() app.ConfigProvider {
	return registryProvider{}
}

type registryProvider struct{}

func (registryProvider) List() []app.ConfigDescriptor {
	copies := make([]app.ConfigDescriptor, len(registry))
	for i, desc := range registry {
		copies[i] = app.ConfigDescriptor{
			Name:        desc.Name,
			Description: desc.Description,
			Config:      cloneConfig(desc.Config),
		}
	}
	return copies
}

func (registryProvider) Get(name string) *app.Config {
	for _, desc := range registry {
		if desc.Name == name {
			return cloneConfig(desc.Config)
		}
	}
	return nil
}

var registry []app.ConfigDescriptor

func cloneConfig(cfg *app.Config) *app.Config {
	if cfg == nil {
		return nil
	}
	copyCfg := *cfg

	if cfg.NodeSchedules != nil {
		copyCfg.NodeSchedules = make(map[string]map[int][]app.ScheduleItem, len(cfg.NodeSchedules))
		for nodeID, schedule := range cfg.NodeSchedules {
			copyCfg.NodeSchedules[nodeID] = make(map[int][]app.ScheduleItem, len(schedule))
			for cycle, items := range schedule {
				itemsCopy := make([]app.ScheduleItem, len(items))
				copy(itemsCopy, items)
				copyCfg.NodeSchedules[nodeID][cycle] = itemsCopy
			}
		}
	}

	if cfg.InitialCacheState != nil {
		copyCfg.InitialCacheState = make(map[int]map[uint64]app.CacheState)
		for nodeID, addrMap := range cfg.InitialCacheState {
			copyCfg.InitialCacheState[nodeID] = make(map[uint64]app.CacheState)
			for addr, state := range addrMap {
				copyCfg.InitialCacheState[nodeID][addr] = state
			}
		}
	}

	if cfg.Graph != nil {
		copyCfg.Graph = cloneGraphConfig(cfg.Graph)
	}

	return &copyCfg
}

func cloneGraphConfig(cfg *app.GraphConfig) *app.GraphConfig {
	if cfg == nil {
		return nil
	}
	cp := &app.GraphConfig{
		Nodes: make([]app.GraphNode, len(cfg.Nodes)),
		Edges: make([]app.GraphEdge, len(cfg.Edges)),
	}
	for i, node := range cfg.Nodes {
		cp.Nodes[i] = app.GraphNode{
			ID:           node.ID,
			Label:        node.Label,
			Capabilities: cloneStrings(node.Capabilities),
			Metadata:     cloneStringMap(node.Metadata),
		}
		if node.Position != nil {
			pos := *node.Position
			cp.Nodes[i].Position = &pos
		}
	}
	for i, edge := range cfg.Edges {
		cp.Edges[i] = app.GraphEdge{
			From:      edge.From,
			To:        edge.To,
			Latency:   edge.Latency,
			Bandwidth: edge.Bandwidth,
			Metadata:  cloneStringMap(edge.Metadata),
		}
	}
	return cp
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cp := make([]string, len(values))
	copy(cp, values)
	return cp
}

func cloneStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
