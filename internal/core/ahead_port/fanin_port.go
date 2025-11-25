package ahead_port

// FaninPort implements AheadPort for synchronization only.
// It is used when multiple upstream ports share the same underlying channel.
// This eliminates the need for data forwarding goroutines and associated race conditions.
type FaninPort struct {
	upstreamPorts []AheadPort
	sharedChan    chan PacketWithCycle
}

// NewFaninPort creates a new FaninPort.
// upstreamPorts: list of upstream ports that share the same underlying channel
// sharedChan: the shared channel that all upstream ports write to
func NewFaninPort(upstreamPorts []AheadPort, sharedChan chan PacketWithCycle) *FaninPort {
	if len(upstreamPorts) == 0 {
		panic("upstreamPorts must not be empty")
	}
	return &FaninPort{
		upstreamPorts: upstreamPorts,
		sharedChan:    sharedChan,
	}
}

func (m *FaninPort) SetDone(cycle int) {
	panic("FaninPort.SetDone should not be called")
}

func (m *FaninPort) GetDone() int {
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

func (m *FaninPort) SendChan() chan<- PacketWithCycle {
	panic("FaninPort.Chan should not be called")
}

func (m *FaninPort) ReceiveChan() <-chan PacketWithCycle {
	return m.sharedChan
}

func (m *FaninPort) Ready(cycle int) bool {
	panic("FaninPort.Ready should not be called")
}

func (m *FaninPort) ReadyNonBlocking(cycle int) (ready bool, configured bool) {
	// Check all
	if len(m.upstreamPorts) == 0 {
		return false, false
	}
	allReady := true
	allConfigured := true
	for _, port := range m.upstreamPorts {
		if impl, ok := port.(*SinglePort); ok {
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

func (m *FaninPort) WaitForDone(targetCycle int) {
	for _, port := range m.upstreamPorts {
		port.WaitForDone(targetCycle)
	}
}

func (m *FaninPort) UpdateReady(cycle int, ready bool) {
	for _, port := range m.upstreamPorts {
		if impl, ok := port.(*SinglePort); ok {
			impl.UpdateReady(cycle, ready)
		}
	}
}

// NewSharedPortGroup creates a group of upstream ports and one downstream aggregator
// that share a single underlying channel.
// Returns:
// - upstreams: Slice of ports to be used by upstream components (senders).
// - aggregator: Single port to be used by the downstream component (receiver).
func NewSharedPortGroup(count int, bufferSize int) ([]AheadPort, AheadPort) {
	if count <= 0 {
		panic("count must be positive")
	}
	if bufferSize <= 0 {
		bufferSize = 8
	}

	sharedChan := make(chan PacketWithCycle, bufferSize)
	upstreams := make([]AheadPort, count)

	for i := 0; i < count; i++ {
		p := NewAheadPort(bufferSize)
		p.SetChannel(sharedChan)
		upstreams[i] = p
	}

	aggregator := NewFaninPort(upstreams, sharedChan)
	return upstreams, aggregator
}
