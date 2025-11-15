package main

// NodeSnapshot describes a node state in a given cycle for visualization.
type NodeSnapshot struct {
	ID           int            `json:"id"`
	Type         NodeType       `json:"type"`
	Label        string         `json:"label"`
	Queues       []QueueInfo    `json:"queues"`
	Capabilities []string       `json:"capabilities,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
}

// PipelineStageInfo represents the state of a single stage in a pipeline
type PipelineStageInfo struct {
	StageIndex  int `json:"stageIndex"` // 0 to latency-1
	PacketCount int `json:"packetCount"`
	LogicCycle  int `json:"logicCycle"`
}

// EdgeSnapshot describes a logical connection between nodes.
type EdgeSnapshot struct {
	Source         int                 `json:"source"`
	Target         int                 `json:"target"`
	Label          string              `json:"label"`
	Latency        int                 `json:"latency"`
	PipelineStages []PipelineStageInfo `json:"pipelineStages,omitempty"`
	BandwidthLimit int                 `json:"bandwidthLimit"`
}

// SimulationFrame aggregates information required by frontends for a cycle.
type SimulationFrame struct {
	Cycle            int               `json:"cycle"`
	Paused           bool              `json:"paused"`
	Nodes            []NodeSnapshot    `json:"nodes"`
	Edges            []EdgeSnapshot    `json:"edges"`
	InFlightCount    int               `json:"inFlightCount"`
	Stats            *SimulationStats  `json:"stats,omitempty"`
	ConfigHash       string            `json:"configHash,omitempty"`       // Hash of current config to detect config changes
	TransactionGraph *TransactionGraph `json:"transactionGraph,omitempty"` // Transaction relationship graph
}
