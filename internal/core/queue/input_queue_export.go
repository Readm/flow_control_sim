package queue

import (
	"github.com/Readm/flow_sim/internal/core/state"
)

// ExportState exports the state of the InputQueue.
func (iq *InputQueue) ExportState(cfg state.ExportConfig) state.QueueState {
	qs := state.QueueState{
		Type:      "InputQueue",
		Length:    iq.Length(),
		Capacity:  iq.Capacity(),
		Bandwidth: iq.inBandwidth,
		Packets:   make([]state.PacketState, 0),
		// Bitmap generation: iterate and build string "1001..." (1=Occupied)
		Bitmap: func() string {
			iq.arrayMu.Lock()
			defer iq.arrayMu.Unlock()
			b := make([]byte, len(iq.freeBitmap))
			for i, free := range iq.freeBitmap {
				if free {
					b[i] = '0'
				} else {
					b[i] = '1'
				}
			}
			return string(b)
		}(),
	}

	// For Full detail, verify or export packets.
	// Summary detail might not need individual packet info to save BW.
	if cfg.DetailLevel >= state.DetailLevelFull {
		// Use PeekPickTo without popping
		// Note: PeekPickTo is destructive to candidatesBuffer but expected to be safe.
		// However, it's safer to just iterate slots if we have access.
		// Since we have array access:
		iq.arrayMu.Lock()
		defer iq.arrayMu.Unlock()
		for i := 0; i < iq.capacity; i++ {
			if !iq.freeBitmap[i] {
				p := iq.slots[i].Packet
				qs.Packets = append(qs.Packets, state.PacketState{
					Src:   p.SourceID,
					Dst:   p.TargetID,
					Cycle: iq.slots[i].Cycle,
					Msg:   p.Payload,
					Type:  "Packet",
				})
			}
		}
	} else {
		// Summary mode: Maybe just counts? Or first few?
		// Stick to counts for now.
	}

	return qs
}
