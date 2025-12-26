package dram

// schedule.go 实现DRAM调度逻辑
//
// 对应 ChampSim 的 schedule_packet 和 service_packet

// schedulePacket 调度算法：FR-FCFS (Row-Hit First, then FCFS)
//
// 对应 ChampSim 的 DRAM_CHANNEL::schedule_packet()
//
// 优先级：
// 1. Bank空闲 > Bank忙
// 2. Row Buffer Hit > Row Buffer Miss
// 3. 年龄（ReadyTime越小越优先）
//
// 返回：选中的请求（可能为nil）
func (dc *DRAMChannel) schedulePacket() *DRAMPacket {
	// 根据当前模式选择队列
	queue := dc.RQ
	if dc.writeMode {
		queue = dc.WQ
	}

	var bestPkt *DRAMPacket
	var bestPriority int = -1

	for _, pkt := range queue {
		if pkt == nil || pkt.Scheduled {
			continue
		}

		// 计算Bank索引
		bankIdx := dc.mapping.GetBankIndex(pkt.Address)
		bank := dc.bankRequest[bankIdx]

		// 计算优先级
		priority := 0

		// 1. Bank空闲优先
		if !bank.Valid && !bank.UnderRefresh {
			priority += 100
		} else {
			// Bank忙或正在refresh，跳过
			continue
		}

		// 2. Row Buffer Hit优先
		row := dc.mapping.GetRow(pkt.Address)
		if bank.OpenRow != nil && *bank.OpenRow == row {
			priority += 1000 // Row Buffer Hit 有很高的优先级
		}

		// 3. 年龄优先（FCFS）
		// ReadyTime越小越优先，用负数表示
		age := int(dc.currentCycle - pkt.ReadyTime)
		priority += age

		// 更新最佳选择
		if priority > bestPriority {
			bestPriority = priority
			bestPkt = pkt
		}
	}

	return bestPkt
}

// servicePacket 服务请求，计算延迟并设置Bank状态
//
// 对应 ChampSim 的 DRAM_CHANNEL::service_packet()
//
// DDR时序：
// - Row Buffer Hit: tCAS
// - Row Buffer Miss (idle bank): tRCD + tCAS
// - Row Buffer Miss (active bank): tRP + tRCD + tCAS
func (dc *DRAMChannel) servicePacket(pkt *DRAMPacket) bool {
	if pkt == nil {
		return false
	}

	// 检查请求是否就绪
	if pkt.ReadyTime > dc.currentCycle {
		return false
	}

	// 获取Bank索引
	bankIdx := dc.mapping.GetBankIndex(pkt.Address)
	bank := dc.bankRequest[bankIdx]

	// Bank必须空闲且不在refresh
	if bank.Valid || bank.UnderRefresh {
		return false
	}

	// 获取行号
	row := dc.mapping.GetRow(pkt.Address)

	// 计算延迟
	var latency uint64

	if bank.OpenRow != nil && *bank.OpenRow == row {
		// Row Buffer Hit
		latency = dc.config.TCAS
		bank.RowBufferHit = true

		// 更新统计
		if dc.writeMode {
			dc.stats.WQRowBufferHit++
		} else {
			dc.stats.RQRowBufferHit++
		}
	} else {
		// Row Buffer Miss
		bank.RowBufferHit = false

		if bank.OpenRow != nil {
			// Bank有打开的行，需要先Precharge
			latency = dc.config.TRP + dc.config.TRCD + dc.config.TCAS
		} else {
			// Bank是idle的，不需要Precharge
			latency = dc.config.TRCD + dc.config.TCAS
		}

		// 更新打开的行
		rowCopy := row
		bank.OpenRow = &rowCopy

		// 更新统计
		if dc.writeMode {
			dc.stats.WQRowBufferMiss++
		} else {
			dc.stats.RQRowBufferMiss++
		}
	}

	// 设置Bank状态
	bank.Valid = true
	bank.Pkt = pkt
	bank.ReadyTime = dc.currentCycle + latency

	// 标记请求已调度
	pkt.Scheduled = true

	return true
}

// removeFromQueue 从队列中移除请求
func (dc *DRAMChannel) removeFromQueue(pkt *DRAMPacket) {
	// 从RQ中移除
	for i, p := range dc.RQ {
		if p == pkt {
			dc.RQ = append(dc.RQ[:i], dc.RQ[i+1:]...)
			return
		}
	}

	// 从WQ中移除
	for i, p := range dc.WQ {
		if p == pkt {
			dc.WQ = append(dc.WQ[:i], dc.WQ[i+1:]...)
			return
		}
	}
}
