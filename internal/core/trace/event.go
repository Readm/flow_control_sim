//go:build trace

package trace

// TraceEvent 表示一个 Chrome trace 事件
// 兼容 Chrome DevTools trace event format
type TraceEvent struct {
	// 事件名称（如 "Receive", "Process", "Send"）
	Name string `json:"name"`

	// 事件分类（如 "node", "link", "sync"）
	Category string `json:"cat"`

	// 事件类型（Phase）:
	// - "X": Complete event (有开始和结束时间)
	// - "B": Begin event
	// - "E": End event
	// - "i": Instant event (瞬时事件，如阻塞点)
	// - "M": Metadata
	Phase string `json:"ph"`

	// 时间戳（微秒，我们用 cycle）
	Timestamp float64 `json:"ts"`

	// 持续时间（微秒，我们用 cycle）
	// 只对 Complete event ("X") 有效
	Duration float64 `json:"dur,omitempty"`

	// Process ID（我们用 NodeID 或 LinkID）
	Pid int `json:"pid"`

	// Thread ID（我们用 PhaseID）:
	// - 1: Receive Phase
	// - 2: Process Phase
	// - 3: Send Phase
	// - 4: Transfer Phase (for Link)
	Tid int `json:"tid"`

	// Color Name (optional)
	// Supported: good, bad, terrible, yellow, olive, etc.
	Cname string `json:"cname,omitempty"`

	// 自定义参数
	Args map[string]interface{} `json:"args,omitempty"`
}

// EventPhase 定义事件类型常量
const (
	PhaseComplete = "X" // 完整事件（有开始和结束时间）
	PhaseBegin    = "B" // 开始事件
	PhaseEnd      = "E" // 结束事件
	PhaseInstant  = "i" // 瞬时事件
	PhaseMetadata = "M" // 元数据
)

// Category 定义事件分类常量
const (
	CategoryNode = "node" // 节点事件
	CategoryLink = "link" // 链路事件
	CategorySync = "sync" // 同步事件（阻塞）
)

// ThreadID 定义线程 ID 常量（对应不同阶段）
const (
	TidReceive  = 1 // Receive 阶段
	TidProcess  = 2 // Process 阶段
	TidSend     = 3 // Send 阶段
	TidTransfer = 4 // Transfer 阶段（Link）
)

// NewCompleteEvent 创建一个完整事件（有开始和结束时间）
func NewCompleteEvent(name, category, cname string, pid, tid int, start, end float64, args map[string]interface{}) TraceEvent {
	return TraceEvent{
		Name:      name,
		Category:  category,
		Phase:     PhaseComplete,
		Timestamp: start,
		Duration:  end - start,
		Pid:       pid,
		Tid:       tid,
		Cname:     cname,
		Args:      args,
	}
}

// NewInstantEvent 创建一个瞬时事件（如阻塞点）
func NewInstantEvent(name, category string, pid, tid int, cycle float64, args map[string]interface{}) TraceEvent {
	return TraceEvent{
		Name:      name,
		Category:  category,
		Phase:     PhaseInstant,
		Timestamp: cycle,
		Pid:       pid,
		Tid:       tid,
		Args:      args,
	}
}

// NewMetadataEvent 创建元数据事件（如线程名称）
func NewMetadataEvent(name string, pid, tid int, args map[string]interface{}) TraceEvent {
	return TraceEvent{
		Name:     name,
		Category: CategoryNode,
		Phase:    PhaseMetadata,
		Pid:      pid,
		Tid:      tid,
		Args:     args,
	}
}
