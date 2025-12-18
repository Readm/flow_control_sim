package queue

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestNewOutputQueue tests OutputQueue creation with default values.
func TestNewOutputQueue(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(10, 2, 3)
	if oq == nil {
		t.Fatal("NewOutputQueue returned nil")
	}

	if oq.Capacity() != 10 {
		t.Fatalf("expected capacity 10, got %d", oq.Capacity())
	}

	if oq.Length() != 0 {
		t.Fatalf("expected initial length 0, got %d", oq.Length())
	}

	if oq.IsFull() {
		t.Fatal("queue should not be full initially")
	}
}

// TestNewOutputQueueDefaults tests default values.
func TestNewOutputQueueDefaults(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(0, 1, 1)
	if oq.Capacity() != 8 {
		t.Fatalf("expected default capacity 8, got %d", oq.Capacity())
	}
}

// TestNewOutputQueuePanics tests panic conditions.
func TestNewOutputQueuePanics(t *testing.T) {
	t.Parallel()

	t.Run("zero_inBandwidth", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic with zero inBandwidth")
			}
		}()
		NewOutputQueue(8, 0, 1)
	})

	t.Run("zero_outBandwidth", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic with zero outBandwidth")
			}
		}()
		NewOutputQueue(8, 1, 0)
	})
}

// TestInjectPackets tests injecting packets into the queue.
func TestInjectPackets(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(10, 2, 2)

	packets := []packet.Packet{
		{SourceID: 1, TargetID: 2, Payload: "packet1"},
		{SourceID: 1, TargetID: 3, Payload: "packet2"},
		{SourceID: 1, TargetID: 4, Payload: "packet3"},
	}

	err := oq.InjectPackets(0, packets)
	if err != nil {
		t.Fatalf("InjectPackets failed: %v", err)
	}

	if oq.Length() != 3 {
		t.Fatalf("expected length 3 after inject, got %d", oq.Length())
	}
}

// TestInjectPacketsOverflow tests behavior when injecting more packets than capacity.
func TestInjectPacketsOverflow(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(3, 1, 1)

	packets := make([]packet.Packet, 5)
	for i := range packets {
		packets[i] = packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"}
	}

	err := oq.InjectPackets(0, packets)
	if err == nil {
		t.Fatal("expected error when injecting more packets than capacity, got nil")
	}

	// Should only store up to capacity (3)
	if oq.Length() != 3 {
		t.Fatalf("expected length 3 (full capacity), got %d", oq.Length())
	}

}

// TestTickWithoutOutPort tests Tick when port is not set.
func TestTickWithoutOutPort(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(10, 1, 1)

	packets := []packet.Packet{{SourceID: 1, TargetID: 2, Payload: "test"}}
	oq.InjectPackets(0, packets)

	// Should not panic when port is nil
	err := oq.Tick(0)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Packet should remain in queue
	if oq.Length() != 1 {
		t.Fatalf("expected length 1, got %d", oq.Length())
	}
}

// TestTickSendPackets tests sending packets via Tick.
func TestTickSendPackets(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(10, 2, 2)

	// Create downstream mock and connect
	downstream := newMockDownstream()
	ahead_port.Connect(oq, downstream)

	// Declare downstream ready
	downstream.UpdateReady(0, true)

	// Inject packets
	packets := []packet.Packet{
		{SourceID: 1, TargetID: 2, Payload: "packet1"},
		{SourceID: 1, TargetID: 3, Payload: "packet2"},
	}
	oq.InjectPackets(0, packets)

	if oq.Length() != 2 {
		t.Fatalf("expected length 2 after inject, got %d", oq.Length())
	}

	// Process tick for OutputQueue (sends packets)
	err := oq.Tick(0)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Packets should be sent (removed from OutputQueue)
	if oq.Length() != 0 {
		t.Fatalf("expected length 0 after Tick, got %d", oq.Length())
	}

	// Wait for downstream and receive
	downstream.WaitUpstreamDone(0)
	received := downstream.ReceivePackets(0)

	// Verify packets were received
	if len(received) != 2 {
		t.Fatalf("expected 2 packets received, got %d", len(received))
	}
}

// TestTickRespectsBandwidth tests that outBandwidth is respected.
func TestTickRespectsBandwidth(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(10, 2, 2) // outBandwidth = 2

	// Create downstream mock and connect
	downstream := newMockDownstream()
	ahead_port.Connect(oq, downstream)

	// Declare downstream ready
	downstream.UpdateReady(0, true)
	downstream.UpdateReady(1, true)

	// Inject 4 packets
	packets := make([]packet.Packet, 4)
	for i := range packets {
		packets[i] = packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"}
	}
	oq.InjectPackets(0, packets)

	if oq.Length() != 4 {
		t.Fatalf("expected length 4 after inject, got %d", oq.Length())
	}

	// First tick should send only 2 packets (outBandwidth limit)
	err := oq.Tick(0)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	if oq.Length() != 2 {
		t.Fatalf("expected length 2 after first Tick (bandwidth=2), got %d", oq.Length())
	}

	// Verify 2 packets received
	downstream.WaitUpstreamDone(0)
	received := downstream.ReceivePackets(0)
	if len(received) != 2 {
		t.Fatalf("expected 2 packets received after first Tick, got %d", len(received))
	}

	// Second tick should send remaining 2 packets
	err = oq.Tick(1)
	if err != nil {
		t.Fatalf("second Tick failed: %v", err)
	}

	if oq.Length() != 0 {
		t.Fatalf("expected length 0 after second Tick, got %d", oq.Length())
	}

	// Verify 2 more packets received
	downstream.WaitUpstreamDone(1)
	received = downstream.ReceivePackets(1)
	if len(received) != 2 {
		t.Fatalf("expected 2 packets received after second Tick, got %d", len(received))
	}
}

// TestPacketSentHook tests the onPacketSent hook.
func TestPacketSentHook(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(10, 2, 2)

	// Create downstream mock and connect
	downstream := newMockDownstream()
	ahead_port.Connect(oq, downstream)

	// Declare downstream ready
	downstream.UpdateReady(0, true)

	// Set up hook to count sent packets
	sentCount := 0
	var sentPackets []packet.Packet
	oq.SetPacketSentHook(func(pkt packet.Packet) {
		sentCount++
		sentPackets = append(sentPackets, pkt)
	})

	// Inject and send packets
	packets := []packet.Packet{
		{SourceID: 1, TargetID: 2, Payload: "packet1"},
		{SourceID: 1, TargetID: 3, Payload: "packet2"},
	}
	oq.InjectPackets(0, packets)

	err := oq.Tick(0)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	if sentCount != 2 {
		t.Fatalf("expected hook called 2 times, got %d", sentCount)
	}

	if len(sentPackets) != 2 {
		t.Fatalf("expected 2 packets in hook, got %d", len(sentPackets))
	}
}

// TestOutputQueueIsFullCapacity tests IsFull and Capacity methods.
func TestOutputQueueIsFullCapacity(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(3, 1, 1)

	if oq.Capacity() != 3 {
		t.Fatalf("expected capacity 3, got %d", oq.Capacity())
	}

	if oq.IsFull() {
		t.Fatal("queue should not be full initially")
	}

	// Fill the queue
	packets := make([]packet.Packet, 3)
	for i := range packets {
		packets[i] = packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"}
	}
	oq.InjectPackets(0, packets)

	if !oq.IsFull() {
		t.Fatal("queue should be full after injecting 3 packets")
	}

	if oq.Length() != 3 {
		t.Fatalf("expected length 3, got %d", oq.Length())
	}
}
