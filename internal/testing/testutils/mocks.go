package testutils

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// MockUpstream is a simple upstream component for testing.
// It sends packets via its downstream port.
type MockUpstream struct {
	ToDownstream ahead_port.InPort
	SentPackets  []packet.Packet
}

func NewMockUpstream() *MockUpstream {
	return &MockUpstream{
		SentPackets: make([]packet.Packet, 0),
	}
}

func (m *MockUpstream) SetDownstreamPort(port ahead_port.InPort) {
	m.ToDownstream = port
}

func (m *MockUpstream) SendPacket(cycle int, pkt packet.Packet) bool {
	pwc := ahead_port.PacketWithCycle{
		Cycle:  cycle,
		Packet: pkt,
	}
	success := m.ToDownstream.TrySend(cycle, pwc)
	if success {
		m.SentPackets = append(m.SentPackets, pkt)
	}
	return success
}

func (m *MockUpstream) MarkDone(cycle int) {
	m.ToDownstream.MarkDone(cycle)
}

// MockDownstream is a simple downstream component for testing.
// It receives packets via its upstream port.
type MockDownstream struct {
	FromUpstream     ahead_port.OutPort
	ReceivedPackets  []packet.Packet
	ReceivedAtCycles []int
}

func NewMockDownstream() *MockDownstream {
	return &MockDownstream{
		ReceivedPackets:  make([]packet.Packet, 0),
		ReceivedAtCycles: make([]int, 0),
	}
}

func (m *MockDownstream) SetUpstreamPort(port ahead_port.OutPort) {
	m.FromUpstream = port
}

func (m *MockDownstream) ReceivePackets(cycle int) []packet.Packet {
	packets := m.FromUpstream.Receive(cycle)
	m.ReceivedPackets = append(m.ReceivedPackets, packets...)
	for range packets {
		m.ReceivedAtCycles = append(m.ReceivedAtCycles, cycle)
	}
	return packets
}

func (m *MockDownstream) UpdateReady(cycle int, ready bool) {
	m.FromUpstream.UpdateReady(cycle, ready)
}

func (m *MockDownstream) WaitDone(cycle int) {
	m.FromUpstream.WaitDone(cycle)
}

// CreateTestConnection creates a Port and connects mock upstream/downstream for testing.
// Returns (port, upstream, downstream).
func CreateTestConnection() (*ahead_port.Port, *MockUpstream, *MockDownstream) {
	upstream := NewMockUpstream()
	downstream := NewMockDownstream()
	port := ahead_port.Connect(upstream, downstream)
	return port, upstream, downstream
}

// AssertPacketsEqual checks if two packet slices are equal.
func AssertPacketsEqual(t *testing.T, got, want []packet.Packet) {
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
