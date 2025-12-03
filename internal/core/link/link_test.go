package link

import (
	"testing"

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

	// Create Link with new API
	link, linkIn, linkOut := NewLink(0, 1, 2, 1) // latency=2, bandwidth=1

	// Create mock ports
	downstreamInPort, upstreamOutPort := createTestPorts(8)

	// Plug Link to upstream and downstream
	linkIn.Plug(upstreamOutPort)
	linkOut.Plug(downstreamInPort)

	// Get access to mock for SetDone
	mockOut := upstreamOutPort.(*mockOutPort)

	// Send packet from upstream
	pkt := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "test",
	}
	sendPacketToOutPort(t, upstreamOutPort, 0, pkt)
	mockOut.SetDone(2) // Upstream done with cycle 2

	// Tick Link at cycle 2 (latency=2, so packet sent at cycle 0 arrives at cycle 2)
	if err := link.Tick(2); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}

	// Receive from downstream
	received := receivePacketsFromInPort(t, downstreamInPort, 1)
	if received[0].Packet.Payload != "test" {
		t.Fatalf("unexpected payload %q", received[0].Packet.Payload)
	}
	ensureNoAdditionalPacketsInPort(t, downstreamInPort)
}

// TestLinkRingBufferMechanism tests that packets are stored in correct ring buffer slots.
func TestLinkRingBufferMechanism(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("Test failed - check cycle and targetCycle alignment")
		}
	})

	link, linkIn, linkOut := NewLink(0, 1, 3, 2) // latency=3, bandwidth=2
	downstreamInPort, upstreamOutPort := createTestPorts(8)
	linkIn.Plug(upstreamOutPort)
	linkOut.Plug(downstreamInPort)

	// Get access to mock for SetDone
	mockOut := upstreamOutPort.(*mockOutPort)

	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test2"}

	sendPacketToOutPort(t, upstreamOutPort, 0, pkt1)
	sendPacketToOutPort(t, upstreamOutPort, 0, pkt2)
	mockOut.SetDone(3) // Upstream done with cycle 3

	if err := link.Tick(3); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}

	received := receivePacketsFromInPort(t, downstreamInPort, 2)
	if received[0].Packet.Payload != "test1" || received[1].Packet.Payload != "test2" {
		t.Fatalf("unexpected payload order: %q, %q", received[0].Packet.Payload, received[1].Packet.Payload)
	}
	ensureNoAdditionalPacketsInPort(t, downstreamInPort)
}

// TestLinkBandwidthLimit tests that bandwidth limits are enforced per cycle.
func TestLinkBandwidthLimit(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("Test failed - check bandwidth limit and cycle alignment")
		}
	})

	link, linkIn, linkOut := NewLink(0, 1, 2, 2) // latency=2, bandwidth=2
	downstreamInPort, upstreamOutPort := createTestPorts(8)
	linkIn.Plug(upstreamOutPort)
	linkOut.Plug(downstreamInPort)

	// Get access to mock for SetDone
	mockOut := upstreamOutPort.(*mockOutPort)

	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test2"}

	sendPacketToOutPort(t, upstreamOutPort, 0, pkt1)
	sendPacketToOutPort(t, upstreamOutPort, 0, pkt2)
	mockOut.SetDone(2) // Upstream done with cycle 2

	if err := link.Tick(2); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}

	received := receivePacketsFromInPort(t, downstreamInPort, 2)
	if len(received) != 2 {
		t.Fatalf("expected 2 packets, got %d", len(received))
	}
	ensureNoAdditionalPacketsInPort(t, downstreamInPort)
}

// TestLinkMultipleUpstream is removed - will be re-added after Fanin/Fanout refactoring
// TODO: Re-enable this test once Fanin ports are refactored to use InPort/OutPort
