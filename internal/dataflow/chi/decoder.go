package chi

import (
	"github.com/Readm/flow_sim/internal/components/decoder"
)

// StaticDecoder maps all addresses to a single Home Node (for testing).
type StaticDecoder struct {
	homeNodeID int
}

// NewStaticDecoder creates a StaticDecoder.
func NewStaticDecoder(homeNodeID int) *StaticDecoder {
	return &StaticDecoder{homeNodeID: homeNodeID}
}

// DecodeAddress implements decoder.Decoder
func (d *StaticDecoder) DecodeAddress(addr uint64) (*decoder.DecodeResult, error) {
	return &decoder.DecodeResult{
		Addr:     addr,
		TargetID: d.homeNodeID,
		Attributes: map[string]interface{}{
			decoder.AttrIsMemory:    true,
			decoder.AttrIsCacheable: true,
			decoder.AttrHomeNodeID:  d.homeNodeID,
		},
	}, nil
}

// HashDecoder uses address hashing to distribute across multiple Home Nodes.
type HashDecoder struct {
	numHomeNodes int
	homeNodeBase int // Base node ID for Home Nodes
	addressBits  int // Number of address bits to use for hashing
}

// NewHashDecoder creates a HashDecoder.
func NewHashDecoder(numHomeNodes, homeNodeBase, addressBits int) *HashDecoder {
	return &HashDecoder{
		numHomeNodes: numHomeNodes,
		homeNodeBase: homeNodeBase,
		addressBits:  addressBits,
	}
}

// DecodeAddress implements decoder.Decoder
func (d *HashDecoder) DecodeAddress(addr uint64) (*decoder.DecodeResult, error) {
	// Hash based on page number (4KB pages)
	pageNum := addr >> 12
	hash := int(pageNum) % d.numHomeNodes
	homeNodeID := d.homeNodeBase + hash

	return &decoder.DecodeResult{
		Addr:     addr,
		TargetID: homeNodeID,
		Attributes: map[string]interface{}{
			decoder.AttrIsMemory:    true,
			decoder.AttrIsCacheable: true,
			decoder.AttrHomeNodeID:  homeNodeID,
		},
	}, nil
}
