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

	// Create a simple downstream InPort to receive packets (not a full Queue)
	downstreamPort := &testInPort{
		ready:   true,
		packets: make([]packet.PacketWithCycle, 0),
	}

	// Connect OutputQueue's OutPort to downstream InPort using Plug
	ch := oq.QueueOutPort().Plug(downstreamPort)

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

	// Verify packets were sent to channel
	if len(ch) != 2 {
		t.Fatalf("expected 2 packets in channel, got %d", len(ch))
	}

	// Read packets from channel
	receivedCount := 0
	for i := 0; i < 2; i++ {
		select {
		case <-ch:
			receivedCount++
		default:
		}
	}

	if receivedCount != 2 {
		t.Fatalf("expected 2 packets received, got %d", receivedCount)
	}
}

// testInPort is a simple test implementation of InPort
type testInPort struct {
	ahead_port.BaseInPort
	ready   bool
	packets []packet.PacketWithCycle
}

func (t *testInPort) TrySendPacket(cycle int, pkt ahead_port.PacketWithCycle) bool {
	if !t.ready {
		return false
	}
	if t.InputChan == nil {
		panic("testInPort.TrySendPacket() called before Plug()")
	}
	t.InputChan <- pkt
	return true
}

func (t *testInPort) IsReadyNonBlocking(cycle int) (bool, bool) {
	return t.ready, true
}

func (t *testInPort) Plug(out ahead_port.OutPort) chan ahead_port.PacketWithCycle {
	return t.BaseInPort.PlugWithSelf(t, out)
}

// TestTickRespectsBandwidth tests that outBandwidth is respected.
func TestTickRespectsBandwidth(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(10, 2, 2) // outBandwidth = 2

	// Create downstream to receive packets
	downstreamPort := &testInPort{ready: true}
	ch := oq.QueueOutPort().Plug(downstreamPort)

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

	// Verify 2 packets in channel
	if len(ch) != 2 {
		t.Fatalf("expected 2 packets in channel after first Tick, got %d", len(ch))
	}

	// Drain channel
	for len(ch) > 0 {
		<-ch
	}

	// Second tick should send remaining 2 packets
	err = oq.Tick(1)
	if err != nil {
		t.Fatalf("second Tick failed: %v", err)
	}

	if oq.Length() != 0 {
		t.Fatalf("expected length 0 after second Tick, got %d", oq.Length())
	}

	// Verify 2 more packets in channel
	if len(ch) != 2 {
		t.Fatalf("expected 2 packets in channel after second Tick, got %d", len(ch))
	}
}

// TestPacketSentHook tests the onPacketSent hook.
// Note: Hook functionality is currently not implemented in the new Queue-based design.
// This test is skipped until hooks are re-implemented.
func TestPacketSentHook(t *testing.T) {
	t.Skip("PacketSentHook not yet implemented in Queue-based OutputQueue")
	t.Parallel()

	oq := NewOutputQueue(10, 2, 2)

	// Create downstream
	// Create downstream
	downstream := &testInPort{ready: true}
	oq.QueueOutPort().Plug(downstream)

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

// TestOutPortGetter tests QueueOutPort getter method.
func TestOutPortGetter(t *testing.T) {
	t.Parallel()

	oq := NewOutputQueue(10, 1, 1)

	// QueueOutPort should always be available after creation
	if oq.QueueOutPort() == nil {
		t.Fatal("expected QueueOutPort to be non-nil")
	}

	// Verify it's the correct type
	if _, ok := oq.QueueOutPort().(ahead_port.OutPort); !ok {
		t.Fatal("QueueOutPort should implement ahead_port.OutPort")
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
