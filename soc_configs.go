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
			Name:        "ring_demo",
			Description: "Ring topology demo: 2 RNs with cache, 1 HN with directory/cache, 2 SNs on ring",
			Config: &Config{
				NumMasters:           2,
				NumSlaves:            2,
				NumRelays:            1,
				TotalCycles:          400,
				MasterRelayLatency:   2,
				RelayMasterLatency:   2,
				RelaySlaveLatency:    1,
				SlaveRelayLatency:    1,
				SlaveProcessRate:     1,
				RequestRateConfig:    0.6,
				BandwidthLimit:       1,
				SlaveWeights:         []int{1, 1},
				Headless:             false,
				VisualMode:           "web",
				RingEnabled:          true,
				RingInterleaveStride: 1,
				RequestCacheCapacity: DefaultRequestCacheCapacity,
				HomeCacheCapacity:    DefaultHomeCacheCapacity,
			},
		},
		{
			Name:        "readonce_mesi_snoop",
			Description: "ReadOnce MESI Snoop Test: 2 RNs (both with cache), 1 HN, 1 SN. RN0 reads first and caches data, RN1 reads same address triggering Snoop to RN0",
			Config: &Config{
				NumMasters:         2,
				NumSlaves:          1,
				NumRelays:          1,
				TotalCycles:        150,
				MasterRelayLatency: 2,
				RelayMasterLatency: 2,
				RelaySlaveLatency:  1,
				SlaveRelayLatency:  1,
				SlaveProcessRate:   1,
				BandwidthLimit:     1,
				SlaveWeights:       []int{1},
				Headless:           true,
				VisualMode:         "web",
				// Schedule: RN0 reads at cycle 0, RN1 reads same address at cycle 30 (triggers Snoop)
				ScheduleConfig: map[int]map[int][]ScheduleItem{
					0: {
						0: {
							{
								SlaveIndex:      0,
								TransactionType: CHITxnReadOnce,
								Address:         DefaultAddressBase, // 0x1000
							},
						},
					},
					30: {
						1: {
							{
								SlaveIndex:      0,
								TransactionType: CHITxnReadOnce,
								Address:         DefaultAddressBase, // Same address to trigger Snoop
							},
						},
					},
				},
				InitialCacheState: nil,
			},
		},
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
				RequestRateConfig:  0.8,
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
				RequestRateConfig:  0.8,
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
				SlaveProcessRate:   1,   // Slow: only 1 packet per cycle
				RequestRateConfig:  1.0, // High: always generate requests
				BandwidthLimit:     3,   // Allow multiple packets per slot
				SlaveWeights:       []int{1},
				Headless:           false,
				VisualMode:         "web",
			},
		},
		{
			Name:        "single_request_10cycle_latency",
			Description: "Single Request Test: 1 RN, 1 HN, 1 SN, 10-cycle latency, single ReadNoSnp request",
			Config: &Config{
				NumMasters:         1,
				NumSlaves:          1,
				NumRelays:          1,
				TotalCycles:        60,
				MasterRelayLatency: 10,
				RelayMasterLatency: 10,
				RelaySlaveLatency:  10,
				SlaveRelayLatency:  10,
				SlaveProcessRate:   1,
				BandwidthLimit:     1,
				SlaveWeights:       []int{1},
				Headless:           false,
				VisualMode:         "web",
				// Use ScheduleGenerator for deterministic single request at cycle 0
				ScheduleConfig: map[int]map[int][]ScheduleItem{
					0: {
						0: {
							{
								SlaveIndex:      0,
								TransactionType: CHITxnReadNoSnp,
							},
						},
					},
				},
			},
		},
	}
}

// GetConfigByName returns a copy of the Config for the specified network configuration name
// Returns nil if the configuration is not found
// Note: RequestGenerator will be created in Simulator initialization (requires rng)
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

			// Deep copy ScheduleConfig if present
			if original.ScheduleConfig != nil {
				cfgCopy.ScheduleConfig = make(map[int]map[int][]ScheduleItem)
				for cycle, masterMap := range original.ScheduleConfig {
					cfgCopy.ScheduleConfig[cycle] = make(map[int][]ScheduleItem)
					for masterIdx, items := range masterMap {
						itemsCopy := make([]ScheduleItem, len(items))
						copy(itemsCopy, items)
						cfgCopy.ScheduleConfig[cycle][masterIdx] = itemsCopy
					}
				}
			}

			// RequestGenerator will be created in Simulator initialization
			return &cfgCopy
		}
	}
	return nil
}
