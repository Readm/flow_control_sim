package ahead_port

import (
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// UpstreamSendWithCycleIncrement demonstrates the logic that upstream code should implement
// when downstream is not ready for a specific cycle.
//
// Logic:
// 1. Upstream wants to send a packet at cycle N
// 2. Check if downstream is ready: Ready(N)
// 3. If Ready(N) == false:
//   - Increment the packet's cycle: cycle++
//   - Check Ready(newCycle) again
//   - Repeat until Ready() returns true
//
// 4. Send the packet with the incremented cycle
// 5. The cycle increment equals the number of consecutive non-ready cycles
func UpstreamSendWithCycleIncrement(port AheadPort, originalCycle int, pkt packet.Packet) {
	currentCycle := originalCycle
	cycleIncrement := 0

	// Keep checking and incrementing until downstream is ready
	for {
		// Check if downstream is ready for current cycle
		if port.Ready(currentCycle) {
			// Ready! Send the packet with incremented cycle
			packetWithCycle := PacketWithCycle{
				Cycle:  currentCycle,
				Packet: pkt,
			}
			port.Chan() <- packetWithCycle

			// Set Done after sending
			port.SetDone(currentCycle)

			// cycleIncrement now equals the number of non-ready cycles skipped
			// originalCycle + cycleIncrement == currentCycle
			return
		}

		// Not ready, increment cycle and retry
		cycleIncrement++
		currentCycle++

		// Safety: prevent infinite loop (in real code, you might want a max retry limit)
		if cycleIncrement > 1000 {
			// Handle error: downstream not ready for too many cycles
			return
		}
	}
}

// Example usage in upstream code:
//
// func (upstream *UpstreamComponent) SendPacket(cycle int, pkt packet.Packet) {
//     // Original logic: just send
//     // packetWithCycle := PacketWithCycle{Cycle: cycle, Packet: pkt}
//     // downstreamPort.Chan() <- packetWithCycle
//
//     // New logic: handle non-ready cycles by incrementing
//     UpstreamSendWithCycleIncrement(downstreamPort, cycle, pkt)
//     // Now the packet's cycle will be automatically incremented if downstream
//     // is not ready for the original cycle
// }
