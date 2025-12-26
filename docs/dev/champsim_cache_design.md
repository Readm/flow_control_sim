# ChampSim Cache 复刻设计

## 1. 目标

1:1 复刻 ChampSim 的 Cache 实现，支持 CPU 集成模式运行。

## 2. 现状分析

### 2.1 现有 Cache 实现 (internal/components/cache/)

**FullyAssociativeCache 特性**：
- ✅ 全相联 (Fully-Associative)
- ✅ 随机替换策略
- ✅ MESI 状态支持
- ✅ Snoop 处理（coherence协议）
- ✅ 线程安全（sync.RWMutex）
- ❌ **不符合** ChampSim架构（ChampSim使用Set-Associative）

### 2.2 ChampSim Cache 特性

**核心特性**：
- Set-Associative Cache: `NUM_SET x NUM_WAY`
- LRU 替换策略（模块化）
- MSHR (Miss Status Holding Registers)
- Prefetch 支持
- Hit/Fill 延迟模拟
- Tag lookup 流水线

**Block 结构** (`champsim::cache_block`):
```cpp
struct cache_block {
    bool valid;        // 是否有效
    bool prefetch;     // 是否是预取的
    bool dirty;        // 是否被修改（需要写回）
    champsim::address address;    // 物理地址（tag）
    champsim::address v_address;  // 虚拟地址
    champsim::address data;       // 数据地址
    uint32_t pf_metadata;         // 预取元数据
};
```

**MSHR 结构**:
```cpp
struct mshr_type {
    champsim::address address;
    champsim::address v_address;
    champsim::address ip;
    uint64_t instr_id;
    // 数据promise（用于异步等待）
    // 依赖跟踪
    // 返回队列
};
```

## 3. 复刻方案

### 3.1 架构决策

**方案选择**：在 `internal/champsim/cache/` 下创建新的 Set-Associative Cache 实现

**原因**：
1. ChampSim 使用 Set-Associative，不能用现有的 Fully-Associative
2. 需要1:1复刻，包括MSHR、延迟模拟等特性
3. 现有的 `internal/components/cache/` 设计用于 coherence 协议，不适合直接修改

### 3.2 实现分阶段

#### 阶段1：基础数据结构 ✅ 目标
- `CacheBlock`: 对应 `champsim::cache_block`
- `SetAssociativeCache`: 基础 Set-Associative 结构
- Set/Way 索引计算
- Block 查找（Hit/Miss 判断）

#### 阶段2：LRU 替换策略 ✅ 目标
- LRU counter per way
- `findVictim()`: 查找LRU victim
- `updateLRU()`: 更新访问时间

#### 阶段3：MSHR 和 Miss 处理 ✅ 目标
- MSHR queue
- Miss 合并（多个请求访问同一地址）
- Fill 处理

#### 阶段4：延迟模拟 ✅ 目标
- HIT_LATENCY
- FILL_LATENCY
- Ready time tracking

#### 阶段5：CPU 集成 ✅ 目标
- CPU Load/Store 请求接口
- 响应返回机制
- 测试 CPU+Cache 集成

## 4. 数据结构设计

### 4.1 CacheBlock (block.go)

```go
package cache

// CacheBlock 对应 ChampSim 的 champsim::cache_block
type CacheBlock struct {
    // 状态标志
    Valid    bool   // 是否有效
    Prefetch bool   // 是否是预取的
    Dirty    bool   // 是否被修改（需要写回）

    // 地址信息
    Address  uint64 // 物理地址（tag + set index）
    VAddress uint64 // 虚拟地址
    Data     uint64 // 数据地址（ChampSim中是64位地址）

    // 预取元数据
    PfMetadata uint32

    // LRU 信息
    LRU uint64 // LRU counter（cycle counter）
}
```

### 4.2 SetAssociativeCache (set_associative_cache.go)

```go
package cache

// CacheConfig Cache配置
type CacheConfig struct {
    NumSets    uint32  // Set 数量
    NumWays    uint32  // Way 数量（每个Set的关联度）
    BlockSize  uint32  // Cache line 大小（字节）
    MSHRSize   uint32  // MSHR 大小
    HitLatency uint64  // Hit 延迟（cycles）
    FillLatency uint64 // Fill 延迟（cycles）
}

// SetAssociativeCache Set-Associative Cache
type SetAssociativeCache struct {
    config CacheConfig

    // blocks: 二维数组 [set][way]
    blocks [][]CacheBlock

    // MSHR: Miss Status Holding Registers
    mshr []*MSHREntry

    // 统计信息
    stats CacheStats
}
```

### 4.3 MSHR (mshr.go)

```go
package cache

// MSHREntry MSHR条目
type MSHREntry struct {
    // 地址信息
    Address   uint64
    VAddress  uint64
    IP        uint64
    InstrID   uint64

    // CPU信息
    CPU uint32

    // 访问类型
    Type AccessType

    // 时间信息
    EnqueueCycle uint64
    ReadyCycle   uint64

    // 依赖跟踪
    InstrDependOnMe []uint64
}

// AccessType 访问类型
type AccessType uint8

const (
    AccessLoad AccessType = iota
    AccessStore
    AccessPrefetch
)
```

### 4.4 CacheStats (stats.go)

```go
package cache

// CacheStats Cache统计信息
type CacheStats struct {
    Hits         uint64
    Misses       uint64
    Accesses     uint64
    Prefetches   uint64
    Writebacks   uint64
    MSHRFull     uint64
}
```

## 5. 核心算法

### 5.1 地址分解

```
地址分解（假设64位地址）:
┌─────────────┬──────────────┬──────────────┐
│    Tag      │  Set Index   │    Offset    │
└─────────────┴──────────────┴──────────────┘

Offset Bits = log2(BlockSize)
Set Index Bits = log2(NumSets)
Tag Bits = 64 - Offset Bits - Set Index Bits
```

```go
func (c *SetAssociativeCache) getSetIndex(addr uint64) uint32 {
    return uint32((addr >> c.offsetBits) & c.setMask)
}

func (c *SetAssociativeCache) getTag(addr uint64) uint64 {
    return addr >> (c.offsetBits + c.setIndexBits)
}
```

### 5.2 查找流程

```
1. 计算 set index
2. 在该 set 的所有 ways 中查找
3. 比较 tag 和 valid 标志
4. 如果找到 → Hit
5. 如果未找到 → Miss → 分配 MSHR
```

### 5.3 LRU 替换

```
LRU 使用访问时间戳（cycle counter）：
- 每次访问时，更新该 way 的 LRU = current_cycle
- 查找 victim 时，选择 LRU 值最小的 way
```

### 5.4 MSHR 处理

```
Miss 流程：
1. 检查 MSHR 中是否已有相同地址的条目
2. 如果有 → 合并请求（追加到依赖列表）
3. 如果没有 → 分配新 MSHR 条目
4. 向下级发送请求（或在standalone模式下立即返回）

Fill 流程：
1. 从 MSHR 中找到对应条目
2. 查找 victim way（LRU）
3. 如果 victim dirty → 写回
4. 填充新数据到 cache
5. 更新 LRU
6. 返回响应给 CPU
```

## 6. 接口设计

### 6.1 CPU → Cache 接口

```go
// Access 处理访问请求
// 返回：hit（是否命中），readyCycle（数据就绪时间）
func (c *SetAssociativeCache) Access(
    addr uint64,
    vaddr uint64,
    instrID uint64,
    accessType AccessType,
    cycle uint64,
) (hit bool, readyCycle uint64)

// HandleFill 处理Fill响应（来自下级）
func (c *SetAssociativeCache) HandleFill(
    addr uint64,
    data uint64,
    cycle uint64,
)
```

### 6.2 Standalone 模式

```go
// SetStandaloneMode 设置独立模式
// standalone=true: Miss自动立即返回（用于测试）
// standalone=false: Miss等待Fill（用于集成）
func (c *SetAssociativeCache) SetStandaloneMode(standalone bool)
```

## 7. 测试策略

### 7.1 单元测试

- ✅ Set/Way 索引计算
- ✅ Hit/Miss 判断
- ✅ LRU 替换
- ✅ MSHR 分配和合并
- ✅ Dirty writeback

### 7.2 集成测试

- ✅ CPU + Cache standalone 模式
- ✅ IPC 验证（应该高于CPU-only）
- ✅ Cache hit/miss 统计

## 8. 实施计划

### 第1步：基础结构（今天）
- 创建目录结构
- 实现 CacheBlock, CacheConfig
- 实现 SetAssociativeCache 框架
- 实现地址分解和查找

### 第2步：LRU（明天）
- 实现 LRU counter
- 实现 findVictim
- 实现 updateLRU

### 第3步：MSHR（明天）
- 实现 MSHR 结构
- 实现 Miss 处理
- 实现 Fill 处理

### 第4步：集成（后天）
- 修改 CPU 集成接口
- 测试 CPU+Cache
- 验证 IPC 提升

## 9. 与现有 Cache 的关系

**保留** `internal/components/cache/`:
- 用于未来的 coherence 协议实现
- 用于 CHI/MESI/MOESI 等

**新建** `internal/champsim/cache/`:
- 专门用于 ChampSim 1:1 复刻
- 简化设计，聚焦CPU性能模拟
- 不考虑 coherence（单核系统）

## 10. 参考

- ChampSim 源码: `/home/readm/flow_sim/ThirdParty/ChampSim/inc/cache.h`
- ChampSim Block: `/home/readm/flow_sim/ThirdParty/ChampSim/inc/block.h`
- 现有Cache: `/home/readm/flow_sim/internal/components/cache/`
