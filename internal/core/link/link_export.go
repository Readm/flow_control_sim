package link

import (
	"github.com/Readm/flow_sim/internal/core/state"
)

// ExportState exports the state of the Link.
func (l *Link) ExportState(cfg state.ExportConfig) state.LinkState {
	// Link access assumes static time (no concurrent modifications)

	ls := state.LinkState{
		SourceID:     l.sourceID,
		TargetID:     l.targetID,
		CurrentCycle: l.currentCycle,
		Latency:      l.latency,
		Bandwidth:    l.bandwidth,
		Occupancy:    l.SnapshotOccupancy(),
	}

	return ls
}
