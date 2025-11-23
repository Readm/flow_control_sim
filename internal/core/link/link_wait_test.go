package link

import (
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/cycle_port"
	"github.com/Readm/flow_sim/internal/dataflow/flow"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestLinkWaitLogic tests that Link waits for DoneUntil(cycle+1-latency) instead of DoneUntil(cycle).
// This test verifies the optimization that allows Link to process packets earlier.
func TestLinkWaitLogic(t *testing.T) {
	t.Parallel()

	flow0 := flow.NewFIFO(0, 8)
	flow1 := flow.NewFIFO(1, 8)

	flow0OutPort := cycle_port.NewCyclePort(8)
	flow1InPort := flow1.InPort()

	flow0.AddOutPort(flow0OutPort)

	// Create Link with latency=3
	// At cycle 5, Link should wait for DoneUntil >= 5+1-3 = 3 (not 5)
	link := NewLink(0, 1, flow0OutPort, flow1InPort, 3, 1)

	// Initialize upstream DoneUntil for flow0
	flow0.InPort().SetDoneUntil(0)

	// Initialize downstream ready state
	if flow1InPortImpl, ok := flow1InPort.(*cycle_port.CyclePortImpl); ok {
		flow1InPortImpl.SetReadyUntil(10)
	}

	// Send packet at cycle 0
	pkt := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}
	env := cycle_port.PacketWithCycle{Cycle: 0, Packet: pkt}
	flow0OutPort.Chan() <- env

	// Process flow0 to send the packet
	flow0.ProcessCycle(0)
	flow0.InPort().SetDoneUntil(1)
	flow0.ProcessCycle(1)
	flow0.InPort().SetDoneUntil(2)

	// At cycle 2, Link should wait for DoneUntil >= 2+1-3 = 0
	// Since flow0OutPort.DoneUntil is already 2, it should proceed immediately
	var wg sync.WaitGroup
	wg.Add(1)
	startTime := time.Now()
	go func() {
		defer wg.Done()
		// This should not block because DoneUntil(2) >= 0
		link.ProcessCycle(2)
	}()
	wg.Wait()
	elapsed := time.Since(startTime)

	// Should complete quickly (no blocking)
	if elapsed > 100*time.Millisecond {
		t.Errorf("Link.ProcessCycle(2) should not block when DoneUntil(2) >= 0, but took %v", elapsed)
	}

	// Now test that it waits correctly when upstream is behind
	// At cycle 5, Link should wait for DoneUntil >= 5+1-3 = 3
	// But flow0OutPort.DoneUntil is only 2, so it should wait
	flow0.InPort().SetDoneUntil(3)
	flow0.ProcessCycle(3)
	// flow0OutPort.DoneUntil is now 3

	// Test blocking behavior
	done := make(chan bool, 1)
	startTime = time.Now()
	go func() {
		// At cycle 4, Link should wait for DoneUntil >= 4+1-3 = 2
		// flow0OutPort.DoneUntil is 3, so it should proceed immediately
		link.ProcessCycle(4)
		done <- true
	}()

	select {
	case <-done:
		elapsed = time.Since(startTime)
		if elapsed > 100*time.Millisecond {
			t.Errorf("Link.ProcessCycle(4) should not block when DoneUntil(3) >= 2, but took %v", elapsed)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Link.ProcessCycle(4) should complete immediately when DoneUntil(3) >= 2")
	}
}

// TestLinkWaitLogicBoundary tests boundary cases for the wait logic.
func TestLinkWaitLogicBoundary(t *testing.T) {
	t.Parallel()

	flow0 := flow.NewFIFO(0, 8)
	flow1 := flow.NewFIFO(1, 8)

	flow0OutPort := cycle_port.NewCyclePort(8)
	flow1InPort := flow1.InPort()

	flow0.AddOutPort(flow0OutPort)

	// Create Link with latency=5
	link := NewLink(0, 1, flow0OutPort, flow1InPort, 5, 1)

	// Initialize
	flow0.InPort().SetDoneUntil(0)
	if flow1InPortImpl, ok := flow1InPort.(*cycle_port.CyclePortImpl); ok {
		flow1InPortImpl.SetReadyUntil(10)
	}

	// Test case: cycle=2, latency=5
	// targetWaitCycle = 2+1-5 = -2, should clamp to 0
	// At cycle 2, Link should wait for DoneUntil >= 0
	flow0.ProcessCycle(0)
	flow0.InPort().SetDoneUntil(1)

	done := make(chan bool, 1)
	go func() {
		// Should not block because DoneUntil(1) >= 0
		link.ProcessCycle(2)
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Link.ProcessCycle(2) should complete when DoneUntil(1) >= 0 (clamped)")
	}
}

// TestLinkWaitLogicEarlyProcessing tests that Link can process packets earlier with latency buffer.
func TestLinkWaitLogicEarlyProcessing(t *testing.T) {
	t.Parallel()

	flow0 := flow.NewFIFO(0, 8)
	flow1 := flow.NewFIFO(1, 8)

	flow0OutPort := cycle_port.NewCyclePort(8)
	flow1InPort := flow1.InPort()

	flow0.AddOutPort(flow0OutPort)

	// Create Link with latency=4
	// This means packets sent at cycle N will arrive at cycle N+4
	link := NewLink(0, 1, flow0OutPort, flow1InPort, 4, 1)

	// Initialize
	flow0.InPort().SetDoneUntil(0)
	if flow1InPortImpl, ok := flow1InPort.(*cycle_port.CyclePortImpl); ok {
		flow1InPortImpl.SetReadyUntil(10)
	}

	// Send packet at cycle 0
	pkt := packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"}
	env := cycle_port.PacketWithCycle{Cycle: 0, Packet: pkt}
	flow0OutPort.Chan() <- env

	// Process flow0
	flow0.ProcessCycle(0)
	flow0.InPort().SetDoneUntil(1)
	flow0.ProcessCycle(1)
	flow0.InPort().SetDoneUntil(2)
	flow0.ProcessCycle(2)
	// flow0OutPort.DoneUntil is now 3

	// At cycle 2, Link should wait for DoneUntil >= 2+1-4 = -1 (clamped to 0)
	// Since flow0OutPort.DoneUntil is 3, it should proceed
	// The packet will be stored in slot for cycle 0+4=4
	link.ProcessCycle(2)

	// Verify packet is in the buffer (not yet sent, as targetCycle is 4)
	if flow1.ProcessedCount() != 0 {
		t.Errorf("expected 0 processed packets at cycle 2 (packet arrives at cycle 4), got %d", flow1.ProcessedCount())
	}

	// Process cycle 4 - packet should be sent
	flow0.InPort().SetDoneUntil(4)
	flow0.ProcessCycle(4)
	link.ProcessCycle(4)
	flow1.ProcessCycle(4)

	// Verify packet was received
	if flow1.ProcessedCount() != 1 {
		t.Errorf("expected 1 processed packet at cycle 4, got %d", flow1.ProcessedCount())
	}
}

