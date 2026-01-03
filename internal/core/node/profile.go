//go:build profile || trace

package node

import (
	"sync/atomic"
	_ "unsafe" // for go:linkname
)

// NodeProfile 节点性能统计数据
// 只记录 Handler.Process 的执行时间，不包含 InputQueue/OutputQueue 的 Tick 时间
type NodeProfile struct {
	processExecTime atomic.Uint64 // Process 执行总时间（cycles）
	processCount    atomic.Uint64 // Process 调用次数

	// 填充避免 false sharing
	_ [64 - 2*8]byte
}

// RecordProcessExec 记录 Process 执行时间
func (p *NodeProfile) RecordProcessExec(cycles uint64) {
	p.processExecTime.Add(cycles)
	p.processCount.Add(1)
}

// GetProcessStats 获取 Process 统计
// 返回: (总时间, 调用次数)
func (p *NodeProfile) GetProcessStats() (totalTime, count uint64) {
	return p.processExecTime.Load(), p.processCount.Load()
}

// GetAvgProcessTime 获取平均 Process 时间
func (p *NodeProfile) GetAvgProcessTime() uint64 {
	count := p.processCount.Load()
	if count == 0 {
		return 0
	}
	return p.processExecTime.Load() / count
}

// GetCPUCycles 使用 runtime.nanotime 获取高精度时间戳
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// GetCPUCycles 返回当前 CPU 周期数（纳秒精度）
func GetCPUCycles() uint64 {
	return uint64(nanotime())
}
