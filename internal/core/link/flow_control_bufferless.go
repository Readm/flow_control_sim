package link

// BufferlessFlowControl implements an always-ready flow control strategy without buffering.
// This is designed for bufferless ring topologies and NoC designs where flow control
// never blocks, but packets still experience latency during transmission.
//
// Key Characteristics:
// - No internal buffering or state (no ring buffer, no slots)
// - Always ready for packet acceptance (no backpressure)
// - No bandwidth limits enforced by flow control
// - Latency is still handled by Link (delay in transmission time)
//
// Use Cases:
// - Ring topologies with circuit switching
// - Bufferless NoC designs where deadlock is avoided by routing
// - Point-to-point connections without flow control
//
// Important:
// - "Bufferless" means no flow control buffering, NOT zero latency
// - Link's latency parameter still controls packet transmission delay
// - The flow control itself imposes no restrictions on packet acceptance
type BufferlessFlowControl struct {
	// No internal state - stateless strategy
}

// NewBufferlessFlowControl creates a BufferlessFlowControl strategy.
//
// Note: Unlike BufferedFlowControl, this strategy has no configuration parameters
// as it maintains no state and imposes no restrictions.
func NewBufferlessFlowControl() *BufferlessFlowControl {
	return &BufferlessFlowControl{}
}

// CanAcceptPacket always returns true for bufferless flow control.
// Packets are never rejected by flow control constraints.
//
// Parameters:
//   cycle: current simulation cycle (unused)
//   targetCycle: target delivery cycle (unused)
//
// Returns:
//   Always true
func (fc *BufferlessFlowControl) CanAcceptPacket(cycle int, targetCycle int) bool {
	return true
}

// OnPacketAccepted is called after a packet is accepted.
// For bufferless strategy, this is a no-op as there's no state to update.
func (fc *BufferlessFlowControl) OnPacketAccepted(cycle int, targetCycle int) {
	// No-op - stateless strategy
}

// OnPacketBlocked is called when a packet cannot be accepted.
// For bufferless strategy, this should never happen as CanAcceptPacket always returns true.
func (fc *BufferlessFlowControl) OnPacketBlocked(cycle int, targetCycle int) {
	// No-op - should never be called
}

// CanSendPacket checks if packets should be sent to downstream.
// For bufferless flow control, this delegates entirely to downstream readiness.
//
// Parameters:
//   cycle: current simulation cycle (unused)
//   downstreamReady: whether downstream port is ready
//
// Returns:
//   The downstreamReady value unchanged
func (fc *BufferlessFlowControl) CanSendPacket(cycle int, downstreamReady bool) bool {
	return downstreamReady
}

// OnPacketSent is called after packets are sent.
// For bufferless strategy, this is a no-op as there's no state to update.
func (fc *BufferlessFlowControl) OnPacketSent(cycle int) {
	// No-op - stateless strategy
}

// GetReadyForCycle returns whether the link is ready to accept packets from upstream.
// For bufferless flow control, this always returns true.
//
// Parameters:
//   cycle: current simulation cycle (unused)
//
// Returns:
//   Always true
func (fc *BufferlessFlowControl) GetReadyForCycle(cycle int) bool {
	return true
}

// Reset resets the flow control state.
// For bufferless strategy, this is a no-op as there's no state to reset.
func (fc *BufferlessFlowControl) Reset() {
	// No-op - stateless strategy
}
