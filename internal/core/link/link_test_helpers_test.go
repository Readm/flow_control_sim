package link

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// createTestPorts creates a pair of InPort and OutPort.
// Returns (inPort, outPort) that need to be plugged.
func createTestPorts(buffer int) (ahead_port.InPort, ahead_port.OutPort) {
	// Create a mock upstream OutPort (channel will be set by Plug)
	upstreamOutPort := &mockOutPort{
		done: -1,
	}

	// Create a mock downstream InPort (channel will be set by Plug)
	downstreamInPort := &mockInPort{
		readyUntil: 1024,
	}

	return downstreamInPort, upstreamOutPort
}

// mockOutPort implements OutPort for testing.
type mockOutPort struct {
	ch       chan ahead_port.PacketWithCycle
	done     int64
	doneMu   sync.Mutex
	doneCond *sync.Cond
}

func (m *mockOutPort) ReceiveChan() <-chan ahead_port.PacketWithCycle { return m.ch }

func (m *mockOutPort) WaitDone(cycle int) {
	m.doneMu.Lock()
	defer m.doneMu.Unlock()

	if m.doneCond == nil {
		m.doneCond = sync.NewCond(&m.doneMu)
	}

	for atomic.LoadInt64(&m.done) < int64(cycle) {
		m.doneCond.Wait()
	}
}

func (m *mockOutPort) GetPackets(cycle int) []packet.Packet {
	// Wait for this mock to complete the cycle
	m.WaitDone(cycle)

	// Simple mock: just drain all packets from channel
	// Real implementation would filter by cycle
	var packets []packet.Packet
	for {
		select {
		case pwc := <-m.ch:
			packets = append(packets, pwc.Packet)
		default:
			return packets
		}
	}
}

func (m *mockOutPort) GetDone() int {
	return int(atomic.LoadInt64(&m.done))
}

func (m *mockOutPort) SetDone(cycle int) {
	atomic.StoreInt64(&m.done, int64(cycle))

	m.doneMu.Lock()
	if m.doneCond != nil {
		m.doneCond.Broadcast()
	}
	m.doneMu.Unlock()
}

func (m *mockOutPort) Plug(in ahead_port.InPort) chan ahead_port.PacketWithCycle {
	panic("mockOutPort.Plug not implemented")
}

func (m *mockOutPort) SetOutChannel(ch chan ahead_port.PacketWithCycle, downstream ahead_port.InPort) {
	m.ch = ch
}

// mockInPort implements InPort for testing.
type mockInPort struct {
	ch         chan ahead_port.PacketWithCycle
	readyUntil int
}

func (m *mockInPort) TrySendPacket(cycle int, pkt ahead_port.PacketWithCycle) bool {
	if cycle >= m.readyUntil {
		return false
	}
	m.ch <- pkt
	return true
}

func (m *mockInPort) IsReadyNonBlocking(cycle int) (bool, bool) {
	return cycle < m.readyUntil, true
}

func (m *mockInPort) Plug(out ahead_port.OutPort) chan ahead_port.PacketWithCycle {
	panic("mockInPort.Plug not implemented")
}

func (m *mockInPort) SetInChannel(ch chan ahead_port.PacketWithCycle, upstream ahead_port.OutPort) {
	m.ch = ch
}

func sendPacketToOutPort(t *testing.T, port ahead_port.OutPort, cycle int, pkt packet.Packet) {
	t.Helper()
	env := ahead_port.PacketWithCycle{
		Cycle:  cycle,
		Packet: pkt,
	}
	// For mockOutPort, send directly to its channel
	if mock, ok := port.(*mockOutPort); ok {
		select {
		case mock.ch <- env:
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout sending packet %+v", pkt)
		}
	} else {
		t.Fatalf("sendPacketToOutPort requires mockOutPort")
	}
}

func receivePacketsFromInPort(t *testing.T, port ahead_port.InPort, expected int) []ahead_port.PacketWithCycle {
	t.Helper()
	results := make([]ahead_port.PacketWithCycle, 0, expected)

	// New API: InPort doesn't expose packets directly
	// We need to read from the channel that was set up during Plug
	if mock, ok := port.(*mockInPort); ok {
		if mock.ch == nil {
			t.Fatalf("mockInPort.ch is nil - port not plugged?")
		}
		for i := 0; i < expected; i++ {
			select {
			case pkt := <-mock.ch:
				results = append(results, pkt)
			case <-time.After(200 * time.Millisecond):
				t.Fatalf("timeout waiting for packet %d (received %d so far)", i, len(results))
			}
		}
	} else {
		t.Fatalf("receivePacketsFromInPort requires mockInPort")
	}
	return results
}

func ensureNoAdditionalPacketsInPort(t *testing.T, port ahead_port.InPort) {
	t.Helper()
	if mock, ok := port.(*mockInPort); ok {
		select {
		case pkt := <-mock.ch:
			t.Fatalf("unexpected extra packet: %+v", pkt)
		default:
		}
	}
}
