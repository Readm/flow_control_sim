package configs

import app "github.com/Readm/flow_sim/framework/app"

func init() {
	Register(app.ConfigDescriptor{
		Name:        "ring_demo",
		Description: "Ring topology demo: 2 RNs with cache, 1 HN with directory/cache, 2 SNs on ring",
		Config: &app.Config{
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
			RequestCacheCapacity: app.DefaultRequestCacheCapacity,
			HomeCacheCapacity:    app.DefaultHomeCacheCapacity,
		},
	})

	Register(app.ConfigDescriptor{
		Name:        "readonce_mesi_snoop",
		Description: "ReadOnce MESI Snoop Test: RN0 reads first, RN1 later triggering snoop",
		Config: &app.Config{
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
			ScheduleConfig: map[int]map[int][]app.ScheduleItem{
				0: {
					0: {
						{
							SlaveIndex:      0,
							TransactionType: app.CHITxnReadOnce,
							Address:         app.DefaultAddressBase,
						},
					},
				},
				30: {
					1: {
						{
							SlaveIndex:      0,
							TransactionType: app.CHITxnReadOnce,
							Address:         app.DefaultAddressBase,
						},
					},
				},
			},
		},
	})

	Register(app.ConfigDescriptor{
		Name:        "multi_master_multi_slave",
		Description: "Multi-Master Multi-Slave Network (3 Masters, 2 Slaves, 1 Home Node)",
		Config: &app.Config{
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
	})

	Register(app.ConfigDescriptor{
		Name:        "simple_single_master_slave",
		Description: "Simple Single Master-Slave Network (1 Master, 1 Slave, 1 Home Node)",
		Config: &app.Config{
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
	})

	Register(app.ConfigDescriptor{
		Name:        "backpressure_test",
		Description: "Backpressure Test: High load, slow processing",
		Config: &app.Config{
			NumMasters:         3,
			NumSlaves:          1,
			NumRelays:          1,
			TotalCycles:        500,
			MasterRelayLatency: 1,
			RelayMasterLatency: 1,
			RelaySlaveLatency:  1,
			SlaveRelayLatency:  1,
			SlaveProcessRate:   1,
			RequestRateConfig:  1.0,
			BandwidthLimit:     3,
			SlaveWeights:       []int{1},
			Headless:           false,
			VisualMode:         "web",
		},
	})

	Register(app.ConfigDescriptor{
		Name:        "single_request_10cycle_latency",
		Description: "Single request test with deterministc 10-cycle latency",
		Config: &app.Config{
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
			ScheduleConfig: map[int]map[int][]app.ScheduleItem{
				0: {
					0: {
						{
							SlaveIndex:      0,
							TransactionType: app.CHITxnReadNoSnp,
						},
					},
				},
			},
		},
	})
}
