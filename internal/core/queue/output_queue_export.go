package queue

import (
	"encoding/json"

	"github.com/Readm/flow_sim/internal/core/state"
)

// ExportState exports the state of the OutputQueue.
func (oq *OutputQueue) ExportState(cfg state.ExportConfig) state.QueueState {
	qs := state.QueueState{
		Type:      "OutputQueue",
		Length:    oq.Length(),
		Capacity:  oq.Capacity(),
		Bandwidth: oq.outBandwidth,
		Packets:   make([]state.PacketState, 0),
	}

	if cfg.DetailLevel >= state.DetailLevelFull {
		// OutputQueue is a ring buffer
		// Iterate from head to tail
		// We can't easily lock here as OutputQueue doesn't seem to have a mutex exposed
		// But in a single-threaded export (or stopped simulation), it should be fine.
		// WARNING: Data race if running concurrently. MockController locks stateMu but not internal components.
		// Assuming we pause or are careful.

		count := oq.count
		curr := oq.head
		for i := 0; i < count; i++ {
			pkt := oq.buffer[curr]

			// Serialize for visualization
			msgBytes, _ := json.Marshal(pkt.Packet)

			qs.Packets = append(qs.Packets, state.PacketState{
				Src:   pkt.Packet.SourceID,
				Dst:   pkt.Packet.TargetID,
				Cycle: pkt.Cycle,
				Msg:   string(msgBytes),
				Type:  "Packet",
			})
			curr = (curr + 1) % oq.capacity
		}
	}

	return qs
}
