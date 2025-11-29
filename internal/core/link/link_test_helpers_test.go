package link

import (
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

func newTestAheadPort(buffer int) ahead_port.AheadPort {
	var port ahead_port.AheadPort = ahead_port.NewAheadPort(buffer)
	if sp, ok := port.(*ahead_port.SinglePort); ok {
		sp.SetReadyUntil(1024)
		sp.UpdateReady(0, true)
	}
	port.SetDone(-1)
	return port
}

func sendPacket(t *testing.T, port ahead_port.AheadPort, cycle int, pkt packet.Packet) {
	t.Helper()
	env := ahead_port.PacketWithCycle{
		Cycle:  cycle,
		Packet: pkt,
	}
	select {
	case port.SendChan() <- env:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timeout sending packet %+v", pkt)
	}
}

func receivePackets(t *testing.T, port ahead_port.AheadPort, expected int) []ahead_port.PacketWithCycle {
	t.Helper()
	results := make([]ahead_port.PacketWithCycle, 0, expected)
	for i := 0; i < expected; i++ {
		select {
		case pkt := <-port.ReceiveChan():
			results = append(results, pkt)
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("timeout waiting for packet %d", i)
		}
	}
	return results
}

func ensureNoAdditionalPackets(t *testing.T, port ahead_port.AheadPort) {
	t.Helper()
	select {
	case pkt := <-port.ReceiveChan():
		t.Fatalf("unexpected extra packet: %+v", pkt)
	default:
	}
}
