package link

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestLinkBasicFunctionality tests basic packet transmission with fixed latency.
func TestLinkBasicFunctionality(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("Test failed - check cycle and targetCycle alignment")
		}
	})

	upstream := newTestAheadPort(8)
	downstream := newTestAheadPort(8)
	upstream.SetDone(10)

	link := NewLink(0, 1, upstream, downstream, 2, 1)

	pkt := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "test",
	}
	sendPacket(t, upstream, 0, pkt)

	if err := link.Tick(2); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}

	received := receivePackets(t, downstream, 1)
	if received[0].Packet.Payload != "test" {
		t.Fatalf("unexpected payload %q", received[0].Packet.Payload)
	}
	ensureNoAdditionalPackets(t, downstream)
}

// TestLinkRingBufferMechanism tests that packets are stored in correct ring buffer slots.
func TestLinkRingBufferMechanism(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("Test failed - check cycle and targetCycle alignment")
		}
	})

	upstream := newTestAheadPort(8)
	downstream := newTestAheadPort(8)
	upstream.SetDone(10)

	link := NewLink(0, 1, upstream, downstream, 3, 2)

	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test2"}

	sendPacket(t, upstream, 0, pkt1)
	sendPacket(t, upstream, 0, pkt2)

	if err := link.Tick(3); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}

	received := receivePackets(t, downstream, 2)
	if received[0].Packet.Payload != "test1" || received[1].Packet.Payload != "test2" {
		t.Fatalf("unexpected payload order: %q, %q", received[0].Packet.Payload, received[1].Packet.Payload)
	}
	ensureNoAdditionalPackets(t, downstream)
}

// TestLinkBandwidthLimit tests that bandwidth limits are enforced per cycle.
func TestLinkBandwidthLimit(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("Test failed - check bandwidth limit and cycle alignment")
		}
	})

	upstream := newTestAheadPort(8)
	downstream := newTestAheadPort(8)
	upstream.SetDone(10)

	link := NewLink(0, 1, upstream, downstream, 2, 2)

	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test2"}

	sendPacket(t, upstream, 0, pkt1)
	sendPacket(t, upstream, 0, pkt2)

	if err := link.Tick(2); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}

	received := receivePackets(t, downstream, 2)
	if len(received) != 2 {
		t.Fatalf("expected 2 packets, got %d", len(received))
	}
	ensureNoAdditionalPackets(t, downstream)
}

// TestLinkMultipleUpstream tests Link with multiple upstream ports.
func TestLinkMultipleUpstream(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("Test failed - check multiple upstream port aggregation")
		}
	})

	upstreams, aggregator := ahead_port.NewSharedPortGroup(3, 8)
	downstream := newTestAheadPort(8)

	for _, port := range upstreams {
		if sp, ok := port.(*ahead_port.SinglePort); ok {
			sp.SetReadyUntil(1024)
			sp.UpdateReady(0, true)
		}
		port.SetDone(10)
	}

	link := NewLink(0, 3, aggregator, downstream, 1, 10)

	pkt0 := packet.Packet{SourceID: 0, TargetID: 3, Payload: "from0"}
	pkt1 := packet.Packet{SourceID: 1, TargetID: 3, Payload: "from1"}
	pkt2 := packet.Packet{SourceID: 2, TargetID: 3, Payload: "from2"}

	sendPacket(t, upstreams[0], 0, pkt0)
	sendPacket(t, upstreams[1], 0, pkt1)
	sendPacket(t, upstreams[2], 0, pkt2)

	if err := link.Tick(1); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}

	received := receivePackets(t, downstream, 3)
	expected := map[string]bool{"from0": true, "from1": true, "from2": true}
	for _, pkt := range received {
		if !expected[pkt.Packet.Payload] {
			t.Fatalf("unexpected payload %q", pkt.Packet.Payload)
		}
		delete(expected, pkt.Packet.Payload)
	}
	if len(expected) != 0 {
		t.Fatalf("missing payloads: %v", expected)
	}
}
