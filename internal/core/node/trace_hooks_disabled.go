//go:build !trace

package node

// traceReceive 禁用版本（空操作，编译器会优化掉）
func (n *BaseNode) traceReceive(start, end int64, cycle uint64, packetCount int) {}

// traceProcess 禁用版本（空操作）
func (n *BaseNode) traceProcess(start, end int64, cycle uint64) {}

// traceSend 禁用版本（空操作）
func (n *BaseNode) traceSend(start, end int64, cycle uint64, sentCount int) {}
