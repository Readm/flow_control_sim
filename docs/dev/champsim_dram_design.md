# ChampSim DRAM Controller 设计文档

## 1. 概述

本文档描述ChampSim DRAM Controller的Go语言实现，保持与原始ChampSim实现的逻辑一致性。

## 2. ChampSim DRAM架构

### 2.1 整体结构

```
MEMORY_CONTROLLER (管理多个Channel)
  └─ DRAM_CHANNEL (单个通道)
       ├─ RQ (Read Queue)
       ├─ WQ (Write Queue)
       ├─ Bank状态数组
       └─ 调度逻辑
```

### 2.2 核心组件

#### DRAM_ADDRESS_MAPPING (地址映射)
将物理地址分解为DRAM内部地址：
```
物理地址 → [Offset | Channel | Bankgroup | Bank | Column | Rank | Row]
```

**字段说明**:
- **Offset**: Block内偏移
- **Channel**: 通道索引 (多通道并行)
- **Bankgroup**: Bank组索引 (DDR4引入)
- **Bank**: Bank索引
- **Column**: 列地址
- **Rank**: Rank索引
- **Row**: 行地址

#### DRAM_CHANNEL (DRAM通道)

**请求队列**:
- `RQ`: Read Queue (读请求队列)
- `WQ`: Write Queue (写请求队列)

**Bank状态**:
```go
type BankRequest struct {
    Valid         bool        // 是否有有效请求
    RowBufferHit  bool        // 是否命中Row Buffer
    NeedRefresh   bool        // 是否需要refresh
    UnderRefresh  bool        // 是否正在refresh
    OpenRow       *uint64     // 当前打开的行号
    ReadyTime     uint64      // 就绪时间
    Pkt           *DRAMPacket // 请求包
}
```

**延迟参数** (DDR时序):
- `tRP`: Row Precharge time (关闭行延迟)
- `tRCD`: RAS to CAS Delay (激活到读写延迟)
- `tCAS`: CAS Latency (列访问延迟)
- `tRAS`: Row Active time (行激活时间)
- `tREF`: Refresh period (刷新周期)
- `tRFC`: Refresh cycle time (刷新延迟)

### 2.3 核心工作流程

#### 请求处理流程

```
1. 接收请求 (从上层Cache)
   ↓
2. 放入RQ或WQ队列
   ↓
3. 调度算法选择请求 (schedule_packet)
   ↓
4. 检查Bank状态
   ├─ Row Buffer Hit → 直接访问 (tCAS延迟)
   └─ Row Buffer Miss → Precharge + Activate + Access (tRP + tRCD + tCAS)
   ↓
5. 服务请求 (service_packet)
   ├─ 设置Bank状态
   └─ 计算就绪时间
   ↓
6. 放到数据总线 (populate_dbus)
   ↓
7. 完成请求 (finish_dbus_request)
   └─ 返回响应到Cache
```

#### operate() 主循环

```go
func (dc *DRAMChannel) Operate() {
    // 1. 检查Write冲突
    dc.checkWriteCollision()

    // 2. 检查Read冲突
    dc.checkReadCollision()

    // 3. 完成数据总线上的请求
    dc.finishDBusRequest()

    // 4. Write/Read模式切换
    dc.swapWriteMode()

    // 5. 处理Refresh
    dc.scheduleRefresh()

    // 6. 调度新请求到数据总线
    dc.populateDBus()

    // 7. 服务选中的请求
    pkt := dc.schedulePacket()
    dc.servicePacket(pkt)
}
```

## 3. Go实现策略

### 3.1 第一阶段：简化实现

**保留的核心机制**:
1. ✅ RQ/WQ队列管理
2. ✅ Bank状态管理
3. ✅ Row Buffer Hit/Miss检测
4. ✅ 基本延迟模型 (tRP, tRCD, tCAS, tRAS)
5. ✅ 调度逻辑 (FCFS或FR-FCFS)
6. ✅ 地址映射 (简化版)

**简化的部分**:
1. 🔸 单Channel (暂不支持多通道)
2. 🔸 简化的Refresh机制 (或暂时忽略)
3. 🔸 简化的Write/Read模式切换
4. 🔸 忽略Bankgroup冲突处理
5. 🔸 固定延迟值 (DDR4-2400标准)

### 3.2 默认配置

```go
// DDR4-2400 标准配置
type DRAMConfig struct {
    // 容量配置
    Channels   uint32 // 通道数 (先固定为1)
    Ranks      uint32 // Rank数 (1或2)
    BankGroups uint32 // Bank组数 (4)
    Banks      uint32 // 每组Bank数 (4)
    Rows       uint32 // 行数 (32K或64K)
    Columns    uint32 // 列数 (1K)

    // 队列大小
    RQSize uint32 // Read Queue大小 (64)
    WQSize uint32 // Write Queue大小 (64)

    // 延迟参数 (以cycles为单位)
    TRP  uint64 // Row Precharge (15ns → ~15 cycles)
    TRCD uint64 // RAS to CAS Delay (15ns → ~15 cycles)
    TCAS uint64 // CAS Latency (15ns → ~15 cycles)
    TRAS uint64 // Row Active time (35ns → ~35 cycles)

    // 数据宽度
    ChannelWidth uint32 // 通道宽度 (8 bytes)
}

// 默认DDR4-2400配置
func DefaultDRAMConfig() DRAMConfig {
    return DRAMConfig{
        Channels:     1,
        Ranks:        1,
        BankGroups:   4,
        Banks:        4,
        Rows:         32768, // 32K rows
        Columns:      1024,  // 1K columns
        RQSize:       64,
        WQSize:       64,
        TRP:          15,
        TRCD:         15,
        TCAS:         15,
        TRAS:         35,
        ChannelWidth: 8,
    }
}
```

### 3.3 数据结构设计

#### DRAMPacket (请求包)

```go
type DRAMPacket struct {
    // 地址信息
    Address  uint64 // 物理地址
    VAddress uint64 // 虚拟地址
    Data     uint64 // 数据

    // 请求信息
    InstrID      uint64 // 指令ID
    IsWrite      bool   // 是否写请求
    Scheduled    bool   // 是否已调度
    ReadyTime    uint64 // 就绪时间

    // 依赖跟踪
    InstrDependOnMe []uint64 // 依赖的指令列表

    // 回调
    Callback func(addr uint64, data uint64, cycle uint64)
}
```

#### BankRequest (Bank状态)

```go
type BankRequest struct {
    Valid        bool    // 是否有有效请求
    RowBufferHit bool    // 是否Row Buffer命中
    OpenRow      *uint64 // 当前打开的行 (nil表示没有打开的行)
    ReadyTime    uint64  // 就绪时间
    Pkt          *DRAMPacket // 关联的请求包
}
```

#### DRAMChannel

```go
type DRAMChannel struct {
    // 配置
    config DRAMConfig

    // 地址映射
    mapping *AddressMapping

    // 队列
    RQ []*DRAMPacket // Read Queue
    WQ []*DRAMPacket // Write Queue

    // Bank状态 (Ranks * BankGroups * Banks个)
    bankRequest []*BankRequest
    activeRequest *BankRequest // 当前在数据总线上的请求

    // 时序状态
    currentCycle uint64
    writeMode    bool   // 当前是否为写模式
    dbusAvailable uint64 // 数据总线可用时间

    // 统计信息
    stats DRAMStats
}
```

### 3.4 核心算法

#### 地址映射 (简化版)

```go
type AddressMapping struct {
    offsetBits     uint32
    channelBits    uint32
    bankgroupBits  uint32
    bankBits       uint32
    columnBits     uint32
    rankBits       uint32
    rowBits        uint32
}

func (m *AddressMapping) GetRow(addr uint64) uint64 {
    shift := m.offsetBits + m.channelBits + m.bankgroupBits +
             m.bankBits + m.columnBits + m.rankBits
    return addr >> shift
}

func (m *AddressMapping) GetBank(addr uint64) uint64 {
    shift := m.offsetBits + m.channelBits + m.bankgroupBits
    mask := (1 << m.bankBits) - 1
    return (addr >> shift) & uint64(mask)
}

// 类似方法获取其他字段...
```

#### 调度算法: FR-FCFS (Row-Hit First)

```go
func (dc *DRAMChannel) schedulePacket() *DRAMPacket {
    queue := dc.RQ
    if dc.writeMode {
        queue = dc.WQ
    }

    var best *DRAMPacket
    var bestPriority int = -1

    for _, pkt := range queue {
        if pkt == nil || pkt.Scheduled {
            continue
        }

        bankIdx := dc.getBankIndex(pkt.Address)
        bank := dc.bankRequest[bankIdx]

        // 计算优先级
        priority := 0
        if !bank.Valid {
            priority += 100 // Bank空闲
        }

        row := dc.mapping.GetRow(pkt.Address)
        if bank.OpenRow != nil && *bank.OpenRow == row {
            priority += 1000 // Row Buffer Hit
        }

        // 年龄优先 (FCFS)
        priority -= int(pkt.ReadyTime)

        if priority > bestPriority {
            bestPriority = priority
            best = pkt
        }
    }

    return best
}
```

#### 服务请求

```go
func (dc *DRAMChannel) servicePacket(pkt *DRAMPacket) {
    if pkt == nil || pkt.ReadyTime > dc.currentCycle {
        return
    }

    bankIdx := dc.getBankIndex(pkt.Address)
    bank := dc.bankRequest[bankIdx]

    if bank.Valid {
        return // Bank忙
    }

    row := dc.mapping.GetRow(pkt.Address)

    // 计算延迟
    var latency uint64
    if bank.OpenRow != nil && *bank.OpenRow == row {
        // Row Buffer Hit
        latency = dc.config.TCAS
        bank.RowBufferHit = true
        dc.stats.RowBufferHits++
    } else {
        // Row Buffer Miss
        if bank.OpenRow != nil {
            // 需要先Precharge
            latency = dc.config.TRP + dc.config.TRCD + dc.config.TCAS
        } else {
            // Bank是idle的
            latency = dc.config.TRCD + dc.config.TCAS
        }
        bank.RowBufferHit = false
        bank.OpenRow = &row
        dc.stats.RowBufferMisses++
    }

    // 设置Bank状态
    bank.Valid = true
    bank.Pkt = pkt
    bank.ReadyTime = dc.currentCycle + latency

    pkt.Scheduled = true
}
```

## 4. 接口设计

### 4.1 对外接口

```go
// AddRequest 添加请求到DRAM
// isWrite: true=写请求, false=读请求
// callback: 完成时的回调函数
func (dc *DRAMChannel) AddRequest(
    addr uint64,
    vaddr uint64,
    data uint64,
    instrID uint64,
    isWrite bool,
    callback func(addr uint64, data uint64, cycle uint64),
) bool {
    // 添加到RQ或WQ
}

// Tick 时钟推进
func (dc *DRAMChannel) Tick() {
    dc.currentCycle++
    dc.operate()
}

// GetStats 获取统计信息
func (dc *DRAMChannel) GetStats() DRAMStats {
    return dc.stats
}
```

### 4.2 与Cache集成接口

```go
// Cache在Miss时调用DRAM
func (c *SetAssociativeCache) handleMiss(...) {
    // ...

    // 向DRAM发送请求
    success := dram.AddRequest(
        blockAddr,
        vaddr,
        0,
        instrID,
        false, // read
        func(addr uint64, data uint64, cycle uint64) {
            // 数据返回时，调用Cache的HandleFill
            c.HandleFill(addr, data, cycle)
        },
    )

    // ...
}
```

## 5. 测试策略

### 5.1 单元测试

1. **地址映射测试**: 验证地址分解的正确性
2. **队列管理测试**: RQ/WQ的入队出队
3. **Bank状态测试**: Row Buffer Hit/Miss检测
4. **调度算法测试**: FR-FCFS优先级
5. **延迟计算测试**: 不同情况下的延迟

### 5.2 集成测试

1. **Cache+DRAM**: 验证Miss请求的处理
2. **端到端**: CPU+Cache+DRAM完整流程
3. **性能测试**: 验证Row Buffer命中率等指标

## 6. 实现计划

### 阶段1: 基础结构 (Day 1)
- [ ] 定义数据结构 (DRAMPacket, BankRequest, DRAMChannel)
- [ ] 实现地址映射
- [ ] 实现队列管理 (AddRequest)
- [ ] 基础测试

### 阶段2: 调度逻辑 (Day 1-2)
- [ ] 实现schedulePacket() (FR-FCFS)
- [ ] 实现servicePacket() (延迟计算)
- [ ] 实现populateDBus()
- [ ] 实现finishDBusRequest()
- [ ] 调度测试

### 阶段3: 主循环 (Day 2)
- [ ] 实现operate()主循环
- [ ] Write/Read模式切换 (简化版)
- [ ] 完整功能测试

### 阶段4: 集成 (Day 2-3)
- [ ] 集成到Cache系统
- [ ] 端到端测试
- [ ] 性能验证

## 7. 性能指标

预期指标:
- **Row Buffer命中率**: 60-80% (取决于workload)
- **平均延迟**:
  - Row Buffer Hit: ~15 cycles
  - Row Buffer Miss: ~45 cycles
- **队列利用率**: RQ/WQ不应频繁满

## 8. 参考

- ChampSim DRAM Controller: `ThirdParty/ChampSim/src/dram_controller.cc`
- DDR4 JEDEC标准: 时序参数参考
