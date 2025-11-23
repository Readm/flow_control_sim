package link

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/cycle_port"
	"github.com/Readm/flow_sim/internal/dataflow/flow"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestLinkBasicFunctionality tests basic packet transmission with fixed latency.
func TestLinkBasicFunctionality(t *testing.T) {
	t.Parallel()

	// Create Flow with CyclePort
	flow0 := flow.NewFIFO(0, 8)
	flow1 := flow.NewFIFO(1, 8)

	// Create ports
	flow0OutPort := cycle_port.NewCyclePort(8)
	flow1InPort := flow1.InPort()

	// Connect Flow0 output to Link upstream
	flow0.AddOutPort(flow0OutPort)

	// Create Link
	link := NewLink(0, 1, []cycle_port.CyclePort{flow0OutPort}, flow1InPort, 2, 1)

	// Initialize upstream DoneUntil for flow0 (no upstream, so set to 0 to allow processing)
	flow0.InPort().SetDoneUntil(0)

	// Initialize downstream ready state for flow1 (allows Link to send packets)
	if flow1InPortImpl, ok := flow1InPort.(*cycle_port.CyclePortImpl); ok {
		flow1InPortImpl.SetReadyUntil(10)
	}

	// Send packet from Flow0
	pkt := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "test",
	}
	env := cycle_port.PacketWithCycle{
		Cycle:  0,
		Packet: pkt,
	}
	flow0OutPort.Chan() <- env

	// Process cycles - framework will automatically manage DoneUntil
	flow0.ProcessCycle(0) // Sets flow0OutPort.DoneUntil = 1
	link.ProcessCycle(0)  // Waits for flow0OutPort.DoneUntil >= 0, sets flow1InPort.DoneUntil = 1
	link.ProcessCycle(1)  // Waits for flow0OutPort.DoneUntil >= 1 (already 1), sets flow1InPort.DoneUntil = 2
	link.ProcessCycle(2)  // Waits for flow0OutPort.DoneUntil >= 2, but flow0 only processed cycle 0
	// Need to process flow0 for cycle 1 to update DoneUntil to 2
	flow0.InPort().SetDoneUntil(1) // Allow flow0 to process cycle 1
	flow0.ProcessCycle(1)          // Sets flow0OutPort.DoneUntil = 2
	link.ProcessCycle(2)           // Now can proceed, sets flow1InPort.DoneUntil = 3
	flow1.ProcessCycle(2)          // Waits for flow1InPort.DoneUntil >= 2 (already 2)

	// Verify packet was received
	if flow1.ProcessedCount() != 1 {
		t.Fatalf("expected 1 processed packet, got %d", flow1.ProcessedCount())
	}
}

// TestLinkRingBufferMechanism tests that packets are stored in correct ring buffer slots.
func TestLinkRingBufferMechanism(t *testing.T) {
	t.Parallel()

	flow0 := flow.NewFIFO(0, 8)
	flow1 := flow.NewFIFO(1, 8)

	flow0OutPort := cycle_port.NewCyclePort(8)
	flow1InPort := flow1.InPort()

	flow0.AddOutPort(flow0OutPort)

	link := NewLink(0, 1, []cycle_port.CyclePort{flow0OutPort}, flow1InPort, 3, 1)

	// Initialize upstream DoneUntil for flow0 (no upstream, so set to 0 to allow processing)
	flow0.InPort().SetDoneUntil(0)

	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test2"}

	// Send packets at cycle 0 and 1
	env1 := cycle_port.PacketWithCycle{Cycle: 0, Packet: pkt1}
	env2 := cycle_port.PacketWithCycle{Cycle: 1, Packet: pkt2}
	flow0OutPort.Chan() <- env1
	flow0OutPort.Chan() <- env2
	flow0OutPort.SetDoneUntil(2)

	// Process cycle 0
	flow0.ProcessCycle(0)
	link.ProcessCycle(0)

	// Verify occupancy (packets should be in slots for cycle 3 and 4)
	occupancy := link.SnapshotOccupancy()
	if len(occupancy) != 3 {
		t.Fatalf("expected 3 slots, got %d", len(occupancy))
	}
	// Slot for cycle 3 (0+3) should have 1 packet
	slot3Index := 3 % len(occupancy)
	if occupancy[slot3Index] != 1 {
		t.Fatalf("expected 1 packet in slot %d (for cycle 3), got %d", slot3Index, occupancy[slot3Index])
	}
}

// TestLinkBandwidthLimit tests that bandwidth limits are enforced per cycle.
func TestLinkBandwidthLimit(t *testing.T) {
	t.Parallel()

	flow0 := flow.NewFIFO(0, 8)
	flow1 := flow.NewFIFO(1, 8)

	flow0OutPort := cycle_port.NewCyclePort(8)
	flow1InPort := flow1.InPort()

	flow0.AddOutPort(flow0OutPort)

	link := NewLink(0, 1, []cycle_port.CyclePort{flow0OutPort}, flow1InPort, 2, 2) // bandwidth = 2

	// Initialize upstream DoneUntil for flow0 (no upstream, so set to 0 to allow processing)
	flow0.InPort().SetDoneUntil(0)

	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test2"}
	pkt3 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test3"}

	// Send 3 packets at cycle 0
	env1 := cycle_port.PacketWithCycle{Cycle: 0, Packet: pkt1}
	env2 := cycle_port.PacketWithCycle{Cycle: 0, Packet: pkt2}
	env3 := cycle_port.PacketWithCycle{Cycle: 0, Packet: pkt3}
	flow0OutPort.Chan() <- env1
	flow0OutPort.Chan() <- env2
	flow0OutPort.Chan() <- env3

	// Initialize downstream ready state for flow1
	if flow1InPortImpl, ok := flow1InPort.(*cycle_port.CyclePortImpl); ok {
		flow1InPortImpl.SetReadyUntil(10) // Allow processing up to cycle 10
	}

	// Process cycles - framework will automatically manage DoneUntil
	flow0.ProcessCycle(0) // Sets flow0OutPort.DoneUntil = 1
	link.ProcessCycle(0)  // Waits for flow0OutPort.DoneUntil >= 0, sets flow1InPort.DoneUntil = 1
	link.ProcessCycle(1)  // Waits for flow0OutPort.DoneUntil >= 1 (already 1), sets flow1InPort.DoneUntil = 2
	// Need to process flow0 for cycle 1 to update DoneUntil to 2 for link.ProcessCycle(2)
	flow0.InPort().SetDoneUntil(1) // Allow flow0 to process cycle 1
	flow0.ProcessCycle(1)          // Sets flow0OutPort.DoneUntil = 2
	link.ProcessCycle(2)           // Now can proceed, sets flow1InPort.DoneUntil = 3
	flow1.ProcessCycle(2)          // Waits for flow1InPort.DoneUntil >= 2 (already 2)

	// Verify at most 2 packets were sent (bandwidth limit per cycle)
	processed := flow1.ProcessedCount()
	if processed > 2 {
		t.Fatalf("expected at most 2 processed packets (bandwidth limit per cycle), got %d", processed)
	}
	if processed < 2 {
		t.Fatalf("expected at least 2 processed packets, got %d", processed)
	}
}

// TestLinkMultipleUpstream tests Link with multiple upstream ports.
func TestLinkMultipleUpstream(t *testing.T) {
	t.Parallel()

	flow0 := flow.NewFIFO(0, 8)
	flow1 := flow.NewFIFO(1, 8)
	flow2 := flow.NewFIFO(2, 8)
	flow3 := flow.NewFIFO(3, 8)

	flow0OutPort := cycle_port.NewCyclePort(8)
	flow1OutPort := cycle_port.NewCyclePort(8)
	flow2OutPort := cycle_port.NewCyclePort(8)
	flow3InPort := flow3.InPort()

	flow0.AddOutPort(flow0OutPort)
	flow1.AddOutPort(flow1OutPort)
	flow2.AddOutPort(flow2OutPort)

	// Create Link with multiple upstream ports
	link := NewLink(0, 3, []cycle_port.CyclePort{flow0OutPort, flow1OutPort, flow2OutPort}, flow3InPort, 1, 10)

	// Initialize upstream DoneUntil for all flows (no upstream, so set to 0 to allow processing)
	flow0.InPort().SetDoneUntil(0)
	flow1.InPort().SetDoneUntil(0)
	flow2.InPort().SetDoneUntil(0)

	// Send packets from all upstream flows using Emit (Flow will route to outPorts)
	pkt0 := packet.Packet{SourceID: 0, TargetID: 3, Payload: "from0"}
	pkt1 := packet.Packet{SourceID: 1, TargetID: 3, Payload: "from1"}
	pkt2 := packet.Packet{SourceID: 2, TargetID: 3, Payload: "from2"}

	flow0.Emit(pkt0)
	flow1.Emit(pkt1)
	flow2.Emit(pkt2)

	// Initialize downstream ready state for flow3 (allows Link to send packets)
	if flow3InPortImpl, ok := flow3InPort.(*cycle_port.CyclePortImpl); ok {
		flow3InPortImpl.SetReadyUntil(10)
	}

	// Initialize downstream ready state for Link's upstream ports (allows Flows to send packets)
	// Flow's outPorts need to be ready, which is managed by Link's updateUpstreamReady
	// But we need to ensure Flow can send packets, so we set outPorts' ReadyUntil
	flow0OutPort.SetReadyUntil(10)
	flow1OutPort.SetReadyUntil(10)
	flow2OutPort.SetReadyUntil(10)

	// Process cycles - framework will automatically manage DoneUntil
	flow0.ProcessCycle(0) // Processes emitQueue, routes to flow0OutPort, sets flow0OutPort.DoneUntil = 1
	flow1.ProcessCycle(0) // Processes emitQueue, routes to flow1OutPort, sets flow1OutPort.DoneUntil = 1
	flow2.ProcessCycle(0) // Processes emitQueue, routes to flow2OutPort, sets flow2OutPort.DoneUntil = 1
	link.ProcessCycle(0)  // Receives packets from flow0/1/2OutPort, waits for DoneUntil >= 0, sets flow3InPort.DoneUntil = 1
	link.ProcessCycle(1)  // Releases packets from slots (latency=1), sends to flow3InPort, sets flow3InPort.DoneUntil = 2
	flow3.ProcessCycle(1) // Receives packets from flow3InPort, waits for DoneUntil >= 1 (already 1)

	// Verify all packets were received
	if flow3.ProcessedCount() != 3 {
		t.Fatalf("expected 3 processed packets, got %d", flow3.ProcessedCount())
	}
}
