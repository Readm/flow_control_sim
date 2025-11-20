package link

import (
	"context"
	"testing"

	"github.com/Readm/flow_sim/internal/dataflow/flow"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestLinkBasicFunctionality tests basic packet transmission with fixed latency.
func TestLinkBasicFunctionality(t *testing.T) {
	t.Parallel()

	f := flow.NewFIFO(1, 8, 0, 0, nil, 0)
	link := NewLink(0, f, nil, 0, 2, 1, 0) // latency = 2, bandwidth = 1

	pkt := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "test",
	}

	// Set noBackpressureUntil to allow transmission
	link.SetNoBackpressureUntil(10)

	// Transmit at cycle 0, should arrive at cycle 2
	link.Transmit(0, pkt)
	link.Advance(2)

	// Verify packet was received
	ctx := context.Background()
	f.Tick(ctx, 2)
	if f.ProcessedCount() != 1 {
		t.Fatalf("expected 1 processed packet, got %d", f.ProcessedCount())
	}
}

// TestLinkRingBufferMechanism tests that packets are stored in correct ring buffer slots.
func TestLinkRingBufferMechanism(t *testing.T) {
	t.Parallel()

	f := flow.NewFIFO(1, 8, 0, 0, nil, 0)
	link := NewLink(0, f, nil, 0, 3, 1, 0) // latency = 3, bandwidth = 1, slotCount = 3

	// Force ring buffer path by setting noBackpressureUntil to a small value
	link.SetNoBackpressureUntil(0)

	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test2"}

	// Transmit at cycle 0, should be in slot (0+3) % 3 = 0
	link.Transmit(0, pkt1)
	// Transmit at cycle 1, should be in slot (1+3) % 3 = 1
	link.Transmit(1, pkt2)

	// Verify occupancy
	occupancy := link.SnapshotOccupancy()
	if occupancy[0] != 1 {
		t.Fatalf("expected 1 packet in slot 0, got %d", occupancy[0])
	}
	if occupancy[1] != 1 {
		t.Fatalf("expected 1 packet in slot 1, got %d", occupancy[1])
	}
}

// TestLinkSFC tests that Link SFC is correctly updated.
func TestLinkSFC(t *testing.T) {
	t.Parallel()

	f := flow.NewFIFO(1, 8, 0, 0, nil, 0)
	link := NewLink(0, f, nil, 0, 1, 1, 0)

	// Set noBackpressureUntil to allow advancement
	link.SetNoBackpressureUntil(10)

	// Initially SFC should be 0
	if link.SendFinishedCycle() != 0 {
		t.Fatalf("expected initial SFC 0, got %d", link.SendFinishedCycle())
	}

	// Advance to cycle 1, SFC should update
	link.Advance(1)
	if link.SendFinishedCycle() != 1 {
		t.Fatalf("expected SFC 1, got %d", link.SendFinishedCycle())
	}

	// Advance to cycle 5, SFC should update
	link.Advance(5)
	if link.SendFinishedCycle() != 5 {
		t.Fatalf("expected SFC 5, got %d", link.SendFinishedCycle())
	}
}

// TestLinkBackpressurePausesCycle tests that backpressure pauses cycle advancement.
func TestLinkBackpressurePausesCycle(t *testing.T) {
	t.Parallel()

	f := flow.NewFIFO(1, 2, 0, 0, nil, 0) // Small mailbox to trigger backpressure
	link := NewLink(0, f, nil, 0, 1, 1, 0)

	pkt := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}

	// Set noBackpressureUntil to 2
	link.SetNoBackpressureUntil(2)

	// Transmit and advance to cycle 1 (within noBackpressureUntil)
	link.Transmit(0, pkt)
	link.Advance(1)

	// Verify currentCycle advanced
	if link.CurrentCycle() != 1 {
		t.Fatalf("expected currentCycle 1, got %d", link.CurrentCycle())
	}

	// Try to advance to cycle 3 (beyond noBackpressureUntil)
	link.Advance(3)

	// Verify currentCycle did NOT advance (still 1)
	if link.CurrentCycle() != 1 {
		t.Fatalf("expected currentCycle to stay at 1, got %d", link.CurrentCycle())
	}

	// Update noBackpressureUntil to 5
	link.SetNoBackpressureUntil(5)

	// Now advance to cycle 3
	link.Advance(3)

	// Verify currentCycle advanced
	if link.CurrentCycle() != 3 {
		t.Fatalf("expected currentCycle 3, got %d", link.CurrentCycle())
	}
}

// TestLinkDirectSendPath tests the optimization path when noBackpressureUntil >= targetCycle.
func TestLinkDirectSendPath(t *testing.T) {
	t.Parallel()

	f := flow.NewFIFO(1, 8, 0, 0, nil, 0)
	link := NewLink(0, f, nil, 0, 2, 1, 0) // latency = 2, bandwidth = 1

	// Set noBackpressureUntil to 5 (>= 0+2)
	link.SetNoBackpressureUntil(5)

	pkt := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}

	// Transmit at cycle 0, should directly send to channel (targetCycle = 2 < 5)
	link.Transmit(0, pkt)

	// Verify SFC updated to targetCycle
	if link.SendFinishedCycle() != 2 {
		t.Fatalf("expected SFC 2, got %d", link.SendFinishedCycle())
	}

	// Verify packet was received directly (no need to call Advance)
	ctx := context.Background()
	f.Tick(ctx, 2)
	if f.ProcessedCount() != 1 {
		t.Fatalf("expected 1 processed packet, got %d", f.ProcessedCount())
	}
}

// TestLinkRingBufferPath tests the ring buffer path when noBackpressureUntil < targetCycle.
func TestLinkRingBufferPath(t *testing.T) {
	t.Parallel()

	f := flow.NewFIFO(1, 8, 0, 0, nil, 0)
	link := NewLink(0, f, nil, 0, 3, 1, 0) // latency = 3, bandwidth = 1

	// Set noBackpressureUntil to 2 (< 0+3)
	link.SetNoBackpressureUntil(2)

	pkt := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}

	// Transmit at cycle 0, should use ring buffer (targetCycle = 3 > 2)
	link.Transmit(0, pkt)

	// Verify SFC not updated yet (not sent)
	if link.SendFinishedCycle() != 0 {
		t.Fatalf("expected SFC 0, got %d", link.SendFinishedCycle())
	}

	// Advance to cycle 3, should send from ring buffer
	link.SetNoBackpressureUntil(5) // Update to allow sending
	link.Advance(3)

	// Verify SFC updated
	if link.SendFinishedCycle() != 3 {
		t.Fatalf("expected SFC 3, got %d", link.SendFinishedCycle())
	}

	// Verify packet was received
	ctx := context.Background()
	f.Tick(ctx, 3)
	if f.ProcessedCount() != 1 {
		t.Fatalf("expected 1 processed packet, got %d", f.ProcessedCount())
	}
}

// TestLinkReadFromFlow tests reading packets from Flow's dispatch_queue based on dispatch queue SFC.
func TestLinkReadFromFlow(t *testing.T) {
	t.Parallel()

	// Create target flow and source flow with one dispatch queue
	targetFlow := flow.NewFIFO(2, 8, 0, 0, nil, 0)
	sourceFlow := flow.NewFIFO(1, 8, 0, 0, []interface{}{targetFlow}, 16)

	// Create link with reference to source flow and dispatch queue index
	link := NewLink(1, targetFlow, sourceFlow, 0, 1, 2, 0) // bandwidth = 2 to allow 2 packets

	// Set Flow currentCycle first
	ctx := context.Background()
	sourceFlow.Tick(ctx, 1) // Advance to cycle 1

	// Emit packets
	pkt1 := packet.Packet{SourceID: 1, TargetID: 2, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 1, TargetID: 2, Payload: "test2"}
	sourceFlow.Emit(pkt1, pkt2)

	// Trigger routing
	sourceFlow.Tick(ctx, 2)

	// Dispatch queue SFC should be updated
	flowSFC := sourceFlow.DispatchQueueSendFinishedCycle(0)
	if flowSFC != 2 {
		t.Fatalf("expected dispatch queue SFC 2, got %d", flowSFC)
	}

	// Advance link to cycle 0 (this will trigger ReadFromFlow internally)
	// Packets will be read from dispatch queue and transmitted
	link.Advance(0)

	// Advance to cycle 1 to deliver packets (latency=1)
	link.Advance(1)

	// Process targetFlow to receive packets
	targetFlow.Tick(ctx, 1)

	// Verify packets were received by targetFlow
	processedCount := targetFlow.ProcessedCount()
	if processedCount != 2 {
		t.Fatalf("expected 2 packets processed by target flow, got %d", processedCount)
	}
}

// TestLinkMultiplePackets tests handling multiple packets in sequence.
func TestLinkMultiplePackets(t *testing.T) {
	t.Parallel()

	f := flow.NewFIFO(1, 8, 0, 0, nil, 0)
	link := NewLink(0, f, nil, 0, 1, 1, 0)
	link.SetNoBackpressureUntil(10)

	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test2"}
	pkt3 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test3"}

	// Transmit multiple packets
	link.Transmit(0, pkt1)
	link.Transmit(1, pkt2)
	link.Transmit(2, pkt3)

	// Advance to deliver all packets
	link.Advance(1)
	link.Advance(2)
	link.Advance(3)

	// Verify all packets were received
	ctx := context.Background()
	f.Tick(ctx, 1)
	f.Tick(ctx, 2)
	f.Tick(ctx, 3)

	if f.ProcessedCount() != 3 {
		t.Fatalf("expected 3 processed packets, got %d", f.ProcessedCount())
	}
}

// TestLinkBandwidthLimit tests that bandwidth limits are enforced per slot and per cycle.
func TestLinkBandwidthLimit(t *testing.T) {
	t.Parallel()

	f := flow.NewFIFO(1, 8, 0, 0, nil, 0)
	link := NewLink(0, f, nil, 0, 2, 2, 0) // latency = 2, bandwidth = 2, slotCount = 2
	// Set noBackpressureUntil to 1 (< 0+2) to force ring buffer path
	link.SetNoBackpressureUntil(1)

	pkt1 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test1"}
	pkt2 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test2"}
	pkt3 := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test3"}

	// Transmit 3 packets at cycle 0, all targeting cycle 2 (slot (0+2) % 2 = 0)
	// Only 2 should be accepted due to bandwidth limit per slot
	link.Transmit(0, pkt1)
	link.Transmit(0, pkt2)
	link.Transmit(0, pkt3)

	// Verify only 2 packets in slot 0
	occupancy := link.SnapshotOccupancy()
	if occupancy[0] != 2 {
		t.Fatalf("expected 2 packets in slot 0 (bandwidth limit), got %d", occupancy[0])
	}

	// Update noBackpressureUntil to allow sending
	link.SetNoBackpressureUntil(10)

	// Advance to cycle 2, should send at most 2 packets per cycle
	link.Advance(2)

	// Verify at most 2 packets were sent (bandwidth limit per cycle)
	ctx := context.Background()
	f.Tick(ctx, 2)
	processed := f.ProcessedCount()
	if processed > 2 {
		t.Fatalf("expected at most 2 processed packets (bandwidth limit per cycle), got %d", processed)
	}
	if processed < 2 {
		t.Fatalf("expected at least 2 processed packets, got %d", processed)
	}
}

