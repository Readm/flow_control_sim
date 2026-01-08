package state

// DetailLevel controls the verbosity of the state export.
type DetailLevel int

const (
	// DetailLevelSummary exports only statistics and metadata (default).
	DetailLevelSummary DetailLevel = iota
	// DetailLevelFull exports all internal details (e.g., Cache Lines, Directory Entries).
	DetailLevelFull
)

// ExportConfig controls the export
type ExportConfig struct {
	DetailLevel DetailLevel
}

// NetworkState is the top-level DTO for the entire network state.
type NetworkState struct {
	CurrentCycle int
	Nodes        []NodeState
	Links        []LinkState
}

// NodeState represents the state of a single node.
type NodeState struct {
	ID           int
	Type         string // e.g., "WorkerNode", "Router"
	CurrentCycle int
	Inputs       []QueueState
	Outputs      []QueueState
	Caches       []CacheState
	Directories  []DirectoryState
	CustomData   map[string]interface{}
}

// LinkState represents the state of a link.
type LinkState struct {
	SourceID     int
	SourcePortID int // 源端口 ID
	TargetID     int
	TargetPortID int // 目标端口 ID
	CurrentCycle int
	Latency      int
	Bandwidth    int
	// Occupancy shows the number of packets in each time slot.
	Occupancy []int
}

// QueueState represents the state of an input or output queue.
type QueueState struct {
	Type      string // "Input" or "Output"
	Length    int
	Capacity  int
	Bandwidth int
	Bitmap    string // "1010..." representation of occupied slots
	// Packets is a list of packet summaries.
	// In Summary mode, this might be empty or partial.
	Packets []PacketState
}

// PacketState represents a summary of a packet.
type PacketState struct {
	// PktID is a unique identifier if avail, or derived hash.
	Src   int
	Dst   int
	Cycle int    // Creation or Injection cycle
	Msg   string // Payload summary
	Type  string // Type of the packet (e.g., "Req", "Resp")
}

// CacheState represents the state of a cache.
type CacheState struct {
	Hits       uint64
	Misses     uint64
	Accesses   uint64
	Evictions  uint64
	Writebacks uint64
	// Lines is populated only if DetailLevel >= DetailLevelFull
	Lines []CacheLineState
}

// CacheLineState represents a single cache line.
type CacheLineState struct {
	Address uint64
	State   string
	Tag     uint64
}

// DirectoryState represents the state of a directory controller.
type DirectoryState struct {
	// Entries is populated only if DetailLevel >= DetailLevelFull
	Entries []DirectoryEntryState
}

// DirectoryEntryState represents a single directory entry.
type DirectoryEntryState struct {
	Address uint64
	State   string
	Sharers []int
	Owner   int // -1 if no owner
}

// Exporter is the interface that components should implement to support visualization.
// T is the specific State struct for that component.
type Exporter[T any] interface {
	ExportState(cfg ExportConfig) T
}
