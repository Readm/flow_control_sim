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
func (fc *BufferlessFlowControl) CanAcceptPacket(cycle int, targetCycle int) bool {
	return true
}

// CanSendPacket checks if packets should be sent to downstream.
// For bufferless flow control, this delegates entirely to downstream readiness.
func (fc *BufferlessFlowControl) CanSendPacket(cycle int, downstreamReady bool) bool {
	return downstreamReady
}

// IsReady returns whether the link is ready to accept packets from upstream.
// For bufferless flow control, this always returns true.
func (fc *BufferlessFlowControl) IsReady(cycle int) bool {
	return true
}

// Reset resets the flow control state.
// For bufferless strategy, this is a no-op as there's no state to reset.
func (fc *BufferlessFlowControl) Reset() {
	// No-op - stateless strategy
}
