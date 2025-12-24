package decoder

// DecodeResult contains the result of address decoding.
type DecodeResult struct {
	Addr       uint64                 // Original address
	TargetID   int                    // Target node ID (e.g., Home Node, Memory Controller)
	Attributes map[string]interface{} // Protocol-specific attributes
}

// Decoder decodes addresses to determine routing targets.
// This is a capability interface that different protocols can implement.
type Decoder interface {
	// DecodeAddress decodes an address and returns routing information.
	// The interpretation of TargetID and Attributes depends on the protocol.
	DecodeAddress(addr uint64) (*DecodeResult, error)
}

// Common attribute keys (protocols can use these or define their own)
const (
	AttrIsMemory    = "IsMemory"    // bool: whether address maps to memory
	AttrIsCacheable = "IsCacheable" // bool: whether address is cacheable
	AttrHomeNodeID  = "HomeNodeID"  // int: Home Node ID (directory-based protocols)
	AttrSliceID     = "SliceID"     // int: Memory slice ID
)
