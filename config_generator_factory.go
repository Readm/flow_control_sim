package main

import (
	"math/rand"
	"time"
)

// ConfigGeneratorFactory prepares request generators based on validated config.
type ConfigGeneratorFactory struct {
	random *rand.Rand
}

// NewConfigGeneratorFactory creates a factory with time-based seed.
func NewConfigGeneratorFactory() *ConfigGeneratorFactory {
	return &ConfigGeneratorFactory{
		random: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// WithRand allows providing custom rand source (useful for tests).
func (f *ConfigGeneratorFactory) WithRand(r *rand.Rand) *ConfigGeneratorFactory {
	f.random = r
	return f
}

// BuildDefault returns the generator that should be used for all masters unless overridden.
func (f *ConfigGeneratorFactory) BuildDefault(cfg *Config) RequestGenerator {
	if cfg == nil {
		return nil
	}
	if cfg.RequestGenerator != nil {
		return cfg.RequestGenerator
	}
	if len(cfg.ScheduleConfig) > 0 {
		return NewScheduleGenerator(cfg.ScheduleConfig)
	}
	if cfg.RequestRateConfig > 0 {
		return NewProbabilityGenerator(cfg.RequestRateConfig, cfg.SlaveWeights, f.random)
	}
	return nil
}

// BuildPerMaster ensures the RequestGenerators slice is filled appropriately.
func (f *ConfigGeneratorFactory) BuildPerMaster(cfg *Config, defaultGen RequestGenerator) []RequestGenerator {
	if cfg == nil {
		return nil
	}
	if len(cfg.RequestGenerators) >= cfg.NumMasters {
		return cfg.RequestGenerators[:cfg.NumMasters]
	}

	generators := make([]RequestGenerator, cfg.NumMasters)
	for i := 0; i < cfg.NumMasters; i++ {
		if i < len(cfg.RequestGenerators) && cfg.RequestGenerators[i] != nil {
			generators[i] = cfg.RequestGenerators[i]
		} else {
			generators[i] = defaultGen
		}
	}
	return generators
}
