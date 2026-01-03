package trace

import (
	"sync"
)

// TracerConfig 配置 tracer 行为
type TracerConfig struct {
	// 是否启用追踪
	Enabled bool

	// StartCycle: 从此周期开始记录 (0 表示从头开始)
	StartCycle int

	// EndCycle: 到此周期停止记录 (0 表示不限制)
	EndCycle int

	// 采样率：每 N 个 cycles 记录一次（1 表示每个 cycle 都记录）
	SampleRate int

	// 最小持续时间：只记录持续时间超过此阈值的事件（cycles）
	MinDuration int64

	// 节点过滤器：只记录这些节点的事件（空表示记录所有节点）
	NodeFilter []int

	// 记录阻塞事件的阈值（cycles）
	// 只有阻塞时间超过此值才会记录瞬时事件
	BlockThreshold int64
}

// DefaultConfig 返回默认配置
func DefaultConfig() TracerConfig {
	return TracerConfig{
		Enabled:        true,
		StartCycle:     0,       // 默认从头记录
		EndCycle:       1000,    // 默认只记录前 1000 个 cycles (安全限制)
		SampleRate:     1,       // 每个 cycle 都记录
		MinDuration:    0,       // 记录所有事件
		NodeFilter:     nil,     // 记录所有节点
		BlockThreshold: 1000000, // 阻塞超过 1ms 才记录
	}
}

// TraceRecorder 记录 trace 事件
type TraceRecorder struct {
	events  []TraceEvent
	sources []TraceSource // 注册的数据源
	mu      sync.Mutex
	config  TracerConfig

	// 用于快速查找节点是否在过滤器中
	nodeFilterMap map[int]bool
}

// NewTraceRecorder 创建一个新的 trace recorder
func NewTraceRecorder(config TracerConfig) *TraceRecorder {
	tr := &TraceRecorder{
		events:  make([]TraceEvent, 0, 10000), // 预分配容量
		sources: make([]TraceSource, 0),       // 初始化数据源列表
		mu:      sync.Mutex{},
		config:  config,
	}

	// 构建节点过滤器 map
	if len(config.NodeFilter) > 0 {
		tr.nodeFilterMap = make(map[int]bool)
		for _, nodeID := range config.NodeFilter {
			tr.nodeFilterMap[nodeID] = true
		}
	}

	return tr
}

// TraceSource 定义了 trace 数据源接口
type TraceSource interface {
	// GetTraceEvents 获取不需要锁的本地事件副本
	GetTraceEvents() []TraceEvent
	ID() int
	Name() string
}

// RegisterSource 注册一个数据源 (Thread-Safe)
func (tr *TraceRecorder) RegisterSource(source TraceSource) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.sources = append(tr.sources, source)
}

// IsNodeTraced 检查节点 ID 是否应该被追踪
func (tr *TraceRecorder) IsNodeTraced(id int) bool {
	// 如果没有过滤器，默认追踪
	if len(tr.nodeFilterMap) == 0 {
		return true
	}
	return tr.nodeFilterMap[id]
}

// RecordComplete 记录一个完整事件（有开始和结束时间）
func (tr *TraceRecorder) RecordComplete(
	name string,
	category string,
	pid int,
	tid int,
	start float64,
	end float64,
	args map[string]interface{},
) {
	// 从 args 中提取 cycle (如果有) 用于过滤
	simCycle := int64(0)
	if args != nil {
		if c, ok := args["cycle"]; ok {
			switch v := c.(type) {
			case int:
				simCycle = int64(v)
			case uint64:
				simCycle = int64(v)
			case int64:
				simCycle = v
			}
		}
	}

	if !tr.shouldRecord(pid, simCycle, int64(end-start)) {
		return
	}

	event := NewCompleteEvent(name, category, "", pid, tid, start, end, args)

	tr.mu.Lock()
	tr.events = append(tr.events, event)
	tr.mu.Unlock()
}

// RecordInstant 记录一个瞬时事件（如阻塞点）
func (tr *TraceRecorder) RecordInstant(
	name string,
	category string,
	pid int,
	tid int,
	cycle float64,
	args map[string]interface{},
) {
	// 从 args 中提取 cycle (如果有) 用于过滤
	simCycle := int64(0)
	if args != nil {
		if c, ok := args["cycle"]; ok {
			switch v := c.(type) {
			case int:
				simCycle = int64(v)
			case uint64:
				simCycle = int64(v)
			case int64:
				simCycle = v
			}
		}
	}

	if !tr.shouldRecord(pid, simCycle, 0) {
		return
	}

	event := NewInstantEvent(name, category, pid, tid, cycle, args)

	tr.mu.Lock()
	tr.events = append(tr.events, event)
	tr.mu.Unlock()
}

// RecordMetadata 记录元数据（如进程名、线程名）
func (tr *TraceRecorder) RecordMetadata(name string, pid, tid int, args map[string]interface{}) {
	event := NewMetadataEvent(name, pid, tid, args)

	tr.mu.Lock()
	tr.events = append(tr.events, event)
	tr.mu.Unlock()
}

// shouldRecord 判断是否应该记录此事件
func (tr *TraceRecorder) shouldRecord(pid int, cycle int64, duration int64) bool {
	if !tr.config.Enabled {
		return false
	}

	// 检查 StartCycle
	if tr.config.StartCycle > 0 && cycle < int64(tr.config.StartCycle) {
		return false
	}

	// 检查 EndCycle
	if tr.config.EndCycle > 0 && cycle > int64(tr.config.EndCycle) {
		return false
	}

	// 检查采样率
	if tr.config.SampleRate > 1 && cycle%int64(tr.config.SampleRate) != 0 {
		return false
	}

	// 检查最小持续时间
	if tr.config.MinDuration > 0 && duration < tr.config.MinDuration {
		return false
	}

	// 检查节点过滤器
	if len(tr.nodeFilterMap) > 0 {
		if !tr.nodeFilterMap[pid] {
			return false
		}
	}

	return true
}

// IsCycleTraced 检查特定周期是否应该被记录 (用于本地缓冲优化)
func (tr *TraceRecorder) IsCycleTraced(cycle int64) bool {
	if !tr.config.Enabled {
		return false
	}
	if tr.config.StartCycle > 0 && cycle < int64(tr.config.StartCycle) {
		return false
	}
	if tr.config.EndCycle > 0 && cycle > int64(tr.config.EndCycle) {
		return false
	}
	return true
}

// GetEvents 获取所有记录的事件（用于导出）
// 合并主 Buffer 和所有注册 Source 的数据
func (tr *TraceRecorder) GetEvents() []TraceEvent {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	// 1. 估算总大小
	totalCount := len(tr.events)
	// 简单估算，不做精确预分配以避免遍历开销
	// 如果需要精确，可以遍历 sources 调用 len(s.GetTraceEvents())，但这可能更慢

	// 2. 复制主 Buffer
	allEvents := make([]TraceEvent, 0, totalCount)
	allEvents = append(allEvents, tr.events...)

	// 3. 收集所有 Source 的数据
	for _, src := range tr.sources {
		sourceEvents := src.GetTraceEvents()
		allEvents = append(allEvents, sourceEvents...)

		// 自动生成 Metadata 事件 (Thread Name, Process Name)
		// 如果 Source 提供了 Name，我们应该生成 Metadata
		if src.Name() != "" {
			// Process Name
			allEvents = append(allEvents, NewMetadataEvent("process_name", src.ID(), 0, map[string]interface{}{
				"name": src.Name(),
			}))
			// Thread Names (Convention: 1=Receive, 2=Process, 3=Send)
			allEvents = append(allEvents, NewMetadataEvent("thread_name", src.ID(), TidReceive, map[string]interface{}{"name": "Receive"}))
			allEvents = append(allEvents, NewMetadataEvent("thread_name", src.ID(), TidProcess, map[string]interface{}{"name": "Process"}))
			allEvents = append(allEvents, NewMetadataEvent("thread_name", src.ID(), TidSend, map[string]interface{}{"name": "Send"}))
		}
	}

	// 4. 按时间排序 (Chrome Trace Viewer 不强制要求排序，但排序后更易读)
	// (可选，暂不实现，Chrome 会自己处理)

	return allEvents
}

// EventCount 返回已记录的事件数量
func (tr *TraceRecorder) EventCount() int {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return len(tr.events)
}

// Clear 清空所有记录的事件
func (tr *TraceRecorder) Clear() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.events = tr.events[:0]
}

// Enable 启用追踪
func (tr *TraceRecorder) Enable() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.config.Enabled = true
}

// Disable 禁用追踪
func (tr *TraceRecorder) Disable() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.config.Enabled = false
}

// IsEnabled 返回是否启用追踪
func (tr *TraceRecorder) IsEnabled() bool {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.config.Enabled
}
