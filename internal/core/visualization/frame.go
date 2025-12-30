package visualization

// Frame represents the exact JSON structure expected by the frontend (web/static/js).
type Frame struct {
	Cycle      int         `json:"cycle"`
	ConfigHash string      `json:"configHash,omitempty"` // Optional: to detect topology changes
	Paused     bool        `json:"paused"`
	InFlight   int         `json:"inFlightCount"`
	Nodes      []FrameNode `json:"nodes"`
	Edges      []FrameEdge `json:"edges"`
	Stats      interface{} `json:"stats,omitempty"` // Placeholder for global stats
}

type FrameNode struct {
	ID                     string       `json:"id"`
	Type                   string       `json:"type"`
	Label                  string       `json:"label"`
	Queues                 []FrameQueue `json:"queues"`
	Payload                interface{}  `json:"payload,omitempty"`
	Capabilities           []string     `json:"capabilities,omitempty"`
	InQueueBackpressure    bool         `json:"inQueueBackpressure"`
	OutQueueBackpressure   bool         `json:"outQueueBackpressure"`
	DownstreamBackpressure bool         `json:"downstreamBackpressure"`
}

type FrameQueue struct {
	Name     string        `json:"name"`
	Length   int           `json:"length"`
	Capacity int           `json:"capacity"`
	Packets  []FramePacket `json:"packets"`
}

type FramePacket struct {
	ID    string `json:"id"`
	Info  string `json:"info"`
	Cycle int    `json:"cycle"`
}

type FrameEdge struct {
	Source         string          `json:"source"`
	Target         string          `json:"target"`
	Label          string          `json:"label"`
	Latency        int             `json:"latency"`
	BandwidthLimit int             `json:"bandwidthLimit"`
	PipelineStages []PipelineStage `json:"pipelineStages"`
	Backpressured  bool            `json:"backpressured"`
}

type PipelineStage struct {
	StageIndex  int `json:"stageIndex"`
	PacketCount int `json:"packetCount"`
}
