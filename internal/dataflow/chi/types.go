package chi

// CHIPayload represents the protocol-specific payload for CHI messages.
// This structure is stored in Message.Payload field to keep the framework protocol-agnostic.
type CHIPayload struct {
	// Address field
	Addr uint64 // Memory address

	// Opcode (redundant with Message.Type, but kept for convenience)
	Opcode int

	// Routing information for Direct Memory Transfer (DMT) and Forwarding
	ReturnNID   int // Return Node ID - target node for forwarded data (used in DMT and Forwarding)
	ReturnTxnID int // Return Transaction ID - original requester's transaction ID

	// Data payload (for DAT channel messages)
	Data []byte

	// Response/Status fields
	RespErr int // Response error code (0 = success)

	// Additional protocol-specific fields can be added here as needed
	// Using a map for extensibility (as requested)
	ExtFields map[string]interface{} // Extended fields for protocol-specific extensions
}

// NewCHIPayload creates a new CHIPayload with default values.
func NewCHIPayload(opcode int, addr uint64) *CHIPayload {
	return &CHIPayload{
		Opcode:    opcode,
		Addr:      addr,
		ExtFields: make(map[string]interface{}),
	}
}

// SetReturnInfo sets the return routing information for DMT/Forwarding.
func (p *CHIPayload) SetReturnInfo(returnNID int, returnTxnID int) {
	p.ReturnNID = returnNID
	p.ReturnTxnID = returnTxnID
}

// SetData sets the data payload.
func (p *CHIPayload) SetData(data []byte) {
	p.Data = data
}

// GetExtField retrieves an extended field value.
func (p *CHIPayload) GetExtField(key string) (interface{}, bool) {
	if p.ExtFields == nil {
		return nil, false
	}
	val, ok := p.ExtFields[key]
	return val, ok
}

// SetExtField sets an extended field value.
func (p *CHIPayload) SetExtField(key string, value interface{}) {
	if p.ExtFields == nil {
		p.ExtFields = make(map[string]interface{})
	}
	p.ExtFields[key] = value
}

