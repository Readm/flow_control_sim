package cycle_port

import (
	"sync"
	"sync/atomic"
)

// MultiUpstreamPort implements CyclePort interface to aggregate multiple upstream ports.
// It allows a downstream component to wait for multiple upstream components and receive
// packets from all of them.
type MultiUpstreamPort struct {
	upstreamPorts []CyclePort // List of upstream ports to aggregate

	// mergedChan merges packets from all upstream ports
	mergedChan chan PacketWithCycle
	// stopChan signals the goroutines to stop
	stopChan chan struct{}
	// wg waits for the goroutines to finish
	wg sync.WaitGroup

	// Synchronization state
	mu            sync.Mutex
	cond          *sync.Cond
	safeDoneUntil []int64 // Index corresponds to upstreamPorts. Protected by mu.

	// Internal communication
	upstreamDone []int64         // Atomic access
	notifyChans  []chan struct{} // Watcher notifies Forwarder
}

// NewMultiUpstreamPort creates a new MultiUpstreamPort that aggregates multiple upstream ports.
func NewMultiUpstreamPort(upstreamPorts []CyclePort) *MultiUpstreamPort {
	if len(upstreamPorts) == 0 {
		panic("MultiUpstreamPort requires at least one upstream port")
	}

	count := len(upstreamPorts)
	multi := &MultiUpstreamPort{
		upstreamPorts: upstreamPorts,
		mergedChan:    make(chan PacketWithCycle, 8*count), // Larger buffer for aggregation
		stopChan:      make(chan struct{}),
		safeDoneUntil: make([]int64, count),
		upstreamDone:  make([]int64, count),
		notifyChans:   make([]chan struct{}, count),
	}
	multi.cond = sync.NewCond(&multi.mu)

	// Initialize states
	for i := range multi.safeDoneUntil {
		multi.safeDoneUntil[i] = -1
		multi.upstreamDone[i] = -1
		multi.notifyChans[i] = make(chan struct{}, 1)
	}

	// Start goroutines for each upstream port
	for i, port := range multi.upstreamPorts {
		multi.wg.Add(2) // Watcher + Forwarder
		go multi.watcher(i, port)
		go multi.forwarder(i, port)
	}

	return multi
}

// watcher monitors the upstream port's DoneUntil and notifies the forwarder.
func (m *MultiUpstreamPort) watcher(index int, port CyclePort) {
	defer m.wg.Done()

	// We start checking from cycle 0.
	// If DoneUntil is already advanced, WaitForDoneUntil will return immediately.
	currentTarget := 0

	for {
		select {
		case <-m.stopChan:
			return
		default:
		}

		// Block until upstream completes currentTarget
		port.WaitForDoneUntil(currentTarget)

		// Update atomic state
		atomic.StoreInt64(&m.upstreamDone[index], int64(currentTarget))

		// Notify forwarder
		select {
		case m.notifyChans[index] <- struct{}{}:
		default:
			// Notification already pending
		}

		// Move to next cycle
		currentTarget++

		// Optimization: if upstream is far ahead, we might want to jump?
		// But WaitForDoneUntil is efficient if already satisfied.
		// However, simple incrementing works correctly.
		// To avoid busy looping if upstream is very far ahead, we rely on the fact that
		// port.WaitForDoneUntil is usually fast.
		// If we want to skip, we could check GetDoneUntil occasionally.
		if currentTarget%10 == 0 {
			actual := port.GetDoneUntil()
			if actual > currentTarget {
				currentTarget = actual
			}
		}
	}
}

// forwarder drains the upstream channel and updates safeDoneUntil.
func (m *MultiUpstreamPort) forwarder(index int, port CyclePort) {
	defer m.wg.Done()

	notifyChan := m.notifyChans[index]
	inputChan := port.ReceiveChan()

	for {
		// Priority 1: Drain data
		select {
		case pkt, ok := <-inputChan:
			if !ok {
				return
			}
			m.mergedChan <- pkt
			// We have processed a packet for this cycle.
			// This implies cycle-1 is definitely done.
			// And we are making progress on cycle.
			// We can conservatively update safeDoneUntil to cycle.
			// Wait, safeDoneUntil N means "Cycle N-1 is complete".
			// If we see packet for Cycle C, it means Cycle C is NOT complete (packets are flowing).
			// But Cycle C-1 IS complete.
			// So we can update to C.
			m.updateSafeDone(index, int64(pkt.Cycle))
			continue
		case <-m.stopChan:
			return
		default:
			// Channel empty, proceed to wait
		}

		// Priority 2: Wait for data or notification
		select {
		case pkt, ok := <-inputChan:
			if !ok {
				return
			}
			m.mergedChan <- pkt
			m.updateSafeDone(index, int64(pkt.Cycle))

		case <-notifyChan:
			// Upstream reported progress.
			// Drain any remaining packets in channel to ensure consistency.
			for {
				select {
				case pkt, ok := <-inputChan:
					if !ok {
						return
					}
					m.mergedChan <- pkt
					m.updateSafeDone(index, int64(pkt.Cycle))
				default:
					// Channel empty. Now safe to sync with upstreamDone.
					ud := atomic.LoadInt64(&m.upstreamDone[index])
					m.updateSafeDone(index, ud)
					goto mainLoop
				}
			}

		case <-m.stopChan:
			return
		}
	mainLoop:
	}
}

// updateSafeDone updates safeDoneUntil and broadcasts if changed.
// It only increases the value.
func (m *MultiUpstreamPort) updateSafeDone(index int, val int64) {
	m.mu.Lock()
	if val > m.safeDoneUntil[index] {
		m.safeDoneUntil[index] = val
		m.cond.Broadcast()
	}
	m.mu.Unlock()
}

// Close stops the goroutines and closes the merged channel.
func (m *MultiUpstreamPort) Close() {
	close(m.stopChan)
	m.wg.Wait()
	// Drain mergedChan to unblock any pending writes
	select {
	case <-m.mergedChan:
	default:
	}
	close(m.mergedChan)
}

// ===== Upstream Operations =====
// These methods should NOT be called on MultiUpstreamPort.

func (m *MultiUpstreamPort) SetDoneUntil(cycle int) {
	panic("MultiUpstreamPort.SetDoneUntil should not be called.")
}

func (m *MultiUpstreamPort) Chan() chan<- PacketWithCycle {
	panic("MultiUpstreamPort.Chan should not be called.")
}

func (m *MultiUpstreamPort) Ready(cycle int) bool {
	panic("MultiUpstreamPort.Ready should not be called.")
}

func (m *MultiUpstreamPort) ReadyNonBlocking(cycle int) (ready bool, configured bool) {
	if len(m.upstreamPorts) == 0 {
		return false, false
	}
	allReady := true
	allConfigured := true
	for _, port := range m.upstreamPorts {
		if impl, ok := port.(*CyclePortImpl); ok {
			pReady, pConfigured := impl.ReadyNonBlocking(cycle)
			if !pConfigured {
				allConfigured = false
			}
			if !pReady {
				allReady = false
			}
		} else {
			allConfigured = false
			allReady = false
		}
	}
	return allReady, allConfigured
}

// GetDoneUntil returns the minimum safe DoneUntil value from all upstream ports.
func (m *MultiUpstreamPort) GetDoneUntil() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.safeDoneUntil) == 0 {
		return -1
	}
	minDone := m.safeDoneUntil[0]
	for i := 1; i < len(m.safeDoneUntil); i++ {
		if m.safeDoneUntil[i] < minDone {
			minDone = m.safeDoneUntil[i]
		}
	}
	return int(minDone)
}

// ===== Downstream Operations =====

// ReceiveChan returns the merged channel.
func (m *MultiUpstreamPort) ReceiveChan() <-chan PacketWithCycle {
	return m.mergedChan
}

// WaitForDoneUntil blocks until all ports have safeDoneUntil >= targetCycle.
func (m *MultiUpstreamPort) WaitForDoneUntil(targetCycle int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for {
		// Check condition
		allSatisfied := true
		for _, done := range m.safeDoneUntil {
			if done < int64(targetCycle) {
				allSatisfied = false
				break
			}
		}

		if allSatisfied {
			return
		}

		m.cond.Wait()
	}
}

// UpdateReady updates the ready status for all upstream ports.
func (m *MultiUpstreamPort) UpdateReady(cycle int, ready bool) {
	for _, port := range m.upstreamPorts {
		if impl, ok := port.(*CyclePortImpl); ok {
			impl.UpdateReady(cycle, ready)
		}
	}
}

// SyncAggregator implements CyclePort for synchronization only.
// It is used when multiple upstream ports share the same underlying channel.
type SyncAggregator struct {
	upstreamPorts []CyclePort
	sharedChan    chan PacketWithCycle
}

// NewSyncAggregator creates a new SyncAggregator.
func NewSyncAggregator(upstreamPorts []CyclePort, sharedChan chan PacketWithCycle) *SyncAggregator {
	return &SyncAggregator{
		upstreamPorts: upstreamPorts,
		sharedChan:    sharedChan,
	}
}

func (s *SyncAggregator) SetDoneUntil(cycle int) {
	panic("SyncAggregator.SetDoneUntil should not be called")
}

func (s *SyncAggregator) GetDoneUntil() int {
	// Return min doneUntil
	if len(s.upstreamPorts) == 0 {
		return -1
	}
	min := s.upstreamPorts[0].GetDoneUntil()
	for i := 1; i < len(s.upstreamPorts); i++ {
		val := s.upstreamPorts[i].GetDoneUntil()
		if val < min {
			min = val
		}
	}
	return min
}

func (s *SyncAggregator) Chan() chan<- PacketWithCycle {
	panic("SyncAggregator.Chan should not be called")
}

func (s *SyncAggregator) ReceiveChan() <-chan PacketWithCycle {
	return s.sharedChan
}

func (s *SyncAggregator) Ready(cycle int) bool {
	panic("SyncAggregator.Ready should not be called")
}

func (s *SyncAggregator) ReadyNonBlocking(cycle int) (ready bool, configured bool) {
	// Check all
	if len(s.upstreamPorts) == 0 {
		return false, false
	}
	allReady := true
	allConfigured := true
	for _, port := range s.upstreamPorts {
		if impl, ok := port.(*CyclePortImpl); ok {
			pReady, pConfigured := impl.ReadyNonBlocking(cycle)
			if !pConfigured {
				allConfigured = false
			}
			if !pReady {
				allReady = false
			}
		} else {
			allConfigured = false
			allReady = false
		}
	}
	return allReady, allConfigured
}

func (s *SyncAggregator) WaitForDoneUntil(targetCycle int) {
	for _, port := range s.upstreamPorts {
		port.WaitForDoneUntil(targetCycle)
	}
}

func (s *SyncAggregator) UpdateReady(cycle int, ready bool) {
	for _, port := range s.upstreamPorts {
		if impl, ok := port.(*CyclePortImpl); ok {
			impl.UpdateReady(cycle, ready)
		}
	}
}

// NewSharedPortGroup creates a group of upstream ports and one downstream aggregator
// that share a single underlying channel.
// Returns:
// - upstreams: Slice of ports to be used by upstream components (senders).
// - aggregator: Single port to be used by the downstream component (receiver).
func NewSharedPortGroup(count int, bufferSize int) ([]CyclePort, CyclePort) {
	if count <= 0 {
		panic("count must be positive")
	}
	if bufferSize <= 0 {
		bufferSize = 8
	}

	sharedChan := make(chan PacketWithCycle, bufferSize)
	upstreams := make([]CyclePort, count)

	for i := 0; i < count; i++ {
		p := NewCyclePort(bufferSize)
		p.SetChannel(sharedChan)
		upstreams[i] = p
	}

	aggregator := NewSyncAggregator(upstreams, sharedChan)
	return upstreams, aggregator
}
