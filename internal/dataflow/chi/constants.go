package chi

import "github.com/Readm/flow_sim/internal/dataflow"

// CHI Channel types (mapped to generic Message channels)
const (
	CHIChannelREQ = dataflow.ChannelREQ // Request channel
	CHIChannelRSP = dataflow.ChannelRSP // Response channel
	CHIChannelDAT = dataflow.ChannelDAT // Data channel
	CHIChannelSNP = dataflow.ChannelSNP // Snoop channel
)

// CHI Opcodes - Request Channel (REQ)
const (
	// Allocating Read requests
	OpcodeReadShared         = 0x00
	OpcodeReadClean          = 0x01
	OpcodeReadNotSharedDirty = 0x02
	OpcodeReadUnique         = 0x03
	OpcodeReadPreferUnique   = 0x04
	OpcodeMakeReadUnique     = 0x05

	// Other request types (to be extended)
	OpcodeReadNoSnp    = 0x10 // Downstream read request
	OpcodeReadNoSnpSep = 0x11 // Downstream read data only request
)

// CHI Opcodes - Response Channel (RSP)
const (
	OpcodeComp             = 0x20 // Completion response
	OpcodeRespSepData      = 0x21 // Separate response (data follows separately)
	OpcodeReadReceipt      = 0x22 // Read receipt from subordinate
	OpcodeCompAck          = 0x23 // Completion acknowledge
	OpcodeSnpResp          = 0x24 // Snoop response
	OpcodeSnpRespFwded     = 0x25 // Snoop response forwarded
	OpcodeSnpRespDataFwded = 0x26 // Snoop response with data forwarded
)

// CHI Opcodes - Data Channel (DAT)
const (
	OpcodeCompData       = 0x30 // Combined response and data
	OpcodeDataSepResp    = 0x31 // Data separate from response
	OpcodeSnpRespData    = 0x32 // Snoop response with data
	OpcodeSnpRespDataPtl = 0x33 // Snoop response with partial data
)

// CHI Opcodes - Snoop Channel (SNP)
const (
	OpcodeSnpSharedFwd = 0x40 // Snoop Shared Forward
	OpcodeSnpUniqueFwd = 0x41 // Snoop Unique Forward
	OpcodeSnpOnceFwd   = 0x42 // Snoop Once Forward
	// Additional snoop opcodes can be added here
)
