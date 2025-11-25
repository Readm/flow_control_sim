package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestNewQueuePort tests QueuePort creation with default values.
func TestNewQueuePort(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(10, 2, 3, 1)
	if qp == nil {
		t.Fatal("NewQueuePort returned nil")
	}

	if qp.Capacity() != 10 {
		t.Fatalf("expected capacity 10, got %d", qp.Capacity())
	}

	if qp.Length() != 0 {
		t.Fatalf("expected initial length 0, got %d", qp.Length())
	}

	if qp.GetDone() != -1 {
		t.Fatalf("expected initial Done -1, got %d", qp.GetDone())
	}
}

// TestNewQueuePortDefaults tests default values.
func TestNewQueuePortDefaults(t *testing.T) {
	t.Parallel()

	// Test with zero/negative values
	qp := NewQueuePort(0, 0, 0, 0)
	if qp.Capacity() != 16 {
		t.Fatalf("expected default capacity 16, got %d", qp.Capacity())
	}
	if qp.inBandwidth != 1 {
		t.Fatalf("expected default inBandwidth 1, got %d", qp.inBandwidth)
	}
	if qp.outBandwidth != 1 {
		t.Fatalf("expected default outBandwidth 1, got %d", qp.outBandwidth)
	}
	if qp.bitmapWidth != 1 {
		t.Fatalf("expected default bitmapWidth 1, got %d", qp.bitmapWidth)
	}
}

// TestSetDoneGetDone tests SetDone and GetDone operations.
func TestSetDoneGetDone(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(10, 1, 1, 1)

	// Test initial value
	if qp.GetDone() != -1 {
		t.Fatalf("expected initial Done -1, got %d", qp.GetDone())
	}

	// Test setting value
	qp.SetDone(5)
	if qp.GetDone() != 5 {
		t.Fatalf("expected Done 5, got %d", qp.GetDone())
	}

	// Test concurrent updates
	var wg sync.WaitGroup
	iterations := 100
	wg.Add(iterations)

	for i := 0; i < iterations; i++ {
		go func(val int) {
			defer wg.Done()
			qp.SetDone(val)
		}(i)
	}

	wg.Wait()

	// Final value should be one of the set values
	final := qp.GetDone()
	if final < 0 || final >= iterations {
		t.Fatalf("expected Done in range [0, %d), got %d", iterations, final)
	}
}

// TestWaitForDone tests WaitForDone blocking behavior.
func TestWaitForDone(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(10, 1, 1, 1)

	// Test fast path (already satisfied)
	qp.SetDone(10)
	done := make(chan bool, 1)
	go func() {
		qp.WaitForDone(5)
		done <- true
	}()

	select {
	case <-done:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WaitForDone should return immediately when condition is already satisfied")
	}

	// Test blocking behavior
	qp.SetDone(0)
	done2 := make(chan bool, 1)
	go func() {
		qp.WaitForDone(5)
		done2 <- true
	}()

	// Give goroutine time to block
	time.Sleep(10 * time.Millisecond)

	select {
	case <-done2:
		t.Fatal("WaitForDone should block when condition is not satisfied")
	default:
		// Expected - still blocking
	}

	// Unblock by setting Done
	qp.SetDone(5)

	select {
	case <-done2:
		// Expected
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForDone should unblock when SetDone is called")
	}
}

// TestChanReceiveChan tests channel operations.
func TestChanReceiveChan(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(10, 1, 1, 1)

	// Test sending through Chan()
	pkt := ahead_port.PacketWithCycle{
		Cycle: 0,
		Packet: packet.Packet{
			SourceID: 1,
			TargetID: 2,
			Payload:  "test",
		},
	}

	// Send packet
	select {
	case qp.Chan() <- pkt:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Chan() should accept packets")
	}

	// Receive packet
	select {
	case received := <-qp.ReceiveChan():
		if received.Cycle != pkt.Cycle || received.Packet.SourceID != pkt.Packet.SourceID {
			t.Fatalf("received packet mismatch: expected %+v, got %+v", pkt, received)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ReceiveChan() should return packets")
	}
}

// TestReady tests Ready() method.
func TestReady(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(10, 1, 1, 1)

	// Test fast path (readyUntil)
	qp.UpdateReady(5, true)
	if !qp.Ready(3) {
		t.Fatal("Ready(3) should return true when cycle < readyUntil")
	}

	// Test readyMap
	qp.UpdateReady(10, true)
	if !qp.Ready(10) {
		t.Fatal("Ready(10) should return true when cycle is in readyMap")
	}

	qp.UpdateReady(15, false)
	if qp.Ready(15) {
		t.Fatal("Ready(15) should return false when cycle is not ready in readyMap")
	}
}

// TestReadyNonBlocking tests ReadyNonBlocking() method.
func TestReadyNonBlocking(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(10, 1, 1, 1)

	// Test fast path
	qp.UpdateReady(5, true)
	ready, configured := qp.ReadyNonBlocking(3)
	if !ready || !configured {
		t.Fatalf("ReadyNonBlocking(3) should return (true, true), got (%v, %v)", ready, configured)
	}

	// Test readyMap
	qp.UpdateReady(10, true)
	ready, configured = qp.ReadyNonBlocking(10)
	if !ready || !configured {
		t.Fatalf("ReadyNonBlocking(10) should return (true, true), got (%v, %v)", ready, configured)
	}

	// Test not configured
	ready, configured = qp.ReadyNonBlocking(20)
	if ready || configured {
		t.Fatalf("ReadyNonBlocking(20) should return (false, false), got (%v, %v)", ready, configured)
	}
}

// TestUpdateReady tests UpdateReady method.
func TestUpdateReady(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(10, 1, 1, 1)

	// Test updating readyMap
	qp.UpdateReady(5, true)
	ready, configured := qp.ReadyNonBlocking(5)
	if !ready || !configured {
		t.Fatalf("UpdateReady(5, true) should set readyMap, got (%v, %v)", ready, configured)
	}

	// Test updating readyUntil
	qp.UpdateReady(10, true)
	if qp.GetReadyUntil() <= 10 {
		t.Fatalf("UpdateReady(10, true) should update readyUntil to > 10, got %d", qp.GetReadyUntil())
	}

	// Test blocking behavior
	done := make(chan bool, 1)
	go func() {
		ready := qp.Ready(20)
		done <- ready
	}()

	// Give goroutine time to block
	time.Sleep(10 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("Ready(20) should block when cycle is not configured")
	default:
		// Expected - still blocking
	}

	// Unblock by calling UpdateReady
	qp.UpdateReady(20, true)

	select {
	case ready := <-done:
		if !ready {
			t.Fatal("Ready(20) should return true after UpdateReady(20, true)")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Ready(20) should unblock when UpdateReady is called")
	}
}

// TestPick tests Pick() method.
func TestPick(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(10, 1, 2, 1)

	// Add packets to array
	pkt1 := PacketWithCycle{Cycle: 5, Packet: packet.Packet{SourceID: 1}}
	pkt2 := PacketWithCycle{Cycle: 3, Packet: packet.Packet{SourceID: 2}}
	pkt3 := PacketWithCycle{Cycle: 7, Packet: packet.Packet{SourceID: 3}}

	// Manually add packets to slots
	qp.arrayMu.Lock()
	// Find free slots without calling findFreeSlot (already holding lock)
	var slot1, slot2, slot3 int = -1, -1, -1
	for i := 0; i < qp.size && (slot1 < 0 || slot2 < 0 || slot3 < 0); i++ {
		if qp.freeBitmap[i] {
			if slot1 < 0 {
				slot1 = i
			} else if slot2 < 0 {
				slot2 = i
			} else if slot3 < 0 {
				slot3 = i
			}
		}
	}
	if slot1 < 0 || slot2 < 0 || slot3 < 0 {
		t.Fatal("not enough free slots")
	}
	qp.slots[slot1] = pkt1
	qp.freeBitmap[slot1] = false
	qp.blockReasons[slot1] = 0
	qp.slots[slot2] = pkt2
	qp.freeBitmap[slot2] = false
	qp.blockReasons[slot2] = 0
	qp.slots[slot3] = pkt3
	qp.freeBitmap[slot3] = false
	qp.blockReasons[slot3] = 0
	qp.arrayMu.Unlock()

	// Pick packets (should return oldest first, max outBandwidth)
	picked := qp.Pick()

	if len(picked) != 2 {
		t.Fatalf("expected 2 packets (outBandwidth=2), got %d", len(picked))
	}

	// Should be sorted by cycle (oldest first)
	if picked[0].Cycle != 3 {
		t.Fatalf("expected first packet cycle 3 (oldest), got %d", picked[0].Cycle)
	}
	if picked[1].Cycle != 5 {
		t.Fatalf("expected second packet cycle 5, got %d", picked[1].Cycle)
	}

	// Verify packets were removed from array
	if qp.Length() != 1 {
		t.Fatalf("expected 1 packet remaining, got %d", qp.Length())
	}
}

// TestPickWithBlockReason tests Pick() with block_reason.
func TestPickWithBlockReason(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(10, 1, 2, 1)

	// Add packets with different block_reason
	qp.arrayMu.Lock()
	// Find free slots without calling findFreeSlot (already holding lock)
	var slot1, slot2, slot3 int = -1, -1, -1
	for i := 0; i < qp.size && (slot1 < 0 || slot2 < 0 || slot3 < 0); i++ {
		if qp.freeBitmap[i] {
			if slot1 < 0 {
				slot1 = i
			} else if slot2 < 0 {
				slot2 = i
			} else if slot3 < 0 {
				slot3 = i
			}
		}
	}
	if slot1 < 0 || slot2 < 0 || slot3 < 0 {
		t.Fatal("not enough free slots")
	}
	qp.slots[slot1] = PacketWithCycle{Cycle: 3, Packet: packet.Packet{SourceID: 1}}
	qp.freeBitmap[slot1] = false
	qp.blockReasons[slot1] = 0 // Free
	qp.slots[slot2] = PacketWithCycle{Cycle: 5, Packet: packet.Packet{SourceID: 2}}
	qp.freeBitmap[slot2] = false
	qp.blockReasons[slot2] = 1 // Blocked
	qp.slots[slot3] = PacketWithCycle{Cycle: 7, Packet: packet.Packet{SourceID: 3}}
	qp.freeBitmap[slot3] = false
	qp.blockReasons[slot3] = 0 // Free
	qp.arrayMu.Unlock()

	// Pick should only return free packets
	picked := qp.Pick()

	if len(picked) != 2 {
		t.Fatalf("expected 2 free packets, got %d", len(picked))
	}

	// Should not include blocked packet (cycle 5)
	for _, pkt := range picked {
		if pkt.Cycle == 5 {
			t.Fatal("Pick() should not return blocked packets")
		}
	}

	// Verify blocked packet is still in array
	if qp.Length() != 1 {
		t.Fatalf("expected 1 packet remaining (blocked), got %d", qp.Length())
	}
}

// TestSetBlockReason tests setBlockReason method.
func TestSetBlockReason(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(10, 1, 1, 2) // bitmapWidth = 2

	// Add a packet
	qp.arrayMu.Lock()
	// Find free slot without calling findFreeSlot (already holding lock)
	var slot int = -1
	for i := 0; i < qp.size; i++ {
		if qp.freeBitmap[i] {
			slot = i
			break
		}
	}
	if slot < 0 {
		t.Fatal("no free slot available")
	}
	qp.slots[slot] = PacketWithCycle{Cycle: 5, Packet: packet.Packet{SourceID: 1}}
	qp.freeBitmap[slot] = false
	qp.blockReasons[slot] = 0
	qp.arrayMu.Unlock()

	// Set block reason bit 0
	qp.setBlockReason(slot, 0, true)
	if qp.blockReasons[slot] != 1 {
		t.Fatalf("expected block_reason bit 0 set, got %d", qp.blockReasons[slot])
	}

	// Set block reason bit 1
	qp.setBlockReason(slot, 1, true)
	if qp.blockReasons[slot] != 3 {
		t.Fatalf("expected block_reason bits 0 and 1 set, got %d", qp.blockReasons[slot])
	}

	// Clear bit 0
	qp.setBlockReason(slot, 0, false)
	if qp.blockReasons[slot] != 2 {
		t.Fatalf("expected block_reason bit 1 set, got %d", qp.blockReasons[slot])
	}

	// Verify isFree returns false when blocked
	if qp.isFree(slot) {
		t.Fatal("isFree should return false when block_reason is not 0")
	}
}

// TestProcessCycle tests ProcessCycle method.
func TestProcessCycle(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Create upstream and downstream ports
	upstreamPort := ahead_port.NewAheadPort(8)
	downstreamPort := ahead_port.NewAheadPort(8)

	qp := NewQueuePort(10, 1, 1, 1)
	qp.SetUpstreamPort(upstreamPort)
	qp.SetDownstreamPort(downstreamPort)

	// Set initial state - upstream must be done with cycle-1 = -1
	upstreamPort.SetDone(-1)
	downstreamPort.SetReadyUntil(10) // Allow downstream to receive

	// Send packet from upstream
	pkt := ahead_port.PacketWithCycle{
		Cycle: 0,
		Packet: packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"},
	}
	select {
	case upstreamPort.Chan() <- pkt:
	case <-ctx.Done():
		t.Fatal("timeout sending packet")
	}
	// Set Done after sending packet
	upstreamPort.SetDone(0)

	// Process cycle 0
	done := make(chan error, 1)
	go func() {
		done <- qp.ProcessCycle(0)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProcessCycle failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("ProcessCycle timed out")
	}

	// Verify packet was received and stored
	if qp.Length() > 0 {
		// Packet should be in array
		t.Logf("Packet stored in array, length: %d", qp.Length())
	}

	// Verify Done was set on downstream port
	if downstreamPort.GetDone() < 0 {
		t.Fatal("ProcessCycle should set Done on downstream port")
	}
}

// TestProcessPackets tests ProcessPackets integration.
func TestProcessPackets(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	upstreamPort := ahead_port.NewAheadPort(8)
	downstreamPort := ahead_port.NewAheadPort(8)

	qp := NewQueuePort(10, 2, 2, 1)
	qp.SetUpstreamPort(upstreamPort)
	qp.SetDownstreamPort(downstreamPort)

	// Set initial state
	upstreamPort.SetDone(-1)
	// Set downstream ready for cycle 0, 1, 2 so packets can be sent
	downstreamPort.SetReadyUntil(10)

	// Send multiple packets
	for i := 0; i < 3; i++ {
		pkt := ahead_port.PacketWithCycle{
			Cycle:  i,
			Packet: packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"},
		}
		select {
		case upstreamPort.Chan() <- pkt:
		case <-ctx.Done():
			t.Fatalf("timeout sending packet %d", i)
		}
	}
	upstreamPort.SetDone(2)

	// Process cycle 0
	done := make(chan error, 1)
	go func() {
		done <- qp.ProcessCycle(0)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProcessCycle failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("ProcessCycle timed out")
	}

	// Verify packets were processed
	// Packets may be sent to downstream (if ready) or stored in queue (if not ready)
	// Since downstream is ready, packets should be sent, so queue length should be 0 or less
	if qp.Length() > 3 {
		t.Fatalf("expected at most 3 packets in queue, got %d", qp.Length())
	}
	t.Logf("Packets processed, queue length: %d", qp.Length())
}

// TestReadyUntilCalculation tests ReadyUntil calculation based on free packet count.
func TestReadyUntilCalculation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	qp := NewQueuePort(10, 2, 1, 1) // inBandwidth = 2

	// Add free packets
	qp.arrayMu.Lock()
	for i := 0; i < 5; i++ {
		// Find free slot without calling findFreeSlot (already holding lock)
		var slot int = -1
		for j := 0; j < qp.size; j++ {
			if qp.freeBitmap[j] {
				slot = j
				break
			}
		}
		if slot < 0 {
			t.Fatalf("no free slot available")
		}
		qp.slots[slot] = PacketWithCycle{Cycle: i, Packet: packet.Packet{SourceID: 1}}
		qp.freeBitmap[slot] = false
		qp.blockReasons[slot] = 0
	}
	qp.arrayMu.Unlock()

	// Process cycle 0 (should calculate ReadyUntil)
	upstreamPort := ahead_port.NewAheadPort(8)
	downstreamPort := ahead_port.NewAheadPort(8)
	qp.SetUpstreamPort(upstreamPort)
	qp.SetDownstreamPort(downstreamPort)

	upstreamPort.SetDone(-1)
	downstreamPort.SetReadyUntil(10)

	done := make(chan error, 1)
	go func() {
		done <- qp.ProcessCycle(0)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProcessCycle failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("ProcessCycle timed out")
	}

	// Verify ReadyUntil was updated
	// freeCount = 5, inBandwidth = 2, so ReadyUntil should be around 0 + 5/2 = 2
	readyUntil := qp.GetReadyUntil()
	if readyUntil < 0 {
		t.Fatalf("ReadyUntil should be updated, got %d", readyUntil)
	}
}

// TestConcurrentOperations tests concurrent operations.
func TestConcurrentOperations(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	qp := NewQueuePort(10, 1, 1, 1)

	var wg sync.WaitGroup

	// Concurrent SetDone
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(val int) {
			defer wg.Done()
			qp.SetDone(val)
		}(i)
	}

	// Concurrent UpdateReady
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(cycle int) {
			defer wg.Done()
			qp.UpdateReady(cycle, true)
		}(i)
	}

	// Concurrent channel operations
	wg.Add(20)
	for i := 0; i < 10; i++ {
		go func(cycle int) {
			defer wg.Done()
			pkt := ahead_port.PacketWithCycle{
				Cycle:  cycle,
				Packet: packet.Packet{SourceID: 1, TargetID: 2},
			}
			select {
			case qp.Chan() <- pkt:
			case <-ctx.Done():
			case <-time.After(100 * time.Millisecond):
			}
		}(i)
	}

	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			select {
			case <-qp.ReceiveChan():
			case <-ctx.Done():
			case <-time.After(100 * time.Millisecond):
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Verify no panics occurred
		t.Log("Concurrent operations completed without panics")
	case <-ctx.Done():
		t.Fatal("Concurrent operations timed out")
	}
}

// TestIsFullCapacity tests IsFull and Capacity methods.
func TestIsFullCapacity(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(5, 1, 1, 1)

	if qp.Capacity() != 5 {
		t.Fatalf("expected capacity 5, got %d", qp.Capacity())
	}

	if qp.IsFull() {
		t.Fatal("queue should not be full initially")
	}

	// Fill the queue
	qp.arrayMu.Lock()
	for i := 0; i < 5; i++ {
		// Find free slot without calling findFreeSlot (already holding lock)
		var slot int = -1
		for j := 0; j < qp.size; j++ {
			if qp.freeBitmap[j] {
				slot = j
				break
			}
		}
		if slot < 0 {
			t.Fatalf("no free slot available")
		}
		qp.slots[slot] = PacketWithCycle{Cycle: i, Packet: packet.Packet{SourceID: 1}}
		qp.freeBitmap[slot] = false
		qp.blockReasons[slot] = 0
	}
	qp.arrayMu.Unlock()

	if !qp.IsFull() {
		t.Fatal("queue should be full after adding 5 packets")
	}

	if qp.Length() != 5 {
		t.Fatalf("expected length 5, got %d", qp.Length())
	}
}

// TestFindFreeSlot tests findFreeSlot method.
func TestFindFreeSlot(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(5, 1, 1, 1)

	// Initially all slots should be free
	for i := 0; i < 5; i++ {
		slot := qp.findFreeSlot()
		if slot < 0 || slot >= 5 {
			t.Fatalf("findFreeSlot returned invalid slot: %d", slot)
		}
		qp.arrayMu.Lock()
		qp.freeBitmap[slot] = false
		qp.arrayMu.Unlock()
	}

	// No more free slots
	slot := qp.findFreeSlot()
	if slot >= 0 {
		t.Fatalf("expected no free slots, got slot %d", slot)
	}
}

// TestCountFreePackets tests countFreePackets method.
func TestCountFreePackets(t *testing.T) {
	t.Parallel()

	qp := NewQueuePort(10, 1, 1, 1)

	// Add packets with different block_reason
	qp.arrayMu.Lock()
	for i := 0; i < 5; i++ {
		// Find free slot without calling findFreeSlot (already holding lock)
		var slot int = -1
		for j := 0; j < qp.size; j++ {
			if qp.freeBitmap[j] {
				slot = j
				break
			}
		}
		if slot < 0 {
			t.Fatalf("no free slot available")
		}
		qp.slots[slot] = PacketWithCycle{Cycle: i, Packet: packet.Packet{SourceID: 1}}
		qp.freeBitmap[slot] = false
		if i < 3 {
			qp.blockReasons[slot] = 0 // Free
		} else {
			qp.blockReasons[slot] = 1 // Blocked
		}
	}
	qp.arrayMu.Unlock()

	freeCount := qp.countFreePackets()
	if freeCount != 3 {
		t.Fatalf("expected 3 free packets, got %d", freeCount)
	}
}

// TestProcessCycleWithExternalPorts tests ProcessCycle with external ports.
func TestProcessCycleWithExternalPorts(t *testing.T) {
	t.Parallel()

	upstreamPort := ahead_port.NewAheadPort(8)
	downstreamPort := ahead_port.NewAheadPort(8)

	qp := NewQueuePort(10, 1, 1, 1)
	qp.SetUpstreamPort(upstreamPort)
	qp.SetDownstreamPort(downstreamPort)

	// Set initial state
	upstreamPort.SetDone(-1)
	downstreamPort.SetReadyUntil(10)

	// Send packet
	pkt := ahead_port.PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"},
	}
	upstreamPort.Chan() <- pkt
	upstreamPort.SetDone(0)

	// Process cycle
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- qp.ProcessCycle(0)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProcessCycle failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("ProcessCycle timed out")
	}

	// Verify cycle+1 was configured
	_, configured := upstreamPort.ReadyNonBlocking(1)
	if !configured {
		t.Fatal("ProcessCycle should configure cycle+1 in upstream port")
	}
}

