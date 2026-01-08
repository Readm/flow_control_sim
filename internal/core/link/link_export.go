package link

import (
	"github.com/Readm/flow_sim/internal/core/state"
)

// ExportState exports the state of the Link.
func (l *Link) ExportState(cfg state.ExportConfig) state.LinkState {
	// Link access assumes static time (no concurrent modifications)

	ls := state.LinkState{
		SourceID:     l.sourceID,
		SourcePortID: l.sourcePortID,
		TargetID:     l.targetID,
		TargetPortID: l.targetPortID,
		CurrentCycle: l.currentCycle,
		Latency:      l.latency,
		Bandwidth:    l.bandwidth,
		Occupancy:    l.SnapshotOccupancy(),
		EdgeID:       l.edgeID,
		PacketTypes:  l.packetTypes,
		DisplayData:  make(map[string]interface{}),
	}

	// Copy DisplayData
	for k, v := range l.displayData {
		ls.DisplayData[k] = v
	}

	return ls
}
