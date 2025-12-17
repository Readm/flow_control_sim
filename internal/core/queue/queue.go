package queue

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// PacketWithCycle is an alias for packet.PacketWithCycle
type PacketWithCycle = packet.PacketWithCycle

// Queue implements array-based packet storage with flow control.
// It provides bidirectional synchronization between upstream and downstream components.
type Queue struct {
	// Port references
	inPort  *queueInPort
	outPort *queueOutPort

	// Synchronization state (managed internally, not exposed via AheadPort)
	done       int64
	readyUntil int64
	readyMap   map[int]bool

	waiterMu sync.Mutex
	cond     *sync.Cond

	doneMu   sync.Mutex
	doneCond *sync.Cond

	// Array storage fields
	slots        []PacketWithCycle // Array storage for packets
	freeBitmap   []bool            // Bitmap marking free slots (true = free, false = occupied)
	blockReasons []uint            // Block reason bitmap for each slot

	// Configuration parameters
	size         int
	inBandwidth  int
	outBandwidth int
	bitmapWidth  int

	// Synchronization for array operations
	arrayMu sync.Mutex

	// Processor fields
	processor  *QueueCycleProcessor
	packetProc *QueuePacketProcessor

	ptMu        sync.RWMutex
	packetTypes []int
}

// queueInPort implements InPort for Queue's input side.
type queueInPort struct {
	ahead_port.BaseInPort
	queue *Queue
}

// TrySendPacket attempts to send a packet to Queue for the given cycle.
func (p *queueInPort) TrySendPacket(cycle int, pkt ahead_port.PacketWithCycle) bool {
	if p.InputChan == nil {
		panic("queueInPort.TrySendPacket() called before Plug()")
	}
	if !p.queue.ready(cycle) {
		return false
	}
	p.InputChan <- pkt
	return true
}

// ready is an internal helper (not part of InPort interface).
func (p *queueInPort) ready(cycle int) bool {
	return p.queue.ready(cycle)
}

// sendChan is an internal helper (not part of InPort interface).
func (p *queueInPort) sendChan() chan<- ahead_port.PacketWithCycle {
	if p.InputChan == nil {
		panic("queueInPort.sendChan() called before Plug()")
	}
	return p.InputChan
}

// IsReadyNonBlocking checks if downstream is ready without blocking.
func (p *queueInPort) IsReadyNonBlocking(cycle int) (ready bool, decided bool) {
	return p.queue.IsReadyNonBlocking(cycle)
}

// Plug connects this InPort to an upstream OutPort.
func (p *queueInPort) Plug(out ahead_port.OutPort) chan ahead_port.PacketWithCycle {
	return p.BaseInPort.PlugWithSelf(p, out)
}

// queueOutPort implements OutPort for Queue's output side.
type queueOutPort struct {
	ahead_port.BaseOutPort
	queue          *Queue
	pendingPackets map[int][]packet.Packet // Cached packets for future cycles
}

// GetPackets retrieves all packets for the specified cycle.
func (p *queueOutPort) GetPackets(cycle int) []packet.Packet {
	// 1. Wait for upstream to complete the previous cycle
	if p.queue.inPort.UpstreamOut != nil {
		waitCycle := cycle - 1 // Queue has no latency, wait for cycle-1
		// Use type assertion to access internal WaitDone method
		type waitDoneProvider interface{ WaitDone(int) }
		if wdp, ok := p.queue.inPort.UpstreamOut.(waitDoneProvider); ok {
			wdp.WaitDone(waitCycle)
		}
	}

	// 2. Check if we have cached packets for this cycle
	if p.pendingPackets == nil {
		p.pendingPackets = make(map[int][]packet.Packet)
	}

	if cached, ok := p.pendingPackets[cycle]; ok {
		delete(p.pendingPackets, cycle)
		return cached
	}

	// 3. Read from channel and filter by cycle
	var result []packet.Packet
	if p.OutputChan == nil {
		return result
	}

	for {
		select {
		case pwc := <-p.OutputChan:
			if pwc.Cycle == cycle {
				result = append(result, pwc.Packet)
			} else if pwc.Cycle > cycle {
				// Future packet, cache it
				p.pendingPackets[pwc.Cycle] = append(p.pendingPackets[pwc.Cycle], pwc.Packet)
			} else {
				// Past packet - skip it (already processed or stale)
				continue
			}
		default:
			return result
		}
	}
}

// WaitDone blocks until upstream has completed the target cycle.
func (p *queueOutPort) WaitDone(targetCycle int) {
	p.queue.waitDone(targetCycle)
}

// Plug connects this OutPort to a downstream InPort.
func (p *queueOutPort) Plug(in ahead_port.InPort) chan ahead_port.PacketWithCycle {
	return p.BaseOutPort.PlugWithSelf(p, in)
}

// QueueCycleProcessor is a custom cycle processor for Queue.
type QueueCycleProcessor struct {
	processor *QueuePacketProcessor
	queue     *Queue
}

// QueuePacketProcessor handles packet processing for Queue.
type QueuePacketProcessor struct {
	queue *Queue
}

// NewQueue creates a new Queue with the specified parameters.
// Returns (queue, inPort, outPort) where inPort connects to upstream and outPort connects to downstream.
// - size: number of slots in the array
// - inBandwidth: maximum packets per cycle from upstream
// - outBandwidth: maximum packets per cycle to downstream
// - bitmapWidth: width of block_reason bitmap (defaults to 1 if <= 0)
func NewQueue(size, inBandwidth, outBandwidth int, bitmapWidth int) (*Queue, ahead_port.InPort, ahead_port.OutPort) {
	if size <= 0 {
		size = 16 // Default size
	}
	if inBandwidth <= 0 {
		inBandwidth = 1
	}
	if outBandwidth <= 0 {
		outBandwidth = 1
	}
	if bitmapWidth <= 0 {
		bitmapWidth = 1 // Default bitmap width
	}

	queue := &Queue{
		done:         -1,
		readyUntil:   -1,
		readyMap:     make(map[int]bool),
		slots:        make([]PacketWithCycle, size),
		freeBitmap:   make([]bool, size),
		blockReasons: make([]uint, size),
		size:         size,
		inBandwidth:  inBandwidth,
		outBandwidth: outBandwidth,
		bitmapWidth:  bitmapWidth,
	}

	// Initialize all slots as free
	for i := range queue.freeBitmap {
		queue.freeBitmap[i] = true
	}

	// Create ports
	inPort := &queueInPort{queue: queue}
	outPort := &queueOutPort{queue: queue}
	queue.inPort = inPort
	queue.outPort = outPort

	// Create packet processor
	queue.packetProc = &QueuePacketProcessor{
		queue: queue,
	}

	// Create cycle processor
	queue.processor = &QueueCycleProcessor{
		queue:     queue,
		processor: queue.packetProc,
	}

	return queue, inPort, outPort
}

// ===== Internal synchronization methods =====
// These methods manage Queue's internal sync state and are called by port implementations.

// setDone updates downstream Done using atomic store.
func (q *Queue) setDone(cycle int) {
	atomic.StoreInt64(&q.done, int64(cycle))

	q.doneMu.Lock()
	if q.doneCond != nil {
		q.doneCond.Broadcast()
	}
	q.doneMu.Unlock()
}

// getDone returns the current downstream Done value.
func (q *Queue) getDone() int {
	return int(atomic.LoadInt64(&q.done))
}

// waitDone blocks until upstream Done >= targetCycle.
func (q *Queue) waitDone(targetCycle int) {
	if q.inPort.UpstreamOut == nil {
		return
	}
	// Use type assertion to access internal WaitDone method
	type waitDoneProvider interface {
		WaitDone(int)
		GetDone() int
	}
	if wdp, ok := q.inPort.UpstreamOut.(waitDoneProvider); ok {
		if wdp.GetDone() >= targetCycle {
			return
		}
		wdp.WaitDone(targetCycle)
	}
}

// ready checks if downstream is ready to process the given cycle.
func (q *Queue) ready(cycle int) bool {
	readyUntil := atomic.LoadInt64(&q.readyUntil)
	if int64(cycle) < readyUntil {
		return true
	}

	q.waiterMu.Lock()
	ready, exists := q.readyMap[cycle]
	q.waiterMu.Unlock()

	if exists {
		return ready
	}

	return q.waitForReady(cycle)
}

// IsReadyNonBlocking checks if downstream is ready without blocking.
func (q *Queue) IsReadyNonBlocking(cycle int) (ready bool, decided bool) {
	readyUntil := atomic.LoadInt64(&q.readyUntil)
	if int64(cycle) < readyUntil {
		return true, true
	}

	q.waiterMu.Lock()
	ready, exists := q.readyMap[cycle]
	q.waiterMu.Unlock()

	if exists {
		return ready, true
	}

	return false, false
}

// waitForReady blocks until the given cycle becomes ready.
func (q *Queue) waitForReady(cycle int) bool {
	q.waiterMu.Lock()
	defer q.waiterMu.Unlock()

	if q.cond == nil {
		q.cond = sync.NewCond(&q.waiterMu)
	}

	for {
		if ready, exists := q.readyMap[cycle]; exists {
			return ready
		}
		q.cond.Wait()
	}
}

// updateReady updates the ready status for upstream.
func (q *Queue) updateReady(cycle int, ready bool) {
	q.waiterMu.Lock()
	defer q.waiterMu.Unlock()

	q.readyMap[cycle] = ready

	if ready {
		currentReadyUntil := atomic.LoadInt64(&q.readyUntil)
		if int64(cycle) >= currentReadyUntil {
			atomic.StoreInt64(&q.readyUntil, int64(cycle)+1)
		}
	}

	if q.cond != nil {
		q.cond.Broadcast()
	}
}

// Tick processes a single cycle.
func (qp *Queue) Tick(cycle int) error {
	return qp.processor.Tick(cycle)
}

// Tick implements the cycle processing workflow for Queue.
func (qcp *QueueCycleProcessor) Tick(cycle int) error {
	if qcp.processor == nil {
		panic("QueueCycleProcessor.processor is nil")
	}

	queue := qcp.queue

	// Wait for upstream Done >= cycle-1 (Queue has implicit latency=1)
	queue.waitDone(cycle - 1)

	// Prepare updateUpstreamReady function
	updateUpstreamReady := func(c int, ready bool) {
		queue.updateReady(c, ready)
	}

	// Get packets from upstream
	var packets []packet.Packet
	if queue.inPort.UpstreamOut != nil {
		packets = queue.inPort.UpstreamOut.GetPackets(cycle)
	}

	// Prepare checkReady function
	checkReady := func(c int) bool {
		if queue.outPort.DownstreamIn == nil {
			return true
		}
		// Use type assertion to access internal ready method
		type readyChecker interface{ ready(int) bool }
		if rc, ok := queue.outPort.DownstreamIn.(readyChecker); ok {
			return rc.ready(c)
		}
		// Fallback
		ready, decided := queue.outPort.DownstreamIn.IsReadyNonBlocking(c)
		if decided {
			return ready
		}
		return false
	}

	// Prepare send function - directly sends to downstream at targetCycle
	sendPacket := func(targetCycle int, pkt packet.Packet) {
		if queue.outPort.DownstreamIn != nil {
			pwc := ahead_port.PacketWithCycle{
				Cycle:  targetCycle,
				Packet: pkt,
			}
			queue.outPort.DownstreamIn.TrySendPacket(targetCycle, pwc)
		}
	}

	// Process all packets
	qcp.processor.ProcessPackets(
		packets,
		cycle,
		checkReady,
		sendPacket,
		queue.setDone,
		updateUpstreamReady,
	)

	// SetDone after processing all packets
	currentDone := queue.getDone()
	if currentDone < cycle {
		queue.setDone(cycle)
	}

	// Assert that cycle+1 has been configured
	_, decided := queue.IsReadyNonBlocking(cycle + 1)
	if !decided {
		panic(fmt.Sprintf("Tick(cycle=%d) completed but cycle+1=%d is not configured. Processor must call updateUpstreamReady(cycle+1, ready) in ProcessPackets.", cycle, cycle+1))
	}

	return nil
}


// ProcessPackets implements PacketProcessor interface for Queue.
func (qpp *QueuePacketProcessor) ProcessPackets(
	packets []packet.Packet,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(targetCycle int, pkt packet.Packet),
	setDone func(int),
	updateUpstreamReady func(cycle int, ready bool),
) {
	qp := qpp.queue

	// Store incoming packets in array
	for _, pkt := range packets {
		slot := qp.findFreeSlot()
		if slot >= 0 {
			qp.slots[slot] = PacketWithCycle{
				Cycle:  cycle,
				Packet: pkt,
			}
			qp.freeBitmap[slot] = false
			qp.blockReasons[slot] = 0 // Initialize block_reason to 0
		}
	}

	// Pick packets from array and send to downstream (loop until no more packets or downstream not ready)
	for {
		pickedPackets := qp.Pick()
		if len(pickedPackets) == 0 {
			break
		}
		allSent := true
		for _, pkt := range pickedPackets {
			if checkReady(pkt.Cycle) {
				// Queue has no latency, send at current cycle
				sendPacket(cycle, pkt.Packet)
			} else {
				// Not ready, put back to array
				slot := qp.findFreeSlot()
				if slot >= 0 {
					qp.slots[slot] = pkt
					qp.freeBitmap[slot] = false
					qp.blockReasons[slot] = 0
				}
				allSent = false
			}
		}
		// If downstream is not ready, stop trying to send more packets
		if !allSent {
			break
		}
	}

	// SetDone
	setDone(cycle)

	// Update upstream ready status based on free slot count
	freeCount := qp.countFreePackets()
	ready := freeCount >= qp.inBandwidth

	// Calculate ReadyUntil: currentCycle + N / inBandwidth
	// This tells upstream how many cycles ahead Queue can receive packets
	if freeCount >= qp.inBandwidth {
		readyUntilCycle := cycle + 1 + freeCount/qp.inBandwidth
		// Update Queue's own readyUntil using atomic CAS (avoids lock)
		for {
			currentReadyUntil := atomic.LoadInt64(&qp.readyUntil)
			if int64(readyUntilCycle) <= currentReadyUntil {
				break
			}
			if atomic.CompareAndSwapInt64(&qp.readyUntil, currentReadyUntil, int64(readyUntilCycle)) {
				break
			}
		}
	}

	// Notify upstream for the next cycle (readyMap is used by blocking wait)
	updateUpstreamReady(cycle+1, ready)
}

// Pick returns at most outBandwidth packets that are free (block_reason is 0).
// Returns packets sorted by cycle (oldest first).
// Note: This function does NOT acquire locks. The caller must ensure proper synchronization.
func (qp *Queue) Pick() []PacketWithCycle {
	type packetInfo struct {
		packet PacketWithCycle
		index  int
	}

	var freePackets []packetInfo

	// Collect all free packets with their indices
	for i := 0; i < qp.size; i++ {
		if !qp.freeBitmap[i] && qp.isFree(i) {
			freePackets = append(freePackets, packetInfo{
				packet: qp.slots[i],
				index:  i,
			})
		}
	}

	// Sort by cycle (oldest first)
	sort.Slice(freePackets, func(i, j int) bool {
		return freePackets[i].packet.Cycle < freePackets[j].packet.Cycle
	})

	// Take at most outBandwidth packets
	count := len(freePackets)
	if count > qp.outBandwidth {
		count = qp.outBandwidth
	}

	result := make([]PacketWithCycle, count)
	for i := 0; i < count; i++ {
		result[i] = freePackets[i].packet
		// Mark slot as free
		qp.freeBitmap[freePackets[i].index] = true
		qp.blockReasons[freePackets[i].index] = 0
	}

	return result
}

// findFreeSlot finds the first free slot in the array.
// Returns -1 if no free slot is available.
// Note: This function does NOT acquire locks. The caller must ensure proper synchronization.
func (qp *Queue) findFreeSlot() int {
	for i := 0; i < qp.size; i++ {
		if qp.freeBitmap[i] {
			return i
		}
	}
	return -1
}

// countFreePackets counts the number of free slots available to receive new packets.
// Note: This function does NOT acquire locks. The caller must ensure proper synchronization.
func (qp *Queue) countFreePackets() int {
	count := 0
	for i := 0; i < qp.size; i++ {
		if qp.freeBitmap[i] {
			count++
		}
	}
	return count
}

// setBlockReason sets or clears a bit in the block_reason bitmap for a slot.
func (qp *Queue) setBlockReason(index int, bit int, value bool) {
	if index < 0 || index >= qp.size {
		return
	}
	if bit < 0 || bit >= qp.bitmapWidth {
		return
	}

	qp.arrayMu.Lock()
	defer qp.arrayMu.Unlock()

	if value {
		qp.blockReasons[index] |= (1 << bit)
	} else {
		qp.blockReasons[index] &^= (1 << bit)
	}
}

// isFree checks if a slot is free (block_reason is all 0s).
func (qp *Queue) isFree(index int) bool {
	if index < 0 || index >= qp.size {
		return false
	}
	return qp.blockReasons[index] == 0
}

// SetCurrentCycle sets the current processing cycle.
// This is used by Tick to update the cycle context.
func (qp *Queue) SetCurrentCycle(cycle int) {
	// This method can be used to track current cycle if needed
	// Currently, cycle is passed as parameter to Tick
}

// Length returns the current number of packets in the queue.
func (qp *Queue) Length() int {
	qp.arrayMu.Lock()
	defer qp.arrayMu.Unlock()

	count := 0
	for i := 0; i < qp.size; i++ {
		if !qp.freeBitmap[i] {
			count++
		}
	}
	return count
}

// Capacity returns the maximum capacity of the queue.
func (qp *Queue) Capacity() int {
	return qp.size
}

// IsFull checks if the queue is at capacity.
func (qp *Queue) IsFull() bool {
	return qp.Length() == qp.Capacity()
}

// SetPacketTypes configures accepted packet type identifiers for this port.
func (qp *Queue) SetPacketTypes(types []int) {
	qp.ptMu.Lock()
	defer qp.ptMu.Unlock()
	if len(types) == 0 {
		qp.packetTypes = nil
		return
	}
	qp.packetTypes = append([]int(nil), types...)
}

// PacketTypes returns the configured packet type identifiers.
func (qp *Queue) PacketTypes() []int {
	qp.ptMu.RLock()
	defer qp.ptMu.RUnlock()
	if len(qp.packetTypes) == 0 {
		return nil
	}
	return append([]int(nil), qp.packetTypes...)
}
