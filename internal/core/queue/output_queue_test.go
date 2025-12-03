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
	if err != nil {
		t.Fatalf("InjectPackets failed: %v", err)
	}

	// Should only store up to capacity (3), rest dropped
	if oq.Length() > 3 {
		t.Fatalf("expected length <= 3, got %d", oq.Length())
	}
}

// TestTickWithoutOutPort tests Tick when outPort is not set.
func TestTickWithoutOutPort(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(10, 1, 1)

	packets := []packet.Packet{{SourceID: 1, TargetID: 2, Payload: "test"}}
	oq.InjectPackets(0, packets)

	// Should not panic when outPort is nil
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

	// Create downstream port and make it ready
	downstreamPort := ahead_port.NewAheadPort(8)
	downstreamPort.SetReadyUntil(100) // Make downstream always ready
	oq.SetOutPort(downstreamPort)

	// Inject packets
	packets := []packet.Packet{
		{SourceID: 1, TargetID: 2, Payload: "packet1"},
		{SourceID: 1, TargetID: 3, Payload: "packet2"},
	}
	oq.InjectPackets(0, packets)

	if oq.Length() != 2 {
		t.Fatalf("expected length 2 after inject, got %d", oq.Length())
	}

	// Process tick
	err := oq.Tick(0)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Packets should be sent (removed from queue)
	if oq.Length() != 0 {
		t.Fatalf("expected length 0 after Tick, got %d", oq.Length())
	}

	// Verify packets were sent to downstream
	receivedCount := 0
	for i := 0; i < 2; i++ {
		select {
		case <-downstreamPort.ReceiveChan():
			receivedCount++
		default:
		}
	}

	if receivedCount != 2 {
		t.Fatalf("expected 2 packets sent, got %d", receivedCount)
	}
}

// TestTickRespectsBandwidth tests that outBandwidth is respected.
func TestTickRespectsBandwidth(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(10, 2, 2) // outBandwidth = 2

	downstreamPort := ahead_port.NewAheadPort(8)
	downstreamPort.SetReadyUntil(100) // Make downstream always ready
	oq.SetOutPort(downstreamPort)

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

	// Second tick should send remaining 2 packets
	err = oq.Tick(1)
	if err != nil {
		t.Fatalf("second Tick failed: %v", err)
	}

	if oq.Length() != 0 {
		t.Fatalf("expected length 0 after second Tick, got %d", oq.Length())
	}
}

// TestPacketSentHook tests the onPacketSent hook.
func TestPacketSentHook(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(10, 2, 2)

	downstreamPort := ahead_port.NewAheadPort(8)
	downstreamPort.SetReadyUntil(100) // Make downstream always ready
	oq.SetOutPort(downstreamPort)

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

// TestOutPortGetter tests OutPort getter method.
func TestOutPortGetter(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(10, 1, 1)

	if oq.OutPort() != nil {
		t.Fatal("expected nil outPort initially")
	}

	downstreamPort := ahead_port.NewAheadPort(8)
	oq.SetOutPort(downstreamPort)

	if oq.OutPort() != downstreamPort {
		t.Fatal("OutPort() should return the set port")
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
