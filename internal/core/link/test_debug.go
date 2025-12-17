package link

import (
	"fmt"
	"testing"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

func TestLinkBasicDebug(t *testing.T) {
	link, linkIn, linkOut := NewLink(0, 1, 2, 1)
	downstreamInPort, upstreamOutPort := createTestPorts(8)
	
	linkIn.Plug(upstreamOutPort)
	linkOut.Plug(downstreamInPort)
	
	mockOut := upstreamOutPort.(*mockOutPort)
	mockIn := downstreamInPort.(*mockInPort)
	
	pkt := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}
	sendPacketToOutPort(t, upstreamOutPort, 0, pkt)
	mockOut.SetDone(2)
	
	fmt.Printf("Before Tick: mockIn.ch=%v\n", mockIn.ch)
	fmt.Printf("Before Tick: mockIn.ch==nil? %v\n", mockIn.ch == nil)
	
	if err := link.Tick(2); err != nil {
		t.Fatalf("link.Tick failed: %v", err)
	}
	
	fmt.Printf("After Tick: checking channel...\n")
	select {
	case pkt := <-mockIn.ch:
		fmt.Printf("Received packet: %+v\n", pkt)
	default:
		fmt.Printf("No packet in channel\n")
	}
}
