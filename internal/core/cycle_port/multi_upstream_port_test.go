package cycle_port

import (
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestNewMultiUpstreamPort tests creating a new MultiUpstreamPort.
func TestNewMultiUpstreamPort(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewCyclePort(8)
	upstreamPort2 := NewCyclePort(8)

	multi := NewMultiUpstreamPort([]CyclePort{upstreamPort1, upstreamPort2})
	defer multi.Close()

	if multi == nil {
		t.Fatal("NewMultiUpstreamPort returned nil")
	}

	if len(multi.upstreamPorts) != 2 {
		t.Fatalf("expected 2 upstream ports, got %d", len(multi.upstreamPorts))
	}
}

// TestNewMultiUpstreamPortEmptyList tests that creating with empty list panics.
func TestNewMultiUpstreamPortEmptyList(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when creating MultiUpstreamPort with empty list")
		}
	}()

	NewMultiUpstreamPort([]CyclePort{})
}

// TestMultiUpstreamPortGetDoneUntil tests GetDoneUntil returns minimum value.
func TestMultiUpstreamPortGetDoneUntil(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewCyclePort(8)
	upstreamPort2 := NewCyclePort(8)
	upstreamPort3 := NewCyclePort(8)

	multi := NewMultiUpstreamPort([]CyclePort{upstreamPort1, upstreamPort2, upstreamPort3})
	defer multi.Close()

	// Initial value should be -1
	if multi.GetDoneUntil() != -1 {
		t.Fatalf("expected initial DoneUntil -1, got %d", multi.GetDoneUntil())
	}

	// Set different values
	upstreamPort1.SetDoneUntil(5)
	upstreamPort2.SetDoneUntil(3)
	upstreamPort3.SetDoneUntil(7)

	// Should return minimum (3)
	if multi.GetDoneUntil() != 3 {
		t.Fatalf("expected DoneUntil 3 (minimum), got %d", multi.GetDoneUntil())
	}

	// Update to new minimum
	upstreamPort2.SetDoneUntil(10)
	if multi.GetDoneUntil() != 5 {
		t.Fatalf("expected DoneUntil 5 (new minimum), got %d", multi.GetDoneUntil())
	}
}

// TestMultiUpstreamPortWaitForDoneUntil tests waiting for all upstream ports.
func TestMultiUpstreamPortWaitForDoneUntil(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewCyclePort(8)
	upstreamPort2 := NewCyclePort(8)
	upstreamPort3 := NewCyclePort(8)

	multi := NewMultiUpstreamPort([]CyclePort{upstreamPort1, upstreamPort2, upstreamPort3})
	defer multi.Close()

	// Set all to cycle 5
	upstreamPort1.SetDoneUntil(5)
	upstreamPort2.SetDoneUntil(5)
	upstreamPort3.SetDoneUntil(5)

	// Should return immediately
	done := make(chan struct{})
	go func() {
		multi.WaitForDoneUntil(5)
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForDoneUntil should return immediately when all upstream are ready")
	}

	// Test blocking: one upstream is behind
	upstreamPort1.SetDoneUntil(10)
	upstreamPort2.SetDoneUntil(10)
	upstreamPort3.SetDoneUntil(8) // Behind

	done2 := make(chan struct{})
	go func() {
		multi.WaitForDoneUntil(10)
		close(done2)
	}()

	// Should block because port3 is at 8
	select {
	case <-done2:
		t.Fatal("WaitForDoneUntil should block when one upstream is behind")
	case <-time.After(100 * time.Millisecond):
		// Expected: still blocking
	}

	// Update port3 to 10, should unblock
	upstreamPort3.SetDoneUntil(10)
	select {
	case <-done2:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForDoneUntil should unblock when all upstream reach target")
	}
}

// TestMultiUpstreamPortReceiveChan tests receiving packets from multiple upstream ports.
func TestMultiUpstreamPortReceiveChan(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewCyclePort(8)
	upstreamPort2 := NewCyclePort(8)
	upstreamPort3 := NewCyclePort(8)

	multi := NewMultiUpstreamPort([]CyclePort{upstreamPort1, upstreamPort2, upstreamPort3})
	defer multi.Close()

	// Send packets from different upstream ports
	packets := []PacketWithCycle{
		{Cycle: 0, Packet: packet.Packet{SourceID: 1, TargetID: 10, Payload: "pkt1"}},
		{Cycle: 1, Packet: packet.Packet{SourceID: 2, TargetID: 10, Payload: "pkt2"}},
		{Cycle: 2, Packet: packet.Packet{SourceID: 3, TargetID: 10, Payload: "pkt3"}},
		{Cycle: 3, Packet: packet.Packet{SourceID: 1, TargetID: 10, Payload: "pkt4"}},
		{Cycle: 4, Packet: packet.Packet{SourceID: 2, TargetID: 10, Payload: "pkt5"}},
	}

	// Send packets from different ports
	upstreamPort1.Chan() <- packets[0]
	upstreamPort2.Chan() <- packets[1]
	upstreamPort3.Chan() <- packets[2]
	upstreamPort1.Chan() <- packets[3]
	upstreamPort2.Chan() <- packets[4]

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

// TestMultiUpstreamPortUpdateReady tests updating ready status for all upstream ports.
func TestMultiUpstreamPortUpdateReady(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewCyclePort(8)
	upstreamPort2 := NewCyclePort(8)
	upstreamPort3 := NewCyclePort(8)

	multi := NewMultiUpstreamPort([]CyclePort{upstreamPort1, upstreamPort2, upstreamPort3})
	defer multi.Close()

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

// TestMultiUpstreamPortReadyNonBlocking tests ReadyNonBlocking for all upstream ports.
func TestMultiUpstreamPortReadyNonBlocking(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewCyclePort(8)
	upstreamPort2 := NewCyclePort(8)
	upstreamPort3 := NewCyclePort(8)

	multi := NewMultiUpstreamPort([]CyclePort{upstreamPort1, upstreamPort2, upstreamPort3})
	defer multi.Close()

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

// TestMultiUpstreamPortWithCycleProcessor tests integration with CycleProcessor.
func TestMultiUpstreamPortWithCycleProcessor(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewCyclePort(8)
	upstreamPort2 := NewCyclePort(8)
	downstreamPort := NewCyclePort(8)

	multi := NewMultiUpstreamPort([]CyclePort{upstreamPort1, upstreamPort2})
	defer multi.Close()

	processor := NewCycleProcessor(multi, downstreamPort, nil)

	// Set initial DoneUntil for upstream ports
	upstreamPort1.SetDoneUntil(0)
	upstreamPort2.SetDoneUntil(0)

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

	upstreamPort1.Chan() <- pkt1
	upstreamPort2.Chan() <- pkt2

	// Give some time for packets to be forwarded to mergedChan
	time.Sleep(10 * time.Millisecond)

	// Set DoneUntil after sending
	upstreamPort1.SetDoneUntil(1)
	upstreamPort2.SetDoneUntil(1)

	// Process cycle 0
	err := processor.ProcessCycle(0)
	if err != nil {
		t.Fatalf("ProcessCycle failed: %v", err)
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

	// Verify downstream DoneUntil was updated
	if downstreamPort.GetDoneUntil() < 1 {
		t.Fatalf("expected downstream DoneUntil >= 1, got %d", downstreamPort.GetDoneUntil())
	}
}

// TestMultiUpstreamPortConcurrent tests concurrent operations.
func TestMultiUpstreamPortConcurrent(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewCyclePort(8)
	upstreamPort2 := NewCyclePort(8)
	upstreamPort3 := NewCyclePort(8)

	multi := NewMultiUpstreamPort([]CyclePort{upstreamPort1, upstreamPort2, upstreamPort3})
	defer multi.Close()

	var wg sync.WaitGroup

	// Concurrently send packets
	wg.Add(3)
	for i, port := range []CyclePort{upstreamPort1, upstreamPort2, upstreamPort3} {
		go func(p CyclePort, id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				pkt := PacketWithCycle{
					Cycle:  uint64(j),
					Packet: packet.Packet{SourceID: id, TargetID: 10, Payload: "pkt"},
				}
				p.Chan() <- pkt
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

	// Concurrently update DoneUntil
	wg.Add(3)
	for i, port := range []CyclePort{upstreamPort1, upstreamPort2, upstreamPort3} {
		go func(p CyclePort, id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				p.SetDoneUntil(j + 1)
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

// TestMultiUpstreamPortClose tests that Close properly cleans up resources.
func TestMultiUpstreamPortClose(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewCyclePort(8)
	upstreamPort2 := NewCyclePort(8)

	multi := NewMultiUpstreamPort([]CyclePort{upstreamPort1, upstreamPort2})

	// Send some packets
	upstreamPort1.Chan() <- PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 1, TargetID: 10, Payload: "pkt"},
	}

	// Close should not panic
	multi.Close()

	// Receiving from closed channel should return zero value immediately
	select {
	case <-multi.ReceiveChan():
		// May receive buffered packets
	case <-time.After(100 * time.Millisecond):
		// Or timeout if channel is closed and empty
	}
}

// TestMultiUpstreamPortUpstreamOperationsPanic tests that upstream operations panic.
func TestMultiUpstreamPortUpstreamOperationsPanic(t *testing.T) {
	t.Parallel()

	upstreamPort1 := NewCyclePort(8)
	upstreamPort2 := NewCyclePort(8)

	multi := NewMultiUpstreamPort([]CyclePort{upstreamPort1, upstreamPort2})
	defer multi.Close()

	// Test SetDoneUntil panics
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected SetDoneUntil to panic")
			}
		}()
		multi.SetDoneUntil(5)
	}()

	// Test Chan panics
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected Chan to panic")
			}
		}()
		_ = multi.Chan()
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

