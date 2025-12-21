package link

import (
	"fmt"
	"testing"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

func TestLinkBasicDebug(t *testing.T) {
	// Create Link
	link := NewLink(0, 1, 2, 1)

	// Create mock components
	upstream := newMockUpstream()
	downstream := newMockDownstream()

	// Connect
	ahead_port.Connect(upstream, link)
	ahead_port.Connect(link, downstream)

	// Link needs to be ready to accept packet at cycle 0
	link.fromUpstream.UpdateReady(0, true)
	// Declare downstream ready
	downstream.UpdateReady(2, true)

	// Send packet
	pkt := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}

	fmt.Printf("Before Send: downstream=%+v\n", downstream)

	if !upstream.SendPacket(0, pkt) {
		t.Fatal("Failed to send packet")
	}
	upstream.MarkDone(0)

	fmt.Printf("After Send, Before Tick\n")

	if err := link.Tick(2, 2); err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	fmt.Printf("After Tick: waiting for upstream done...\n")
	downstream.WaitDone(2)

	received := downstream.ReceivePackets(2)
	fmt.Printf("Received %d packets\n", len(received))

	if len(received) > 0 {
		fmt.Printf("First packet: %+v\n", received[0])
	}
}
