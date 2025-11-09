package main

// NodeCycleBindings manages the SFC/RFC signals and component metadata shared between
// a node and the global cycle coordinator.
type NodeCycleBindings struct {
	componentID string
	coordinator *CycleCoordinator

	incomingSignals map[EdgeKey]*CycleSignal // Link -> Node (RFC from link perspective)
	outgoingSignals map[EdgeKey]*CycleSignal // Node -> Link (SFC)

	lastReceive map[EdgeKey]int
}

// NewNodeCycleBindings creates an empty binding set.
func NewNodeCycleBindings() *NodeCycleBindings {
	return &NodeCycleBindings{
		incomingSignals: make(map[EdgeKey]*CycleSignal),
		outgoingSignals: make(map[EdgeKey]*CycleSignal),
		lastReceive:     make(map[EdgeKey]int),
	}
}

// SetComponent configures the component identifier for coordinator registration.
func (b *NodeCycleBindings) SetComponent(componentID string) {
	b.componentID = componentID
}

// SetCoordinator attaches the cycle coordinator reference.
func (b *NodeCycleBindings) SetCoordinator(coord *CycleCoordinator) {
	b.coordinator = coord
}

// ComponentID returns the registered component identifier.
func (b *NodeCycleBindings) ComponentID() string {
	return b.componentID
}

// Coordinator returns the assigned cycle coordinator.
func (b *NodeCycleBindings) Coordinator() *CycleCoordinator {
	return b.coordinator
}

// RegisterIncoming stores the signal a node should wait on before processing cycle `n`.
func (b *NodeCycleBindings) RegisterIncoming(edge EdgeKey, signal *CycleSignal) {
	if signal == nil {
		return
	}
	b.incomingSignals[edge] = signal
}

// RegisterOutgoing stores the signal the node must update after finishing its send stage.
func (b *NodeCycleBindings) RegisterOutgoing(edge EdgeKey, signal *CycleSignal) {
	if signal == nil {
		return
	}
	b.outgoingSignals[edge] = signal
}

// WaitIncoming waits until all incoming signals reach the given logical cycle.
func (b *NodeCycleBindings) WaitIncoming(cycle int) {
	for _, signal := range b.incomingSignals {
		target := cycle - 1
		if target < -1 {
			target = -1
		}
		signal.WaitUntil(target)
	}
}

// SignalReceive records that the node completed its receive stage for the cycle.
// (Currently stored for diagnostics; can be extended for additional coordination.)
func (b *NodeCycleBindings) SignalReceive(cycle int) {
	for edge := range b.incomingSignals {
		b.lastReceive[edge] = cycle
	}
}

// SignalSend notifies all outgoing links that the node has completed sending for the cycle.
func (b *NodeCycleBindings) SignalSend(cycle int) {
	for _, signal := range b.outgoingSignals {
		signal.Update(cycle)
	}
}
