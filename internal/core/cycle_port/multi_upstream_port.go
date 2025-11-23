package cycle_port

// MultiUpstreamPort implements CyclePort interface to aggregate multiple upstream ports.
// DEPRECATED: Use NewSharedPortGroup instead for better performance and simpler architecture.
// This implementation is kept for backward compatibility but should not be used in new code.
type MultiUpstreamPort struct {
	upstreamPorts []CyclePort // List of upstream ports to aggregate
}

// NewMultiUpstreamPort creates a new MultiUpstreamPort that aggregates multiple upstream ports.
// DEPRECATED: Use NewSharedPortGroup instead.
// This implementation simply delegates to SyncAggregator for synchronization.
// Note: This does NOT perform data forwarding - upstream ports must share a channel
// or be handled differently. For proper multi-upstream support, use NewSharedPortGroup.
func NewMultiUpstreamPort(upstreamPorts []CyclePort) *MultiUpstreamPort {
	if len(upstreamPorts) == 0 {
		panic("MultiUpstreamPort requires at least one upstream port")
	}

	// For backward compatibility, we create a simple wrapper
	// that delegates synchronization to the upstream ports directly.
	// This assumes upstream ports already share a channel or are handled separately.
	return &MultiUpstreamPort{
		upstreamPorts: upstreamPorts,
	}
}

// Close is a no-op for backward compatibility.
func (m *MultiUpstreamPort) Close() {
	// No goroutines to stop, no channel to close
}

// ===== Upstream Operations =====
// These methods should NOT be called on MultiUpstreamPort.

func (m *MultiUpstreamPort) SetDone(cycle int) {
	panic("MultiUpstreamPort.SetDone should not be called.")
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

// GetDone returns the minimum Done value from all upstream ports.
func (m *MultiUpstreamPort) GetDone() int {
	if len(m.upstreamPorts) == 0 {
		return -1
	}
	min := m.upstreamPorts[0].GetDone()
	for i := 1; i < len(m.upstreamPorts); i++ {
		val := m.upstreamPorts[i].GetDone()
		if val < min {
			min = val
		}
	}
	return min
}

// ===== Downstream Operations =====

// ReceiveChan returns the first upstream port's channel.
// WARNING: This is a simplified implementation that only works if all upstream ports
// share the same channel (e.g., created via NewSharedPortGroup).
// For proper multi-upstream support, use NewSharedPortGroup instead.
func (m *MultiUpstreamPort) ReceiveChan() <-chan PacketWithCycle {
	if len(m.upstreamPorts) == 0 {
		return nil
	}
	// Return first port's channel - assumes all ports share the same channel
	return m.upstreamPorts[0].ReceiveChan()
}

// WaitForDone blocks until all ports have Done >= targetCycle.
func (m *MultiUpstreamPort) WaitForDone(targetCycle int) {
	for _, port := range m.upstreamPorts {
		port.WaitForDone(targetCycle)
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

func (s *SyncAggregator) SetDone(cycle int) {
	panic("SyncAggregator.SetDone should not be called")
}

func (s *SyncAggregator) GetDone() int {
	// Return min done
	if len(s.upstreamPorts) == 0 {
		return -1
	}
	min := s.upstreamPorts[0].GetDone()
	for i := 1; i < len(s.upstreamPorts); i++ {
		val := s.upstreamPorts[i].GetDone()
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

func (s *SyncAggregator) WaitForDone(targetCycle int) {
	for _, port := range s.upstreamPorts {
		port.WaitForDone(targetCycle)
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
