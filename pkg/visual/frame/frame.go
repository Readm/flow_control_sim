package frame

// Frame represents a snapshot emitted each simulation cycle for visualization.
type Frame struct {
	Cycle         int    `json:"cycle"`
	Paused        bool   `json:"paused"`
	InFlightCount int    `json:"inFlightCount"`
	ConfigHash    string `json:"configHash"`
	Nodes         []Node `json:"nodes"`
	Edges         []Edge `json:"edges"`
	Stats         *Stats `json:"stats,omitempty"`
}

// Node describes a single vertex in the topology graph.
type Node struct {
	ID           int               `json:"id"`
	Label        string            `json:"label"`
	Type         string            `json:"type"`
	Queues       []Queue           `json:"queues,omitempty"`
	Payload      map[string]any    `json:"payload,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// Queue communicates per-node queue depth for Flow View overlays.
type Queue struct {
	Name     string        `json:"name"`
	Length   int           `json:"length"`
	Capacity int           `json:"capacity"`
	Packets  []PacketEntry `json:"packets,omitempty"`
}

// PacketEntry captures lightweight packet metadata required by the UI.
type PacketEntry struct {
	ID              int    `json:"id"`
	Type            string `json:"type"`
	SrcID           int    `json:"srcID"`
	DstID           int    `json:"dstID"`
	TransactionType string `json:"transactionType"`
	MessageType     string `json:"messageType"`
}

// Edge represents a link between two nodes and its pipeline state.
type Edge struct {
	Source         int             `json:"source"`
	Target         int             `json:"target"`
	Label          string          `json:"label"`
	Latency        int             `json:"latency"`
	BandwidthLimit int             `json:"bandwidthLimit"`
	PipelineStages []PipelineStage `json:"pipelineStages,omitempty"`
}

// PipelineStage expresses the occupancy for a latency stage.
type PipelineStage struct {
	StageIndex  int `json:"stageIndex"`
	PacketCount int `json:"packetCount"`
}

// Stats aggregates simulator level metrics for the dashboard panels.
type Stats struct {
	Global    *GlobalStats  `json:"Global,omitempty"`
	PerMaster []MasterStats `json:"PerMaster,omitempty"`
	PerSlave  []SlaveStats  `json:"PerSlave,omitempty"`
}

// GlobalStats mirrors the current implementation in the front-end.
type GlobalStats struct {
	TotalRequests    int     `json:"TotalRequests"`
	Completed        int     `json:"Completed"`
	CompletionRate   float64 `json:"CompletionRate"`
	AvgEndToEndDelay float64 `json:"AvgEndToEndDelay"`
	MaxDelay         int     `json:"MaxDelay"`
	MinDelay         int     `json:"MinDelay"`
}

// MasterStats contains per-master metrics.
type MasterStats struct {
	CompletedRequests int     `json:"CompletedRequests"`
	AvgDelay          float64 `json:"AvgDelay"`
	MaxDelay          int     `json:"MaxDelay"`
	MinDelay          int     `json:"MinDelay"`
}

// SlaveStats contains per-slave metrics.
type SlaveStats struct {
	TotalProcessed int     `json:"TotalProcessed"`
	MaxQueueLength int     `json:"MaxQueueLength"`
	AvgQueueLength float64 `json:"AvgQueueLength"`
}
