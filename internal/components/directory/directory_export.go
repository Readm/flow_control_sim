package directory

import (
	"github.com/Readm/flow_sim/internal/core/state"
)

// ExportState exports the state of the FullyAssociativeDirectory.
func (d *FullyAssociativeDirectory) ExportState(cfg state.ExportConfig) state.DirectoryState {
	d.mu.RLock()
	defer d.mu.RUnlock()

	ds := state.DirectoryState{}

	if cfg.DetailLevel >= state.DetailLevelFull {
		ds.Entries = make([]state.DirectoryEntryState, 0, len(d.entries))
		for addr, entry := range d.entries {
			sharers := make([]int, len(entry.Sharers))
			copy(sharers, entry.Sharers)

			ds.Entries = append(ds.Entries, state.DirectoryEntryState{
				Address: addr,
				State:   string(entry.State),
				Sharers: sharers,
				Owner:   entry.Owner,
			})
		}
	}

	return ds
}
