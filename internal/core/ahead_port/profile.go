//go:build profile

package ahead_port

import (
	"sync/atomic"
	_ "unsafe" // for go:linkname
)

// PortProfile 端口性能统计数据
// 每个 Port 独立维护，避免 false sharing
type PortProfile struct {
	// WaitDone 统计
	waitDoneFastPath  atomic.Uint64 // 快速路径成功次数（无需阻塞）
	waitDoneSlowPath  atomic.Uint64 // 慢速路径次数（发生阻塞）
	waitDoneBlockTime atomic.Uint64 // WaitDone 总阻塞时间（cycles）

	// Ready 统计
	readyFastPath  atomic.Uint64 // 快速路径成功次数
	readySlowPath  atomic.Uint64 // 慢速路径次数
	readyBlockTime atomic.Uint64 // Ready 总阻塞时间（cycles）

	// 填充避免 false sharing (假设 cache line 64 字节)
	_ [64 - 6*8]byte
}

// RecordWaitDoneFast 记录 WaitDone 快速路径
func (p *PortProfile) RecordWaitDoneFast() {
	p.waitDoneFastPath.Add(1)
}

// RecordWaitDoneSlow 记录 WaitDone 慢速路径及阻塞时间
func (p *PortProfile) RecordWaitDoneSlow(blockCycles uint64) {
	p.waitDoneSlowPath.Add(1)
	p.waitDoneBlockTime.Add(blockCycles)
}

// RecordReadyFast 记录 Ready 快速路径
func (p *PortProfile) RecordReadyFast() {
	p.readyFastPath.Add(1)
}

// RecordReadySlow 记录 Ready 慢速路径及阻塞时间
func (p *PortProfile) RecordReadySlow(blockCycles uint64) {
	p.readySlowPath.Add(1)
	p.readyBlockTime.Add(blockCycles)
}

// GetWaitDoneStats 获取 WaitDone 统计
func (p *PortProfile) GetWaitDoneStats() (fastPath, slowPath, blockTime uint64) {
	return p.waitDoneFastPath.Load(), p.waitDoneSlowPath.Load(), p.waitDoneBlockTime.Load()
}

// GetReadyStats 获取 Ready 统计
func (p *PortProfile) GetReadyStats() (fastPath, slowPath, blockTime uint64) {
	return p.readyFastPath.Load(), p.readySlowPath.Load(), p.readyBlockTime.Load()
}

// GetCPUCycles 使用 runtime.nanotime 获取高精度时间戳
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// GetCPUCycles 返回当前 CPU 周期数（纳秒精度）
func GetCPUCycles() uint64 {
	return uint64(nanotime())
}
