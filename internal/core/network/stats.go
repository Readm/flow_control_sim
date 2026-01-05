package network

import (
	"fmt"
	metrics "runtime/metrics"
	"sort"
	"strings"
)

// BlockStat 记录一对 Node 之间的同步阻塞统计信息
type BlockStat struct {
	SourceID int
	TargetID int

	// WaitDone 统计
	DoneBlocks    uint64 // WaitDone 阻塞次数（走慢速路径）
	DoneFast      uint64 // WaitDone 快速路径次数
	DoneBlockTime uint64 // WaitDone 总阻塞时间（cycles）

	// Ready 统计
	ReadyBlocks    uint64 // Ready 阻塞次数（走慢速路径）
	ReadyFast      uint64 // Ready 快速路径次数
	ReadyBlockTime uint64 // Ready 总阻塞时间（cycles）

	// 阻塞率
	DoneBlockRate  float64 // WaitDone 阻塞率 (%)
	ReadyBlockRate float64 // Ready 阻塞率 (%)
}

// CollectSyncProfile 收集所有 Link 的同步阻塞统计信息
func (n *Network) CollectSyncProfile() []BlockStat {
	var stats []BlockStat

	for _, lk := range n.links {
		// 获取 Link 的两个 Port
		upstreamPort := lk.GetUpstreamPort()     // OutputQueue -> Link
		downstreamPort := lk.GetDownstreamPort() // Link -> InputQueue

		if upstreamPort == nil && downstreamPort == nil {
			continue // 跳过没有 Port 的 Link
		}

		var sourceID, targetID int
		var doneBlocks, doneFast, doneBlockTime uint64
		var readyBlocks, readyFast, readyBlockTime uint64

		// 从任一 Port 获取 Node ID
		if upstreamPort != nil {
			sourceID = upstreamPort.SourceNodeID()
			targetID = upstreamPort.TargetNodeID()
			// WaitDone 统计来自 upstreamPort (Link 调用 Receive)
			doneFast, doneBlocks, doneBlockTime = upstreamPort.GetWaitDoneProfile()
		} else {
			sourceID = downstreamPort.SourceNodeID()
			targetID = downstreamPort.TargetNodeID()
		}

		// Ready 统计来自 downstreamPort (Link 调用 TrySend)
		if downstreamPort != nil {
			readyFast, readyBlocks, readyBlockTime = downstreamPort.GetReadyProfile()
		}

		// 计算阻塞率
		doneTotal := doneBlocks + doneFast
		doneBlockRate := 0.0
		if doneTotal > 0 {
			doneBlockRate = float64(doneBlocks) / float64(doneTotal) * 100
		}

		readyTotal := readyBlocks + readyFast
		readyBlockRate := 0.0
		if readyTotal > 0 {
			readyBlockRate = float64(readyBlocks) / float64(readyTotal) * 100
		}

		// 只记录有阻塞的统计
		if doneBlocks > 0 || readyBlocks > 0 {
			stats = append(stats, BlockStat{
				SourceID:       sourceID,
				TargetID:       targetID,
				DoneBlocks:     doneBlocks,
				DoneFast:       doneFast,
				DoneBlockTime:  doneBlockTime,
				ReadyBlocks:    readyBlocks,
				ReadyFast:      readyFast,
				ReadyBlockTime: readyBlockTime,
				DoneBlockRate:  doneBlockRate,
				ReadyBlockRate: readyBlockRate,
			})
		}
	}

	// 按 WaitDone 阻塞次数降序排序
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].DoneBlocks > stats[j].DoneBlocks
	})

	return stats
}

// PrintSyncProfile 打印同步阻塞统计信息
func (n *Network) PrintSyncProfile() {
	stats := n.CollectSyncProfile()

	if len(stats) == 0 {
		fmt.Println("\n===== 同步阻塞 Profile =====")
		fmt.Println("没有检测到同步阻塞")
		return
	}

	fmt.Println("\n===== 同步阻塞 Profile (按 WaitDone 阻塞次数排序) =====")
	fmt.Printf("%-10s %-10s %-15s %-15s %-12s %-15s %-15s %-12s\n",
		"Source", "Target",
		"DoneBlock", "DoneFast", "BlockRate%",
		"ReadyBlock", "ReadyFast", "BlockRate%")
	fmt.Println(strings.Repeat("-", 110))

	for _, s := range stats {
		fmt.Printf("%-10d %-10d %-15d %-15d %10.2f%%   %-15d %-15d %10.2f%%\n",
			s.SourceID, s.TargetID,
			s.DoneBlocks, s.DoneFast, s.DoneBlockRate,
			s.ReadyBlocks, s.ReadyFast, s.ReadyBlockRate)
	}

	// 汇总统计
	var totalDoneBlocks, totalDoneFast, totalReadyBlocks, totalReadyFast uint64
	for _, s := range stats {
		totalDoneBlocks += s.DoneBlocks
		totalDoneFast += s.DoneFast
		totalReadyBlocks += s.ReadyBlocks
		totalReadyFast += s.ReadyFast
	}

	totalDone := totalDoneBlocks + totalDoneFast
	totalReady := totalReadyBlocks + totalReadyFast

	doneRate := 0.0
	if totalDone > 0 {
		doneRate = float64(totalDoneBlocks) / float64(totalDone) * 100
	}

	readyRate := 0.0
	if totalReady > 0 {
		readyRate = float64(totalReadyBlocks) / float64(totalReady) * 100
	}

	fmt.Println(strings.Repeat("-", 110))
	fmt.Printf("%-10s %-10s %-15d %-15d %10.2f%%   %-15d %-15d %10.2f%%\n",
		"TOTAL", "",
		totalDoneBlocks, totalDoneFast, doneRate,
		totalReadyBlocks, totalReadyFast, readyRate)
	fmt.Println()
}

// PrintTopBlockers 打印阻塞次数最多的前 N 个连接
func (n *Network) PrintTopBlockers(topN int) {
	stats := n.CollectSyncProfile()

	if len(stats) == 0 {
		fmt.Println("\n===== Top Blockers =====")
		fmt.Println("没有检测到同步阻塞")
		return
	}

	if topN > len(stats) {
		topN = len(stats)
	}

	fmt.Printf("\n===== Top %d Blockers (WaitDone) =====\n", topN)
	fmt.Printf("%-10s %-10s %-15s %-12s\n",
		"Source", "Target", "DoneBlock", "BlockRate%")
	fmt.Println(strings.Repeat("-", 50))

	for i := 0; i < topN; i++ {
		s := stats[i]
		fmt.Printf("%-10d %-10d %-15d %10.2f%%\n",
			s.SourceID, s.TargetID,
			s.DoneBlocks, s.DoneBlockRate)
	}
	fmt.Println()
}

// PrintBlockingTimeProfile 打印阻塞时间统计（按 WaitDone 阻塞时间排序）
func (n *Network) PrintBlockingTimeProfile(topN int) {
	stats := n.CollectSyncProfile()

	if len(stats) == 0 {
		fmt.Println("\n===== 阻塞时间 Profile =====")
		fmt.Println("没有检测到同步阻塞")
		return
	}

	// 按 WaitDone 阻塞时间降序排序
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].DoneBlockTime > stats[j].DoneBlockTime
	})

	if topN > len(stats) || topN <= 0 {
		topN = len(stats)
	}

	fmt.Printf("\n===== Top %d 阻塞时间 (按 WaitDone 阻塞时间排序) =====\n", topN)
	fmt.Printf("%-10s %-10s %-15s %-15s %-15s %-15s\n",
		"Source", "Target",
		"DoneBlockTime", "AvgBlockTime",
		"ReadyBlockTime", "AvgBlockTime")
	fmt.Println(strings.Repeat("-", 90))

	for i := 0; i < topN; i++ {
		s := stats[i]
		avgDoneBlockTime := uint64(0)
		if s.DoneBlocks > 0 {
			avgDoneBlockTime = s.DoneBlockTime / s.DoneBlocks
		}
		avgReadyBlockTime := uint64(0)
		if s.ReadyBlocks > 0 {
			avgReadyBlockTime = s.ReadyBlockTime / s.ReadyBlocks
		}
		fmt.Printf("%-10d %-10d %-15d %-15d %-15d %-15d\n",
			s.SourceID, s.TargetID,
			s.DoneBlockTime, avgDoneBlockTime,
			s.ReadyBlockTime, avgReadyBlockTime)
	}

	// 汇总统计
	var totalDoneBlockTime, totalReadyBlockTime uint64
	var totalDoneBlocks, totalReadyBlocks uint64
	for _, s := range stats {
		totalDoneBlockTime += s.DoneBlockTime
		totalReadyBlockTime += s.ReadyBlockTime
		totalDoneBlocks += s.DoneBlocks
		totalReadyBlocks += s.ReadyBlocks
	}

	avgTotalDoneBlockTime := uint64(0)
	if totalDoneBlocks > 0 {
		avgTotalDoneBlockTime = totalDoneBlockTime / totalDoneBlocks
	}
	avgTotalReadyBlockTime := uint64(0)
	if totalReadyBlocks > 0 {
		avgTotalReadyBlockTime = totalReadyBlockTime / totalReadyBlocks
	}

	fmt.Println(strings.Repeat("-", 90))
	fmt.Printf("%-10s %-10s %-15d %-15d %-15d %-15d\n",
		"TOTAL", "",
		totalDoneBlockTime, avgTotalDoneBlockTime,
		totalReadyBlockTime, avgTotalReadyBlockTime)
	fmt.Println()
}

// ===== 节点执行时间 Profiling =====

// NodeTimeStat 记录一个节点的执行时间统计信息
type NodeTimeStat struct {
	NodeID            int
	TotalCycles       uint64  // 累计处理时间（CPU cycles）
	ProcessCount      uint64  // 处理次数
	AvgCycles         uint64  // 平均每次处理时间（cycles）
	AvgCyclesPerCount float64 // 平均每次处理时间（用于排序，浮点数）
}

// CollectNodeTimeProfile 收集所有节点的执行时间统计
func (n *Network) CollectNodeTimeProfile() []NodeTimeStat {
	var stats []NodeTimeStat

	for _, handle := range n.nodeList {
		node := handle.Node

		// 尝试获取 profiling 数据（需要 type assertion）
		// 使用接口检查来获取 profiling 方法
		type profilable interface {
			TotalProcessCycles() uint64
			ProcessCount() uint64
			AvgProcessCycles() uint64
		}

		if p, ok := node.(profilable); ok {
			totalCycles := p.TotalProcessCycles()
			processCount := p.ProcessCount()
			avgCycles := p.AvgProcessCycles()

			if processCount > 0 {
				stats = append(stats, NodeTimeStat{
					NodeID:            node.ID(),
					TotalCycles:       totalCycles,
					ProcessCount:      processCount,
					AvgCycles:         avgCycles,
					AvgCyclesPerCount: float64(totalCycles) / float64(processCount),
				})
			}
		}
	}

	// 按平均处理时间降序排序（最慢的在前）
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].AvgCycles > stats[j].AvgCycles
	})

	return stats
}

// PrintNodeTimeProfile 打印节点执行时间统计
func (n *Network) PrintNodeTimeProfile() {
	stats := n.CollectNodeTimeProfile()

	if len(stats) == 0 {
		fmt.Println("\n===== 节点执行时间 Profile =====")
		fmt.Println("没有收集到节点执行时间数据")
		return
	}

	fmt.Println("\n===== 节点执行时间 Profile (按平均处理时间排序) =====")
	fmt.Printf("%-10s %-20s %-15s %-15s\n",
		"NodeID", "TotalCycles", "Count", "AvgCycles")
	fmt.Println(strings.Repeat("-", 65))

	for _, s := range stats {
		fmt.Printf("%-10d %-20d %-15d %-15d\n",
			s.NodeID,
			s.TotalCycles,
			s.ProcessCount,
			s.AvgCycles)
	}
	fmt.Println()
}

// PrintTopSlowestNodes 打印处理最慢的前 N 个节点
func (n *Network) PrintTopSlowestNodes(topN int) {
	stats := n.CollectNodeTimeProfile()

	if len(stats) == 0 {
		fmt.Println("\n===== Top Slowest Nodes =====")
		fmt.Println("没有收集到节点执行时间数据")
		return
	}

	if topN > len(stats) {
		topN = len(stats)
	}

	fmt.Printf("\n===== Top %d Slowest Nodes (按平均处理时间) =====\n", topN)
	fmt.Printf("%-10s %-15s %-15s\n",
		"NodeID", "AvgCycles", "Count")
	fmt.Println(strings.Repeat("-", 45))

	for i := 0; i < topN; i++ {
		s := stats[i]
		fmt.Printf("%-10d %-15d %-15d\n",
			s.NodeID,
			s.AvgCycles,
			s.ProcessCount)
	}
	fmt.Println()
}

// ===== 三阶段详细时间 Profiling =====

// NodeDetailedTimeStat 记录节点的三阶段执行时间统计
type NodeDetailedTimeStat struct {
	NodeID       int
	ProcessCount uint64

	// 三阶段时间
	TotalReceive uint64 // Receive 总时间（包含同步等待）
	TotalProcess uint64 // Process 总时间（实际计算）
	TotalSend    uint64 // Send 总时间（发送下游）

	// 平均时间
	AvgReceive uint64
	AvgProcess uint64
	AvgSend    uint64

	// 百分比
	ReceivePercent float64
	ProcessPercent float64
	SendPercent    float64
}

// CollectNodeDetailedTimeProfile 收集所有节点的三阶段时间统计
func (n *Network) CollectNodeDetailedTimeProfile() []NodeDetailedTimeStat {
	var stats []NodeDetailedTimeStat

	for _, handle := range n.nodeList {
		node := handle.Node

		// 使用接口检查来获取三阶段 profiling 方法
		type detailedProfilable interface {
			ProcessCount() uint64
			ReceiveCycles() uint64
			ProcessCycles() uint64
			SendCycles() uint64
			AvgReceiveCycles() uint64
			AvgProcessCyclesDetailed() uint64
			AvgSendCycles() uint64
		}

		if p, ok := node.(detailedProfilable); ok {
			processCount := p.ProcessCount()
			if processCount == 0 {
				continue
			}

			totalReceive := p.ReceiveCycles()
			totalProcess := p.ProcessCycles()
			totalSend := p.SendCycles()
			totalAll := totalReceive + totalProcess + totalSend

			receivePercent := 0.0
			processPercent := 0.0
			sendPercent := 0.0
			if totalAll > 0 {
				receivePercent = float64(totalReceive) / float64(totalAll) * 100
				processPercent = float64(totalProcess) / float64(totalAll) * 100
				sendPercent = float64(totalSend) / float64(totalAll) * 100
			}

			stats = append(stats, NodeDetailedTimeStat{
				NodeID:         node.ID(),
				ProcessCount:   processCount,
				TotalReceive:   totalReceive,
				TotalProcess:   totalProcess,
				TotalSend:      totalSend,
				AvgReceive:     p.AvgReceiveCycles(),
				AvgProcess:     p.AvgProcessCyclesDetailed(),
				AvgSend:        p.AvgSendCycles(),
				ReceivePercent: receivePercent,
				ProcessPercent: processPercent,
				SendPercent:    sendPercent,
			})
		}
	}

	// 按 Receive 时间降序排序（找出同步等待最多的节点）
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].AvgReceive > stats[j].AvgReceive
	})

	return stats
}

// PrintNodeDetailedTimeProfile 打印节点的三阶段时间统计
func (n *Network) PrintNodeDetailedTimeProfile(topN int) {
	stats := n.CollectNodeDetailedTimeProfile()

	if len(stats) == 0 {
		fmt.Println("\n===== 节点三阶段时间 Profile =====")
		fmt.Println("没有收集到三阶段时间数据")
		return
	}

	if topN > len(stats) {
		topN = len(stats)
	}

	fmt.Printf("\n===== Top %d 节点三阶段时间 Profile (按 Receive 时间排序) =====\n", topN)
	fmt.Printf("%-10s %-12s %-12s %-12s | %-10s %-10s %-10s\n",
		"NodeID", "AvgReceive", "AvgProcess", "AvgSend", "Recv%", "Proc%", "Send%")
	fmt.Println(strings.Repeat("-", 90))

	for i := 0; i < topN; i++ {
		s := stats[i]
		fmt.Printf("%-10d %-12d %-12d %-12d | %9.1f%% %9.1f%% %9.1f%%\n",
			s.NodeID,
			s.AvgReceive,
			s.AvgProcess,
			s.AvgSend,
			s.ReceivePercent,
			s.ProcessPercent,
			s.SendPercent)
	}
	fmt.Println()

	// 汇总统计
	var totalReceive, totalProcess, totalSend uint64
	for _, s := range stats {
		totalReceive += s.TotalReceive
		totalProcess += s.TotalProcess
		totalSend += s.TotalSend
	}

	totalAll := totalReceive + totalProcess + totalSend
	if totalAll > 0 {
		fmt.Println("===== 全局三阶段时间汇总 =====")
		fmt.Printf("Receive (含同步等待): %15d cycles (%5.1f%%)\n",
			totalReceive, float64(totalReceive)/float64(totalAll)*100)
		fmt.Printf("Process (实际计算):   %15d cycles (%5.1f%%)\n",
			totalProcess, float64(totalProcess)/float64(totalAll)*100)
		fmt.Printf("Send (发送下游):      %15d cycles (%5.1f%%)\n",
			totalSend, float64(totalSend)/float64(totalAll)*100)
		fmt.Printf("Total:               %15d cycles\n", totalAll)
		fmt.Println()
	}
}

// ===== 全局运行时性能统计 (Go 1.25+ runtime/metrics) =====

// GlobalRuntimeStat 记录从 Go 运行时获取的 CPU 统计信息
type GlobalRuntimeStat struct {
	TotalCPUSec    float64 // /cpu/classes/total:cpu-seconds
	IdleCPUSec     float64 // /cpu/classes/idle:cpu-seconds
	UserCPUSec     float64 // /cpu/classes/user:cpu-seconds
	GCTotalCPUSec  float64 // /cpu/classes/gc/total:cpu-seconds
	ScavengeCPUSec float64 // /cpu/classes/scavenge/total:cpu-seconds
}

// CollectGlobalRuntimeStats 从 runtime/metrics 获取全局 CPU 数据
func CollectGlobalRuntimeStats() GlobalRuntimeStat {
	// 定义我们需要读取的指标名称
	// 注意: 这些指标需要 Go 1.23+
	samples := []metrics.Sample{
		{Name: "/cpu/classes/total:cpu-seconds"},
		{Name: "/cpu/classes/idle:cpu-seconds"},
		{Name: "/cpu/classes/user:cpu-seconds"},
		{Name: "/cpu/classes/gc/total:cpu-seconds"},
		{Name: "/cpu/classes/scavenge/total:cpu-seconds"},
	}

	// 从运行时读取指标
	metrics.Read(samples)

	stat := GlobalRuntimeStat{}

	// 解析结果
	for _, s := range samples {
		val := 0.0
		if s.Value.Kind() == metrics.KindFloat64 {
			val = s.Value.Float64()
		}

		switch s.Name {
		case "/cpu/classes/total:cpu-seconds":
			stat.TotalCPUSec = val
		case "/cpu/classes/idle:cpu-seconds":
			stat.IdleCPUSec = val
		case "/cpu/classes/user:cpu-seconds":
			stat.UserCPUSec = val
		case "/cpu/classes/gc/total:cpu-seconds":
			stat.GCTotalCPUSec = val
		case "/cpu/classes/scavenge/total:cpu-seconds":
			stat.ScavengeCPUSec = val
		}
	}

	return stat
}

// PrintGlobalPerformanceSummary 打印全局性能摘要（结合 Runtime Metrics 和 Internal Profiling）
// baseline: 可选的基础统计数据，如果提供，则打印相对于 baseline 的增量数据
func (n *Network) PrintGlobalPerformanceSummary(baseline *GlobalRuntimeStat) {
	// 1. 获取 Runtime Metrics (宏观数据)
	current := CollectGlobalRuntimeStats()
	rStat := current

	// 如果提供了 baseline，计算增量
	if baseline != nil {
		rStat.TotalCPUSec = current.TotalCPUSec - baseline.TotalCPUSec
		rStat.IdleCPUSec = current.IdleCPUSec - baseline.IdleCPUSec
		rStat.UserCPUSec = current.UserCPUSec - baseline.UserCPUSec
		rStat.GCTotalCPUSec = current.GCTotalCPUSec - baseline.GCTotalCPUSec
		rStat.ScavengeCPUSec = current.ScavengeCPUSec - baseline.ScavengeCPUSec
	}

	// 如果 runtime metrics 数据全为 0 (可能是 Go 版本太低不支持，或刚启动)，则只打印内部数据
	hasRuntimeMetrics := rStat.TotalCPUSec > 0

	// 2. 获取 Internal Profiling (微观数据)
	// 汇总所有节点的 Process 时间
	nodeStats := n.CollectNodeDetailedTimeProfile()
	var totalProcessCycles uint64
	for _, s := range nodeStats {
		totalProcessCycles += s.TotalProcess
	}

	if !hasRuntimeMetrics {
		fmt.Printf("Global Stats: InternalLogic=%d Cycles (Runtime Metrics Unavailable)\n", totalProcessCycles)
		return
	}

	// 计算各部分占比
	total := rStat.TotalCPUSec
	if total == 0 {
		total = 1.0 // 避免除零
	}

	idlePct := (rStat.IdleCPUSec / total) * 100
	userPct := (rStat.UserCPUSec / total) * 100
	gcPct := (rStat.GCTotalCPUSec / total) * 100
	sysPct := (rStat.ScavengeCPUSec / total) * 100

	overheadPct := gcPct + sysPct

	// 简洁的一行输出
	fmt.Printf("CPU Stats: Idle=%.1f%% User=%.1f%% Sys=%.1f%%\n", idlePct, userPct, overheadPct)
}
