package cycle_port

import (
	"sync"
)

// MultiUpstreamPort implements CyclePort interface to aggregate multiple upstream ports.
// It allows a downstream component to wait for multiple upstream components and receive
// packets from all of them.
//
// Usage:
//   - Create multiple upstream ports (CyclePortImpl instances)
//   - Create a MultiUpstreamPort with these upstream ports
//   - Use MultiUpstreamPort as the upstreamPort in CycleProcessor
//
// Example:
//
//	upstreamPort1 := NewCyclePort(8)
//	upstreamPort2 := NewCyclePort(8)
//	multiUpstream := NewMultiUpstreamPort([]CyclePort{upstreamPort1, upstreamPort2})
//	processor := NewCycleProcessor(multiUpstream, downstreamPort, nil)
//	defer multiUpstream.Close() // Clean up when done
type MultiUpstreamPort struct {
	upstreamPorts []CyclePort // List of upstream ports to aggregate

	// mergedChan merges packets from all upstream ports
	mergedChan chan PacketWithCycle
	// stopChan signals the merge goroutine to stop
	stopChan chan struct{}
	// wg waits for the merge goroutine to finish
	wg sync.WaitGroup
}

// NewMultiUpstreamPort creates a new MultiUpstreamPort that aggregates multiple upstream ports.
func NewMultiUpstreamPort(upstreamPorts []CyclePort) *MultiUpstreamPort {
	if len(upstreamPorts) == 0 {
		panic("MultiUpstreamPort requires at least one upstream port")
	}

	multi := &MultiUpstreamPort{
		upstreamPorts: upstreamPorts,
		mergedChan:    make(chan PacketWithCycle, 8),
		stopChan:      make(chan struct{}),
	}

	// Start a goroutine for each upstream port to forward packets
	for _, port := range multi.upstreamPorts {
		multi.wg.Add(1)
		go multi.forwardPackets(port)
	}

	return multi
}

// forwardPackets forwards packets from a single upstream port to mergedChan.
func (m *MultiUpstreamPort) forwardPackets(port CyclePort) {
	defer m.wg.Done()
	for {
		select {
		case pkt, ok := <-port.ReceiveChan():
			if !ok {
				// Channel closed, stop forwarding
				return
			}
			select {
			case m.mergedChan <- pkt:
			case <-m.stopChan:
				return
			}
		case <-m.stopChan:
			return
		}
	}
}

// Close stops the merge goroutine and closes the merged channel.
// This should be called when the MultiUpstreamPort is no longer needed.
func (m *MultiUpstreamPort) Close() {
	close(m.stopChan)
	m.wg.Wait()
	close(m.mergedChan)
}

// ===== Upstream Operations =====
// These methods should NOT be called on MultiUpstreamPort.
// MultiUpstreamPort is only used as upstreamPort in CycleProcessor (downstream perspective).

// SetDoneUntil panics because MultiUpstreamPort should not be used as upstream.
func (m *MultiUpstreamPort) SetDoneUntil(cycle int) {
	panic("MultiUpstreamPort.SetDoneUntil should not be called. MultiUpstreamPort is only used as upstreamPort in CycleProcessor.")
}

// Chan panics because MultiUpstreamPort should not be used as upstream.
func (m *MultiUpstreamPort) Chan() chan<- PacketWithCycle {
	panic("MultiUpstreamPort.Chan should not be called. MultiUpstreamPort is only used as upstreamPort in CycleProcessor.")
}

// Ready panics because MultiUpstreamPort should not be used as upstream.
func (m *MultiUpstreamPort) Ready(cycle int) bool {
	panic("MultiUpstreamPort.Ready should not be called. MultiUpstreamPort is only used as upstreamPort in CycleProcessor.")
}

// ReadyNonBlocking checks if all upstream ports have the cycle configured.
// Returns (ready, configured):
//   - ready: true if all upstream ports are ready for the cycle
//   - configured: true if all upstream ports have the cycle configured
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
			// If port is not *CyclePortImpl, we can't check its status
			// Assume it's not configured
			allConfigured = false
			allReady = false
		}
	}
	return allReady, allConfigured
}

// GetDoneUntil returns the minimum DoneUntil value from all upstream ports.
// This represents the cycle until which ALL upstream components have completed.
func (m *MultiUpstreamPort) GetDoneUntil() int {
	if len(m.upstreamPorts) == 0 {
		return -1
	}
	minDoneUntil := m.upstreamPorts[0].GetDoneUntil()
	for i := 1; i < len(m.upstreamPorts); i++ {
		doneUntil := m.upstreamPorts[i].GetDoneUntil()
		if doneUntil < minDoneUntil {
			minDoneUntil = doneUntil
		}
	}
	return minDoneUntil
}

// ===== Downstream Operations =====

// ReceiveChan returns the merged channel that receives packets from all upstream ports.
func (m *MultiUpstreamPort) ReceiveChan() <-chan PacketWithCycle {
	return m.mergedChan
}

// WaitForDoneUntil blocks until ALL upstream ports have DoneUntil >= targetCycle.
// This ensures that all upstream components have completed cycle targetCycle-1.
func (m *MultiUpstreamPort) WaitForDoneUntil(targetCycle int) {
	// Wait for all upstream ports to reach targetCycle
	for _, port := range m.upstreamPorts {
		port.WaitForDoneUntil(targetCycle)
	}
}

// UpdateReady updates the ready status for all upstream ports.
// This is an internal method, similar to CyclePortImpl.UpdateReady.
// It should be called by CycleProcessor to notify all upstream ports about readiness.
func (m *MultiUpstreamPort) UpdateReady(cycle int, ready bool) {
	for _, port := range m.upstreamPorts {
		if impl, ok := port.(*CyclePortImpl); ok {
			impl.UpdateReady(cycle, ready)
		}
		// If port is not *CyclePortImpl (e.g., a mock), skip it
	}
}
