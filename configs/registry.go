package configs

import (
	app "github.com/Readm/flow_sim/framework/app"
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

	if cfg.SlaveWeights != nil {
		copyCfg.SlaveWeights = make([]int, len(cfg.SlaveWeights))
		copy(copyCfg.SlaveWeights, cfg.SlaveWeights)
	}

	if cfg.ScheduleConfig != nil {
		copyCfg.ScheduleConfig = make(map[int]map[int][]app.ScheduleItem)
		for cycle, masterMap := range cfg.ScheduleConfig {
			copyCfg.ScheduleConfig[cycle] = make(map[int][]app.ScheduleItem)
			for masterIdx, items := range masterMap {
				itemsCopy := make([]app.ScheduleItem, len(items))
				copy(itemsCopy, items)
				copyCfg.ScheduleConfig[cycle][masterIdx] = itemsCopy
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

	return &copyCfg
}
