package pipeline

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

// QueuePort implements AheadPort interface with array-based storage.
// It provides bidirectional synchronization between upstream and downstream components.
type QueuePort struct {
	// AheadPort interface fields
	done       int64
	readyUntil int64
	readyMap   map[int]bool
	packetChan chan ahead_port.PacketWithCycle

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
}

// QueueCycleProcessor is a custom cycle processor for QueuePort.
type QueueCycleProcessor struct {
	upstreamPort   ahead_port.AheadPort
	downstreamPort ahead_port.AheadPort
	processor      ahead_port.PacketProcessor
	queuePort      *QueuePort
}

// QueuePacketProcessor implements PacketProcessor for QueuePort.
type QueuePacketProcessor struct {
	queuePort *QueuePort
}

// NewQueuePort creates a new QueuePort with the specified parameters.
// - size: number of slots in the array
// - inBandwidth: maximum packets per cycle from upstream
// - outBandwidth: maximum packets per cycle to downstream
// - bitmapWidth: width of block_reason bitmap (defaults to 1 if <= 0)
func NewQueuePort(size, inBandwidth, outBandwidth int, bitmapWidth int) *QueuePort {
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

	qp := &QueuePort{
		done:         -1,
		readyUntil:   -1,
		readyMap:     make(map[int]bool),
		packetChan:   make(chan ahead_port.PacketWithCycle, 8), // Small buffer for channel
		slots:        make([]PacketWithCycle, size),
		freeBitmap:   make([]bool, size),
		blockReasons: make([]uint, size),
		size:         size,
		inBandwidth:  inBandwidth,
		outBandwidth: outBandwidth,
		bitmapWidth:  bitmapWidth,
	}

	// Initialize all slots as free
	for i := range qp.freeBitmap {
		qp.freeBitmap[i] = true
	}

	// Create packet processor
	qp.packetProc = &QueuePacketProcessor{
		queuePort: qp,
	}

	// Create cycle processor (ports will be set via SetUpstreamPort/SetDownstreamPort)
	qp.processor = &QueueCycleProcessor{
		queuePort: qp,
		processor: qp.packetProc,
	}

	return qp
}

// SetDone updates Done using atomic store.
func (qp *QueuePort) SetDone(cycle int) {
	atomic.StoreInt64(&qp.done, int64(cycle))

	qp.doneMu.Lock()
	if qp.doneCond != nil {
		qp.doneCond.Broadcast()
	}
	qp.doneMu.Unlock()
}

// GetDone returns the current Done value.
func (qp *QueuePort) GetDone() int {
	return int(atomic.LoadInt64(&qp.done))
}

// SendChan returns a write-only channel for upstream to push packets.
func (qp *QueuePort) SendChan() chan<- ahead_port.PacketWithCycle {
	return qp.packetChan
}

// ReceiveChan returns a read-only channel for downstream to receive packets.
func (qp *QueuePort) ReceiveChan() <-chan ahead_port.PacketWithCycle {
	return qp.packetChan
}

// Ready checks if downstream is ready to process the given cycle.
func (qp *QueuePort) Ready(cycle int) bool {
	readyUntil := atomic.LoadInt64(&qp.readyUntil)
	if int64(cycle) < readyUntil {
		return true
	}

	qp.waiterMu.Lock()
	ready, exists := qp.readyMap[cycle]
	qp.waiterMu.Unlock()

	if exists {
		return ready
	}

	return qp.waitForReady(cycle)
}

// ReadyNonBlocking checks if downstream is ready without blocking.
func (qp *QueuePort) ReadyNonBlocking(cycle int) (ready bool, configured bool) {
	readyUntil := atomic.LoadInt64(&qp.readyUntil)
	if int64(cycle) < readyUntil {
		return true, true
	}

	qp.waiterMu.Lock()
	ready, exists := qp.readyMap[cycle]
	qp.waiterMu.Unlock()

	if exists {
		return ready, true
	}

	return false, false
}

// waitForReady blocks until the given cycle becomes ready.
func (qp *QueuePort) waitForReady(cycle int) bool {
	qp.waiterMu.Lock()
	defer qp.waiterMu.Unlock()

	if qp.cond == nil {
		qp.cond = sync.NewCond(&qp.waiterMu)
	}

	for {
		if ready, exists := qp.readyMap[cycle]; exists {
			return ready
		}
		qp.cond.Wait()
	}
}

// UpdateReady updates the ready status for a specific cycle.
func (qp *QueuePort) UpdateReady(cycle int, ready bool) {
	qp.waiterMu.Lock()
	defer qp.waiterMu.Unlock()

	qp.readyMap[cycle] = ready

	if ready {
		currentReadyUntil := atomic.LoadInt64(&qp.readyUntil)
		if int64(cycle) >= currentReadyUntil {
			atomic.StoreInt64(&qp.readyUntil, int64(cycle)+1)
		}
	}

	if qp.cond != nil {
		qp.cond.Broadcast()
	}
}

// WaitForDone blocks until upstream's Done >= targetCycle.
func (qp *QueuePort) WaitForDone(targetCycle int) {
	if qp.GetDone() >= targetCycle {
		return
	}

	qp.doneMu.Lock()
	defer qp.doneMu.Unlock()

	if qp.doneCond == nil {
		qp.doneCond = sync.NewCond(&qp.doneMu)
	}

	for qp.GetDone() < targetCycle {
		qp.doneCond.Wait()
	}
}

// GetReadyUntil returns the current readyUntil value.
func (qp *QueuePort) GetReadyUntil() int {
	return int(atomic.LoadInt64(&qp.readyUntil))
}

// ProcessCycle processes a single cycle.
func (qp *QueuePort) ProcessCycle(cycle int) error {
	return qp.processor.ProcessCycle(cycle)
}

// ProcessCycle implements the cycle processing workflow for QueuePort.
func (qcp *QueueCycleProcessor) ProcessCycle(cycle int) error {
	if qcp.processor == nil {
		panic("QueueCycleProcessor.processor is nil")
	}

	// Use QueuePort itself as ports if external ports are not set
	upstreamPort := qcp.upstreamPort
	downstreamPort := qcp.downstreamPort
	if upstreamPort == nil {
		upstreamPort = qcp.queuePort
	}
	if downstreamPort == nil {
		downstreamPort = qcp.queuePort
	}

	// Wait for upstream Done >= cycle-1
	upstreamPort.WaitForDone(cycle - 1)

	// Prepare updateUpstreamReady function
	var updateUpstreamReady func(cycle int, ready bool)
	if upstreamPort != nil {
		if updater, ok := upstreamPort.(interface{ UpdateReady(int, bool) }); ok && updater != nil {
			updateUpstreamReady = updater.UpdateReady
		} else {
			updateUpstreamReady = func(cycle int, ready bool) {}
		}
	} else {
		updateUpstreamReady = func(cycle int, ready bool) {}
	}

	// Process all packets
	qcp.processor.ProcessPackets(
		upstreamPort.ReceiveChan(),
		cycle,
		downstreamPort.Ready,
		qcp.sendPacket,
		downstreamPort.SetDone,
		updateUpstreamReady,
	)

	// SetDone after processing all packets
	currentDone := downstreamPort.GetDone()
	if currentDone < cycle {
		downstreamPort.SetDone(cycle)
	}

	// Assert that cycle+1 has been configured
	if upstreamPort != nil {
		if checker, ok := upstreamPort.(interface{ ReadyNonBlocking(int) (bool, bool) }); ok && checker != nil {
			_, configured := checker.ReadyNonBlocking(cycle + 1)
			if !configured {
				panic(fmt.Sprintf("ProcessCycle(cycle=%d) completed but cycle+1=%d is not configured in upstream port. Processor must call updateUpstreamReady(cycle+1, ready) in ProcessPackets.", cycle, cycle+1))
			}
		}
	}

	return nil
}

// sendPacket sends a packet to downstream.
func (qcp *QueueCycleProcessor) sendPacket(pkt ahead_port.PacketWithCycle) {
	downstreamPort := qcp.downstreamPort
	if downstreamPort == nil {
		downstreamPort = qcp.queuePort
	}
	downstreamPort.SendChan() <- pkt
}

// ProcessPackets implements PacketProcessor interface for QueuePort.
func (qpp *QueuePacketProcessor) ProcessPackets(
	receiveChan <-chan ahead_port.PacketWithCycle,
	cycle int,
	checkReady func(int) bool,
	sendPacket func(ahead_port.PacketWithCycle),
	setDone func(int),
	updateUpstreamReady func(cycle int, ready bool),
) {
	qp := qpp.queuePort

	// Receive packets from channel and store in array (non-blocking, drain all)
	for {
		select {
		case pkt := <-receiveChan:
			slot := qp.findFreeSlot()
			if slot >= 0 {
				qp.arrayMu.Lock()
				qp.slots[slot] = PacketWithCycle(pkt)
				qp.freeBitmap[slot] = false
				qp.blockReasons[slot] = 0 // Initialize block_reason to 0
				qp.arrayMu.Unlock()
			}
		default:
			goto process
		}
	}

process:
	// Pick packets from array and send to downstream
	pickedPackets := qp.Pick()
	for _, pkt := range pickedPackets {
		if checkReady(pkt.Cycle) {
			sendPacket(ahead_port.PacketWithCycle(pkt))
		} else {
			// Not ready, put back to array
			slot := qp.findFreeSlot()
			if slot >= 0 {
				qp.arrayMu.Lock()
				qp.slots[slot] = pkt
				qp.freeBitmap[slot] = false
				qp.blockReasons[slot] = 0
				qp.arrayMu.Unlock()
			}
		}
	}

	// SetDone
	setDone(cycle)

	// Update upstream ready status based on free packet count
	freeCount := qp.countFreePackets()
	ready := freeCount > 0

	// Calculate ReadyUntil: currentCycle + N / inBandwidth
	// This tells upstream how many cycles ahead QueuePort can receive packets
	if ready {
		readyUntilCycle := cycle + freeCount/qp.inBandwidth
		// Update QueuePort's own readyUntil for fast path
		qp.updateReadyUntil(readyUntilCycle)
	}

	// Notify upstream via UpdateReady (updates readyMap and readyUntil)
	qp.UpdateReady(cycle+1, ready)

	// Also notify upstream via the provided function (for external upstream ports)
	updateUpstreamReady(cycle+1, ready)
}

// Pick returns at most outBandwidth packets that are free (block_reason is 0).
// Returns packets sorted by cycle (oldest first).
func (qp *QueuePort) Pick() []PacketWithCycle {
	qp.arrayMu.Lock()
	defer qp.arrayMu.Unlock()

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
func (qp *QueuePort) findFreeSlot() int {
	qp.arrayMu.Lock()
	defer qp.arrayMu.Unlock()

	for i := 0; i < qp.size; i++ {
		if qp.freeBitmap[i] {
			return i
		}
	}
	return -1
}

// countFreePackets counts the number of packets that are free (block_reason is 0).
func (qp *QueuePort) countFreePackets() int {
	qp.arrayMu.Lock()
	defer qp.arrayMu.Unlock()

	count := 0
	for i := 0; i < qp.size; i++ {
		if !qp.freeBitmap[i] && qp.isFree(i) {
			count++
		}
	}
	return count
}

// updateReadyUntil updates readyUntil based on free packet count and current cycle.
func (qp *QueuePort) updateReadyUntil(cycle int) {
	qp.waiterMu.Lock()
	defer qp.waiterMu.Unlock()

	currentReadyUntil := atomic.LoadInt64(&qp.readyUntil)
	if int64(cycle) >= currentReadyUntil {
		atomic.StoreInt64(&qp.readyUntil, int64(cycle))
	}
}

// setBlockReason sets or clears a bit in the block_reason bitmap for a slot.
func (qp *QueuePort) setBlockReason(index int, bit int, value bool) {
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
func (qp *QueuePort) isFree(index int) bool {
	if index < 0 || index >= qp.size {
		return false
	}
	return qp.blockReasons[index] == 0
}

// SetCurrentCycle sets the current processing cycle.
// This is used by ProcessCycle to update the cycle context.
func (qp *QueuePort) SetCurrentCycle(cycle int) {
	// This method can be used to track current cycle if needed
	// Currently, cycle is passed as parameter to ProcessCycle
}

// SetUpstreamPort sets the upstream port for QueueCycleProcessor.
func (qp *QueuePort) SetUpstreamPort(upstreamPort ahead_port.AheadPort) {
	qp.processor.upstreamPort = upstreamPort
}

// SetDownstreamPort sets the downstream port for QueueCycleProcessor.
func (qp *QueuePort) SetDownstreamPort(downstreamPort ahead_port.AheadPort) {
	qp.processor.downstreamPort = downstreamPort
}

// Length returns the current number of packets in the queue.
func (qp *QueuePort) Length() int {
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
func (qp *QueuePort) Capacity() int {
	return qp.size
}

// IsFull checks if the queue is at capacity.
func (qp *QueuePort) IsFull() bool {
	return qp.Length() == qp.Capacity()
}
