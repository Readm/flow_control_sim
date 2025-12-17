package queue

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// readyItem represents a ready state for a specific cycle
type readyItem struct {
	cycle int
	ready bool
}

// PacketWithCycle is an alias for packet.PacketWithCycle
type PacketWithCycle = packet.PacketWithCycle

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
	done            int64
	readyUntil      int64
	readyQueue      []readyItem // Sorted queue of future ready states
	lastAccessCycle int         // For debug: tracking monotonic access

	waiterMu sync.Mutex
	cond     *sync.Cond

	doneMu   sync.Mutex
	doneCond *sync.Cond

	// Array storage fields
	slots        []PacketWithCycle // Array storage for packets
	freeBitmap   []bool            // Bitmap marking free slots (true = free, false = occupied)
	blockReasons []uint            // Block reason bitmap for each slot

	// Configuration parameters
	capacity    int // formerly size
	bandwidth   int // formerly inBandwidth
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

// sendChan is an internal helper.
func (p *inputQueueInPort) sendChan() chan<- ahead_port.PacketWithCycle {
	if p.InputChan == nil {
		panic("inputQueueInPort.sendChan() called before Plug()")
	}
	return p.InputChan
}

// IsReadyNonBlocking checks if downstream is ready without blocking.
func (p *inputQueueInPort) IsReadyNonBlocking(cycle int) (ready bool, decided bool) {
	return p.inputQueue.IsReadyNonBlocking(cycle)
}

// Plug connects this InPort to an upstream OutPort.
func (p *inputQueueInPort) Plug(out ahead_port.OutPort) chan ahead_port.PacketWithCycle {
	return p.BaseInPort.PlugWithSelf(p, out)
}

// NewInputQueue creates a new InputQueue with the specified buffer size and bandwidth parameters.
func NewInputQueue(bufferSize int, inBandwidth int, outBandwidth int) *InputQueue {
	if bufferSize <= 0 {
		bufferSize = 8
	}
	if inBandwidth <= 0 {
		panic("inBandwidth must be positive")
	}
	// outBandwidth is ignored as InputQueue doesn't send downstream automatically

	iq := &InputQueue{
		done:             -1,
		readyUntil:       -1,
		readyQueue:       make([]readyItem, 0),
		lastAccessCycle:  -1,
		capacity:         bufferSize,
		bandwidth:        inBandwidth,
		bitmapWidth:      1,
		slots:            make([]PacketWithCycle, bufferSize),
		freeBitmap:       make([]bool, bufferSize),
		blockReasons:     make([]uint, bufferSize),
		lastCyclePackets: make([]packet.Packet, 0),
	}

	// Initialize all slots as free
	for i := range iq.freeBitmap {
		iq.freeBitmap[i] = true
	}

	// Create ports
	iq.inPort = &inputQueueInPort{inputQueue: iq}
	// For queueOutPort, we can leave it nil or create a dummy if strictly required by interface users.
	// Previous InputQueue returned queue.outPort.
	// If we truly make InputQueue independent, we might drop queueOutPort if it's not used.
	// Let's assume for now we don't need a functional OutPort for InputQueue since it consumes packets.

	// Initialize ready state for initial cycles
	iq.primeReady(bufferSize)

	return iq
}

// QueueInPort returns the InPort for Network connections.
func (iq *InputQueue) QueueInPort() ahead_port.InPort {
	return iq.inPort
}

// QueueOutPort returns the OutPort. Currently nil as InputQueue is a sink in this context.
func (iq *InputQueue) QueueOutPort() ahead_port.OutPort {
	// Previously this returned internal queue's outPort.
	// Since InputQueue logic never sends to it, it might be unused?
	// If existing code calls this, we might need a stub.
	return iq.queueOutPort
}

// AsInPort is a convenience method that returns QueueInPort.
func (iq *InputQueue) AsInPort() ahead_port.InPort {
	return iq.inPort
}

// Tick processes a cycle by receiving packets from upstream and storing them internally.
func (iq *InputQueue) Tick(cycle int) error {
	// Wait for upstream Done >= cycle-1
	if iq.inPort.UpstreamOut != nil {
		// Use type assertion to access internal WaitDone method
		type waitDoneProvider interface{ WaitDone(int) }
		if wdp, ok := iq.inPort.UpstreamOut.(waitDoneProvider); ok {
			wdp.WaitDone(cycle - 1)
		}
	}

	// Prepare updateUpstreamReady function
	updateUpstreamReady := func(c int, ready bool) {
		iq.updateReady(c, ready)
	}

	// Get receive channel
	var receiveChan <-chan ahead_port.PacketWithCycle
	if iq.inPort.InputChan != nil {
		receiveChan = iq.inPort.InputChan
	}
	if receiveChan == nil {
		receiveChan = make(chan ahead_port.PacketWithCycle)
	}

	// Process packets
	iq.processPackets(receiveChan, cycle, nil, updateUpstreamReady)

	// Ensure Done state is correct
	currentDone := iq.getDone()
	if currentDone < cycle {
		iq.setDone(cycle)
	}

	return nil
}

// processPackets processes packets for InputQueue.
func (iq *InputQueue) processPackets(
	receiveChan <-chan ahead_port.PacketWithCycle,
	cycle int,
	setDone func(int), // Optional override, usually nil
	updateUpstreamReady func(cycle int, ready bool),
) {
	var received []packet.Packet

	for {
		select {
		case pkt := <-receiveChan:
			slot := iq.findFreeSlot()
			if slot >= 0 {
				iq.arrayMu.Lock()
				iq.slots[slot] = packet.PacketWithCycle(pkt)
				iq.freeBitmap[slot] = false
				iq.blockReasons[slot] = 0
				iq.arrayMu.Unlock()
				received = append(received, pkt.Packet)
				if iq.onPacketReceived != nil {
					iq.onPacketReceived(pkt.Packet)
				}
			}
		default:
			goto done
		}
	}

done:
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
		packet PacketWithCycle
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
		atomic.StoreInt64(&iq.readyUntil, math.MaxInt64)
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

// ===== Internal synchronization methods (Migrated from Queue) =====

func (iq *InputQueue) setDone(cycle int) {
	atomic.StoreInt64(&iq.done, int64(cycle))
	iq.doneMu.Lock()
	if iq.doneCond != nil {
		iq.doneCond.Broadcast()
	}
	iq.doneMu.Unlock()
}

func (iq *InputQueue) getDone() int {
	return int(atomic.LoadInt64(&iq.done))
}

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

func (iq *InputQueue) ready(cycle int) bool {
	if debug.Enabled() {
		if cycle < iq.lastAccessCycle {
			panic(fmt.Sprintf("Ready access violation: cycle %d < last %d (must be monotonic)", cycle, iq.lastAccessCycle))
		}
		iq.lastAccessCycle = cycle
	}

	readyUntil := atomic.LoadInt64(&iq.readyUntil)
	if int64(cycle) < readyUntil {
		return true
	}

	iq.waiterMu.Lock()
	currentReadyUntil := atomic.LoadInt64(&iq.readyUntil)
	if int64(cycle) < currentReadyUntil {
		iq.waiterMu.Unlock()
		return true
	}

	// Prune logic
	pruneIdx := 0
	found := false
	var result bool

	for i, item := range iq.readyQueue {
		if item.cycle < cycle {
			continue
		}
		if item.cycle == cycle {
			result = item.ready
			found = true
			pruneIdx = i + 1
			break
		}
		pruneIdx = i
		break
	}

	// Handle full prune if everything < cycle
	if !found && len(iq.readyQueue) > 0 && iq.readyQueue[len(iq.readyQueue)-1].cycle < cycle {
		pruneIdx = len(iq.readyQueue)
	}

	if pruneIdx > 0 {
		if pruneIdx >= len(iq.readyQueue) {
			iq.readyQueue = nil
		} else {
			iq.readyQueue = iq.readyQueue[pruneIdx:]
		}
	}

	iq.waiterMu.Unlock()

	if found {
		return result
	}

	return iq.waitForReady(cycle)
}

func (iq *InputQueue) IsReadyNonBlocking(cycle int) (ready bool, decided bool) {
	if debug.Enabled() {
		if cycle < iq.lastAccessCycle {
			panic(fmt.Sprintf("Ready access violation (NB): cycle %d < last %d", cycle, iq.lastAccessCycle))
		}
		iq.lastAccessCycle = cycle
	}

	readyUntil := atomic.LoadInt64(&iq.readyUntil)
	if int64(cycle) < readyUntil {
		return true, true
	}

	iq.waiterMu.Lock()
	defer iq.waiterMu.Unlock()

	currentReadyUntil := atomic.LoadInt64(&iq.readyUntil)
	if int64(cycle) < currentReadyUntil {
		return true, true
	}

	pruneIdx := 0
	found := false
	var result bool

	for i, item := range iq.readyQueue {
		if item.cycle < cycle {
			continue
		}
		if item.cycle == cycle {
			result = item.ready
			found = true
			pruneIdx = i
			break
		}
		pruneIdx = i
		break
	}

	if !found && len(iq.readyQueue) > 0 && iq.readyQueue[len(iq.readyQueue)-1].cycle < cycle {
		pruneIdx = len(iq.readyQueue)
	}

	if pruneIdx > 0 {
		if pruneIdx >= len(iq.readyQueue) {
			iq.readyQueue = nil
		} else {
			iq.readyQueue = iq.readyQueue[pruneIdx:]
		}
	}

	if found {
		return result, true
	}

	return false, false
}

func (iq *InputQueue) waitForReady(cycle int) bool {
	iq.waiterMu.Lock()
	defer iq.waiterMu.Unlock()

	if iq.cond == nil {
		iq.cond = sync.NewCond(&iq.waiterMu)
	}

	for {
		currentReadyUntil := atomic.LoadInt64(&iq.readyUntil)
		if int64(cycle) < currentReadyUntil {
			return true
		}

		found := false
		var result bool

		for i, item := range iq.readyQueue {
			if item.cycle == cycle {
				result = item.ready
				found = true
				if i+1 >= len(iq.readyQueue) {
					iq.readyQueue = nil
				} else {
					iq.readyQueue = iq.readyQueue[i+1:]
				}
				break
			}
			if item.cycle > cycle {
				break
			}
		}

		if found {
			return result
		}

		iq.cond.Wait()
	}
}

func (iq *InputQueue) updateReady(cycle int, ready bool) {
	iq.waiterMu.Lock()
	defer iq.waiterMu.Unlock()

	currentReadyUntil := atomic.LoadInt64(&iq.readyUntil)
	if int64(cycle) < currentReadyUntil {
		return
	}

	inserted := false
	if len(iq.readyQueue) == 0 {
		iq.readyQueue = append(iq.readyQueue, readyItem{cycle, ready})
		inserted = true
	} else {
		if cycle > iq.readyQueue[len(iq.readyQueue)-1].cycle {
			iq.readyQueue = append(iq.readyQueue, readyItem{cycle, ready})
			inserted = true
		} else {
			for i, item := range iq.readyQueue {
				if item.cycle == cycle {
					iq.readyQueue[i].ready = ready
					inserted = true
					break
				}
				if item.cycle > cycle {
					iq.readyQueue = append(iq.readyQueue[:i+1], iq.readyQueue[i:]...)
					iq.readyQueue[i] = readyItem{cycle, ready}
					inserted = true
					break
				}
			}
			if !inserted {
				iq.readyQueue = append(iq.readyQueue, readyItem{cycle, ready})
			}
		}
	}

	for len(iq.readyQueue) > 0 {
		head := iq.readyQueue[0]
		if int64(head.cycle) == currentReadyUntil {
			if head.ready {
				currentReadyUntil++
				iq.readyQueue = iq.readyQueue[1:]
			} else {
				break
			}
		} else if int64(head.cycle) < currentReadyUntil {
			iq.readyQueue = iq.readyQueue[1:]
		} else {
			break
		}
	}

	atomic.StoreInt64(&iq.readyUntil, currentReadyUntil)

	if iq.cond != nil {
		iq.cond.Broadcast()
	}
}

func (iq *InputQueue) primeReady(limit int) {
	for cycle := 0; cycle <= limit; cycle++ {
		iq.updateReady(cycle, true)
	}
}
