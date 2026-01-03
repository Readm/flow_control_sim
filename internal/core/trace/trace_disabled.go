//go:build !trace

package trace

// TraceEvent 空结构体（禁用时）
type TraceEvent struct{}

// TracerConfig 配置（禁用时也需要定义）
type TracerConfig struct {
	Enabled        bool
	StartCycle     int
	EndCycle       int
	SampleRate     int
	MinDuration    int64
	NodeFilter     []int
	BlockThreshold int64
}

// DefaultConfig 返回默认配置（禁用）
func DefaultConfig() TracerConfig {
	return TracerConfig{Enabled: false}
}

// TraceRecorder 空结构体（禁用时零开销）
type TraceRecorder struct{}

// NewTraceRecorder 创建禁用的 recorder
func NewTraceRecorder(config TracerConfig) *TraceRecorder {
	return &TraceRecorder{}
}

// TraceSource 接口定义（即使禁用 trace 也需要，因为 node.go 引用了它）
type TraceSource interface {
	GetTraceEvents() []TraceEvent
	ID() int
	Name() string
}

func (tr *TraceRecorder) RegisterSource(source TraceSource) {}
func (tr *TraceRecorder) IsNodeTraced(id int) bool          { return false }

// 所有方法都是空操作，编译器会内联优化掉

func (tr *TraceRecorder) RecordComplete(
	name string,
	category string,
	pid int,
	tid int,
	start float64,
	end float64,
	args map[string]interface{},
) {
}

func (tr *TraceRecorder) RecordInstant(
	name string,
	category string,
	pid int,
	tid int,
	cycle float64,
	args map[string]interface{},
) {
}

func (tr *TraceRecorder) RecordMetadata(name string, pid, tid int, args map[string]interface{}) {
}

func (tr *TraceRecorder) GetEvents() []TraceEvent {
	return nil
}

func (tr *TraceRecorder) EventCount() int {
	return 0
}

func (tr *TraceRecorder) Clear() {
}

func (tr *TraceRecorder) Enable() {
}

func (tr *TraceRecorder) Disable() {
}

func (tr *TraceRecorder) IsEnabled() bool {
	return false
}

func (tr *TraceRecorder) IsCycleTraced(cycle int64) bool {
	return false
}

func (tr *TraceRecorder) Export(filename string) error {
	return nil
}

func (tr *TraceRecorder) ExportWithMetadata(
	filename string,
	nodeNames map[int]string,
	threadNames map[int]string,
) error {
	return nil
}

// 常量定义（禁用时也需要）
const (
	PhaseComplete = "X"
	PhaseBegin    = "B"
	PhaseEnd      = "E"
	PhaseInstant  = "i"
	PhaseMetadata = "M"

	CategoryNode = "node"
	CategoryLink = "link"
	CategorySync = "sync"

	TidReceive  = 1
	TidProcess  = 2
	TidSend     = 3
	TidTransfer = 4
)
