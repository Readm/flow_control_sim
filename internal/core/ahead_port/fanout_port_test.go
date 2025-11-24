package ahead_port

import (
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestNewFanoutPort tests creating a new FanoutPort.
func TestNewFanoutPort(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort1 := NewAheadPort(8)
	downstreamPort2 := NewAheadPort(8)

	router := func(ctx RouterContext) int {
		return 0 // Simple router: always route to first port
	}

	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort1, downstreamPort2}, router, nil)

	if fanout == nil {
		t.Fatal("NewFanoutPort returned nil")
	}

	if len(fanout.downstreamPorts) != 2 {
		t.Fatalf("expected 2 downstream ports, got %d", len(fanout.downstreamPorts))
	}
}

// TestNewFanoutPortNilUpstream tests that creating with nil upstream panics.
func TestNewFanoutPortNilUpstream(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when creating FanoutPort with nil upstream")
		}
	}()

	downstreamPort := NewAheadPort(8)
	router := func(ctx RouterContext) int { return 0 }
	NewFanoutPort(nil, []AheadPort{downstreamPort}, router, nil)
}

// TestNewFanoutPortEmptyDownstream tests that creating with empty downstream panics.
func TestNewFanoutPortEmptyDownstream(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when creating FanoutPort with empty downstream")
		}
	}()

	upstreamPort := NewAheadPort(8)
	router := func(ctx RouterContext) int { return 0 }
	NewFanoutPort(upstreamPort, []AheadPort{}, router, nil)
}

// TestNewFanoutPortNilRouter tests that creating with nil router panics.
func TestNewFanoutPortNilRouter(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when creating FanoutPort with nil router")
		}
	}()

	upstreamPort := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)
	NewFanoutPort(upstreamPort, []AheadPort{downstreamPort}, nil, nil)
}

// TestFanoutPortSetDone tests SetDone sets all downstream ports.
func TestFanoutPortSetDone(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort1 := NewAheadPort(8)
	downstreamPort2 := NewAheadPort(8)
	downstreamPort3 := NewAheadPort(8)

	router := func(ctx RouterContext) int { return 0 }
	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort1, downstreamPort2, downstreamPort3}, router, nil)

	// Set Done through fanout
	fanout.SetDone(5)

	// Check all downstream ports have Done = 5
	if downstreamPort1.GetDone() != 5 {
		t.Fatalf("expected downstreamPort1 Done 5, got %d", downstreamPort1.GetDone())
	}
	if downstreamPort2.GetDone() != 5 {
		t.Fatalf("expected downstreamPort2 Done 5, got %d", downstreamPort2.GetDone())
	}
	if downstreamPort3.GetDone() != 5 {
		t.Fatalf("expected downstreamPort3 Done 5, got %d", downstreamPort3.GetDone())
	}
}

// TestFanoutPortGetDone tests GetDone returns upstream value.
func TestFanoutPortGetDone(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)

	router := func(ctx RouterContext) int { return 0 }
	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort}, router, nil)

	// Set upstream Done
	upstreamPort.SetDone(7)

	// Fanout should return upstream value
	if fanout.GetDone() != 7 {
		t.Fatalf("expected Done 7, got %d", fanout.GetDone())
	}
}

// TestFanoutPortReadyAnyReady tests Ready (AnyReady) returns true if any downstream is ready.
func TestFanoutPortReadyAnyReady(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort1 := NewAheadPort(8)
	downstreamPort2 := NewAheadPort(8)
	downstreamPort3 := NewAheadPort(8)

	router := func(ctx RouterContext) int { return 0 }
	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort1, downstreamPort2, downstreamPort3}, router, nil)

	// Set all downstreams to ready for cycle 0
	downstreamPort1.SetReadyUntil(1)
	downstreamPort2.SetReadyUntil(1)
	downstreamPort3.SetReadyUntil(1)

	// AnyReady should return true
	if !fanout.Ready(0) {
		t.Fatal("expected Ready(0) to return true when all downstreams are ready")
	}

	// Set only one downstream to ready
	downstreamPort1.SetReadyUntil(1)
	downstreamPort2.SetReadyUntil(-1)
	downstreamPort2.UpdateReady(0, false)
	downstreamPort3.SetReadyUntil(-1)
	downstreamPort3.UpdateReady(0, false)

	// AnyReady should still return true (at least one is ready)
	if !fanout.Ready(0) {
		t.Fatal("expected Ready(0) to return true when at least one downstream is ready")
	}

	// Set all to not ready
	downstreamPort1.SetReadyUntil(-1)
	downstreamPort1.UpdateReady(0, false)

	// AnyReady should return false (none are ready)
	// Note: This will block, so we use ReadyNonBlocking for testing
	ready, configured := fanout.ReadyNonBlocking(0)
	if ready {
		t.Fatal("expected ReadyNonBlocking(0) to return ready=false when no downstreams are ready")
	}
	if !configured {
		t.Fatal("expected ReadyNonBlocking(0) to return configured=true")
	}
}

// TestFanoutPortAllReady tests AllReady returns true only if all downstreams are ready.
func TestFanoutPortAllReady(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort1 := NewAheadPort(8)
	downstreamPort2 := NewAheadPort(8)
	downstreamPort3 := NewAheadPort(8)

	router := func(ctx RouterContext) int { return 0 }
	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort1, downstreamPort2, downstreamPort3}, router, nil)

	// Set all downstreams to ready
	downstreamPort1.SetReadyUntil(1)
	downstreamPort2.SetReadyUntil(1)
	downstreamPort3.SetReadyUntil(1)

	// AllReady should return true
	if !fanout.AllReady(0) {
		t.Fatal("expected AllReady(0) to return true when all downstreams are ready")
	}

	// Set one downstream to not ready
	downstreamPort2.SetReadyUntil(-1)
	downstreamPort2.UpdateReady(0, false)

	// AllReady should return false (not all are ready)
	ready, configured, readyMap := fanout.AllReadyNonBlocking(0)
	if ready {
		t.Fatal("expected AllReadyNonBlocking(0) to return ready=false when not all downstreams are ready")
	}
	if !configured {
		t.Fatal("expected AllReadyNonBlocking(0) to return configured=true")
	}
	if readyMap[0] != true || readyMap[1] != false || readyMap[2] != true {
		t.Fatalf("unexpected readyMap: %v", readyMap)
	}
}

// TestFanoutPortReadyNonBlocking tests ReadyNonBlocking (AnyReady).
func TestFanoutPortReadyNonBlocking(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort1 := NewAheadPort(8)
	downstreamPort2 := NewAheadPort(8)

	router := func(ctx RouterContext) int { return 0 }
	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort1, downstreamPort2}, router, nil)

	// No downstreams configured
	ready, configured := fanout.ReadyNonBlocking(0)
	if ready {
		t.Fatal("expected ready=false when no downstreams are configured")
	}
	if configured {
		t.Fatal("expected configured=false when no downstreams are configured")
	}

	// Set one downstream to ready
	downstreamPort1.SetReadyUntil(1)

	ready, configured = fanout.ReadyNonBlocking(0)
	if !ready {
		t.Fatal("expected ready=true when at least one downstream is ready")
	}
	if !configured {
		t.Fatal("expected configured=true when at least one downstream is configured")
	}
}

// TestFanoutPortAllReadyNonBlocking tests AllReadyNonBlocking.
func TestFanoutPortAllReadyNonBlocking(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort1 := NewAheadPort(8)
	downstreamPort2 := NewAheadPort(8)
	downstreamPort3 := NewAheadPort(8)

	router := func(ctx RouterContext) int { return 0 }
	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort1, downstreamPort2, downstreamPort3}, router, nil)

	// All ready
	downstreamPort1.SetReadyUntil(1)
	downstreamPort2.SetReadyUntil(1)
	downstreamPort3.SetReadyUntil(1)

	ready, configured, readyMap := fanout.AllReadyNonBlocking(0)
	if !ready {
		t.Fatal("expected ready=true when all downstreams are ready")
	}
	if !configured {
		t.Fatal("expected configured=true when all downstreams are configured")
	}
	if len(readyMap) != 3 || !readyMap[0] || !readyMap[1] || !readyMap[2] {
		t.Fatalf("unexpected readyMap: %v", readyMap)
	}

	// One not ready
	downstreamPort2.SetReadyUntil(-1)
	downstreamPort2.UpdateReady(0, false)

	ready, configured, readyMap = fanout.AllReadyNonBlocking(0)
	if ready {
		t.Fatal("expected ready=false when not all downstreams are ready")
	}
	if !configured {
		t.Fatal("expected configured=true when all downstreams are configured")
	}
	if readyMap[1] {
		t.Fatal("expected readyMap[1]=false")
	}
}

// TestFanoutPortWaitForDone tests WaitForDone waits for upstream.
func TestFanoutPortWaitForDone(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)

	router := func(ctx RouterContext) int { return 0 }
	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort}, router, nil)

	// Set upstream Done
	upstreamPort.SetDone(5)

	// Wait should return immediately
	done := make(chan bool, 1)
	go func() {
		fanout.WaitForDone(5)
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForDone(5) should return immediately when Done >= 5")
	}
}

// TestFanoutPortRoutePacket tests RoutePacket routes packets correctly.
func TestFanoutPortRoutePacket(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort1 := NewAheadPort(8)
	downstreamPort2 := NewAheadPort(8)
	downstreamPort3 := NewAheadPort(8)

	// Router that routes to port 1
	router := func(ctx RouterContext) int {
		return 1
	}

	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort1, downstreamPort2, downstreamPort3}, router, nil)

	// Set all downstreams to ready
	downstreamPort1.SetReadyUntil(1)
	downstreamPort2.SetReadyUntil(1)
	downstreamPort3.SetReadyUntil(1)

	// Create packet
	pkt := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"},
	}

	// Route packet
	fanout.RoutePacket(pkt)

	// Check packet was sent to downstreamPort2 (index 1)
	select {
	case receivedPkt := <-downstreamPort2.ReceiveChan():
		if receivedPkt.Packet.Payload != "test" {
			t.Fatalf("expected packet payload 'test', got '%s'", receivedPkt.Packet.Payload)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected packet to be received on downstreamPort2")
	}

	// Verify other ports didn't receive
	select {
	case <-downstreamPort1.ReceiveChan():
		t.Fatal("downstreamPort1 should not receive packet")
	case <-downstreamPort3.ReceiveChan():
		t.Fatal("downstreamPort3 should not receive packet")
	case <-time.After(50 * time.Millisecond):
		// Expected: no packets on other ports
	}
}

// TestFanoutPortRoutePacketWithReadyStatus tests router can access ready status.
func TestFanoutPortRoutePacketWithReadyStatus(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort1 := NewAheadPort(8)
	downstreamPort2 := NewAheadPort(8)
	downstreamPort3 := NewAheadPort(8)

	// Router that selects first ready port
	router := func(ctx RouterContext) int {
		for i, ready := range ctx.ReadyStatus {
			if ready {
				return i
			}
		}
		return -1 // Discard if none ready
	}

	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort1, downstreamPort2, downstreamPort3}, router, nil)

	// Set only downstreamPort2 to ready
	downstreamPort1.SetReadyUntil(-1)
	downstreamPort1.UpdateReady(0, false)
	downstreamPort2.SetReadyUntil(1)
	downstreamPort3.SetReadyUntil(-1)
	downstreamPort3.UpdateReady(0, false)

	pkt := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"},
	}

	// Route packet - should go to downstreamPort2 (first ready)
	fanout.RoutePacket(pkt)

	select {
	case receivedPkt := <-downstreamPort2.ReceiveChan():
		if receivedPkt.Packet.Payload != "test" {
			t.Fatalf("expected packet payload 'test', got '%s'", receivedPkt.Packet.Payload)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected packet to be received on downstreamPort2")
	}
}

// TestFanoutPortRoutePacketDiscard tests router can discard packets.
func TestFanoutPortRoutePacketDiscard(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort1 := NewAheadPort(8)
	downstreamPort2 := NewAheadPort(8)

	// Router that discards all packets
	router := func(ctx RouterContext) int {
		return -1
	}

	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort1, downstreamPort2}, router, nil)

	// Set all to ready
	downstreamPort1.SetReadyUntil(1)
	downstreamPort2.SetReadyUntil(1)

	pkt := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"},
	}

	// Route packet - should be discarded
	fanout.RoutePacket(pkt)

	// Verify no ports received
	select {
	case <-downstreamPort1.ReceiveChan():
		t.Fatal("downstreamPort1 should not receive discarded packet")
	case <-downstreamPort2.ReceiveChan():
		t.Fatal("downstreamPort2 should not receive discarded packet")
	case <-time.After(50 * time.Millisecond):
		// Expected: packet discarded
	}
}

// TestFanoutPortSetRouter tests SetRouter.
func TestFanoutPortSetRouter(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)

	router1 := func(ctx RouterContext) int { return 0 }
	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort}, router1, nil)

	// Change router
	router2 := func(ctx RouterContext) int { return -1 }
	fanout.SetRouter(router2)

	// Verify router changed
	pkt := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"},
	}

	downstreamPort.SetReadyUntil(1)

	fanout.RoutePacket(pkt)

	// Packet should be discarded by new router
	select {
	case <-downstreamPort.ReceiveChan():
		t.Fatal("packet should be discarded by new router")
	case <-time.After(50 * time.Millisecond):
		// Expected: discarded
	}
}

// TestFanoutPortSetTopology tests SetTopology.
func TestFanoutPortSetTopology(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)

	router := func(ctx RouterContext) int {
		// Access topology
		if ctx.Topology != nil {
			return 0
		}
		return -1
	}

	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort}, router, nil)

	// Set topology
	topology := map[string]int{"test": 1}
	fanout.SetTopology(topology)

	// Verify topology is accessible in router
	pkt := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"},
	}

	downstreamPort.SetReadyUntil(1)

	fanout.RoutePacket(pkt)

	// Router should route to port 0 (topology is set)
	select {
	case <-downstreamPort.ReceiveChan():
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected packet to be routed when topology is set")
	}
}

// TestFanoutPortGetDownstreamReadyStatus tests GetDownstreamReadyStatus.
func TestFanoutPortGetDownstreamReadyStatus(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort1 := NewAheadPort(8)
	downstreamPort2 := NewAheadPort(8)
	downstreamPort3 := NewAheadPort(8)

	router := func(ctx RouterContext) int { return 0 }
	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort1, downstreamPort2, downstreamPort3}, router, nil)

	// Set different ready statuses
	downstreamPort1.SetReadyUntil(1) // ready
	downstreamPort2.SetReadyUntil(-1)
	downstreamPort2.UpdateReady(0, false) // not ready
	downstreamPort3.SetReadyUntil(1) // ready

	statuses := fanout.GetDownstreamReadyStatus(0)

	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}
	if !statuses[0].Ready || !statuses[0].Configured {
		t.Fatal("expected downstreamPort1 to be ready and configured")
	}
	if statuses[1].Ready || !statuses[1].Configured {
		t.Fatal("expected downstreamPort2 to be not ready but configured")
	}
	if !statuses[2].Ready || !statuses[2].Configured {
		t.Fatal("expected downstreamPort3 to be ready and configured")
	}
}

// TestFanoutPortReceiveChanPanic tests ReceiveChan panics.
func TestFanoutPortReceiveChanPanic(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)

	router := func(ctx RouterContext) int { return 0 }
	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort}, router, nil)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when calling ReceiveChan on FanoutPort")
		}
	}()

	fanout.ReceiveChan()
}

// TestFanoutPortConcurrent tests concurrent operations.
func TestFanoutPortConcurrent(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort1 := NewAheadPort(8)
	downstreamPort2 := NewAheadPort(8)
	downstreamPort3 := NewAheadPort(8)

	router := func(ctx RouterContext) int {
		// Round-robin based on packet payload
		if len(ctx.Packet.Payload) > 0 {
			return int(ctx.Packet.Payload[0]) % len(ctx.DownstreamPorts)
		}
		return 0
	}

	fanout := NewFanoutPort(upstreamPort, []AheadPort{downstreamPort1, downstreamPort2, downstreamPort3}, router, nil)

	// Set all downstreams to ready
	downstreamPort1.SetReadyUntil(10)
	downstreamPort2.SetReadyUntil(10)
	downstreamPort3.SetReadyUntil(10)

	var wg sync.WaitGroup
	packetCount := 100

	// Concurrent SetDone
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < packetCount; i++ {
			fanout.SetDone(i)
		}
	}()

	// Concurrent RoutePacket
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < packetCount; i++ {
			pkt := PacketWithCycle{
				Cycle:  i,
				Packet: packet.Packet{SourceID: 1, TargetID: 2, Payload: string([]byte{byte(i)})},
			}
			fanout.RoutePacket(pkt)
		}
	}()

	// Concurrent Ready checks
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < packetCount; i++ {
			fanout.ReadyNonBlocking(i)
			fanout.AllReadyNonBlocking(i)
		}
	}()

	wg.Wait()
	// Test passes if no deadlock or panic occurs
}

