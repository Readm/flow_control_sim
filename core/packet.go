package core

// CHITransactionType represents CHI protocol transaction types.
type CHITransactionType string

const (
	CHITxnReadNoSnp   CHITransactionType = "ReadNoSnp"
	CHITxnWriteNoSnp  CHITransactionType = "WriteNoSnp"
	CHITxnReadOnce    CHITransactionType = "ReadOnce"
	CHITxnWriteUnique CHITransactionType = "WriteUnique"
)

// CHIMessageType represents CHI protocol message types.
type CHIMessageType string

const (
	CHIMsgReq     CHIMessageType = "Req"     // Request message
	CHIMsgResp    CHIMessageType = "Resp"    // Response message
	CHIMsgData    CHIMessageType = "Data"    // Data message
	CHIMsgComp    CHIMessageType = "Comp"    // Completion message
	CHIMsgSnp     CHIMessageType = "Snp"     // Snoop request message
	CHIMsgSnpResp CHIMessageType = "SnpResp" // Snoop response message
)

// CHIResponseType represents CHI response types.
type CHIResponseType string

const (
	CHIRespCompData   CHIResponseType = "CompData"   // Completion with data
	CHIRespCompAck    CHIResponseType = "CompAck"    // Completion acknowledgment
	CHIRespSnpData    CHIResponseType = "SnpData"    // Snoop response with data
	CHIRespSnpInvalid CHIResponseType = "SnpInvalid" // Snoop response invalidating cache
	CHIRespSnpNoData  CHIResponseType = "SnpNoData"  // Snoop response with no data
)

// Packet represents a CHI protocol message flowing through the simulator.
// Migration status:
// - Primary fields: Use CHI protocol fields (TransactionType, MessageType, ResponseType)
// - Legacy fields: Kept for backward compatibility, will be deprecated in future versions
// - When to use: New code should prefer CHI fields; legacy fields are checked as fallback
type Packet struct {
	ID    int64  // unique packet id
	Type  string // legacy: "request" or "response" - use MessageType instead (kept for compatibility)
	SrcID int    // source node id
	DstID int    // destination node id

	GeneratedAt int // cycle when generated (for requests)
	SentAt      int // cycle when sent to channel
	ReceivedAt  int // cycle when received by next hop
	CompletedAt int // cycle when processed (for requests)

	// Legacy fields (deprecated, kept for backward compatibility)
	// These fields are maintained for compatibility with older code but should not be used in new code.
	// Migration: Use CHI protocol fields (MessageType, TransactionType) instead of Type.
	// MasterID is equivalent to the original Request Node ID in CHI terminology.
	MasterID  int   // legacy: original master/request node id - use CHI MessageType + DstID instead
	RequestID int64 // legacy: request id - same as ID for request packets, preserved for compatibility

	// CHI protocol fields (preferred)
	// These are the primary fields for packet identification and routing in CHI protocol.
	TransactionType CHITransactionType // CHI transaction type (ReadNoSnp, WriteNoSnp, etc.)
	MessageType     CHIMessageType     // CHI message type (Req, Resp, Data, Comp) - primary field for message type
	ResponseType    CHIResponseType    // CHI response type (CompData, CompAck, etc.)
	Address         uint64             // memory address for the transaction
	DataSize        int                // data size in bytes (default: DefaultCacheLineSize)

	// Transaction tracking
	TransactionID int64 // ID of the transaction this packet belongs to (0 if not associated)

	// Snoop-related fields
	OriginalTxnID int64 // For Snoop responses: the original transaction ID that triggered this snoop
	SnoopTargetID int   // For Snoop requests: the target Request Node ID to snoop

	// Packet generation tracking
	ParentPacketID int64 // ID of the parent packet that generated this packet (0 if no parent)

	// Extension metadata, used by plugins to attach custom fields.
	Metadata map[string]string
}

// PacketInfo represents packet information for visualization.
type PacketInfo struct {
	ID              int64              `json:"id"`
	Type            string             `json:"type"`
	SrcID           int                `json:"srcID"`
	DstID           int                `json:"dstID"`
	GeneratedAt     int                `json:"generatedAt"`
	SentAt          int                `json:"sentAt"`
	ReceivedAt      int                `json:"receivedAt"`
	CompletedAt     int                `json:"completedAt"`
	MasterID        int                `json:"masterID"`
	RequestID       int64              `json:"requestID"`
	TransactionType CHITransactionType `json:"transactionType"`
	MessageType     CHIMessageType     `json:"messageType"`
	ResponseType    CHIResponseType    `json:"responseType"`
	Address         uint64             `json:"address"`
	DataSize        int                `json:"dataSize"`
	TransactionID   int64              `json:"transactionID"` // Transaction ID this packet belongs to
	Metadata        map[string]string  `json:"metadata,omitempty"`
}

// QueueInfo represents queue information for visualization.
type QueueInfo struct {
	Name     string       `json:"name"`
	Length   int          `json:"length"`
	Capacity int          `json:"capacity"` // -1 means unlimited capacity
	Packets  []PacketInfo `json:"packets,omitempty"`
}

// SetMetadata attaches a key/value pair to the packet metadata.
func (p *Packet) SetMetadata(key, value string) {
	if p == nil || key == "" {
		return
	}
	if p.Metadata == nil {
		p.Metadata = make(map[string]string)
	}
	p.Metadata[key] = value
}

// GetMetadata returns the value for a metadata key.
func (p *Packet) GetMetadata(key string) (string, bool) {
	if p == nil || key == "" || p.Metadata == nil {
		return "", false
	}
	value, ok := p.Metadata[key]
	return value, ok
}

// DeleteMetadata removes a metadata key from the packet.
func (p *Packet) DeleteMetadata(key string) {
	if p == nil || key == "" || p.Metadata == nil {
		return
	}
	delete(p.Metadata, key)
	if len(p.Metadata) == 0 {
		p.Metadata = nil
	}
}

// CloneMetadata creates a shallow copy of the metadata map.
func CloneMetadata(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
