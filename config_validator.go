package main

import (
	"errors"
	"fmt"
)

// ValidateConfig applies structural checks to Config and populates defaults where required.
func ValidateConfig(cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}

	if cfg.NumMasters <= 0 {
		return fmt.Errorf("NumMasters must be positive, got %d", cfg.NumMasters)
	}
	if cfg.NumSlaves <= 0 {
		return fmt.Errorf("NumSlaves must be positive, got %d", cfg.NumSlaves)
	}
	if cfg.NumRelays < 0 {
		return fmt.Errorf("NumRelays must be non-negative, got %d", cfg.NumRelays)
	}
	if cfg.TotalCycles <= 0 {
		return fmt.Errorf("TotalCycles must be positive, got %d", cfg.TotalCycles)
	}
	if cfg.RequestRateConfig < 0 || cfg.RequestRateConfig > 1 {
		return fmt.Errorf("RequestRateConfig must be within [0,1], got %.3f", cfg.RequestRateConfig)
	}
	if cfg.DispatchQueueCapacity == -1 {
		return errors.New("DispatchQueueCapacity cannot be -1")
	}

	if cfg.SlaveWeights == nil || len(cfg.SlaveWeights) != cfg.NumSlaves {
		cfg.SlaveWeights = make([]int, cfg.NumSlaves)
		for i := range cfg.SlaveWeights {
			cfg.SlaveWeights[i] = 1
		}
	}

	if cfg.BandwidthLimit <= 0 {
		cfg.BandwidthLimit = DefaultBandwidthLimit
	}
	if cfg.DispatchQueueCapacity <= 0 {
		cfg.DispatchQueueCapacity = DefaultDispatchQueueCapacity
	}
	if cfg.SlaveProcessRate < 0 {
		cfg.SlaveProcessRate = 1
	}

	if cfg.RequestCacheCapacity <= 0 {
		cfg.RequestCacheCapacity = DefaultRequestCacheCapacity
	}
	if cfg.HomeCacheCapacity <= 0 {
		cfg.HomeCacheCapacity = DefaultHomeCacheCapacity
	}
	if cfg.RingInterleaveStride <= 0 {
		cfg.RingInterleaveStride = DefaultRingInterleaveStride
	}

	return nil
}
