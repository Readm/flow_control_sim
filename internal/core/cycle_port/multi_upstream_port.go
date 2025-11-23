package cycle_port

// MultiUpstreamPort implements CyclePort for synchronization only.
// It is used when multiple upstream ports share the same underlying channel.
// This eliminates the need for data forwarding goroutines and associated race conditions.
type MultiUpstreamPort struct {
	upstreamPorts []CyclePort
	sharedChan    chan PacketWithCycle
}

// NewMultiUpstreamPort creates a new MultiUpstreamPort.
// upstreamPorts: list of upstream ports that share the same underlying channel
// sharedChan: the shared channel that all upstream ports write to
func NewMultiUpstreamPort(upstreamPorts []CyclePort, sharedChan chan PacketWithCycle) *MultiUpstreamPort {
	if len(upstreamPorts) == 0 {
		panic("upstreamPorts must not be empty")
	}
	return &MultiUpstreamPort{
		upstreamPorts: upstreamPorts,
		sharedChan:    sharedChan,
	}
}

func (m *MultiUpstreamPort) SetDone(cycle int) {
	panic("MultiUpstreamPort.SetDone should not be called")
}

func (m *MultiUpstreamPort) GetDone() int {
	// Return min done
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

func (m *MultiUpstreamPort) Chan() chan<- PacketWithCycle {
	panic("MultiUpstreamPort.Chan should not be called")
}

func (m *MultiUpstreamPort) ReceiveChan() <-chan PacketWithCycle {
	return m.sharedChan
}

func (m *MultiUpstreamPort) Ready(cycle int) bool {
	panic("MultiUpstreamPort.Ready should not be called")
}

func (m *MultiUpstreamPort) ReadyNonBlocking(cycle int) (ready bool, configured bool) {
	// Check all
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

func (m *MultiUpstreamPort) WaitForDone(targetCycle int) {
	for _, port := range m.upstreamPorts {
		port.WaitForDone(targetCycle)
	}
}

func (m *MultiUpstreamPort) UpdateReady(cycle int, ready bool) {
	for _, port := range m.upstreamPorts {
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

	aggregator := NewMultiUpstreamPort(upstreams, sharedChan)
	return upstreams, aggregator
}
