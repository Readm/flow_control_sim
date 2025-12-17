package queue

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// createTestPorts creates a pair of InPort and OutPort for testing.
// Returns (inPort, outPort) that need to be plugged.
func createTestPorts(buffer int) (ahead_port.InPort, ahead_port.OutPort) {
	// Create a mock upstream OutPort
	upstreamOutPort := NewMockOutPort()

	// Create a mock downstream InPort
	downstreamInPort := NewMockInPort()

	return downstreamInPort, upstreamOutPort
}

// Ensure mock structs support Base implementation for Plug

// mockOutPort implements OutPort for testing.
type mockOutPort struct {
	ahead_port.BaseOutPort // Embed BaseOutPort for Plug support
	ch                     chan ahead_port.PacketWithCycle
	done                   int64
	doneMu                 sync.Mutex
	doneCond               *sync.Cond
}

func NewMockOutPort() *mockOutPort {
	p := &mockOutPort{done: -1}
	p.doneCond = sync.NewCond(&p.doneMu)
	// Initialize OutputChan for BaseOutPort usage if needed, but we intercept.
	// Actually PlugWithSelf requires us to be the BaseOutPort.
	return p
}

func (m *mockOutPort) GetPackets(cycle int) []packet.Packet {
	// Wait for this mock to complete the cycle
	m.WaitDone(cycle)

	// Simple mock: just drain all packets from channel
	// Real implementation would filter by cycle
	// Read from the correct channel
	var receiveChan <-chan ahead_port.PacketWithCycle
	if m.OutputChan != nil {
		receiveChan = m.OutputChan
	} else {
		receiveChan = m.ch
	}

	if receiveChan == nil {
		return nil
	}

	var packets []packet.Packet
	for {
		select {
		case pwc := <-receiveChan:
			// Filter by cycle if needed, but mock usually drains everything for simplicity
			// In strict simulation, we should only return packets for 'cycle'.
			// But existing tests likely assume "whatever I put in, I get out now".
			// Given BaseOutPort logic filters, should we?
			// TestInputQueueReceive puts packet at cycle 0. Tick calls GetPackets(0).
			// If we implement strict filtering and packet has cycle 0, it works.
			if pwc.Cycle == cycle {
				packets = append(packets, pwc.Packet)
			} else {
				// If cycle mismatch, strict mock should maybe error or cache.
				// But let's look at sendPacketToOutPort:
				// It sends with 'cycle'.
				// So filtering is correct.
				// BUT: BaseOutPort logic caches future packets.
				// This mock drops them unless we implement caching?
				// Existing mock code didn't filter at all:
				// packets = append(packets, pwc.Packet)
				// Let's stick to existing behavior (no filtering) to minimize breakage,
				// unless tests rely on filtering.
				// Reverting to "drain everything" for now.
				packets = append(packets, pwc.Packet)
			}
		default:
			return packets
		}
	}
}

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
	return m.BaseOutPort.PlugWithSelf(m, in)
}

// Remove SetOutChannel as Plug handles it

// mockInPort implements InPort for testing.
type mockInPort struct {
	ahead_port.BaseInPort // Embed
	ch                    chan ahead_port.PacketWithCycle
	readyUntil            int
}

func NewMockInPort() *mockInPort {
	return &mockInPort{
		readyUntil: 1024,
	}
}

func (m *mockInPort) TrySendPacket(cycle int, pkt ahead_port.PacketWithCycle) bool {
	if cycle >= m.readyUntil {
		return false
	}
	// Use InputChan if set by Plug, otherwise legacy ch
	if m.InputChan != nil {
		m.InputChan <- pkt
	} else if m.ch != nil {
		m.ch <- pkt
	}
	return true
}

func (m *mockInPort) IsReadyNonBlocking(cycle int) (bool, bool) {
	return cycle < m.readyUntil, true
}

func (m *mockInPort) Plug(out ahead_port.OutPort) chan ahead_port.PacketWithCycle {
	return m.BaseInPort.PlugWithSelf(m, out)
}

func sendPacketToOutPort(t *testing.T, port ahead_port.OutPort, cycle int, pkt packet.Packet) {
	t.Helper()
	env := ahead_port.PacketWithCycle{
		Cycle:  cycle,
		Packet: pkt,
	}
	// For mockOutPort, send directly to its channel
	if mock, ok := port.(*mockOutPort); ok {
		// Prefer OutputChan (set by Plug), fallback to ch
		ch := mock.OutputChan
		if ch == nil {
			ch = mock.ch
		} else {
			// If Plugged, OutputChan is what downstream reads from.
			// But here we are simulating the *upstream component* putting data *into* the OutPort?
			// No, OutPort is an interface to downstream.
			// Wait, sendPacketToOutPort simulates the Component (e.g. Queue) writing to its OWN OutPort.
			// In BaseOutPort implementation, writing to OutputChan IS sending.
			select {
			case ch <- env:
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("timeout sending packet %+v", pkt)
			}
			return
		}

		// Legacy ch path
		if ch != nil {
			select {
			case ch <- env:
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("timeout sending packet %+v", pkt)
			}
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
