package pipeline

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestNewFIFO tests FIFO pipeline creation.
func TestNewFIFO(t *testing.T) {
	t.Parallel()

	p := NewFIFO(1, 8)
	if p == nil {
		t.Fatal("NewFIFO returned nil")
	}

	if p.ID() != 1 {
		t.Fatalf("expected ID 1, got %d", p.ID())
	}

	if p.InPort() == nil {
		t.Fatal("InPort should not be nil")
	}

	if p.OutPort() != nil {
		t.Fatal("OutPort should be nil initially")
	}

	if p.ProcessedCount() != 0 {
		t.Fatalf("expected initial ProcessedCount 0, got %d", p.ProcessedCount())
	}
}

// TestNewFIFODefaults tests default buffer size.
func TestNewFIFODefaults(t *testing.T) {
	t.Parallel()

	// Test with zero buffer size
	p := NewFIFO(1, 0)
	if p == nil {
		t.Fatal("NewFIFO returned nil")
	}

	// Should use default buffer size (8)
	if p.InPort() == nil {
		t.Fatal("InPort should not be nil")
	}
}

// TestFIFOSetOutPort tests setting output port.
func TestFIFOSetOutPort(t *testing.T) {
	t.Parallel()

	p := NewFIFO(1, 8)
	outPort := ahead_port.NewAheadPort(8)

	p.SetOutPort(outPort)

	if p.OutPort() != outPort {
		t.Fatal("OutPort should be set correctly")
	}
}

// TestFIFOTickEmpty tests processing cycle with no packets.
func TestFIFOTickEmpty(t *testing.T) {
	t.Parallel()

	p := NewFIFO(1, 8)

	// Initialize upstream Done
	p.InPort().SetDone(-1)

	// Process cycle 0
	if err := p.Tick(0); err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify no packets were processed
	if p.ProcessedCount() != 0 {
		t.Fatalf("expected ProcessedCount 0, got %d", p.ProcessedCount())
	}
}

// TestFIFOTickWithPackets tests processing cycle with incoming packets.
// Pipeline only processes up to Pick(), packets are available via GetProcessedPackets().
func TestFIFOTickWithPackets(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := NewFIFO(1, 8)

	// Initialize upstream Done
	p.InPort().SetDone(-1)

	// Send packet to inPort
	pkt := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "test",
	}
	env := ahead_port.PacketWithCycle{
		Cycle:  0,
		Packet: pkt,
	}

	select {
	case p.InPort().SendChan() <- env:
	case <-ctx.Done():
		t.Fatal("timeout sending packet")
	}

	// Set Done after sending
	p.InPort().SetDone(0)

	// Process cycle 0
	if err := p.Tick(0); err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify packet was processed
	if p.ProcessedCount() != 1 {
		t.Fatalf("expected ProcessedCount 1, got %d", p.ProcessedCount())
	}

	// Verify packet is available via GetProcessedPackets()
	processedPackets := p.GetProcessedPackets()
	if len(processedPackets) != 1 {
		t.Fatalf("expected 1 processed packet, got %d", len(processedPackets))
	}
	if processedPackets[0].SourceID != 0 {
		t.Fatalf("expected SourceID 0, got %d", processedPackets[0].SourceID)
	}
	if processedPackets[0].TargetID != 1 {
		t.Fatalf("expected TargetID 1, got %d", processedPackets[0].TargetID)
	}
}

// TestFIFOTickMultiplePackets tests processing multiple packets.
// Pipeline processes all packets up to Pick(), they are available via GetProcessedPackets().
func TestFIFOTickMultiplePackets(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := NewFIFO(1, 8)

	// Initialize upstream Done
	p.InPort().SetDone(-1)

	// Send multiple packets
	for i := 0; i < 3; i++ {
		pkt := packet.Packet{
			SourceID: 0,
			TargetID: 1,
			Payload:  fmt.Sprintf("test-%d", i),
		}
		env := ahead_port.PacketWithCycle{
			Cycle:  0,
			Packet: pkt,
		}
		select {
		case p.InPort().SendChan() <- env:
		case <-ctx.Done():
			t.Fatalf("timeout sending packet %d", i)
		}
	}

	// Set Done after sending
	p.InPort().SetDone(0)

	// Process cycle 0 - receives and processes all 3 packets up to Pick()
	if err := p.Tick(0); err != nil {
		t.Fatalf("Tick(0) failed: %v", err)
	}

	// Verify all packets were processed
	if p.ProcessedCount() != 3 {
		t.Fatalf("expected ProcessedCount 3, got %d", p.ProcessedCount())
	}

	// Verify all packets are available via GetProcessedPackets()
	processedPackets := p.GetProcessedPackets()
	if len(processedPackets) != 3 {
		t.Fatalf("expected 3 processed packets, got %d", len(processedPackets))
	}
	for i, pkt := range processedPackets {
		if pkt.SourceID != 0 {
			t.Fatalf("packet %d: expected SourceID 0, got %d", i, pkt.SourceID)
		}
		if string(pkt.Payload) != fmt.Sprintf("test-%d", i) {
			t.Fatalf("packet %d: expected Payload 'test-%d', got '%s'", i, i, string(pkt.Payload))
		}
	}
}

// TestFIFOTickWithEmitAndIncoming tests processing with incoming packets.
// Pipeline processes packets up to Pick(), they are available via GetProcessedPackets().
func TestFIFOTickWithEmitAndIncoming(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := NewFIFO(1, 8)

	// Initialize upstream Done
	p.InPort().SetDone(-1)

	// Send incoming packet
	incomingPkt := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "incoming",
	}
	env := ahead_port.PacketWithCycle{
		Cycle:  0,
		Packet: incomingPkt,
	}
	select {
	case p.InPort().SendChan() <- env:
	case <-ctx.Done():
		t.Fatal("timeout sending incoming packet")
	}

	// Set Done after sending
	p.InPort().SetDone(0)

	// Process cycle 0
	if err := p.Tick(0); err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify packet was processed
	if p.ProcessedCount() != 1 {
		t.Fatalf("expected ProcessedCount 1, got %d", p.ProcessedCount())
	}

	// Verify packet is available via GetProcessedPackets()
	processedPackets := p.GetProcessedPackets()
	if len(processedPackets) != 1 {
		t.Fatalf("expected 1 processed packet, got %d", len(processedPackets))
	}
	if processedPackets[0].SourceID != 0 {
		t.Fatalf("expected SourceID 0, got %d", processedPackets[0].SourceID)
	}
	if processedPackets[0].TargetID != 1 {
		t.Fatalf("expected TargetID 1, got %d", processedPackets[0].TargetID)
	}
	if string(processedPackets[0].Payload) != "incoming" {
		t.Fatalf("expected Payload 'incoming', got '%s'", string(processedPackets[0].Payload))
	}
}

// TestFIFOTickMultipleCycles tests processing across multiple cycles.
// Pipeline processes packets up to Pick() in each cycle.
func TestFIFOTickMultipleCycles(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := NewFIFO(1, 8)

	// Initialize upstream Done
	p.InPort().SetDone(-1)

	// Process cycle 0
	pkt0 := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "cycle-0",
	}
	env0 := ahead_port.PacketWithCycle{
		Cycle:  0,
		Packet: pkt0,
	}
	select {
	case p.InPort().SendChan() <- env0:
	case <-ctx.Done():
		t.Fatal("timeout sending packet for cycle 0")
	}
	p.InPort().SetDone(0)

	if err := p.Tick(0); err != nil {
		t.Fatalf("Tick(0) failed: %v", err)
	}

	// Verify packet from cycle 0 was processed
	processed0 := p.GetProcessedPackets()
	if len(processed0) != 1 {
		t.Fatalf("cycle 0: expected 1 processed packet, got %d", len(processed0))
	}
	if string(processed0[0].Payload) != "cycle-0" {
		t.Fatalf("cycle 0: expected Payload 'cycle-0', got '%s'", string(processed0[0].Payload))
	}

	// Process cycle 1
	pkt1 := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "cycle-1",
	}
	env1 := ahead_port.PacketWithCycle{
		Cycle:  1,
		Packet: pkt1,
	}
	select {
	case p.InPort().SendChan() <- env1:
	case <-ctx.Done():
		t.Fatal("timeout sending packet for cycle 1")
	}
	p.InPort().SetDone(1)

	if err := p.Tick(1); err != nil {
		t.Fatalf("Tick(1) failed: %v", err)
	}

	// Verify both packets were processed
	if p.ProcessedCount() != 2 {
		t.Fatalf("expected ProcessedCount 2, got %d", p.ProcessedCount())
	}

	// Verify packet from cycle 1 was processed
	processed1 := p.GetProcessedPackets()
	if len(processed1) != 1 {
		t.Fatalf("cycle 1: expected 1 processed packet, got %d", len(processed1))
	}
		if string(processed1[0].Payload) != "cycle-1" {
			t.Fatalf("cycle 1: expected Payload 'cycle-1', got '%s'", string(processed1[0].Payload))
		}
}

// TestOutputQueue tests OutputQueue functionality.
func TestOutputQueue(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	oq := NewOutputQueue(8)
	outPort := ahead_port.NewAheadPort(8)
	oq.SetOutPort(outPort)

	// Set downstream ready
	outPort.SetReadyUntil(10)
	outPort.UpdateReady(0, true)

	// Inject packets
	packets := []packet.Packet{
		{SourceID: 0, TargetID: 1, Payload: "test-0"},
		{SourceID: 0, TargetID: 1, Payload: "test-1"},
	}
	if err := oq.InjectPackets(0, packets); err != nil {
		t.Fatalf("InjectPackets failed: %v", err)
	}

	// Verify packets are in queue
	if oq.Length() != 2 {
		t.Fatalf("expected queue length 2, got %d", oq.Length())
	}

	// Process cycle 0 - should send packets
	if err := oq.Tick(0); err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify packets were sent (with outBandwidth=1, only 1 packet should be sent)
	receivedCount := 0
	for receivedCount < 1 {
		select {
		case received := <-outPort.ReceiveChan():
			receivedCount++
			if received.Packet.SourceID != 0 {
				t.Fatalf("expected SourceID 0, got %d", received.Packet.SourceID)
			}
		case <-ctx.Done():
			t.Fatalf("timeout receiving packets, got %d", receivedCount)
		}
	}

	// Process cycle 1 - should send remaining packet
	outPort.UpdateReady(1, true)
	if err := oq.Tick(1); err != nil {
		t.Fatalf("Tick(1) failed: %v", err)
	}

	// Verify remaining packet was sent
	select {
	case received := <-outPort.ReceiveChan():
		if received.Packet.SourceID != 0 {
			t.Fatalf("expected SourceID 0, got %d", received.Packet.SourceID)
		}
	case <-ctx.Done():
		t.Fatal("timeout receiving remaining packet")
	}
}
