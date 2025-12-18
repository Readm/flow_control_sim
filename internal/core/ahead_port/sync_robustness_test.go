package ahead_port

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestAsynchronousDrift verifies that two components can run at very different speeds
// without losing packets or deadlocking.
func TestAsynchronousDrift(t *testing.T) {
	port := NewPort()
	in := port.AsInPort()
	out := port.AsOutPort()

	const cycles = 100
	receivedCount := int64(0)

	// Upstream runs as fast as possible
	go func() {
		for cycle := 0; cycle < cycles; cycle++ {
			// Sender must wait for Ready
			if !in.IsReady(cycle) {
				t.Errorf("Upstream: cycle %d not ready", cycle)
				return
			}
			in.TrySend(cycle, PacketWithCycle{
				Cycle:  cycle,
				Packet: packet.Packet{Payload: fmt.Sprintf("p%d", cycle)},
			})
			in.MarkDone(cycle)
		}
	}()

	// Downstream is slow
	for cycle := 0; cycle < cycles; cycle++ {
		out.UpdateReady(cycle, true)
		time.Sleep(2 * time.Millisecond) // Artificial delay

		pkts := out.Receive(cycle)
		if len(pkts) != 1 {
			t.Fatalf("Downstream: cycle %d expected 1 packet, got %d", cycle, len(pkts))
		}
		if pkts[0].Payload != fmt.Sprintf("p%d", cycle) {
			t.Errorf("Downstream: cycle %d payload mismatch", cycle)
		}
		atomic.AddInt64(&receivedCount, 1)
	}

	if atomic.LoadInt64(&receivedCount) != int64(cycles) {
		t.Errorf("Expected %d packets, got %d", cycles, receivedCount)
	}
}

// TestProtocolViolation_LateReceive verifies that calling Receive before WaitUpstreamDone
// still works because Receive is now internally blocking and draining.
func TestProtocolViolation_LateReceive(t *testing.T) {
	port := NewPort()
	in := port.AsInPort()
	out := port.AsOutPort()

	go func() {
		time.Sleep(100 * time.Millisecond)
		// ...
	}()

	go func() {
		// Mock downstream ready
		out.UpdateReady(0, true)
		time.Sleep(200 * time.Millisecond)
		in.TrySend(0, PacketWithCycle{Cycle: 0, Packet: packet.Packet{Payload: "late"}})
		in.MarkDone(0)
	}()

	// Downstream calls Receive(0) WITHOUT calling WaitUpstreamDone(0)
	// It should still block and eventually return the packet.
	pkts := out.Receive(0)
	if len(pkts) != 1 || pkts[0].Payload != "late" {
		t.Fatalf("Expected 1 'late' packet, got %v", pkts)
	}
}

// TestBackpressureDeadlockResilience verifies that filling the channel doesn't hang the simulation.
func TestBackpressureDeadlockResilience(t *testing.T) {
	// Use small capacity to trigger backpressure quickly
	p := &Port{
		channel:        make(chan PacketWithCycle, 4),
		upstreamSync:   NewComponentSync(),
		downstreamSync: NewComponentSync(),
		pendingPackets: make(map[int][]packet.Packet),
	}
	in := p.AsInPort()
	out := p.AsOutPort()

	const totalPackets = 20

	// Downstream is ready for all cycles
	for i := 0; i < totalPackets; i++ {
		out.UpdateReady(i, true)
	}

	senderDone := make(chan bool)
	go func() {
		for i := 0; i < totalPackets; i++ {
			// This will block when channel (cap 4) is full
			in.TrySend(i, PacketWithCycle{Cycle: i, Packet: packet.Packet{Payload: "data"}})
			in.MarkDone(i)
		}
		senderDone <- true
	}()

	// Receiver waits a bit to ensure sender blocks
	time.Sleep(50 * time.Millisecond)

	for i := 0; i < totalPackets; i++ {
		pkts := out.Receive(i)
		if len(pkts) != 1 {
			t.Fatalf("Cycle %d: expected 1 packet, got %d", i, len(pkts))
		}
	}

	select {
	case <-senderDone:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Sender is still deadlocked")
	}
}
