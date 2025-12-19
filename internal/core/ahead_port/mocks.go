package ahead_port

import "github.com/Readm/flow_sim/internal/dataflow/packet"

// MockUpstream is a simple upstream component for testing.
// It can send packets and mark cycles as done.
type MockUpstream struct {
	toDownstream InPort
}

// SetDownstreamPort sets the downstream port.
func (m *MockUpstream) SetDownstreamPort(port InPort) {
	m.toDownstream = port
}

// SendPacket sends a packet to the downstream component.
// This uses the blocking TrySend method.
func (m *MockUpstream) SendPacket(cycle int, pkt packet.Packet) bool {
	pwc := PacketWithCycle{
		Cycle:  cycle,
		Packet: pkt,
	}
	return m.toDownstream.TrySend(cycle, pwc)
}

// TryPeekSendPacket attempts to send a packet using non-blocking check.
func (m *MockUpstream) TryPeekSendPacket(cycle int, pkt packet.Packet) bool {
	pwc := PacketWithCycle{
		Cycle:  cycle,
		Packet: pkt,
	}
	ready, decided := m.toDownstream.PeekReady(cycle)
	if decided && ready {
		return m.toDownstream.TrySend(cycle, pwc)
	}
	return false
}

// MarkDone marks a cycle as done.
func (m *MockUpstream) MarkDone(cycle int) {
	m.toDownstream.MarkDone(cycle)
}

// MockDownstream is a simple downstream component for testing.
// It can receive packets and update ready status.
type MockDownstream struct {
	fromUpstream OutPort
}

// SetUpstreamPort sets the upstream port.
func (m *MockDownstream) SetUpstreamPort(port OutPort) {
	m.fromUpstream = port
}

// ReceivePackets receives packets for a specific cycle.
func (m *MockDownstream) ReceivePackets(cycle int) []packet.Packet {
	return m.fromUpstream.Receive(cycle)
}

// UpdateReady updates the ready status for a specific cycle.
func (m *MockDownstream) UpdateReady(cycle int, ready bool) {
	m.fromUpstream.UpdateReady(cycle, ready)
}

// WaitDone waits for the upstream to complete a cycle.
func (m *MockDownstream) WaitDone(cycle int) {
	m.fromUpstream.WaitDone(cycle)
}
