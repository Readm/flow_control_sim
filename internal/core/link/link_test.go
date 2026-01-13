package link

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestLinkBasicFunctionality tests basic packet transmission with fixed latency.
func TestLinkBasicFunctionality(t *testing.T) {
	t.Parallel()

	// Create Link with new API (latency=2, bandwidth=1)
	link := NewLink(0, 1, 2, 1)

	// Create mock components
	upstream := newMockUpstream()
	downstream := newMockDownstream()

	// Connect: upstream -> link -> downstream
	ahead_port.Connect(upstream, link)
	ahead_port.Connect(link, downstream)

	// Link needs to be ready to accept packet at cycle 0
	// Downstream (for Link's output) needs to be ready for cycle 2 (0+latency)
	link.fromUpstream.UpdateReady(0, true)
	downstream.UpdateReady(2, true)

	// Upstream sends packet at cycle 0
	pkt := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Metadata: map[string]interface{}{"payload": "test"},
	}

	// Send and mark done (latency=2, so packet arrives at cycle 2)
	if !upstream.SendPacket(0, pkt) {
		t.Fatal("Failed to send packet")
	}
	upstream.MarkDone(0)

	// Tick Link at cycle 2 (packet sent at cycle 0 arrives at cycle 2)
	// targetCycle=2 because we are single stepping
	if err := link.Tick(2, 2); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}

	// Wait and receive from downstream
	downstream.WaitDone(2)
	received := downstream.ReceivePackets(2)

	if len(received) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(received))
	}
	if received[0].Payload != "test" {
		t.Fatalf("unexpected payload %q", received[0].Payload)
	}
}

// TestLinkRingBufferMechanism tests that packets are stored in correct ring buffer slots.
func TestLinkRingBufferMechanism(t *testing.T) {
	t.Parallel()

	// Create Link (latency=3, bandwidth=2)
	link := NewLink(0, 1, 3, 2)

	// Create mock components
	upstream := newMockUpstream()
	downstream := newMockDownstream()

	// Connect
	ahead_port.Connect(upstream, link)
	ahead_port.Connect(link, downstream)

	// Link needs to be ready to accept packets at cycle 0
	link.fromUpstream.UpdateReady(0, true)
	// Downstream declares ready for cycle 3
	downstream.UpdateReady(3, true)

	// Send two packets at cycle 0
	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Metadata: map[string]interface{}{"payload": "test1"}}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 1, Metadata: map[string]interface{}{"payload": "test2"}}

	if !upstream.SendPacket(0, pkt1) {
		t.Fatal("Failed to send packet 1")
	}
	if !upstream.SendPacket(0, pkt2) {
		t.Fatal("Failed to send packet 2")
	}
	upstream.MarkDone(0)

	// Tick Link at cycle 3
	if err := link.Tick(3, 3); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}

	// Receive packets
	downstream.WaitDone(3)
	received := downstream.ReceivePackets(3)

	if len(received) != 2 {
		t.Fatalf("expected 2 packets, got %d", len(received))
	}
	if received[0].Payload != "test1" || received[1].Payload != "test2" {
		t.Fatalf("unexpected payload order: %q, %q", received[0].Payload, received[1].Payload)
	}
}

// TestLinkBandwidthLimit tests that bandwidth limits are enforced per cycle.
func TestLinkBandwidthLimit(t *testing.T) {
	t.Parallel()

	// Create Link (latency=2, bandwidth=2)
	link := NewLink(0, 1, 2, 2)

	// Create mock components
	upstream := newMockUpstream()
	downstream := newMockDownstream()

	// Connect
	ahead_port.Connect(upstream, link)
	ahead_port.Connect(link, downstream)

	// Link needs to be ready to accept packets at cycle 0
	link.fromUpstream.UpdateReady(0, true)
	// Downstream declares ready for cycle 2 and 3
	downstream.UpdateReady(2, true)
	downstream.UpdateReady(3, true)

	// Send 3 packets at cycle 0 (bandwidth=2, so should split across cycles)
	packets := []packet.Packet{
		{SourceID: 0, TargetID: 1, Metadata: map[string]interface{}{"payload": "pkt1"}},
		{SourceID: 0, TargetID: 1, Metadata: map[string]interface{}{"payload": "pkt2"}},
		{SourceID: 0, TargetID: 1, Metadata: map[string]interface{}{"payload": "pkt3"}},
	}

	for _, pkt := range packets {
		if !upstream.SendPacket(0, pkt) {
			t.Fatalf("Failed to send packet %s", pkt.Payload)
		}
	}
	upstream.MarkDone(0)

	// Tick Link at cycle 2
	if err := link.Tick(2, 2); err != nil {
		t.Fatalf("link.Tick(2) failed: %v", err)
	}

	// Should receive at most 2 packets (bandwidth limit)
	downstream.WaitDone(2)
	received1 := downstream.ReceivePackets(2)

	if len(received1) > 2 {
		t.Fatalf("expected at most 2 packets in cycle 2, got %d", len(received1))
	}

	// Mark upstream done for cycle 1 (required for Link.Tick(3) which reads cycle 1)
	upstream.MarkDone(1)

	// Tick Link at cycle 3 to send remaining packets
	if err := link.Tick(3, 3); err != nil {
		t.Fatalf("link.Tick(3) failed: %v", err)
	}

	downstream.WaitDone(3)
	received2 := downstream.ReceivePackets(3)

	// Total should be 3 packets across both cycles
	total := len(received1) + len(received2)
	if total != 3 {
		t.Fatalf("expected 3 total packets, got %d (%d + %d)", total, len(received1), len(received2))
	}
}
