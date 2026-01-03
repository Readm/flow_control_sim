//go:build !profile && !trace

package node

// NodeProfile 空结构体（禁用 profiling 时零开销）
type NodeProfile struct{}

// 所有方法都是空操作
func (p *NodeProfile) RecordProcessExec(cycles uint64)   {}
func (p *NodeProfile) GetProcessStats() (uint64, uint64) { return 0, 0 }
func (p *NodeProfile) GetAvgProcessTime() uint64         { return 0 }

// GetCPUCycles 返回 0（禁用时）
func GetCPUCycles() uint64 {
	return 0
}
