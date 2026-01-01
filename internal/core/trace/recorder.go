//go:build trace

package trace

import (
	"sync"
)

// TracerConfig 配置 tracer 行为
type TracerConfig struct {
	// 是否启用追踪
	Enabled bool

	// 只记录前 N 个 cycles（0 表示不限制）
	MaxCycles int

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
		MaxCycles:      1000,      // 默认只记录前 1000 个 cycles
		SampleRate:     1,         // 每个 cycle 都记录
		MinDuration:    0,         // 记录所有事件
		NodeFilter:     nil,       // 记录所有节点
		BlockThreshold: 1000000,   // 阻塞超过 1ms 才记录
	}
}

// TraceRecorder 记录 trace 事件
type TraceRecorder struct {
	events []TraceEvent
	mu     sync.Mutex
	config TracerConfig

	// 用于快速查找节点是否在过滤器中
	nodeFilterMap map[int]bool
}

// NewTraceRecorder 创建一个新的 trace recorder
func NewTraceRecorder(config TracerConfig) *TraceRecorder {
	tr := &TraceRecorder{
		events: make([]TraceEvent, 0, 10000), // 预分配容量
		config: config,
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

// RecordComplete 记录一个完整事件（有开始和结束时间）
func (tr *TraceRecorder) RecordComplete(
	name string,
	category string,
	pid int,
	tid int,
	startCycle int64,
	endCycle int64,
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

	if !tr.shouldRecord(pid, simCycle, endCycle-startCycle) {
		return
	}

	event := NewCompleteEvent(name, category, pid, tid, startCycle, endCycle, args)

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
	cycle int64,
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

	// 检查 MaxCycles 限制
	if tr.config.MaxCycles > 0 && cycle > int64(tr.config.MaxCycles) {
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

// GetEvents 获取所有记录的事件（用于导出）
func (tr *TraceRecorder) GetEvents() []TraceEvent {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	// 返回副本以避免并发问题
	events := make([]TraceEvent, len(tr.events))
	copy(events, tr.events)
	return events
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
