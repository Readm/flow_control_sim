package ahead_port

import (
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Mock types are now defined in mocks.go

// timeAfter is a helper function for timeout tests
func timeAfter(ms int) <-chan time.Time {
	return time.After(time.Duration(ms) * time.Millisecond)
}

func TestConnect_BasicFlow(t *testing.T) {
	upstream := &MockUpstream{}
	downstream := &MockDownstream{}

	// Connect components using Connect function
	port := Connect(upstream, downstream)

	// Verify port was created
	if port == nil {
		t.Fatal("Connect returned nil port")
	}

	// Verify components are connected
	if upstream.toDownstream == nil {
		t.Fatal("Upstream toDownstream not set")
	}
	if downstream.fromUpstream == nil {
		t.Fatal("Downstream fromUpstream not set")
	}

	// Test data flow
	downstream.UpdateReady(0, true)

	pkt := packet.Packet{
		SourceID: 1,
		TargetID: 2,
	}

	if !upstream.SendPacket(0, pkt) {
		t.Fatal("Failed to send packet")
	}

	upstream.MarkDone(0)

	packets := downstream.ReceivePackets(0)
	if len(packets) != 1 {
		t.Fatalf("Expected 1 packet, got %d", len(packets))
	}

	if packets[0].SourceID != 1 || packets[0].TargetID != 2 {
		t.Errorf("Packet mismatch: got %+v", packets[0])
	}
}

func TestConnect_MultiCycleFlow(t *testing.T) {
	upstream := &MockUpstream{}
	downstream := &MockDownstream{}

	Connect(upstream, downstream)

	cycles := 5
	for cycle := 0; cycle < cycles; cycle++ {
		// Downstream declares ready
		downstream.UpdateReady(cycle, true)

		// Upstream sends packet
		pkt := packet.Packet{
			SourceID: cycle,
			TargetID: 100,
		}

		if !upstream.SendPacket(cycle, pkt) {
			t.Fatalf("Failed to send packet at cycle %d", cycle)
		}

		upstream.MarkDone(cycle)

		// Downstream waits and receives
		downstream.WaitDone(cycle)
		packets := downstream.ReceivePackets(cycle)

		if len(packets) != 1 {
			t.Fatalf("Cycle %d: expected 1 packet, got %d", cycle, len(packets))
		}

		if packets[0].SourceID != cycle {
			t.Errorf("Cycle %d: expected SourceID=%d, got %d", cycle, cycle, packets[0].SourceID)
		}
	}
}

func TestConnect_Backpressure(t *testing.T) {
	upstream := &MockUpstream{}
	downstream := &MockDownstream{}

	Connect(upstream, downstream)

	// Downstream is NOT ready
	pkt := packet.Packet{
		SourceID: 1,
		TargetID: 2,
	}

	// Send should fail (using non-blocking check)
	if upstream.TryPeekSendPacket(0, pkt) {
		t.Fatal("TryPeekSendPacket should fail when downstream is not ready")
	}

	// Now downstream becomes ready
	downstream.UpdateReady(0, true)

	// Send should succeed
	if !upstream.SendPacket(0, pkt) {
		t.Fatal("Send should succeed when downstream is ready")
	}
}

func TestConnect_TypeSafety(t *testing.T) {
	upstream := &MockUpstream{}
	downstream := &MockDownstream{}

	Connect(upstream, downstream)

	// This test verifies that the type system prevents misuse.
	// The following would cause compile errors:

	// ❌ upstream.toDownstream.Receive(0)     // InPort doesn't have Receive
	// ❌ upstream.toDownstream.UpdateReady()  // InPort doesn't have UpdateReady

	// ❌ downstream.fromUpstream.Send()       // OutPort doesn't have Send
	// ❌ downstream.fromUpstream.MarkDone()   // OutPort doesn't have MarkDone

	// This test just verifies the code compiles correctly.
	// The actual type safety is enforced at compile time.
	_ = upstream
	_ = downstream
}

func TestConnectWithPort(t *testing.T) {
	upstream := &MockUpstream{}
	downstream := &MockDownstream{}

	// Create port manually
	port := NewPort(0, 0)

	// Connect using existing port
	ConnectWithPort(port, upstream, downstream)

	// Verify connection works
	downstream.UpdateReady(0, true)

	pkt := packet.Packet{SourceID: 1, TargetID: 2}
	if !upstream.SendPacket(0, pkt) {
		t.Fatal("Failed to send packet")
	}

	upstream.MarkDone(0)

	packets := downstream.ReceivePackets(0)
	if len(packets) != 1 {
		t.Fatalf("Expected 1 packet, got %d", len(packets))
	}
}

func TestIsReady_Blocking(t *testing.T) {
	upstream := &MockUpstream{}
	downstream := &MockDownstream{}

	Connect(upstream, downstream)

	// Test 1: IsReady blocks when state is undecided
	done := make(chan bool, 1)
	go func() {
		ready := upstream.toDownstream.IsReady(0)
		done <- ready
	}()

	// Verify it's blocking (should not return immediately)
	select {
	case <-done:
		t.Fatal("IsReady returned before ready state was decided")
	case <-timeAfter(50):
		// Expected to block
	}

	// Now downstream declares ready
	downstream.UpdateReady(0, true)

	// Now IsReady should return
	select {
	case ready := <-done:
		if !ready {
			t.Fatal("Expected ready=true, got false")
		}
	case <-timeAfter(100):
		t.Fatal("IsReady did not return after UpdateReady")
	}
}

func TestIsReady_ImmediateReturn(t *testing.T) {
	upstream := &MockUpstream{}
	downstream := &MockDownstream{}

	Connect(upstream, downstream)

	// Downstream declares ready BEFORE IsReady
	downstream.UpdateReady(0, true)

	// IsReady should return immediately
	ready := upstream.toDownstream.IsReady(0)
	if !ready {
		t.Fatal("Expected ready=true, got false")
	}
}

func TestIsReady_NotReady(t *testing.T) {
	upstream := &MockUpstream{}
	downstream := &MockDownstream{}

	Connect(upstream, downstream)

	// Test blocking when downstream is NOT ready
	done := make(chan bool, 1)
	go func() {
		ready := upstream.toDownstream.IsReady(0)
		done <- ready
	}()

	// Verify it's blocking
	select {
	case <-done:
		t.Fatal("IsReady returned before ready state was decided")
	case <-timeAfter(50):
		// Expected to block
	}

	// Downstream declares NOT ready
	downstream.UpdateReady(0, false)

	// IsReady should return false
	select {
	case ready := <-done:
		if ready {
			t.Fatal("Expected ready=false, got true")
		}
	case <-timeAfter(100):
		t.Fatal("IsReady did not return after UpdateReady")
	}
}
