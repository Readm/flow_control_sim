package core

// PacketEventType represents the type of packet event.
type PacketEventType string

const (
	PacketEnqueued        PacketEventType = "PacketEnqueued"
	PacketDequeued        PacketEventType = "PacketDequeued"
	PacketProcessingStart PacketEventType = "PacketProcessingStart"
	PacketProcessingEnd   PacketEventType = "PacketProcessingEnd"
	PacketSent            PacketEventType = "PacketSent"
	PacketInTransitEnd    PacketEventType = "PacketInTransitEnd"
	PacketReceived        PacketEventType = "PacketReceived"
	PacketGenerated       PacketEventType = "PacketGenerated"
)

// PacketEvent represents a packet event in the transaction timeline.
type PacketEvent struct {
	Sequence       int64             `json:"sequence"`
	TransactionID  int64             `json:"transactionID"`
	PacketID       int64             `json:"packetID"`
	ParentPacketID int64             `json:"parentPacketID"` // 0 means no parent packet
	NodeID         int               `json:"nodeID"`
	NodeLabel      string            `json:"nodeLabel"` // "RN 0", "SN 1", etc.
	EventType      PacketEventType   `json:"eventType"`
	Cycle          int               `json:"cycle"`
	EdgeKey        *EdgeKey          `json:"edgeKey,omitempty"` // nil for in-node events
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// EdgeKey represents a unique edge in the network (fromID -> toID).
type EdgeKey struct {
	FromID int `json:"fromID"`
	ToID   int `json:"toID"`
}

// NodeInfo represents node information for timeline.
type NodeInfo struct {
	ID    int      `json:"id"`
	Label string   `json:"label"`
	Type  NodeType `json:"type"`
}

// TimeRange represents a time range.
type TimeRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// TransactionTimeline represents the timeline data for a transaction.
type TransactionTimeline struct {
	TransactionID int64         `json:"transactionID"`
	Events        []*PacketEvent `json:"events"`
	Nodes         []NodeInfo    `json:"nodes"`
	TimeRange     TimeRange     `json:"timeRange"`
	Packets       []PacketInfo  `json:"packets"`
}


