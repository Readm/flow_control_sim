package ahead_port

import (
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestNewFaninPort tests creating a new FaninPort.
func TestNewFaninPort(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewAheadPort(8)
	upstreamPort2 := NewAheadPort(8)
	sharedChan := make(chan PacketWithCycle, 8)
	upstreamPort1.SetChannel(sharedChan)
	upstreamPort2.SetChannel(sharedChan)

	multi := NewFaninPort([]AheadPort{upstreamPort1, upstreamPort2}, sharedChan)

	if multi == nil {
		t.Fatal("NewFaninPort returned nil")
	}

	if len(multi.upstreamPorts) != 2 {
		t.Fatalf("expected 2 upstream ports, got %d", len(multi.upstreamPorts))
	}
}

// TestNewFaninPortEmptyList tests that creating with empty list panics.
func TestNewFaninPortEmptyList(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when creating FaninPort with empty list")
		}
	}()

	sharedChan := make(chan PacketWithCycle, 8)
	NewFaninPort([]AheadPort{}, sharedChan)
}

// TestFaninPortGetDone tests GetDone returns minimum value.
func TestFaninPortGetDone(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewAheadPort(8)
	upstreamPort2 := NewAheadPort(8)
	upstreamPort3 := NewAheadPort(8)
	sharedChan := make(chan PacketWithCycle, 8)
	upstreamPort1.SetChannel(sharedChan)
	upstreamPort2.SetChannel(sharedChan)
	upstreamPort3.SetChannel(sharedChan)

	multi := NewFaninPort([]AheadPort{upstreamPort1, upstreamPort2, upstreamPort3}, sharedChan)

	// Initial value should be -1
	if multi.GetDone() != -1 {
		t.Fatalf("expected initial Done -1, got %d", multi.GetDone())
	}

	// Set different values
	upstreamPort1.SetDone(5)
	upstreamPort2.SetDone(3)
	upstreamPort3.SetDone(7)

	// Should return minimum (3)
	if multi.GetDone() != 3 {
		t.Fatalf("expected Done 3 (minimum), got %d", multi.GetDone())
	}

	// Update to new minimum
	upstreamPort2.SetDone(10)
	if multi.GetDone() != 5 {
		t.Fatalf("expected Done 5 (new minimum), got %d", multi.GetDone())
	}
}

// TestFaninPortWaitForDone tests waiting for all upstream ports.
func TestFaninPortWaitForDone(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewAheadPort(8)
	upstreamPort2 := NewAheadPort(8)
	upstreamPort3 := NewAheadPort(8)
	sharedChan := make(chan PacketWithCycle, 8)
	upstreamPort1.SetChannel(sharedChan)
	upstreamPort2.SetChannel(sharedChan)
	upstreamPort3.SetChannel(sharedChan)

	multi := NewFaninPort([]AheadPort{upstreamPort1, upstreamPort2, upstreamPort3}, sharedChan)

	// Set all to cycle 5
	upstreamPort1.SetDone(5)
	upstreamPort2.SetDone(5)
	upstreamPort3.SetDone(5)

	// Should return immediately
	done := make(chan struct{})
	go func() {
		multi.WaitForDone(5)
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForDone should return immediately when all upstream are ready")
	}

	// Test blocking: one upstream is behind
	upstreamPort1.SetDone(10)
	upstreamPort2.SetDone(10)
	upstreamPort3.SetDone(8) // Behind

	done2 := make(chan struct{})
	go func() {
		multi.WaitForDone(10)
		close(done2)
	}()

	// Should block because port3 is at 8
	select {
	case <-done2:
		t.Fatal("WaitForDone should block when one upstream is behind")
	case <-time.After(100 * time.Millisecond):
		// Expected: still blocking
	}

	// Update port3 to 10, should unblock
	upstreamPort3.SetDone(10)
	select {
	case <-done2:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForDone should unblock when all upstream reach target")
	}
}

// TestFaninPortReceiveChan tests receiving packets from multiple upstream ports.
func TestFaninPortReceiveChan(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewAheadPort(8)
	upstreamPort2 := NewAheadPort(8)
	upstreamPort3 := NewAheadPort(8)
	sharedChan := make(chan PacketWithCycle, 8)
	upstreamPort1.SetChannel(sharedChan)
	upstreamPort2.SetChannel(sharedChan)
	upstreamPort3.SetChannel(sharedChan)

	multi := NewFaninPort([]AheadPort{upstreamPort1, upstreamPort2, upstreamPort3}, sharedChan)

	// Send packets from different upstream ports
	packets := []PacketWithCycle{
		{Cycle: 0, Packet: packet.Packet{SourceID: 1, TargetID: 10, Payload: "pkt1"}},
		{Cycle: 1, Packet: packet.Packet{SourceID: 2, TargetID: 10, Payload: "pkt2"}},
		{Cycle: 2, Packet: packet.Packet{SourceID: 3, TargetID: 10, Payload: "pkt3"}},
		{Cycle: 3, Packet: packet.Packet{SourceID: 1, TargetID: 10, Payload: "pkt4"}},
		{Cycle: 4, Packet: packet.Packet{SourceID: 2, TargetID: 10, Payload: "pkt5"}},
	}

	// Send packets from different ports
	upstreamPort1.SendChan() <- packets[0]
	upstreamPort2.SendChan() <- packets[1]
	upstreamPort3.SendChan() <- packets[2]
	upstreamPort1.SendChan() <- packets[3]
	upstreamPort2.SendChan() <- packets[4]

	// Receive all packets from merged channel
	received := make([]PacketWithCycle, 0, len(packets))
	timeout := time.After(2 * time.Second)
	for len(received) < len(packets) {
		select {
		case pkt := <-multi.ReceiveChan():
			received = append(received, pkt)
		case <-timeout:
			t.Fatalf("timeout waiting for packets, received %d/%d", len(received), len(packets))
		}
	}

	// Verify all packets were received (order may vary)
	receivedMap := make(map[string]bool)
	for _, pkt := range received {
		receivedMap[pkt.Packet.Payload] = true
	}

	for _, pkt := range packets {
		if !receivedMap[pkt.Packet.Payload] {
			t.Fatalf("packet %s was not received", pkt.Packet.Payload)
		}
	}
}

// TestFaninPortUpdateReady tests updating ready status for all upstream ports.
func TestFaninPortUpdateReady(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewAheadPort(8)
	upstreamPort2 := NewAheadPort(8)
	upstreamPort3 := NewAheadPort(8)
	sharedChan := make(chan PacketWithCycle, 8)
	upstreamPort1.SetChannel(sharedChan)
	upstreamPort2.SetChannel(sharedChan)
	upstreamPort3.SetChannel(sharedChan)

	multi := NewFaninPort([]AheadPort{upstreamPort1, upstreamPort2, upstreamPort3}, sharedChan)

	// Update ready for cycle 5
	multi.UpdateReady(5, true)

	// Check that all upstream ports have cycle 5 ready
	if !upstreamPort1.Ready(5) {
		t.Fatal("upstreamPort1 should be ready for cycle 5")
	}
	if !upstreamPort2.Ready(5) {
		t.Fatal("upstreamPort2 should be ready for cycle 5")
	}
	if !upstreamPort3.Ready(5) {
		t.Fatal("upstreamPort3 should be ready for cycle 5")
	}

	// Update ready for cycle 10 to false
	multi.UpdateReady(10, false)

	// Check that all upstream ports have cycle 10 not ready
	// Note: Ready(10) will block if not configured, so we use ReadyNonBlocking
	ready1, configured1 := upstreamPort1.ReadyNonBlocking(10)
	ready2, configured2 := upstreamPort2.ReadyNonBlocking(10)
	ready3, configured3 := upstreamPort3.ReadyNonBlocking(10)

	if !configured1 || ready1 {
		t.Fatal("upstreamPort1 should have cycle 10 configured as not ready")
	}
	if !configured2 || ready2 {
		t.Fatal("upstreamPort2 should have cycle 10 configured as not ready")
	}
	if !configured3 || ready3 {
		t.Fatal("upstreamPort3 should have cycle 10 configured as not ready")
	}
}

// TestFaninPortReadyNonBlocking tests ReadyNonBlocking for all upstream ports.
func TestFaninPortReadyNonBlocking(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewAheadPort(8)
	upstreamPort2 := NewAheadPort(8)
	upstreamPort3 := NewAheadPort(8)
	sharedChan := make(chan PacketWithCycle, 8)
	upstreamPort1.SetChannel(sharedChan)
	upstreamPort2.SetChannel(sharedChan)
	upstreamPort3.SetChannel(sharedChan)

	multi := NewFaninPort([]AheadPort{upstreamPort1, upstreamPort2, upstreamPort3}, sharedChan)

	// Initially, cycle 5 is not configured
	ready, configured := multi.ReadyNonBlocking(5)
	if configured {
		t.Fatal("cycle 5 should not be configured initially")
	}
	if ready {
		t.Fatal("cycle 5 should not be ready if not configured")
	}

	// Configure all ports for cycle 5 as ready
	upstreamPort1.UpdateReady(5, true)
	upstreamPort2.UpdateReady(5, true)
	upstreamPort3.UpdateReady(5, true)

	ready, configured = multi.ReadyNonBlocking(5)
	if !configured {
		t.Fatal("cycle 5 should be configured after UpdateReady")
	}
	if !ready {
		t.Fatal("cycle 5 should be ready after UpdateReady(true)")
	}

	// Configure one port as not ready
	upstreamPort2.UpdateReady(10, false)
	upstreamPort1.UpdateReady(10, true)
	upstreamPort3.UpdateReady(10, true)

	ready, configured = multi.ReadyNonBlocking(10)
	if !configured {
		t.Fatal("cycle 10 should be configured")
	}
	if ready {
		t.Fatal("cycle 10 should not be ready if one port is not ready")
	}

	// Set all to ready
	upstreamPort2.UpdateReady(10, true)
	ready, configured = multi.ReadyNonBlocking(10)
	if !configured || !ready {
		t.Fatal("cycle 10 should be ready after all ports are ready")
	}
}

// TestFaninPortWithCycleProcessor tests integration with CycleProcessor.
func TestFaninPortWithCycleProcessor(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewAheadPort(8)
	upstreamPort2 := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)
	sharedChan := make(chan PacketWithCycle, 8)
	upstreamPort1.SetChannel(sharedChan)
	upstreamPort2.SetChannel(sharedChan)

	multi := NewFaninPort([]AheadPort{upstreamPort1, upstreamPort2}, sharedChan)

	processor := NewCycleProcessor(multi, downstreamPort, nil)

	// Set initial Done for upstream ports
	upstreamPort1.SetDone(-1)
	upstreamPort2.SetDone(-1)

	// Set downstream ready for cycle 0
	downstreamPort.UpdateReady(0, true)

	// Send packets from upstream ports
	pkt1 := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 1, TargetID: 10, Payload: "pkt1"},
	}
	pkt2 := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 2, TargetID: 10, Payload: "pkt2"},
	}

	upstreamPort1.SendChan() <- pkt1
	upstreamPort2.SendChan() <- pkt2

	// Give some time for packets to be forwarded to mergedChan
	time.Sleep(10 * time.Millisecond)

	// Set Done after sending
	upstreamPort1.SetDone(1)
	upstreamPort2.SetDone(1)

	// Process cycle 0
	err := processor.Tick(0)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify packets were received by downstream
	received := make([]PacketWithCycle, 0)
	timeout := time.After(1 * time.Second)
	for len(received) < 2 {
		select {
		case pkt := <-downstreamPort.ReceiveChan():
			received = append(received, pkt)
		case <-timeout:
			t.Fatalf("timeout waiting for packets, received %d/2", len(received))
		}
	}

	// Verify all packets were received
	receivedMap := make(map[string]bool)
	for _, pkt := range received {
		receivedMap[pkt.Packet.Payload] = true
	}

	if !receivedMap["pkt1"] || !receivedMap["pkt2"] {
		t.Fatal("not all packets were received by downstream")
	}

	// Verify downstream Done was updated (should be 0 after processing cycle 0)
	if downstreamPort.GetDone() < 0 {
		t.Fatalf("expected downstream Done >= 0, got %d", downstreamPort.GetDone())
	}
}

// TestFaninPortConcurrent tests concurrent operations.
func TestFaninPortConcurrent(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewAheadPort(8)
	upstreamPort2 := NewAheadPort(8)
	upstreamPort3 := NewAheadPort(8)
	sharedChan := make(chan PacketWithCycle, 8)
	upstreamPort1.SetChannel(sharedChan)
	upstreamPort2.SetChannel(sharedChan)
	upstreamPort3.SetChannel(sharedChan)

	multi := NewFaninPort([]AheadPort{upstreamPort1, upstreamPort2, upstreamPort3}, sharedChan)

	var wg sync.WaitGroup

	// Concurrently send packets
	wg.Add(3)
	for i, port := range []AheadPort{upstreamPort1, upstreamPort2, upstreamPort3} {
		go func(p AheadPort, id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				pkt := PacketWithCycle{
					Cycle:  j,
					Packet: packet.Packet{SourceID: id, TargetID: 10, Payload: "pkt"},
				}
				p.SendChan() <- pkt
			}
		}(port, i)
	}

	// Concurrently receive packets
	received := make(chan PacketWithCycle, 30)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			select {
			case pkt := <-multi.ReceiveChan():
				received <- pkt
			case <-time.After(2 * time.Second):
				return
			}
		}
	}()

	// Concurrently update Done
	wg.Add(3)
	for i, port := range []AheadPort{upstreamPort1, upstreamPort2, upstreamPort3} {
		go func(p AheadPort, id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				p.SetDone(j)
				time.Sleep(time.Millisecond)
			}
		}(port, i)
	}

	wg.Wait()

	// Verify we received all packets
	if len(received) != 30 {
		t.Fatalf("expected 30 packets, received %d", len(received))
	}
}

// TestFaninPortClose tests that closing shared channel properly cleans up resources.
func TestFaninPortClose(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewAheadPort(8)
	upstreamPort2 := NewAheadPort(8)
	sharedChan := make(chan PacketWithCycle, 8)
	upstreamPort1.SetChannel(sharedChan)
	upstreamPort2.SetChannel(sharedChan)

	multi := NewFaninPort([]AheadPort{upstreamPort1, upstreamPort2}, sharedChan)

	// Send some packets
	upstreamPort1.SendChan() <- PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 1, TargetID: 10, Payload: "pkt"},
	}

	// Close shared channel
	close(sharedChan)

	// Receiving from closed channel should return zero value immediately
	select {
	case <-multi.ReceiveChan():
		// May receive buffered packets
	case <-time.After(100 * time.Millisecond):
		// Or timeout if channel is closed and empty
	}
}

// TestFaninPortUpstreamOperationsPanic tests that upstream operations panic.
func TestFaninPortUpstreamOperationsPanic(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewAheadPort(8)
	upstreamPort2 := NewAheadPort(8)
	sharedChan := make(chan PacketWithCycle, 8)
	upstreamPort1.SetChannel(sharedChan)
	upstreamPort2.SetChannel(sharedChan)

	multi := NewFaninPort([]AheadPort{upstreamPort1, upstreamPort2}, sharedChan)

	// Test SetDone panics
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected SetDone to panic")
			}
		}()
		multi.SetDone(5)
	}()

	// Test Chan panics
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected Chan to panic")
			}
		}()
		_ = multi.SendChan()
	}()

	// Test Ready panics
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected Ready to panic")
			}
		}()
		_ = multi.Ready(5)
	}()
}
