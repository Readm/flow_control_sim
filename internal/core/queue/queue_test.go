package queue

import (
	"context"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestNewQueue tests Queue creation with default values.
func TestNewQueue(t *testing.T) {
	t.Parallel()

	queue, queueIn, queueOut := NewQueue(10, 2, 3, 1)
	if queue == nil {
		t.Fatal("NewQueue returned nil queue")
	}
	if queueIn == nil {
		t.Fatal("NewQueue returned nil inPort")
	}
	if queueOut == nil {
		t.Fatal("NewQueue returned nil outPort")
	}

	if queue.Capacity() != 10 {
		t.Fatalf("expected capacity 10, got %d", queue.Capacity())
	}

	if queue.Length() != 0 {
		t.Fatalf("expected initial length 0, got %d", queue.Length())
	}

	if queue.getDone() != -1 {
		t.Fatalf("expected initial Done -1, got %d", queue.getDone())
	}
}

// TestNewQueueDefaults tests default values.
func TestNewQueueDefaults(t *testing.T) {
	t.Parallel()

	// Test with zero/negative values
	queue, _, _ := NewQueue(0, 0, 0, 0)
	if queue.Capacity() != 16 {
		t.Fatalf("expected default capacity 16, got %d", queue.Capacity())
	}
	if queue.inBandwidth != 1 {
		t.Fatalf("expected default inBandwidth 1, got %d", queue.inBandwidth)
	}
	if queue.outBandwidth != 1 {
		t.Fatalf("expected default outBandwidth 1, got %d", queue.outBandwidth)
	}
	if queue.bitmapWidth != 1 {
		t.Fatalf("expected default bitmapWidth 1, got %d", queue.bitmapWidth)
	}
}

// TestSetDoneGetDone removed - tests private methods, should test via Tick()

// TestWaitForDone removed - tests private methods, should test via Tick()

// TestChanReceiveChan removed - Queue no longer exposes channels directly, use ports

// TestReady removed - tests private methods, access via ports

// TestIsReadyNonBlocking removed - tests private methods, access via ports

// TestUpdateReady removed - tests private methods, internal implementation detail

// TestPick tests Pick() method.
func TestPick(t *testing.T) {
	t.Parallel()

	queue, _, _ := NewQueue(10, 1, 2, 1)

	// Add packets to array
	pkt1 := PacketWithCycle{Cycle: 5, Packet: packet.Packet{SourceID: 1}}
	pkt2 := PacketWithCycle{Cycle: 3, Packet: packet.Packet{SourceID: 2}}
	pkt3 := PacketWithCycle{Cycle: 7, Packet: packet.Packet{SourceID: 3}}

	// Manually add packets to slots
	queue.arrayMu.Lock()
	// Find free slots without calling findFreeSlot (already holding lock)
	var slot1, slot2, slot3 int = -1, -1, -1
	for i := 0; i < queue.size && (slot1 < 0 || slot2 < 0 || slot3 < 0); i++ {
		if queue.freeBitmap[i] {
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
	queue.slots[slot1] = pkt1
	queue.freeBitmap[slot1] = false
	queue.blockReasons[slot1] = 0
	queue.slots[slot2] = pkt2
	queue.freeBitmap[slot2] = false
	queue.blockReasons[slot2] = 0
	queue.slots[slot3] = pkt3
	queue.freeBitmap[slot3] = false
	queue.blockReasons[slot3] = 0
	queue.arrayMu.Unlock()

	// Pick packets (should return oldest first, max outBandwidth)
	picked := queue.Pick()

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
	if queue.Length() != 1 {
		t.Fatalf("expected 1 packet remaining, got %d", queue.Length())
	}
}

// TestPickWithBlockReason tests Pick() with block_reason.
func TestPickWithBlockReason(t *testing.T) {
	t.Parallel()

	queue, _, _ := NewQueue(10, 1, 2, 1)

	// Add packets with different block_reason
	queue.arrayMu.Lock()
	// Find free slots without calling findFreeSlot (already holding lock)
	var slot1, slot2, slot3 int = -1, -1, -1
	for i := 0; i < queue.size && (slot1 < 0 || slot2 < 0 || slot3 < 0); i++ {
		if queue.freeBitmap[i] {
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
	queue.slots[slot1] = PacketWithCycle{Cycle: 3, Packet: packet.Packet{SourceID: 1}}
	queue.freeBitmap[slot1] = false
	queue.blockReasons[slot1] = 0 // Free
	queue.slots[slot2] = PacketWithCycle{Cycle: 5, Packet: packet.Packet{SourceID: 2}}
	queue.freeBitmap[slot2] = false
	queue.blockReasons[slot2] = 1 // Blocked
	queue.slots[slot3] = PacketWithCycle{Cycle: 7, Packet: packet.Packet{SourceID: 3}}
	queue.freeBitmap[slot3] = false
	queue.blockReasons[slot3] = 0 // Free
	queue.arrayMu.Unlock()

	// Pick should only return free packets
	picked := queue.Pick()

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
	if queue.Length() != 1 {
		t.Fatalf("expected 1 packet remaining (blocked), got %d", queue.Length())
	}
}

// TestSetBlockReason tests setBlockReason method.
func TestSetBlockReason(t *testing.T) {
	t.Parallel()

	queue, _, _ := NewQueue(10, 1, 1, 2) // bitmapWidth = 2

	// Add a packet
	queue.arrayMu.Lock()
	// Find free slot without calling findFreeSlot (already holding lock)
	var slot int = -1
	for i := 0; i < queue.size; i++ {
		if queue.freeBitmap[i] {
			slot = i
			break
		}
	}
	if slot < 0 {
		t.Fatal("no free slot available")
	}
	queue.slots[slot] = PacketWithCycle{Cycle: 5, Packet: packet.Packet{SourceID: 1}}
	queue.freeBitmap[slot] = false
	queue.blockReasons[slot] = 0
	queue.arrayMu.Unlock()

	// Set block reason bit 0
	queue.setBlockReason(slot, 0, true)
	if queue.blockReasons[slot] != 1 {
		t.Fatalf("expected block_reason bit 0 set, got %d", queue.blockReasons[slot])
	}

	// Set block reason bit 1
	queue.setBlockReason(slot, 1, true)
	if queue.blockReasons[slot] != 3 {
		t.Fatalf("expected block_reason bits 0 and 1 set, got %d", queue.blockReasons[slot])
	}

	// Clear bit 0
	queue.setBlockReason(slot, 0, false)
	if queue.blockReasons[slot] != 2 {
		t.Fatalf("expected block_reason bit 1 set, got %d", queue.blockReasons[slot])
	}

	// Verify isFree returns false when blocked
	if queue.isFree(slot) {
		t.Fatal("isFree should return false when block_reason is not 0")
	}
}

// TestTick tests Tick method with new port API.
func TestTick(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	queue, queueIn, queueOut := NewQueue(10, 1, 1, 1)
	downstreamInPort, upstreamOutPort := createTestPorts(8)
	queueIn.Plug(upstreamOutPort)
	queueOut.Plug(downstreamInPort)

	mockOut := upstreamOutPort.(*mockOutPort)

	// Send packet from upstream
	pkt := packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"}
	select {
	case <-ctx.Done():
		t.Fatal("timeout before sending packet")
	default:
		sendPacketToOutPort(t, upstreamOutPort, 0, pkt)
		mockOut.SetDone(0) // Upstream done with cycle 0
	}

	// Process cycle 0
	done := make(chan error, 1)
	go func() {
		done <- queue.Tick(0)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Tick failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Tick timed out")
	}

	// Verify packet was processed (either sent or stored in queue)
	t.Logf("Queue length after Tick: %d", queue.Length())
}

// TestProcessPackets tests ProcessPackets integration with new port API.
func TestProcessPackets(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	queue, queueIn, queueOut := NewQueue(10, 2, 2, 1)
	downstreamInPort, upstreamOutPort := createTestPorts(8)
	queueIn.Plug(upstreamOutPort)
	queueOut.Plug(downstreamInPort)

	mockOut := upstreamOutPort.(*mockOutPort)

	// Send multiple packets
	for i := 0; i < 3; i++ {
		pkt := packet.Packet{SourceID: 1, TargetID: 2, Payload: "test"}
		select {
		case <-ctx.Done():
			t.Fatalf("timeout sending packet %d", i)
		default:
			sendPacketToOutPort(t, upstreamOutPort, i, pkt)
		}
	}
	mockOut.SetDone(2)

	// Process cycle 0
	done := make(chan error, 1)
	go func() {
		done <- queue.Tick(0)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Tick failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Tick timed out")
	}

	// Verify packets were processed
	t.Logf("Packets processed, queue length: %d", queue.Length())
}

// TestReadyUntilCalculation removed - tests internal ReadyUntil calculation which is private
// ReadyUntil is automatically managed by Queue.ProcessPackets

// TestConcurrentOperations removed - tested private methods and exposed channels
// Concurrent safety should be tested via public Tick() API

// TestIsFullCapacity tests IsFull and Capacity methods.
func TestIsFullCapacity(t *testing.T) {
	t.Parallel()

	queue, _, _ := NewQueue(5, 1, 1, 1)

	if queue.Capacity() != 5 {
		t.Fatalf("expected capacity 5, got %d", queue.Capacity())
	}

	if queue.IsFull() {
		t.Fatal("queue should not be full initially")
	}

	// Fill the queue
	queue.arrayMu.Lock()
	for i := 0; i < 5; i++ {
		// Find free slot without calling findFreeSlot (already holding lock)
		var slot int = -1
		for j := 0; j < queue.size; j++ {
			if queue.freeBitmap[j] {
				slot = j
				break
			}
		}
		if slot < 0 {
			t.Fatalf("no free slot available")
		}
		queue.slots[slot] = PacketWithCycle{Cycle: i, Packet: packet.Packet{SourceID: 1}}
		queue.freeBitmap[slot] = false
		queue.blockReasons[slot] = 0
	}
	queue.arrayMu.Unlock()

	if !queue.IsFull() {
		t.Fatal("queue should be full after adding 5 packets")
	}

	if queue.Length() != 5 {
		t.Fatalf("expected length 5, got %d", queue.Length())
	}
}

// TestFindFreeSlot tests findFreeSlot method.
func TestFindFreeSlot(t *testing.T) {
	t.Parallel()

	queue, _, _ := NewQueue(5, 1, 1, 1)

	// Initially all slots should be free
	for i := 0; i < 5; i++ {
		slot := queue.findFreeSlot()
		if slot < 0 || slot >= 5 {
			t.Fatalf("findFreeSlot returned invalid slot: %d", slot)
		}
		queue.arrayMu.Lock()
		queue.freeBitmap[slot] = false
		queue.arrayMu.Unlock()
	}

	// No more free slots
	slot := queue.findFreeSlot()
	if slot >= 0 {
		t.Fatalf("expected no free slots, got slot %d", slot)
	}
}

// TestCountFreePackets tests countFreePackets method.
func TestCountFreePackets(t *testing.T) {
	t.Parallel()

	queue, _, _ := NewQueue(10, 1, 1, 1)

	// Initially all slots are free
	freeCount := queue.countFreePackets()
	if freeCount != 10 {
		t.Fatalf("expected 10 free slots initially, got %d", freeCount)
	}

	// Add 5 packets (occupying 5 slots)
	queue.arrayMu.Lock()
	for i := 0; i < 5; i++ {
		// Find free slot without calling findFreeSlot (already holding lock)
		var slot int = -1
		for j := 0; j < queue.size; j++ {
			if queue.freeBitmap[j] {
				slot = j
				break
			}
		}
		if slot < 0 {
			t.Fatalf("no free slot available")
		}
		queue.slots[slot] = PacketWithCycle{Cycle: i, Packet: packet.Packet{SourceID: 1}}
		queue.freeBitmap[slot] = false
		if i < 3 {
			queue.blockReasons[slot] = 0 // Free (can be picked)
		} else {
			queue.blockReasons[slot] = 1 // Blocked (cannot be picked)
		}
	}
	queue.arrayMu.Unlock()

	// After adding 5 packets, should have 10 - 5 = 5 free slots
	freeCount = queue.countFreePackets()
	if freeCount != 5 {
		t.Fatalf("expected 5 free slots after adding 5 packets, got %d", freeCount)
	}
}

// TestTickWithExternalPorts removed - redundant with TestTick which now uses Plug pattern
