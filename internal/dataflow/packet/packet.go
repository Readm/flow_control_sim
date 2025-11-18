package packet

// Packet represents the minimal unit exchanged between nodes through links.
// Higher level metadata (transaction IDs, QoS, etc.) can be layered on top of
// this structure without affecting the Core/Entity contracts.
type Packet struct {
	SourceID int
	TargetID int
	Payload  string
}

// Envelope associates a packet with the cycle in which it becomes visible to
// the destination flow. This is the element transmitted through link channels.
type Envelope struct {
	Cycle  uint64
	Packet Packet
}
