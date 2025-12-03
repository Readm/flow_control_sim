package queue

import (
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// createTestPorts creates a pair of InPort and OutPort for testing.
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
	done     int
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

	for m.done < cycle {
		m.doneCond.Wait()
	}
}

func (m *mockOutPort) GetDone() int {
	m.doneMu.Lock()
	defer m.doneMu.Unlock()
	return m.done
}

func (m *mockOutPort) SetDone(cycle int) {
	m.doneMu.Lock()
	m.done = cycle
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

func (m *mockInPort) SendChan() chan<- ahead_port.PacketWithCycle { return m.ch }
func (m *mockInPort) Ready(cycle int) bool                        { return cycle < m.readyUntil }
func (m *mockInPort) ReadyNonBlocking(cycle int) (bool, bool) {
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
	// For mockInPort, read directly from its channel
	if mock, ok := port.(*mockInPort); ok {
		for i := 0; i < expected; i++ {
			select {
			case pkt := <-mock.ch:
				results = append(results, pkt)
			case <-time.After(200 * time.Millisecond):
				t.Fatalf("timeout waiting for packet %d", i)
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
