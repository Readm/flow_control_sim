package dram

// operate.go 实现DRAM主循环和数据总线逻辑
//
// 对应 ChampSim 的 DRAM_CHANNEL::operate() 相关函数

// operate DRAM主循环
//
// 对应 ChampSim 的 DRAM_CHANNEL::operate()
//
// 执行顺序（与ChampSim一致）：
// 1. checkWriteCollision() - 检查写冲突
// 2. checkReadCollision() - 检查读冲突
// 3. finishDBusRequest() - 完成数据总线上的请求
// 4. swapWriteMode() - Write/Read模式切换
// 5. scheduleRefresh() - 处理Refresh (简化版暂时跳过)
// 6. populateDBus() - 调度新请求到数据总线
// 7. servicePacket(schedulePacket()) - 服务选中的请求
func (dc *DRAMChannel) operate() {
	// 1. 检查冲突 (简化版：暂时跳过)
	// dc.checkWriteCollision()
	// dc.checkReadCollision()

	// 2. 完成数据总线上的请求
	dc.finishDBusRequest()

	// 3. Write/Read模式切换
	dc.swapWriteMode()

	// 4. Refresh处理 (简化版：暂时跳过)
	// dc.scheduleRefresh()

	// 5. 调度新请求到数据总线
	dc.populateDBus()

	// 6. 服务选中的请求
	pkt := dc.schedulePacket()
	dc.servicePacket(pkt)
}

// finishDBusRequest 完成数据总线上的请求
//
// 对应 ChampSim 的 DRAM_CHANNEL::finish_dbus_request()
//
// 检查activeRequest是否完成，如果完成：
// 1. 调用callback返回响应
// 2. 清除Bank状态
// 3. 从队列中移除请求
func (dc *DRAMChannel) finishDBusRequest() {
	if dc.activeRequest == nil {
		return
	}

	// 检查是否就绪
	if dc.activeRequest.ReadyTime > dc.currentCycle {
		return
	}

	pkt := dc.activeRequest.Pkt

	// 调用callback返回响应
	if pkt.Callback != nil {
		pkt.Callback(pkt.Address, pkt.Data, dc.currentCycle)
	}

	// 清除Bank状态
	dc.activeRequest.Valid = false
	dc.activeRequest.Pkt = nil

	// 清除activeRequest
	dc.activeRequest = nil

	// 从队列中移除
	dc.removeFromQueue(pkt)
}

// populateDBus 调度请求到数据总线
//
// 对应 ChampSim 的 DRAM_CHANNEL::populate_dbus()
//
// 查找ready_time最早的有效Bank请求，放到数据总线上
func (dc *DRAMChannel) populateDBus() {
	// 数据总线必须空闲
	if dc.activeRequest != nil {
		return
	}

	// 数据总线必须可用
	if dc.dbusAvailable > dc.currentCycle {
		dc.stats.DBusCongested++
		dc.stats.DBusCycles += (dc.dbusAvailable - dc.currentCycle)
		return
	}

	// 查找ready_time最早的有效请求
	var nextRequest *BankRequest
	var minReadyTime uint64 = ^uint64(0) // 最大值

	for _, bank := range dc.bankRequest {
		if !bank.Valid {
			continue
		}

		if bank.ReadyTime <= dc.currentCycle && bank.ReadyTime < minReadyTime {
			minReadyTime = bank.ReadyTime
			nextRequest = bank
		}
	}

	if nextRequest == nil {
		return
	}

	// 放到数据总线上
	dc.activeRequest = nextRequest

	// 计算数据总线返回时间
	// 简化版：固定延迟 (实际ChampSim会根据burst length计算)
	burstTime := uint64(8) // 假设burst length = 8 cycles
	dc.activeRequest.ReadyTime = dc.currentCycle + burstTime

	// 更新数据总线可用时间
	// 简化版：不考虑bankgroup冲突
	dc.dbusAvailable = dc.currentCycle + burstTime
}

// swapWriteMode Write/Read模式切换
//
// 对应 ChampSim 的 DRAM_CHANNEL::swap_write_mode()
//
// 切换条件：
// - 从Read切换到Write: WQ占用率 >= 7/8 或 (RQ为空 且 WQ非空)
// - 从Write切换到Read: WQ为空 或 (RQ非空 且 WQ占用率 < 6/8)
func (dc *DRAMChannel) swapWriteMode() {
	// 计算队列占用率
	wqOccupancy := len(dc.WQ)
	rqOccupancy := len(dc.RQ)

	wqHighWM := int(dc.config.WQSize) * 7 / 8 // 7/8
	wqLowWM := int(dc.config.WQSize) * 6 / 8  // 6/8

	// 判断是否需要切换模式
	shouldSwitch := false

	if !dc.writeMode {
		// 当前是Read模式，判断是否切换到Write模式
		if wqOccupancy >= wqHighWM || (rqOccupancy == 0 && wqOccupancy > 0) {
			shouldSwitch = true
		}
	} else {
		// 当前是Write模式，判断是否切换到Read模式
		if wqOccupancy == 0 || (rqOccupancy > 0 && wqOccupancy < wqLowWM) {
			shouldSwitch = true
		}
	}

	if !shouldSwitch {
		return
	}

	// 切换模式
	dc.writeMode = !dc.writeMode

	// 重置已调度的请求（除了在数据总线上的）
	// 这样它们可以重新被调度
	for _, bank := range dc.bankRequest {
		if bank != dc.activeRequest && bank.Valid {
			// 保留打开的行（如果ReadyTime很近的话）
			if bank.ReadyTime >= dc.currentCycle+dc.config.TCAS {
				// 行还很"新鲜"，保留
			} else {
				// 关闭行
				bank.OpenRow = nil
			}

			// 重置Bank状态
			bank.Valid = false
			if bank.Pkt != nil {
				bank.Pkt.Scheduled = false
				bank.Pkt.ReadyTime = dc.currentCycle
			}
			bank.Pkt = nil
		}
	}

	// 添加模式切换延迟
	// 简化版：固定延迟
	turnAroundTime := dc.config.TRAS
	if dc.activeRequest != nil {
		dc.dbusAvailable = dc.activeRequest.ReadyTime + turnAroundTime
	} else {
		dc.dbusAvailable = dc.currentCycle + turnAroundTime
	}
}

// scheduleRefresh Refresh处理 (简化版：暂时跳过)
//
// 对应 ChampSim 的 DRAM_CHANNEL::schedule_refresh()
//
// TODO: 实现完整的Refresh机制
func (dc *DRAMChannel) scheduleRefresh() {
	// 简化版：暂时不实现Refresh
	// ChampSim每隔tREF时间refresh一部分行
}

// checkWriteCollision 检查写冲突 (简化版：暂时跳过)
//
// 对应 ChampSim 的 DRAM_CHANNEL::check_write_collision()
func (dc *DRAMChannel) checkWriteCollision() {
	// 简化版：暂时不实现
}

// checkReadCollision 检查读冲突 (简化版：暂时跳过)
//
// 对应 ChampSim 的 DRAM_CHANNEL::check_read_collision()
func (dc *DRAMChannel) checkReadCollision() {
	// 简化版：暂时不实现
}
