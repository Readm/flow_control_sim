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

// TestFIFOProcessCycleEmpty tests processing cycle with no packets.
func TestFIFOProcessCycleEmpty(t *testing.T) {
	t.Parallel()

	p := NewFIFO(1, 8)
	outPort := ahead_port.NewAheadPort(8)
	p.SetOutPort(outPort)

	// Initialize upstream Done
	p.InPort().SetDone(-1)

	// Set downstream ready - both SetReadyUntil and UpdateReady for cycle 0
	outPort.SetReadyUntil(10)
	outPort.UpdateReady(0, true)

	// Process cycle 0
	if err := p.ProcessCycle(0); err != nil {
		t.Fatalf("ProcessCycle failed: %v", err)
	}

	// Verify Done was set
	if p.OutPort().GetDone() < 0 {
		t.Fatal("ProcessCycle should set Done on output port")
	}
}

// TestFIFOProcessCycleWithPackets tests processing cycle with incoming packets.
func TestFIFOProcessCycleWithPackets(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := NewFIFO(1, 8)
	outPort := ahead_port.NewAheadPort(8)
	p.SetOutPort(outPort)

	// Initialize upstream Done
	p.InPort().SetDone(-1)

	// Set downstream ready - both SetReadyUntil and UpdateReady for cycle 0
	outPort.SetReadyUntil(10)
	outPort.UpdateReady(0, true)

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
	if err := p.ProcessCycle(0); err != nil {
		t.Fatalf("ProcessCycle failed: %v", err)
	}

	// Verify packet was processed
	if p.ProcessedCount() != 1 {
		t.Fatalf("expected ProcessedCount 1, got %d", p.ProcessedCount())
	}

	// Verify packet was sent to outPort
	select {
	case received := <-outPort.ReceiveChan():
		if received.Packet.SourceID != 0 {
			t.Fatalf("expected SourceID 0, got %d", received.Packet.SourceID)
		}
		if received.Packet.TargetID != 1 {
			t.Fatalf("expected TargetID 1, got %d", received.Packet.TargetID)
		}
	case <-ctx.Done():
		t.Fatal("timeout receiving packet from outPort")
	}
}

// TestFIFOProcessCycleMultiplePackets tests processing multiple packets.
func TestFIFOProcessCycleMultiplePackets(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := NewFIFO(1, 8)
	outPort := ahead_port.NewAheadPort(8)
	p.SetOutPort(outPort)

	// Initialize upstream Done
	p.InPort().SetDone(-1)

	// Set downstream ready - both SetReadyUntil and UpdateReady for cycle 0
	outPort.SetReadyUntil(10)
	outPort.UpdateReady(0, true)

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

	// Process cycle 0
	if err := p.ProcessCycle(0); err != nil {
		t.Fatalf("ProcessCycle failed: %v", err)
	}

	// Verify all packets were processed
	if p.ProcessedCount() != 3 {
		t.Fatalf("expected ProcessedCount 3, got %d", p.ProcessedCount())
	}

	// Verify all packets were sent to outPort
	receivedCount := 0
	for receivedCount < 3 {
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
}

// TestFIFOProcessCycleWithEmitAndIncoming tests processing with both incoming and emitted packets.
func TestFIFOProcessCycleWithEmitAndIncoming(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := NewFIFO(1, 8)
	outPort := ahead_port.NewAheadPort(8)
	p.SetOutPort(outPort)

	// Initialize upstream Done
	p.InPort().SetDone(-1)

	// Set downstream ready - both SetReadyUntil and UpdateReady for cycle 0
	outPort.SetReadyUntil(10)
	outPort.UpdateReady(0, true)

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
	if err := p.ProcessCycle(0); err != nil {
		t.Fatalf("ProcessCycle failed: %v", err)
	}

	// Verify packet was processed
	if p.ProcessedCount() != 1 {
		t.Fatalf("expected ProcessedCount 1, got %d", p.ProcessedCount())
	}

	// Verify packet was sent to outPort
	select {
	case received := <-outPort.ReceiveChan():
		if received.Packet.SourceID != 0 {
			t.Fatalf("expected SourceID 0, got %d", received.Packet.SourceID)
		}
		if received.Packet.TargetID != 1 {
			t.Fatalf("expected TargetID 1, got %d", received.Packet.TargetID)
		}
		if string(received.Packet.Payload) != "incoming" {
			t.Fatalf("expected Payload 'incoming', got '%s'", received.Packet.Payload)
		}
	case <-ctx.Done():
		t.Fatal("timeout receiving packet from outPort")
	}
}

// TestFIFOProcessCycleMultipleCycles tests processing across multiple cycles.
func TestFIFOProcessCycleMultipleCycles(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := NewFIFO(1, 8)
	outPort := ahead_port.NewAheadPort(8)
	p.SetOutPort(outPort)

	// Initialize upstream Done
	p.InPort().SetDone(-1)

	// Set downstream ready - both SetReadyUntil and UpdateReady for cycle 0
	outPort.SetReadyUntil(10)
	outPort.UpdateReady(0, true)

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

	if err := p.ProcessCycle(0); err != nil {
		t.Fatalf("ProcessCycle(0) failed: %v", err)
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

	if err := p.ProcessCycle(1); err != nil {
		t.Fatalf("ProcessCycle(1) failed: %v", err)
	}

	// Verify both packets were processed
	if p.ProcessedCount() != 2 {
		t.Fatalf("expected ProcessedCount 2, got %d", p.ProcessedCount())
	}

	// Verify both packets were sent to outPort
	receivedCount := 0
	for receivedCount < 2 {
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
}


