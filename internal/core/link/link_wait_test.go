package link

import (
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/pipeline"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestLinkWaitLogic tests that Link waits for Done(cycle+1-latency) instead of Done(cycle).
// This test verifies the optimization that allows Link to process packets earlier.
func TestLinkWaitLogic(t *testing.T) {
	t.Parallel()

	flow0 := pipeline.NewFIFO(0, 8)
	flow1 := pipeline.NewFIFO(1, 8)

	flow0OutPort := ahead_port.NewAheadPort(8)
	flow1InPort := flow1.InPort()

	flow0.SetOutPort(flow0OutPort)

	// Create Link with latency=3
	// At cycle 5, Link should wait for Done >= 5+1-3 = 3 (not 5)
	link := NewLink(0, 1, flow0OutPort, flow1InPort, 3, 1)

	// Initialize upstream Done for flow0
	flow0.InPort().SetDone(-1)

	// Initialize downstream ready state
	if flow1InPortImpl, ok := flow1InPort.(*ahead_port.SinglePort); ok {
		flow1InPortImpl.SetReadyUntil(10)
	}

	// Initialize upstream ready state for flow0OutPort (allows Flow0 to send packets)
	flow0OutPort.SetReadyUntil(10)

	// Send packet at cycle 0
	pkt := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}
	env := ahead_port.PacketWithCycle{Cycle: 0, Packet: pkt}
	flow0OutPort.SendChan() <- env

	// Process flow0 to send the packet
	flow0.ProcessCycle(0)
	flow0.InPort().SetDone(1)
	flow0.ProcessCycle(1)
	flow0.InPort().SetDone(2)

	// At cycle 2, Link should wait for Done >= 2+1-3 = 0
	// Since flow0OutPort.Done is already 2, it should proceed immediately
	var wg sync.WaitGroup
	wg.Add(1)
	startTime := time.Now()
	go func() {
		defer wg.Done()
		// This should not block because Done(2) >= 0
		link.ProcessCycle(2)
	}()
	wg.Wait()
	elapsed := time.Since(startTime)

	// Should complete quickly (no blocking)
	if elapsed > 100*time.Millisecond {
		t.Errorf("Link.ProcessCycle(2) should not block when Done(2) >= 0, but took %v", elapsed)
	}

	// Now test that it waits correctly when upstream is behind
	// At cycle 5, Link should wait for Done >= 5+1-3 = 3
	// But flow0OutPort.Done is only 2, so it should wait
	flow0.InPort().SetDone(3)
	flow0.ProcessCycle(3)
	// flow0OutPort.Done is now 3

	// Test blocking behavior
	done := make(chan bool, 1)
	startTime = time.Now()
	go func() {
		// At cycle 4, Link should wait for Done >= 4+1-3 = 2
		// flow0OutPort.Done is 3, so it should proceed immediately
		link.ProcessCycle(4)
		done <- true
	}()

	select {
	case <-done:
		elapsed = time.Since(startTime)
		if elapsed > 100*time.Millisecond {
			t.Errorf("Link.ProcessCycle(4) should not block when Done(3) >= 2, but took %v", elapsed)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Link.ProcessCycle(4) should complete immediately when Done(3) >= 2")
	}
}

// TestLinkWaitLogicBoundary tests boundary cases for the wait logic.
func TestLinkWaitLogicBoundary(t *testing.T) {
	t.Parallel()

	flow0 := pipeline.NewFIFO(0, 8)
	flow1 := pipeline.NewFIFO(1, 8)

	flow0OutPort := ahead_port.NewAheadPort(8)
	flow1InPort := flow1.InPort()

	flow0.SetOutPort(flow0OutPort)

	// Create Link with latency=5
	link := NewLink(0, 1, flow0OutPort, flow1InPort, 5, 1)

	// Initialize
	flow0.InPort().SetDone(-1)
	if flow1InPortImpl, ok := flow1InPort.(*ahead_port.SinglePort); ok {
		flow1InPortImpl.SetReadyUntil(10)
	}

	// Initialize upstream ready state for flow0OutPort (allows Flow0 to send packets)
	flow0OutPort.SetReadyUntil(10)

	// Test case: cycle=2, latency=5
	// targetWaitCycle = 2+1-5 = -2, should clamp to 0
	// At cycle 2, Link should wait for Done >= 0
	flow0.ProcessCycle(0)
	flow0.InPort().SetDone(1)

	done := make(chan bool, 1)
	go func() {
		// Should not block because Done(1) >= 0
		link.ProcessCycle(2)
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Link.ProcessCycle(2) should complete when Done(1) >= 0 (clamped)")
	}
}

// TestLinkWaitLogicEarlyProcessing tests that Link can process packets earlier with latency buffer.
func TestLinkWaitLogicEarlyProcessing(t *testing.T) {
	t.Parallel()

	flow0 := pipeline.NewFIFO(0, 8)
	flow1 := pipeline.NewFIFO(1, 8)

	flow0OutPort := ahead_port.NewAheadPort(8)
	flow1InPort := flow1.InPort()

	flow0.SetOutPort(flow0OutPort)

	// Create Link with latency=4
	// This means packets sent at cycle N will arrive at cycle N+4
	link := NewLink(0, 1, flow0OutPort, flow1InPort, 4, 1)

	// Initialize
	flow0.InPort().SetDone(-1)
	if flow1InPortImpl, ok := flow1InPort.(*ahead_port.SinglePort); ok {
		flow1InPortImpl.SetReadyUntil(10)
	}

	// Initialize upstream ready state for flow0OutPort (allows Flow0 to send packets)
	flow0OutPort.SetReadyUntil(10)

	// Send packet at cycle 0
	pkt := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}
	env := ahead_port.PacketWithCycle{Cycle: 0, Packet: pkt}
	flow0OutPort.SendChan() <- env

	// Process flow0
	flow0.ProcessCycle(0)
	flow0.InPort().SetDone(1)
	flow0.ProcessCycle(1)
	flow0.InPort().SetDone(2)
	flow0.ProcessCycle(2)
	// flow0OutPort.Done is now 3

	// At cycle 2, Link should wait for Done >= 2+1-4 = -1 (clamped to 0)
	// Since flow0OutPort.Done is 3, it should proceed
	// The packet will be stored in slot for cycle 0+4=4
	link.ProcessCycle(2)

	// Verify packet is in the buffer (not yet sent, as targetCycle is 4)
	if flow1.ProcessedCount() != 0 {
		t.Errorf("expected 0 processed packets at cycle 2 (packet arrives at cycle 4), got %d", flow1.ProcessedCount())
	}

	// Process cycle 4 - packet should be sent
	flow0.InPort().SetDone(4)
	flow0.ProcessCycle(4)
	link.ProcessCycle(4)
	flow1.ProcessCycle(4)

	// Verify packet was received
	if flow1.ProcessedCount() != 1 {
		t.Errorf("expected 1 processed packet at cycle 4, got %d", flow1.ProcessedCount())
	}
}

