package link

// FlowControlStrategy defines the interface for link flow control strategies.
// This interface abstracts different flow control mechanisms (buffered, credit-based,
// bufferless, virtual channels, etc.) allowing Link to be decoupled from specific
// flow control implementations.
//
// Design Principles:
// - Link holds a FlowControlStrategy and delegates flow control decisions to it
// - Different strategies implement different mechanisms internally
// - Queue and other components remain unaware of the specific strategy
//
// Lifecycle:
// 1. Check if can accept packet from upstream (CanAcceptPacket)
// 2. Check if can send packet to downstream (CanSendPacket)
// 3. Report ready state to upstream (IsReady)
// 4. Reset state when needed (Reset)
//
// Note: OnPacket* lifecycle methods have been removed as they were mostly no-ops.
// Concrete implementations handle state management directly in their helper methods.
type FlowControlStrategy interface {
	// CanAcceptPacket checks if the strategy can accept a new packet.
	// This is called when Link receives a packet from upstream.
	//
	// Parameters:
	//   cycle: current cycle
	//   targetCycle: the cycle when the packet will arrive at downstream (after latency)
	//
	// Returns:
	//   true if the packet can be accepted (e.g., buffer has space, credits available)
	//   false if the packet should be delayed (e.g., buffer full, no credits)
	CanAcceptPacket(cycle int, targetCycle int) bool

	// CanSendPacket checks if the strategy allows sending packets to downstream.
	// This is called in each cycle to decide whether to transmit buffered packets.
	//
	// Parameters:
	//   cycle: current cycle
	//   downstreamReady: whether downstream is ready to receive
	//
	// Returns:
	//   true if packets should be sent
	//   false if packets should be held (e.g., downstream not ready, no credits)
	CanSendPacket(cycle int, downstreamReady bool) bool

	// IsReady returns the ready state for a given cycle.
	// This is called to update upstream about Link's readiness.
	//
	// Parameters:
	//   cycle: the cycle to query
	//
	// Returns:
	//   true if Link is ready to accept packets for that cycle
	//   false otherwise
	IsReady(cycle int) bool

	// Reset resets the flow control state.
	// This is called when Network.Reset() is invoked.
	Reset()
}
