package config

import (
	"errors"
	"fmt"
	"time"
)

// EntityConfig contains the minimal parameters required to build the Core/Entity
// layer. Higher level packages are expected to extend this struct.
type EntityConfig struct {
	Nodes []NodeConfig `json:"nodes" yaml:"nodes"`
	Edges []EdgeConfig `json:"edges" yaml:"edges"`
	Link  LinkConfig   `json:"link" yaml:"link"`
}

// NodeConfig describes a single node in the topology.
type NodeConfig struct {
	ID   int    `json:"id" yaml:"id"`
	Type string `json:"type" yaml:"type"`
}

// EdgeConfig describes a directional link between two nodes.
type EdgeConfig struct {
	Src int `json:"src" yaml:"src"`
	Dst int `json:"dst" yaml:"dst"`
}

// LinkConfig exposes the knobs that impact link delay.
type LinkConfig struct {
	BaseDelay  time.Duration `json:"base_delay" yaml:"base_delay"`
	Multiplier uint64        `json:"multiplier" yaml:"multiplier"`
}

// Validate enforces basic invariants required by the Core/Entity skeleton.
func (c EntityConfig) Validate() error {
	if len(c.Nodes) == 0 {
		return errors.New("entity config requires at least one node")
	}

	set := make(map[int]struct{}, len(c.Nodes))
	for _, n := range c.Nodes {
		if n.ID < 0 {
			return errors.New("node id cannot be negative")
		}
		if _, ok := set[n.ID]; ok {
			return fmt.Errorf("duplicate node id %d", n.ID)
		}
		set[n.ID] = struct{}{}
	}

	if c.Link.BaseDelay < 0 {
		return errors.New("link base delay cannot be negative")
	}

	return nil
}

// EffectiveDelay returns the computed delay for the provided cycle based on the
// configured multiplier. This helper is primarily used in tests or when the
// caller does not need a dedicated link strategy.
func (c LinkConfig) EffectiveDelay() time.Duration {
	multiplier := c.Multiplier
	if multiplier == 0 {
		multiplier = 1
	}
	return time.Duration(multiplier) * c.BaseDelay
}
