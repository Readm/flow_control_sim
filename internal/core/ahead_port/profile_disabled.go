//go:build !profile

package ahead_port

// PortProfile 空结构体（禁用 profiling 时零开销）
type PortProfile struct{}

// 所有方法都是空操作，编译器会内联优化掉

func (p *PortProfile) RecordWaitDoneFast()                    {}
func (p *PortProfile) RecordWaitDoneSlow(blockCycles uint64)  {}
func (p *PortProfile) RecordReadyFast()                       {}
func (p *PortProfile) RecordReadySlow(blockCycles uint64)     {}
func (p *PortProfile) GetWaitDoneStats() (uint64, uint64, uint64) { return 0, 0, 0 }
func (p *PortProfile) GetReadyStats() (uint64, uint64, uint64)    { return 0, 0, 0 }

// GetCPUCycles 返回 0（禁用时）
func GetCPUCycles() uint64 {
	return 0
}
