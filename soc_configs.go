package main

// SOCNetworkConfig represents a predefined SOC network configuration
type SOCNetworkConfig struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Config      *Config `json:"-"`
}

// GetPredefinedConfigs returns all available predefined SOC network configurations
func GetPredefinedConfigs() []SOCNetworkConfig {
	return []SOCNetworkConfig{
		{
			Name:        "multi_master_multi_slave",
			Description: "Multi-Master Multi-Slave Network (3 Masters, 2 Slaves, 1 Home Node)",
			Config: &Config{
				NumMasters:         3,
				NumSlaves:          2,
				NumRelays:          1,
				TotalCycles:        1000,
				MasterRelayLatency: 2,
				RelayMasterLatency: 2,
				RelaySlaveLatency:  1,
				SlaveRelayLatency:  1,
				SlaveProcessRate:   1,
				RequestRate:        0.8,
				BandwidthLimit:     1,
				SlaveWeights:       []int{1, 1},
				Headless:           false,
				VisualMode:         "web",
			},
		},
		{
			Name:        "simple_single_master_slave",
			Description: "Simple Single Master-Slave Network (1 Master, 1 Slave, 1 Home Node)",
			Config: &Config{
				NumMasters:         1,
				NumSlaves:          1,
				NumRelays:          1,
				TotalCycles:        1000,
				MasterRelayLatency: 2,
				RelayMasterLatency: 2,
				RelaySlaveLatency:  1,
				SlaveRelayLatency:  1,
				SlaveProcessRate:   1,
				RequestRate:        0.8,
				BandwidthLimit:     1,
				SlaveWeights:       []int{1},
				Headless:           false,
				VisualMode:         "web",
			},
		},
		{
			Name:        "backpressure_test",
			Description: "Backpressure Test: High load, slow processing (3 Masters, 1 Slave, triggers backpressure)",
			Config: &Config{
				NumMasters:         3,
				NumSlaves:          1,
				NumRelays:          1,
				TotalCycles:        500,
				MasterRelayLatency: 1,
				RelayMasterLatency: 1,
				RelaySlaveLatency:  1,
				SlaveRelayLatency:  1,
				SlaveProcessRate:   1,  // Slow: only 1 packet per cycle
				RequestRate:        1.0, // High: always generate requests
				BandwidthLimit:     3,  // Allow multiple packets per slot
				SlaveWeights:       []int{1},
				Headless:           false,
				VisualMode:         "web",
			},
		},
	}
}

// GetConfigByName returns a copy of the Config for the specified network configuration name
// Returns nil if the configuration is not found
func GetConfigByName(name string) *Config {
	configs := GetPredefinedConfigs()
	for _, cfg := range configs {
		if cfg.Name == name {
			// Create a copy to avoid modifying the original
			original := cfg.Config
			if original == nil {
				return nil
			}
			// Use struct value copy for the base config
			cfgCopy := *original
			// Deep copy the slice to avoid sharing the underlying array
			if original.SlaveWeights != nil {
				cfgCopy.SlaveWeights = make([]int, len(original.SlaveWeights))
				copy(cfgCopy.SlaveWeights, original.SlaveWeights)
			}
			return &cfgCopy
		}
	}
	return nil
}

