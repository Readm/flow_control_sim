package chi

import "github.com/Readm/flow_sim/core"

// PacketRecorder allows protocol capabilities to record timeline events.
type PacketRecorder interface {
	RecordPacketEvent(event *core.PacketEvent)
}

// MetadataRecorder records transaction-level metadata (e.g., cache hit/miss info).
type MetadataRecorder func(txnID int64, key, value string)

// OutgoingPacket describes a packet to be delivered by the node pipeline.
type OutgoingPacket struct {
	Packet      *core.Packet
	TargetID    int
	Kind        string
	LatencyHint string
}

const (
	// LatencyToMaster indicates latency from Home to Request nodes.
	LatencyToMaster = "relay_master"
	// LatencyToSlave indicates latency from Home to Slave nodes.
	LatencyToSlave = "relay_slave"
)
