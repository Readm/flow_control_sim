package queue

import (
	"fmt"
	"sort"
	"sync"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// InputQueue handles packet reception from upstream components.
// It provides storage and exposes received packets through Pick().
type InputQueue struct {
	// ===== Port reference (not owned) =====
	fromUpstream ahead_port.OutPort // Receive from upstream

	// ===== Array storage fields =====
	slots        []packet.PacketWithCycle // Array storage for packets
	freeBitmap   []bool                   // Bitmap marking free slots (true = free, false = occupied)
	blockReasons []uint                   // Block reason bitmap for each slot

	// ===== Configuration parameters =====
	capacity        int // Maximum number of packets that can be stored
	inBandwidth     int // Maximum packets per cycle that can be received
	bitmapWidth     int
	nextUpdateCycle int

	// ===== Synchronization for array operations =====
	arrayMu sync.Mutex

	// ===== Scratch buffers =====
	candidatesBuffer []int // Reused buffer for sorting in PeekTo

	// ===== Hooks and extra state =====
	lastCyclePackets []packet.Packet
	onPacketReceived func(packet.Packet)
}

// Freeable allows freeing a slot.
type Freeable interface {
	Free(slot int)
}

// PacketRef serves as a handle to a packet waiting in the queue.
// It allows non-destructive peeking and explicit freeing.
type PacketRef struct {
	Packet packet.Packet
	Slot   int
	Queue  Freeable
}

// ByteSize returns the estimated size of the PacketRef in bytes.
// This is useful for memory accounting.
func (pr PacketRef) ByteSize() int {
	return 0 // Struct overhead is small, packet payload handled elsewhere
}

// NewInputQueue creates a new InputQueue with the specified capacity and bandwidth parameters.
// Ports must be set separately using SetUpstreamPort, or via Connect().
func NewInputQueue(capacity int, inBandwidth int) *InputQueue {
	if capacity <= 0 {
		panic("capacity must be positive")
	}
	if inBandwidth <= 0 {
		panic("inBandwidth must be positive")
	}

	iq := &InputQueue{
		capacity:         capacity,
		inBandwidth:      inBandwidth,
		bitmapWidth:      1,
		nextUpdateCycle:  0,
		slots:            make([]packet.PacketWithCycle, capacity),
		freeBitmap:       make([]bool, capacity),
		blockReasons:     make([]uint, capacity),
		lastCyclePackets: make([]packet.Packet, 0),
		candidatesBuffer: make([]int, 0, capacity),
	}

	// Initialize all slots as free
	for i := range iq.freeBitmap {
		iq.freeBitmap[i] = true
	}

	return iq
}

// SetUpstreamPort sets the port for receiving data from upstream.
// InputQueue acts as downstream for this port.
func (iq *InputQueue) SetUpstreamPort(port ahead_port.OutPort) {
	iq.fromUpstream = port
	// Initialize ready state for cycle 0 and 1
	// We initialize cycle 1 as well to support links with latency=1 checking ahead
	if iq.fromUpstream != nil {
		hasCapacity := iq.Length() < iq.Capacity()
		iq.fromUpstream.UpdateReady(0, hasCapacity)
		iq.fromUpstream.UpdateReady(1, hasCapacity)
	}
}

// Tick processes a cycle by receiving packets from upstream and storing them internally.
// This is dramatically simpler than the old implementation because Port handles all synchronization.
func (iq *InputQueue) Tick(cycle int) error {
	if iq.fromUpstream == nil {
		return nil
	}

	// ===== 1. Receive packets from upstream =====
	// Receive() internally handle synchronization: it waits for the current cycle to be complete.
	packets := iq.fromUpstream.Receive(cycle)

	// ===== 3. Store packets in slots =====
	var received []packet.Packet

	for _, pkt := range packets {
		slot := iq.findFreeSlot()
		if slot >= 0 {
			iq.arrayMu.Lock()
			iq.slots[slot] = packet.PacketWithCycle{
				Cycle:  cycle,
				Packet: pkt,
			}
			iq.freeBitmap[slot] = false
			iq.blockReasons[slot] = 0
			iq.arrayMu.Unlock()

			received = append(received, pkt)
			if iq.onPacketReceived != nil {
				iq.onPacketReceived(pkt)
			}
		} else {
			return fmt.Errorf("InputQueue: capacity exceeded (%d/%d), cannot store packet Src=%d Dst=%d at cycle %d",
				iq.Length(), iq.capacity, pkt.SourceID, pkt.TargetID, cycle)
		}
	}

	iq.lastCyclePackets = received

	// ===== 4. Update ready state for upstream =====
	// Only signal Ready if we have enough space for a full bandwidth burst in the next cycle.
	// This prevents overflow if the upstream component obeys the Ready signal.
	remainReadyCycle := (iq.capacity - iq.Length()) / iq.inBandwidth
	switch remainReadyCycle {
	case 0:
		iq.fromUpstream.UpdateReady(cycle+1, false)
		iq.nextUpdateCycle = cycle + 2
	default:
		for i := iq.nextUpdateCycle; i <= cycle+remainReadyCycle; i++ {
			iq.fromUpstream.UpdateReady(i, true)
			iq.nextUpdateCycle = i + 1
		}
	}

	return nil
}

// Pick returns packets stored in the queue in FIFO order.
func (iq *InputQueue) Pick() []packet.Packet {
	type packetInfo struct {
		packet packet.PacketWithCycle
		index  int
	}

	var freePackets []packetInfo

	// Collect all occupied slots that are not blocked
	for i := 0; i < iq.capacity; i++ {
		if !iq.freeBitmap[i] && iq.isFree(i) {
			freePackets = append(freePackets, packetInfo{
				packet: iq.slots[i],
				index:  i,
			})
		}
	}

	// Sort by cycle (oldest first)
	sort.Slice(freePackets, func(i, j int) bool {
		return freePackets[i].packet.Cycle < freePackets[j].packet.Cycle
	})

	// Return all available packets
	result := make([]packet.Packet, len(freePackets))
	for i, info := range freePackets {
		result[i] = info.packet.Packet
		// Mark slot as free
		iq.freeBitmap[info.index] = true
		iq.blockReasons[info.index] = 0

		debug.Logf("InputQueue: Picked packet: Src=%d Dst=%d", info.packet.Packet.SourceID, info.packet.Packet.TargetID)
	}

	return result
}

// findFreeSlot finds the first free slot in the array.
func (iq *InputQueue) findFreeSlot() int {
	for i := 0; i < iq.capacity; i++ {
		if iq.freeBitmap[i] {
			return i
		}
	}
	return -1
}

// isFree checks if a slot is not blocked.
func (iq *InputQueue) isFree(index int) bool {
	if index < 0 || index >= iq.capacity {
		return false
	}
	return iq.blockReasons[index] == 0
}

// Length returns the number of packets currently stored in the queue.
func (iq *InputQueue) Length() int {
	count := 0
	for i := 0; i < iq.capacity; i++ {
		if !iq.freeBitmap[i] {
			count++
		}
	}
	return count
}

// Capacity returns the queue capacity.
func (iq *InputQueue) Capacity() int {
	return iq.capacity
}

// IsFull reports whether the queue is at capacity.
func (iq *InputQueue) IsFull() bool {
	return iq.findFreeSlot() == -1
}

// GetReceivedPackets returns packets received during the last Tick call.
func (iq *InputQueue) GetReceivedPackets() []packet.Packet {
	result := make([]packet.Packet, len(iq.lastCyclePackets))
	copy(result, iq.lastCyclePackets)
	return result
}

// SetPacketReceivedHook configures a hook to be called when a packet is received.
func (iq *InputQueue) SetPacketReceivedHook(hook func(packet.Packet)) {
	iq.onPacketReceived = hook
}

// GetVisualState returns the visual representation.
func (iq *InputQueue) GetVisualState() string {
	if visualization.VisualizationMode == "none" {
		return ""
	}
	if visualization.VisualizationMode == "ascii" {
		return fmt.Sprintf("[%d/%d]", iq.Length(), iq.Capacity())
	}
	return ""
}

// PeekTo populates the provided slice with references to available packets.
// It does NOT mark them as free. Callers must explicitly call Free() on the ref.
// Returns the number of packets peeked.
func (iq *InputQueue) PeekTo(out []PacketRef) int {
	// Optimization: This function should be zero-allocation

	// Fast path: if empty, return 0
	if iq.Length() == 0 {
		return 0
	}

	count := 0
	maxOut := len(out)

	// Use scratch buffer
	iq.candidatesBuffer = iq.candidatesBuffer[:0]

	// Collect all occupied slots that are not blocked
	iq.arrayMu.Lock()
	for i := 0; i < iq.capacity; i++ {
		if !iq.freeBitmap[i] && iq.blockReasons[i] == 0 {
			iq.candidatesBuffer = append(iq.candidatesBuffer, i)
		}
	}

	// Sort by cycle (FIFO)
	// Using sort.Slice on slice member
	sort.Slice(iq.candidatesBuffer, func(i, j int) bool {
		idxI := iq.candidatesBuffer[i]
		idxJ := iq.candidatesBuffer[j]
		return iq.slots[idxI].Cycle < iq.slots[idxJ].Cycle
	})

	// Populate out buffer
	for _, index := range iq.candidatesBuffer {
		if count >= maxOut {
			break
		}
		out[count] = PacketRef{
			Packet: iq.slots[index].Packet,
			Slot:   index,
			Queue:  iq,
		}
		count++
	}
	iq.arrayMu.Unlock()

	return count
}

// Free releases a slot, making it available for new packets.
func (iq *InputQueue) Free(slot int) {
	iq.arrayMu.Lock()
	defer iq.arrayMu.Unlock()

	if slot >= 0 && slot < iq.capacity {
		if !iq.freeBitmap[slot] {
			iq.freeBitmap[slot] = true
			iq.blockReasons[slot] = 0
			// We don't clear the slot data itself, it will be overwritten
		}
	}
}

// Block marks a slot as blocked for a specific reason.
// The slot will not be returned by PeekTo/Pick until unblocked (or freed).
func (iq *InputQueue) Block(slot int, reason uint) {
	iq.arrayMu.Lock()
	defer iq.arrayMu.Unlock()

	if slot >= 0 && slot < iq.capacity {
		iq.blockReasons[slot] |= reason
	}
}
