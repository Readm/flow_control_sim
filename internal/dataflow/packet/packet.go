package packet

// Packet represents the minimal unit exchanged between nodes through links.
// Higher level metadata (transaction IDs, QoS, etc.) can be layered on top of
// this structure without affecting the Core/Entity contracts.
type Packet struct {
	SourceID      int
	TargetID      int
	Payload       string
	TransactionID int64 // Associated Transaction ID (0 means not associated)
	MessageID     int64 // Associated Message ID (0 means not associated)
	Sequence      int   // Sequence number in Message (for multi-packet messages)
}

// PacketWithCycle associates a packet with the cycle in which it becomes visible to
// the destination flow. This is the element transmitted through link channels.
type PacketWithCycle struct {
	Cycle  int
	Packet Packet
}
