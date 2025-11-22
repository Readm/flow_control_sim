package cycle_port

import (
	"testing"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestPendingPacketsIsolation tests that each CycleProcessor instance has its own pendingPackets
func TestPendingPacketsIsolation(t *testing.T) {
	t.Parallel()

	// Create two processors with separate DefaultProcessor instances
	upstreamPort1 := NewCyclePort(8)
	downstreamPort1 := NewCyclePort(8)
	proc1 := &DefaultProcessor{} // Separate instance
	processor1 := NewCycleProcessor(upstreamPort1, downstreamPort1, proc1)

	upstreamPort2 := NewCyclePort(8)
	downstreamPort2 := NewCyclePort(8)
	proc2 := &DefaultProcessor{} // Separate instance
	processor2 := NewCycleProcessor(upstreamPort2, downstreamPort2, proc2)

	// Set initial state for both
	upstreamPort1.SetDoneUntil(0)
	downstreamPort1.UpdateReady(0, true)
	upstreamPort2.SetDoneUntil(0)
	downstreamPort2.UpdateReady(0, false) // processor2's downstream is not ready

	// Send a packet to processor1
	pkt1 := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "processor1"},
	}
	upstreamPort1.Chan() <- pkt1
	upstreamPort1.SetDoneUntil(1)

	// Send a packet to processor2
	pkt2 := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "processor2"},
	}
	upstreamPort2.Chan() <- pkt2
	upstreamPort2.SetDoneUntil(1)

	// Process cycle 0 for both
	err1 := processor1.ProcessCycle(0)
	if err1 != nil {
		t.Fatalf("processor1.ProcessCycle failed: %v", err1)
	}

	err2 := processor2.ProcessCycle(0)
	if err2 != nil {
		t.Fatalf("processor2.ProcessCycle failed: %v", err2)
	}

	// Verify: processor1's packet should be sent (downstream was ready)
	select {
	case received := <-downstreamPort1.ReceiveChan():
		if received.Packet.Payload != "processor1" {
			t.Errorf("processor1: expected 'processor1', got '%s'", received.Packet.Payload)
		}
	default:
		t.Error("processor1: packet should have been sent")
	}

	// Verify: processor2's packet should NOT be sent (downstream was not ready)
	// It should be in pendingPackets
	select {
	case <-downstreamPort2.ReceiveChan():
		t.Error("processor2: packet should NOT have been sent (downstream not ready)")
	default:
		// Good, packet is pending
	}

	// Verify: processor2's pendingPackets should contain the packet
	if len(proc2.pendingPackets) != 1 {
		t.Errorf("processor2: expected 1 pending packet, got %d", len(proc2.pendingPackets))
	} else if proc2.pendingPackets[0].Packet.Payload != "processor2" {
		t.Errorf("processor2: expected pending packet 'processor2', got '%s'", proc2.pendingPackets[0].Packet.Payload)
	}

	// Verify: processor1's pendingPackets should be empty (packet was sent)
	if len(proc1.pendingPackets) != 0 {
		t.Errorf("processor1: expected 0 pending packets, got %d", len(proc1.pendingPackets))
	}
}

// TestPendingPacketsSharing tests that sharing the same DefaultHooks instance shares pendingPackets
func TestPendingPacketsSharing(t *testing.T) {
	t.Parallel()

	// Create two processors sharing the SAME DefaultProcessor instance
	sharedProc := &DefaultProcessor{} // Shared instance

	upstreamPort1 := NewCyclePort(8)
	downstreamPort1 := NewCyclePort(8)
	processor1 := NewCycleProcessor(upstreamPort1, downstreamPort1, sharedProc)

	upstreamPort2 := NewCyclePort(8)
	downstreamPort2 := NewCyclePort(8)
	processor2 := NewCycleProcessor(upstreamPort2, downstreamPort2, sharedProc) // Same processor instance

	// Set initial state
	upstreamPort1.SetDoneUntil(0)
	downstreamPort1.UpdateReady(0, false) // Not ready
	upstreamPort2.SetDoneUntil(0)
	downstreamPort2.UpdateReady(0, false) // Not ready

	// Send packets to both processors
	pkt1 := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "processor1"},
	}
	upstreamPort1.Chan() <- pkt1
	upstreamPort1.SetDoneUntil(1)

	pkt2 := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "processor2"},
	}
	upstreamPort2.Chan() <- pkt2
	upstreamPort2.SetDoneUntil(1)

	// Process cycle 0 for both
	err1 := processor1.ProcessCycle(0)
	if err1 != nil {
		t.Fatalf("processor1.ProcessCycle failed: %v", err1)
	}

	err2 := processor2.ProcessCycle(0)
	if err2 != nil {
		t.Fatalf("processor2.ProcessCycle failed: %v", err2)
	}

	// Verify: both packets should be in the shared pendingPackets
	if len(sharedProc.pendingPackets) != 2 {
		t.Errorf("shared processor: expected 2 pending packets, got %d", len(sharedProc.pendingPackets))
	}

	// Verify both packets are present
	found1, found2 := false, false
	for _, pkt := range sharedProc.pendingPackets {
		if pkt.Packet.Payload == "processor1" {
			found1 = true
		}
		if pkt.Packet.Payload == "processor2" {
			found2 = true
		}
	}
	if !found1 {
		t.Error("shared processor: processor1's packet not found in pendingPackets")
	}
	if !found2 {
		t.Error("shared processor: processor2's packet not found in pendingPackets")
	}
}

