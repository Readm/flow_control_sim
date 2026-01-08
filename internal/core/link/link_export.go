package link

import (
	"fmt"

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
		DisplayData:  make(map[string]interface{}),
	}

	// Phase 2: 从 configRef 读取 Config 和 Display 数据
	if l.configRef != nil {
		ls.EdgeID = l.configRef.EdgeId

		// Copy PacketTypes from configRef
		if l.configRef.PacketTypes != nil && len(*l.configRef.PacketTypes) > 0 {
			packetTypes := make([]string, len(*l.configRef.PacketTypes))
			for i, pt := range *l.configRef.PacketTypes {
				packetTypes[i] = fmt.Sprintf("%d", pt)
			}
			ls.PacketTypes = packetTypes
		}

		// Copy DisplayData from configRef
		ls.DisplayData["data"] = l.configRef.Data
	} else {
		// Fallback: 如果没有 configRef,使用旧方式 (Phase 1 兼容)
		ls.EdgeID = l.edgeID
		ls.PacketTypes = l.packetTypes
		for k, v := range l.displayData {
			ls.DisplayData[k] = v
		}
	}

	return ls
}
