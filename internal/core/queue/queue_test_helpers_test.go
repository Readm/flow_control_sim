package queue

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// mockUpstream is a simple upstream component for testing.
// It sends packets via its downstream port.
type mockUpstream struct {
	toDownstream ahead_port.InPort
	sentPackets  []packet.Packet
}

func newMockUpstream() *mockUpstream {
	return &mockUpstream{
		sentPackets: make([]packet.Packet, 0),
	}
}

func (m *mockUpstream) SetDownstreamPort(port ahead_port.InPort) {
	m.toDownstream = port
}

func (m *mockUpstream) SendPacket(cycle int, pkt packet.Packet) bool {
	pwc := ahead_port.PacketWithCycle{
		Cycle:  cycle,
		Packet: pkt,
	}
	success := m.toDownstream.TrySend(cycle, pwc)
	if success {
		m.sentPackets = append(m.sentPackets, pkt)
	}
	return success
}

func (m *mockUpstream) MarkDone(cycle int) {
	m.toDownstream.MarkDone(cycle)
}

// mockDownstream is a simple downstream component for testing.
// It receives packets via its upstream port.
type mockDownstream struct {
	fromUpstream     ahead_port.OutPort
	receivedPackets  []packet.Packet
	receivedAtCycles []int
}

func newMockDownstream() *mockDownstream {
	return &mockDownstream{
		receivedPackets:  make([]packet.Packet, 0),
		receivedAtCycles: make([]int, 0),
	}
}

func (m *mockDownstream) SetUpstreamPort(port ahead_port.OutPort) {
	m.fromUpstream = port
}

func (m *mockDownstream) ReceivePackets(cycle int) []packet.Packet {
	packets := m.fromUpstream.Receive(cycle)
	m.receivedPackets = append(m.receivedPackets, packets...)
	for range packets {
		m.receivedAtCycles = append(m.receivedAtCycles, cycle)
	}
	return packets
}

func (m *mockDownstream) UpdateReady(cycle int, ready bool) {
	m.fromUpstream.UpdateReady(cycle, ready)
}

func (m *mockDownstream) WaitDone(cycle int) {
	m.fromUpstream.WaitDone(cycle)
}

// createTestConnection creates a Port and connects mock upstream/downstream for testing.
// Returns (port, upstream, downstream).
func createTestConnection() (*ahead_port.Port, *mockUpstream, *mockDownstream) {
	upstream := newMockUpstream()
	downstream := newMockDownstream()
	port := ahead_port.Connect(upstream, downstream)
	return port, upstream, downstream
}

// assertPacketsEqual checks if two packet slices are equal.
func assertPacketsEqual(t *testing.T, got, want []packet.Packet) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("packet count mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].SourceID != want[i].SourceID || got[i].TargetID != want[i].TargetID {
			t.Errorf("packet %d mismatch: got (Src=%d, Dst=%d), want (Src=%d, Dst=%d)",
				i, got[i].SourceID, got[i].TargetID, want[i].SourceID, want[i].TargetID)
		}
	}
}
