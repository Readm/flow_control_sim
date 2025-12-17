package queue

import (
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// InputQueue handles packet reception from upstream components.
// It integrates the storage and flow control logic previously found in Queue.
// It provides InPort interface for upstream connections and exposes received packets through Pick.
type InputQueue struct {
	// Port references
	inPort *inputQueueInPort
	// queueOutPort is kept for compatibility if needed, but InputQueue doesn't send downstream.
	// In the original design, InputQueue had a Queue which had an OutPort.
	// checking codebase, we might not need to expose an OutPort if it's unused.
	// But to match the previous struct field types:
	queueOutPort ahead_port.OutPort

	// Synchronization state
	componentSync *ahead_port.ComponentSync

	// Array storage fields
	slots        []packet.PacketWithCycle // Array storage for packets
	freeBitmap   []bool                   // Bitmap marking free slots (true = free, false = occupied)
	blockReasons []uint                   // Block reason bitmap for each slot

	// Configuration parameters
	capacity    int // Maximum number of packets that can be stored
	inBandwidth int // Maximum packets per cycle that can be received
	bitmapWidth int

	// Synchronization for array operations
	arrayMu sync.Mutex

	// Hooks and extra state
	lastCyclePackets []packet.Packet
	onPacketReceived func(packet.Packet)
	readyOnce        sync.Once
}

// inputQueueInPort implements InPort for InputQueue.
type inputQueueInPort struct {
	ahead_port.BaseInPort
	inputQueue *InputQueue
}

// TrySendPacket attempts to send a packet to InputQueue for the given cycle.
func (p *inputQueueInPort) TrySendPacket(cycle int, pkt ahead_port.PacketWithCycle) bool {
	if p.InputChan == nil {
		panic("inputQueueInPort.TrySendPacket() called before Plug()")
	}
	if !p.inputQueue.ready(cycle) {
		return false
	}
	p.InputChan <- pkt
	return true
}

// ready is an internal helper.
func (p *inputQueueInPort) ready(cycle int) bool {
	return p.inputQueue.ready(cycle)
}

// IsReadyNonBlocking checks if downstream is ready without blocking.
func (p *inputQueueInPort) IsReadyNonBlocking(cycle int) (ready bool, decided bool) {
	return p.inputQueue.IsReadyNonBlocking(cycle)
}

// Plug connects this InPort to an upstream OutPort.
func (p *inputQueueInPort) Plug(out ahead_port.OutPort) chan ahead_port.PacketWithCycle {
	return p.BaseInPort.PlugWithSelf(p, out)
}

// NewInputQueue creates a new InputQueue with the specified capacity and bandwidth parameters.
// 改为指针 *bool，仅允许传入nil或一个bool指针，实现“单一可选参数”语义
// Global variable to control allowAheadReady for all InputQueue instances.
// It must be set at process-init. Default is true.
var AllowAheadReadyGlobal = true

func NewInputQueue(capacity int, inBandwidth int) *InputQueue {
	if capacity <= 0 {
		panic("capacity must be positive")
	}
	if inBandwidth <= 0 {
		panic("inBandwidth must be positive")
	}

	iq := &InputQueue{
		componentSync:    ahead_port.NewComponentSync(),
		capacity:         capacity,
		inBandwidth:      inBandwidth,
		bitmapWidth:      1,
		slots:            make([]packet.PacketWithCycle, capacity),
		freeBitmap:       make([]bool, capacity),
		blockReasons:     make([]uint, capacity),
		lastCyclePackets: make([]packet.Packet, 0),
	}

	// Initialize all slots as free
	for i := range iq.freeBitmap {
		iq.freeBitmap[i] = true
	}

	// Create ports
	iq.inPort = &inputQueueInPort{inputQueue: iq}
	// For queueOutPort, we can leave it nil or create a dummy if strictly required by interface users.

	// Initialize ready state for initial cycles
	limit := capacity / inBandwidth
	if limit < 1 || !AllowAheadReadyGlobal {
		limit = 1
	}
	iq.componentSync.InitReady(limit)

	return iq
}

// QueueInPort returns the InPort for Network connections.
func (iq *InputQueue) QueueInPort() ahead_port.InPort {
	return iq.inPort
}

// Tick processes a cycle by receiving packets from upstream and storing them internally.
func (iq *InputQueue) Tick(cycle int) error {
	// Wait for upstream Done >= cycle-1
	// This is still needed if UpstreamOut doesn't implement beforeGetHook,
	// but ReceiveFromUpstream might handle some of it if configured.
	// For safety, and to match previous behavior, we keep explicit wait if we can,
	// but ReceiveFromUpstream abstracts UpstreamOut.
	// Actually, best practice is to let ReceiveFromUpstream/GetPackets handle synchronization if possible,
	// but currently OutputQueue doesn't set beforeGetHook.
	// So we keep the manual wait logic for now, or move it?
	// The original code did manual wait. Let's keep it to ensure correctness.
	if iq.inPort.UpstreamOut != nil {
		type waitDoneProvider interface{ WaitDone(int) }
		if wdp, ok := iq.inPort.UpstreamOut.(waitDoneProvider); ok {
			wdp.WaitDone(cycle - 1)
		}
	}

	// Prepare updateUpstreamReady function
	updateUpstreamReady := func(c int, ready bool) {
		iq.updateReady(c, ready)
	}

	// Receive packets from upstream using internal API
	packets := iq.inPort.ReceiveFromUpstream(cycle)

	// Process packets
	iq.processPackets(packets, cycle, nil, updateUpstreamReady)

	// Ensure Done state is correct
	currentDone := iq.componentSync.GetDone()
	if currentDone < cycle {
		iq.componentSync.SetDone(cycle)
	}

	return nil
}

// processPackets processes packets for InputQueue.
func (iq *InputQueue) processPackets(
	packets []packet.Packet,
	cycle int,
	setDone func(int), // Optional override, usually nil
	updateUpstreamReady func(cycle int, ready bool),
) {
	var received []packet.Packet

	for _, pkt := range packets {
		slot := iq.findFreeSlot()
		if slot >= 0 {
			iq.arrayMu.Lock()
			iq.slots[slot] = packet.PacketWithCycle{
				Cycle: cycle, // Ingress packet assumes current cycle or we preserve its cycle?
				// GetPackets returns []packet.Packet, loosing Cycle info (implied 'cycle').
				// Wait, ReceiveFromUpstream returns []packet.Packet.
				// PacketWithCycle requires a cycle.
				// InputQueue usually stores them with current cycle or reception cycle.
				// Original code: iq.slots[slot] = packet.PacketWithCycle(pkt) where pkt was PacketWithCycle from channel.
				// The channel provided PacketWithCycle.
				// GetPackets returns just Packet.
				// So we should construct PacketWithCycle using current cycle?
				// Or should we trust the cycle is 'cycle'?
				// Yes, GetPackets(cycle) returns packets FOR that cycle.
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
			// Queue full - this shouldn't happen if Ready protocol is obeyed,
			// but if it does, we must drop or panic.
			// Dropping is safer for sim.
			debug.Logf("InputQueue: DROPPED packet (queue full): Src=%d Dst=%d at cycle %d",
				pkt.SourceID, pkt.TargetID, cycle)
		}
	}

	iq.lastCyclePackets = received

	if setDone != nil {
		setDone(cycle)
	}

	hasCapacity := iq.Length() < iq.Capacity()
	updateUpstreamReady(cycle+1, hasCapacity)
}

// Pick returns packets stored in the queue in FIFO order.
func (iq *InputQueue) Pick() []packet.Packet {
	// Reuse logic from Queue.Pick but adapted
	type packetInfo struct {
		packet packet.PacketWithCycle
		index  int
	}

	var freePackets []packetInfo

	// Collect all occupied slots (Queue logic was confusing naming "free" vs "occupied")
	// Queue.Pick picks "free for sending" which means "occupied by packet and not blocked".
	// Queue.freeBitmap: true = empty slot, false = occupied.
	for i := 0; i < iq.capacity; i++ {
		if !iq.freeBitmap[i] && iq.isFree(i) { // isFree checks blockReasons==0
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

	// Return all available packets (InputQueue typically drains everything available)
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
	// A queue is full if no free slots are available
	return iq.findFreeSlot() == -1
}

// GetReceivedPackets returns packets received during the last Tick call.
func (iq *InputQueue) GetReceivedPackets() []packet.Packet {
	result := make([]packet.Packet, len(iq.lastCyclePackets))
	copy(result, iq.lastCyclePackets)
	return result
}

// SetPacketReceivedHook configures a hook.
func (iq *InputQueue) SetPacketReceivedHook(hook func(packet.Packet)) {
	iq.onPacketReceived = hook
}

// EnableAlwaysReady configures the queue to stay ready for all future cycles.
func (iq *InputQueue) EnableAlwaysReady() {
	iq.readyOnce.Do(func() {
		iq.componentSync.SetReadyUntil(math.MaxInt64)
	})
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

// ===== Internal synchronization methods =====

// ready checks if InputQueue is ready to receive data for the given cycle.
func (iq *InputQueue) ready(cycle int) bool {
	return iq.componentSync.Ready(cycle)
}

// IsReadyNonBlocking checks ready state without blocking.
func (iq *InputQueue) IsReadyNonBlocking(cycle int) (ready bool, decided bool) {
	return iq.componentSync.IsReadyNonBlocking(cycle)
}

// updateReady updates InputQueue's ready state.
func (iq *InputQueue) updateReady(cycle int, ready bool) {
	iq.componentSync.UpdateReady(cycle, ready)
}

// waitDone waits for upstream to complete the target cycle.
func (iq *InputQueue) waitDone(targetCycle int) {
	if iq.inPort.UpstreamOut == nil {
		return
	}
	type waitDoneProvider interface {
		WaitDone(int)
		GetDone() int
	}
	if wdp, ok := iq.inPort.UpstreamOut.(waitDoneProvider); ok {
		if wdp.GetDone() >= targetCycle {
			return
		}
		wdp.WaitDone(targetCycle)
	}
}
