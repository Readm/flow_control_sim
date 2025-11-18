package loader

import (
	app "github.com/Readm/flow_sim/framework/app"
)

// TopologyDocument represents the JSON schema parsed from disk.
type TopologyDocument struct {
	Meta     MetaBlock      `json:"meta"`
	Defaults DefaultsBlock  `json:"defaults"`
	Nodes    []NodeDocument `json:"nodes"`
	Links    []LinkDocument `json:"links"`
	// Schedules describe deterministic transactions to inject.
	Schedules []ScheduleDocument `json:"schedules"`
	// InitialStates allows cache warm-up by node id (string) and address string.
	InitialStates map[string]map[string]string `json:"initial_states"`
}

// MetaBlock contains name/description metadata required for registration.
type MetaBlock struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Category    string   `json:"category"`
}

// DefaultsBlock contains simulator-level defaults replicated from app.Config.
type DefaultsBlock struct {
	TotalCycles           int              `json:"total_cycles"`
	BandwidthLimit        int              `json:"bandwidth_limit"`
	DispatchQueueCapacity int              `json:"dispatch_queue_capacity"`
	RequestRate           float64          `json:"request_rate"`
	RequestCacheCapacity  int              `json:"request_cache_capacity"`
	HomeCacheCapacity     int              `json:"home_cache_capacity"`
	Headless              *bool            `json:"headless"`
	VisualMode            string           `json:"visual_mode"`
	Plugins               app.PluginConfig `json:"plugins"`
	EnablePacketHistory   *bool            `json:"enable_packet_history"`
	MaxPacketHistorySize  int              `json:"max_packet_history_size"`
	HistoryOverflowMode   string           `json:"history_overflow_mode"`
	MaxTransactionHistory int              `json:"max_transaction_history"`
	RequestCacheParams    map[string]any   `json:"request_cache_params"`
}

// NodeDocument describes a single node definition within the JSON file.
type NodeDocument struct {
	ID           string            `json:"id"`
	Label        string            `json:"label"`
	Capabilities []string          `json:"capabilities"`
	Params       map[string]any    `json:"params"`
	Position     *NodePosition     `json:"position"`
	Metadata     map[string]string `json:"metadata"`
}

// NodePosition captures optional placement information for visualization.
type NodePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// LinkDocument represents a directed edge between two nodes with latency/bandwidth.
type LinkDocument struct {
	From          string            `json:"from"`
	To            string            `json:"to"`
	Latency       int               `json:"latency"`
	Bandwidth     int               `json:"bandwidth"`
	Bidirectional bool              `json:"bidirectional"`
	Metadata      map[string]string `json:"metadata"`
}

// ScheduleDocument captures deterministic traffic injection.
type ScheduleDocument struct {
	Tick         int                   `json:"tick"`
	Source       string                `json:"source"`
	Transactions []TransactionDocument `json:"transactions"`
	Metadata     map[string]string     `json:"metadata"`
}

// TransactionDocument defines a single scheduled transaction.
type TransactionDocument struct {
	Type     string `json:"type"`
	Address  string `json:"address"`
	Target   string `json:"target"`
	DataSize int    `json:"data_size"`
}
