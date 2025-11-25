package ahead_port

import (
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// ReadyStatus contains detailed ready information for a downstream port.
type ReadyStatus struct {
	Ready      bool // Whether the port is ready
	Configured bool // Whether the port is configured
}

// RouterContext provides context information for routing decisions.
type RouterContext struct {
	Packet            packet.Packet  // The packet to route
	Cycle             int            // Current cycle
	DownstreamPorts   []AheadPort    // All downstream ports
	ReadyStatus       []bool         // Ready status for each downstream port (index matches DownstreamPorts)
	ReadyNonBlocking  []ReadyStatus  // Detailed ready status for each downstream
	Topology          interface{}    // Network topology information (optional)
}

// RouterFunc determines which downstream port a packet should be routed to.
// Parameters are provided via RouterContext.
// Returns:
//   - index of the selected downstream port (0-based), or -1 to discard the packet
type RouterFunc func(ctx RouterContext) int

// FanoutPort implements AheadPort for packet distribution.
// It is used when a single upstream port needs to send packets to multiple downstream ports.
// FanoutPort routes packets based on a router function that can access downstream ready status.
type FanoutPort struct {
	upstreamPort    AheadPort
	downstreamPorts []AheadPort
	router          RouterFunc
	topology       interface{}
}

// NewFanoutPort creates a new FanoutPort.
// Parameters:
//   - upstreamPort: single upstream port
//   - downstreamPorts: list of downstream ports
//   - router: routing function that can access downstream ready status
//   - topology: optional topology information for router
func NewFanoutPort(
	upstreamPort AheadPort,
	downstreamPorts []AheadPort,
	router RouterFunc,
	topology interface{},
) *FanoutPort {
	if upstreamPort == nil {
		panic("upstreamPort must not be nil")
	}
	if len(downstreamPorts) == 0 {
		panic("downstreamPorts must not be empty")
	}
	if router == nil {
		panic("router must not be nil")
	}
	return &FanoutPort{
		upstreamPort:    upstreamPort,
		downstreamPorts: downstreamPorts,
		router:          router,
		topology:        topology,
	}
}

// SetDone is called by upstream to notify all downstream ports that it has completed processing up to cycle N.
// This sets Done for all downstream ports.
func (f *FanoutPort) SetDone(cycle int) {
	for _, port := range f.downstreamPorts {
		port.SetDone(cycle)
	}
}

// GetDone returns the current Done value from upstream port.
func (f *FanoutPort) GetDone() int {
	return f.upstreamPort.GetDone()
}

// Chan returns the write-only channel from upstream port.
// Upstream sends packets through this channel, and FanoutPort routes them to downstream ports.
func (f *FanoutPort) SendChan() chan<- PacketWithCycle {
	return f.upstreamPort.SendChan()
}

// ReceiveChan should not be called on FanoutPort.
// Each downstream port has its own ReceiveChan().
func (f *FanoutPort) ReceiveChan() <-chan PacketWithCycle {
	panic("FanoutPort.ReceiveChan should not be called")
}

// Ready checks if ANY downstream port is ready for the given cycle (AnyReady).
// Returns true if at least one downstream port is ready, false otherwise.
// This method may block waiting for at least one downstream to become ready.
func (f *FanoutPort) Ready(cycle int) bool {
	// Check if any downstream is ready
	for _, port := range f.downstreamPorts {
		if port.Ready(cycle) {
			return true
		}
	}
	return false
}

// ReadyNonBlocking checks if ANY downstream port is ready without blocking (AnyReady).
// Returns (ready, configured):
//   - ready: true if at least one downstream is ready
//   - configured: true if at least one downstream is configured
func (f *FanoutPort) ReadyNonBlocking(cycle int) (ready bool, configured bool) {
	if len(f.downstreamPorts) == 0 {
		return false, false
	}
	
	for _, port := range f.downstreamPorts {
		pReady, pConfigured := port.ReadyNonBlocking(cycle)
		if pReady {
			ready = true
		}
		if pConfigured {
			configured = true
		}
	}
	return ready, configured
}

// AllReady checks if ALL downstream ports are ready for the given cycle.
// Returns true only if all downstream ports are ready, false otherwise.
// This method may block waiting for all downstreams to become ready.
func (f *FanoutPort) AllReady(cycle int) bool {
	for _, port := range f.downstreamPorts {
		if !port.Ready(cycle) {
			return false
		}
	}
	return true
}

// AllReadyNonBlocking checks if ALL downstream ports are ready without blocking.
// Returns (ready, configured, readyMap):
//   - ready: true if all downstreams are ready
//   - configured: true if all downstreams are configured
//   - readyMap: map of downstream port index to its ready status
func (f *FanoutPort) AllReadyNonBlocking(cycle int) (ready bool, configured bool, readyMap map[int]bool) {
	readyMap = make(map[int]bool)
	ready = true
	configured = true
	
	for i, port := range f.downstreamPorts {
		pReady, pConfigured := port.ReadyNonBlocking(cycle)
		readyMap[i] = pReady
		if !pReady {
			ready = false
		}
		if !pConfigured {
			configured = false
		}
	}
	return ready, configured, readyMap
}

// WaitForDone blocks until upstream's Done >= targetCycle.
func (f *FanoutPort) WaitForDone(targetCycle int) {
	f.upstreamPort.WaitForDone(targetCycle)
}

// SetTopology sets topology information for the router.
func (f *FanoutPort) SetTopology(topology interface{}) {
	f.topology = topology
}

// SetRouter sets the routing function.
func (f *FanoutPort) SetRouter(router RouterFunc) {
	if router == nil {
		panic("router must not be nil")
	}
	f.router = router
}

// DownstreamPorts returns all downstream ports.
func (f *FanoutPort) DownstreamPorts() []AheadPort {
	return f.downstreamPorts
}

// GetDownstreamReadyStatus returns the ready status of all downstream ports for a given cycle.
func (f *FanoutPort) GetDownstreamReadyStatus(cycle int) []ReadyStatus {
	statuses := make([]ReadyStatus, len(f.downstreamPorts))
	for i, port := range f.downstreamPorts {
		ready, configured := port.ReadyNonBlocking(cycle)
		statuses[i] = ReadyStatus{
			Ready:      ready,
			Configured: configured,
		}
	}
	return statuses
}

// RoutePacket routes a packet from upstream to the appropriate downstream port.
// This method:
// 1. Checks ready status of all downstream ports
// 2. Calls router function with context including ready status
// 3. Sends packet to the selected downstream port
// This is a helper method that can be used by components that need to route packets.
func (f *FanoutPort) RoutePacket(pkt PacketWithCycle) {
	// 1. Collect ready status of all downstream ports
	readyStatus := make([]bool, len(f.downstreamPorts))
	readyNonBlocking := make([]ReadyStatus, len(f.downstreamPorts))
	
	for i, port := range f.downstreamPorts {
		ready, configured := port.ReadyNonBlocking(pkt.Cycle)
		readyStatus[i] = ready
		readyNonBlocking[i] = ReadyStatus{
			Ready:      ready,
			Configured: configured,
		}
	}
	
	// 2. Build router context
	ctx := RouterContext{
		Packet:           pkt.Packet,
		Cycle:            pkt.Cycle,
		DownstreamPorts:  f.downstreamPorts,
		ReadyStatus:      readyStatus,
		ReadyNonBlocking: readyNonBlocking,
		Topology:         f.topology,
	}
	
	// 3. Call router function
	selectedIndex := f.router(ctx)
	
	// 4. Send to selected downstream (or discard)
	if selectedIndex >= 0 && selectedIndex < len(f.downstreamPorts) {
		// Double-check that selected downstream is ready
		if readyStatus[selectedIndex] {
			f.downstreamPorts[selectedIndex].SendChan() <- pkt
		}
		// If not ready, packet is effectively discarded (router should have checked)
	}
	// selectedIndex == -1 means discard packet
}

