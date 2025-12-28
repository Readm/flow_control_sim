package cache

import (
	"github.com/Readm/flow_sim/internal/core/state"
)

// ExportState exports the state of the FullyAssociativeCache.
func (c *FullyAssociativeCache) ExportState(cfg state.ExportConfig) state.CacheState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cs := state.CacheState{
		Hits:       c.stats.Hits,
		Misses:     c.stats.Misses,
		Accesses:   c.stats.Accesses,
		Evictions:  c.stats.Evictions,
		Writebacks: c.stats.Writebacks,
	}

	if cfg.DetailLevel >= state.DetailLevelFull {
		cs.Lines = make([]state.CacheLineState, 0, len(c.lines))
		for addr, line := range c.lines {
			cs.Lines = append(cs.Lines, state.CacheLineState{
				Address: addr,
				State:   string(line.State),
				Tag:     addr, // In fully associative, tag is essentially the address
			})
		}
	}

	return cs
}
