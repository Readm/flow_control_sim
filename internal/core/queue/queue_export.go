package queue

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/core/state"
)

// ExportState exports the state of the InputQueue.
func (iq *InputQueue) ExportState(cfg state.ExportConfig) state.QueueState {
	iq.arrayMu.Lock()
	defer iq.arrayMu.Unlock()

	qs := state.QueueState{
		Type:      "Input",
		Length:    iq.Length(),
		Capacity:  iq.Capacity(),
		Bandwidth: iq.inBandwidth,
		Packets:   make([]state.PacketState, 0, iq.Length()),
	}

	// In DetailLevelSummary, we might only show length, but user plan said "empty or partial".
	// Let's include packet summaries even in summary mode for now as they are useful.
	// Or maybe strict to Summary?
	// Plan said: "Brief summary". Let's output all packets for now, optimization later if needed.

	// Iterate slots to find valid packets
	for i, slot := range iq.slots {
		if !iq.freeBitmap[i] {
			qs.Packets = append(qs.Packets, state.PacketState{
				Src:   slot.Packet.SourceID,
				Dst:   slot.Packet.TargetID,
				Cycle: slot.Cycle,
				Msg:   fmt.Sprintf("%v", slot.Packet.Payload),
				Type:  "Packet", // Default type
			})
		}
	}

	return qs
}

// ExportState exports the state of the OutputQueue.
func (oq *OutputQueue) ExportState(cfg state.ExportConfig) state.QueueState {
	// OutputQueue has no mutex, assuming static time access (no concurrent modifications)

	qs := state.QueueState{
		Type:      "Output",
		Length:    oq.Length(),
		Capacity:  oq.Capacity(),
		Bandwidth: oq.outBandwidth,
		Packets:   make([]state.PacketState, 0, oq.Length()),
	}

	// Iterate ring buffer
	// Head to Tail
	idx := oq.head
	for i := 0; i < oq.count; i++ {
		pkt := oq.buffer[idx]
		qs.Packets = append(qs.Packets, state.PacketState{
			Src:   pkt.Packet.SourceID,
			Dst:   pkt.Packet.TargetID,
			Cycle: pkt.Cycle,
			Msg:   fmt.Sprintf("%v", pkt.Packet.Payload),
			Type:  "Packet",
		})
		idx = (idx + 1) % oq.capacity
	}

	return qs
}
