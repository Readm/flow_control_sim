# ChampSim O3_CPU Go 实现详细计划

## 项目概述

将 ChampSim 的完整 O3_CPU 模型用纯 Go 重写，作为框架的 CPU 激励源，生成内存访问事务。

**目标：**
- 纯 Go 实现，无 CGo 依赖
- 完整支持 ChampSim trace 格式
- 保留完整的乱序执行模型特性
- 集成到现有框架的 IncentiveHook

**预估工作量：** 2,000-3,000 行代码，5-7 天

---

## 一、目录结构设计

```
internal/
└── champsim/
    ├── trace/              # Trace 读取相关
    │   ├── reader.go       # Trace 读取器接口
    │   ├── format.go       # Trace 格式定义
    │   └── reader_test.go
    ├── instruction/        # 指令定义
    │   ├── instruction.go  # ooo_model_instr 定义
    │   ├── types.go        # 分支类型等常量
    │   └── lsq_entry.go    # LSQ 条目定义
    ├── cpu/                # CPU 核心
    │   ├── o3_cpu.go       # O3_CPU 主结构
    │   ├── pipeline.go     # 流水线阶段实现
    │   ├── lsq.go          # Load/Store Queue
    │   ├── rob.go          # Reorder Buffer 操作
    │   ├── register.go     # 寄存器分配器
    │   ├── dib.go          # DIB (Decoded Instruction Buffer)
    │   └── cpu_test.go
    ├── branch/             # 分支预测（可选模块化）
    │   ├── predictor.go    # 分支预测器接口
    │   └── bimodal.go      # 简单的 bimodal 预测器
    ├── btb/                # Branch Target Buffer
    │   └── btb.go
    └── integration/        # 框架集成
        ├── incentive.go    # IncentiveHook 实现
        └── bridge.go       # CPU 与框架的桥接
```

---

## 二、核心组件详细设计

### 2.1 Trace 读取器 (`internal/champsim/trace/`)

**文件：`format.go`**
```go
// 对应 ChampSim 的 trace_instruction.h
type InputInstr struct {
    IP               uint64    // 指令地址
    IsBranch         uint8     // 是否分支
    BranchTaken      uint8     // 分支是否跳转
    DestRegisters    [2]uint8  // 目标寄存器
    SrcRegisters     [4]uint8  // 源寄存器
    DestMemory       [2]uint64 // 写内存地址
    SrcMemory        [4]uint64 // 读内存地址
}

type CloudsuiteInstr struct {
    IP               uint64
    IsBranch         uint8
    BranchTaken      uint8
    DestRegisters    [4]uint8  // CloudSuite 有 4 个目标寄存器
    SrcRegisters     [4]uint8
    DestMemory       [4]uint64
    SrcMemory        [4]uint64
    ASID             [2]uint8  // Address Space ID
}
```

**文件：`reader.go`**
```go
type TraceReader interface {
    ReadInstruction() (*instruction.OOOModelInstr, error)
    EOF() bool
    Close() error
}

// 支持压缩格式 (.xz, .gz)
type BulkTraceReader struct {
    cpuID      uint8
    file       io.ReadCloser
    buffer     []*instruction.OOOModelInstr
    instrID    uint64
    eof        bool
    isCloudsuite bool
}

func NewTraceReader(filename string, cpuID uint8, isCloudsuite bool) (TraceReader, error)
```

**关键实现：**
- 批量读取（buffer_size = 128）提升性能
- 支持 gzip/xz 压缩格式（使用 Go 标准库）
- 自动设置分支目标（`set_branch_targets`）

---

### 2.2 指令定义 (`internal/champsim/instruction/`)

**文件：`instruction.go`**
```go
// 对应 ChampSim 的 ooo_model_instr
type OOOModelInstr struct {
    // 程序顺序
    InstrID uint64

    // 基本信息
    IP          uint64
    CPUID       uint8
    ASID        [2]uint8

    // 时间
    ReadyTime   uint64  // 以 cycle 为单位

    // 分支信息
    IsBranch         bool
    BranchTaken      bool
    BranchPrediction bool
    BranchMispredicted bool
    BranchType       BranchType
    BranchTarget     uint64

    // 流水线状态标记
    DIBChecked      bool
    FetchIssued     bool
    FetchCompleted  bool
    Decoded         bool
    Scheduled       bool
    Executed        bool
    Completed       bool

    // 寄存器和内存操作
    DestRegisters   []PhysicalRegisterID  // 物理寄存器ID
    SrcRegisters    []PhysicalRegisterID
    DestMemory      []uint64              // 内存地址
    SrcMemory       []uint64

    // 依赖关系
    CompletedMemOps int
    NumRegDependent int
    RegInstrsDepend []*OOOModelInstr  // 依赖于我的指令
}

type BranchType int
const (
    BranchDirectJump BranchType = iota
    BranchIndirect
    BranchConditional
    BranchDirectCall
    BranchIndirectCall
    BranchReturn
    BranchOther
    NotBranch
)

type PhysicalRegisterID int16  // -1 表示无寄存器
```

**文件：`lsq_entry.go`**
```go
type LSQEntry struct {
    InstrID        uint64
    VirtualAddr    uint64
    IP             uint64
    ReadyTime      uint64
    ASID           [2]uint8
    FetchIssued    bool
    ProducerID     uint64
    LQDependOnMe   []*LSQEntry  // Load Queue 中依赖我的条目
}
```

**关键实现：**
- 从 `InputInstr` 构造时自动识别分支类型（通过寄存器读写模式）
- 实现程序顺序比较方法

---

### 2.3 O3_CPU 核心 (`internal/champsim/cpu/`)

**文件：`o3_cpu.go`**
```go
type O3CPU struct {
    // 基本信息
    CPUID       uint8
    CurrentCycle uint64

    // 统计信息
    NumRetired   int64
    BeginPhaseInstr int64

    // Trace 读取
    traceReader  *trace.TraceReader
    inputQueue   []*instruction.OOOModelInstr

    // 流水线缓冲区
    IFetchBuffer    []*instruction.OOOModelInstr
    DecodeBuffer    []*instruction.OOOModelInstr
    DispatchBuffer  []*instruction.OOOModelInstr
    ROB             []*instruction.OOOModelInstr
    DIBHitBuffer    []*instruction.OOOModelInstr

    // LSQ
    LoadQueue   []*instruction.LSQEntry
    StoreQueue  []*instruction.LSQEntry

    // DIB (Decoded Instruction Buffer)
    DIB         *DIBTable

    // 寄存器分配器
    regAllocator *RegisterAllocator

    // 配置参数（对应 ChampSim 的常量）
    IFetchBufferSize   int
    DecodeBufferSize   int
    DispatchBufferSize int
    ROBSize            int
    LQSize             int
    SQSize             int

    FetchWidth         int
    DecodeWidth        int
    DispatchWidth      int
    SchedulerSize      int
    ExecWidth          int
    RetireWidth        int

    // 延迟参数
    BranchMispredictPenalty uint64
    DispatchLatency         uint64
    DecodeLatency           uint64
    SchedulingLatency       uint64
    ExecLatency             uint64
    DIBHitLatency           uint64

    // 分支预测和 BTB
    branchPredictor BranchPredictor
    btb             *BTB

    // 内存访问回调（桥接到框架）
    memoryRequestCallback  func(addr uint64, isWrite bool, cycle uint64) error
    memoryResponseCallback func(instrID uint64, cycle uint64)
}

// 核心方法
func (cpu *O3CPU) Initialize()
func (cpu *O3CPU) Operate() int  // 返回 progress
func (cpu *O3CPU) BeginPhase()
func (cpu *O3CPU) EndPhase()
```

**文件：`pipeline.go` - 流水线各阶段**
```go
// 对应 ChampSim ooo_cpu.cc 中的各个阶段
func (cpu *O3CPU) initializeInstruction() int
func (cpu *O3CPU) checkDIB() int
func (cpu *O3CPU) fetchInstruction() int
func (cpu *O3CPU) promoteToDecod() int
func (cpu *O3CPU) decodeInstruction() int
func (cpu *O3CPU) dispatchInstruction() int
func (cpu *O3CPU) scheduleInstruction() int
func (cpu *O3CPU) executeInstruction() int
func (cpu *O3CPU) operateLSQ() int
func (cpu *O3CPU) completeInflightInstruction() int
func (cpu *O3CPU) handleMemoryReturn() int
func (cpu *O3CPU) retireROB() int

// 辅助方法
func (cpu *O3CPU) doInitInstruction(instr *instruction.OOOModelInstr) bool
func (cpu *O3CPU) doPredictBranch(instr *instruction.OOOModelInstr) bool
func (cpu *O3CPU) doCheckDIB(instr *instruction.OOOModelInstr)
func (cpu *O3CPU) doFetchInstruction(instrs []*instruction.OOOModelInstr) bool
func (cpu *O3CPU) doScheduling(instr *instruction.OOOModelInstr)
func (cpu *O3CPU) doExecution(instr *instruction.OOOModelInstr)
func (cpu *O3CPU) doMemoryScheduling(instr *instruction.OOOModelInstr)
```

**文件：`lsq.go` - Load/Store Queue**
```go
type LoadQueue struct {
    entries []*instruction.LSQEntry
    maxSize int
}

type StoreQueue struct {
    entries []*instruction.LSQEntry
    maxSize int
}

func (lq *LoadQueue) IsFull() bool
func (lq *LoadQueue) Add(entry *instruction.LSQEntry) error
func (lq *LoadQueue) Remove(instrID uint64)
func (lq *LoadQueue) FindByID(instrID uint64) *instruction.LSQEntry

// Store-to-Load Forwarding
func (cpu *O3CPU) doSQForwardToLQ(sqEntry, lqEntry *instruction.LSQEntry)
func (cpu *O3CPU) executeLoad(lqEntry *instruction.LSQEntry) bool
func (cpu *O3CPU) doFinishStore(sqEntry *instruction.LSQEntry)
```

**文件：`register.go` - 寄存器分配器**
```go
type RegisterAllocator struct {
    numPhysical int
    allocated   map[PhysicalRegisterID]bool
    nextFreeID  PhysicalRegisterID
}

func NewRegisterAllocator(size int) *RegisterAllocator
func (ra *RegisterAllocator) Allocate() (PhysicalRegisterID, error)
func (ra *RegisterAllocator) Free(id PhysicalRegisterID)
func (ra *RegisterAllocator) AvailableCount() int
```

**文件：`dib.go` - Decoded Instruction Buffer (类似 uop cache)**
```go
type DIBTable struct {
    entries map[uint64]*DIBEntry  // key 是 IP 的高位
    maxSize int
    shift   int  // 用于计算索引的位移量
}

type DIBEntry struct {
    ip      uint64
    valid   bool
    lastUse uint64  // LRU
}

func (dib *DIBTable) Check(ip uint64) bool
func (dib *DIBTable) Insert(ip uint64)
func (dib *DIBTable) Evict()  // LRU 替换
```

---

### 2.4 分支预测 (`internal/champsim/branch/`)

**文件：`predictor.go`**
```go
type BranchPredictor interface {
    Initialize()
    Predict(ip uint64, target uint64, alwaysTaken bool, branchType BranchType) bool
    LastBranchResult(ip, target uint64, taken bool, branchType BranchType)
}

// 简单的 Bimodal 预测器
type BimodalPredictor struct {
    table []uint8  // 2-bit 饱和计数器
    size  int
}
```

**文件：`btb/btb.go`**
```go
type BTB struct {
    entries map[uint64]*BTBEntry
    maxSize int
}

type BTBEntry struct {
    ip         uint64
    target     uint64
    branchType BranchType
}

func (btb *BTB) Lookup(ip uint64) (target uint64, found bool)
func (btb *BTB) Update(ip, target uint64, branchType BranchType)
```

---

## 三、与框架集成 (`internal/champsim/integration/`)

**文件：`incentive.go`**
```go
// 实现 pkg/hook/IncentiveHook 接口
type ChampSimIncentive struct {
    cpu         *cpu.O3CPU
    txnManager  *transaction.TxnManager

    // 内存请求队列（待处理）
    pendingRequests map[uint64]*MemoryRequest
}

type MemoryRequest struct {
    InstrID  uint64
    Addr     uint64
    IsWrite  bool
    IssueCycle uint64
}

func NewChampSimIncentive(traceFile string, cpuID uint8, txnMgr *transaction.TxnManager) *ChampSimIncentive

// 实现 IncentiveHook 接口
func (csi *ChampSimIncentive) ShouldCreateTransaction(nodeID int, cycle uint64) bool
func (csi *ChampSimIncentive) CreateTransaction(nodeID int, cycle uint64) (*transaction.Transaction, error)

// 内部方法
func (csi *ChampSimIncentive) stepCPU() error
func (csi *ChampSimIncentive) handleMemoryRequest(req *MemoryRequest) (*transaction.Transaction, error)
func (csi *ChampSimIncentive) handleMemoryResponse(instrID uint64, cycle uint64)
```

**文件：`bridge.go`**
```go
// CPU 的内存请求回调
func (csi *ChampSimIncentive) onMemoryRequest(addr uint64, isWrite bool, cycle uint64) error {
    // 创建待处理请求
    req := &MemoryRequest{
        InstrID: nextReqID,
        Addr: addr,
        IsWrite: isWrite,
        IssueCycle: cycle,
    }
    csi.pendingRequests[nextReqID] = req
    return nil
}

// 当事务完成时，通知 CPU
func (csi *ChampSimIncentive) onTransactionComplete(txn *transaction.Transaction) {
    if req, ok := csi.pendingRequests[txn.ID]; ok {
        csi.cpu.HandleMemoryResponse(req.InstrID, txn.CompleteCycle)
        delete(csi.pendingRequests, txn.ID)
    }
}
```

---

## 四、实施步骤

### 阶段 1：基础设施（第 1 天）
- [ ] 创建目录结构
- [ ] 实现 Trace 格式定义 (`trace/format.go`)
- [ ] 实现指令数据结构 (`instruction/instruction.go`, `types.go`)
- [ ] 编写单元测试验证数据结构

### 阶段 2：Trace 读取器（第 1-2 天）
- [ ] 实现 `TraceReader` 接口 (`trace/reader.go`)
- [ ] 支持压缩格式读取（gzip/xz）
- [ ] 实现批量缓冲和分支目标设置
- [ ] 测试：读取真实 ChampSim trace 文件

### 阶段 3：CPU 核心组件（第 2-3 天）
- [ ] 实现寄存器分配器 (`cpu/register.go`)
- [ ] 实现 DIB (`cpu/dib.go`)
- [ ] 实现 LSQ (`cpu/lsq.go`)
- [ ] 单元测试各组件

### 阶段 4：流水线逻辑（第 3-5 天）
- [ ] 实现 O3_CPU 主结构 (`cpu/o3_cpu.go`)
- [ ] 实现流水线各阶段：
  - [ ] `initializeInstruction`
  - [ ] `checkDIB` / `fetchInstruction`
  - [ ] `decodeInstruction` / `dispatchInstruction`
  - [ ] `scheduleInstruction` / `executeInstruction`
  - [ ] `operateLSQ` / `retireROB`
- [ ] 实现依赖关系跟踪
- [ ] 测试：简单指令序列执行

### 阶段 5：分支预测（第 5-6 天）
- [ ] 实现分支预测器接口 (`branch/predictor.go`)
- [ ] 实现简单的 Bimodal 预测器
- [ ] 实现 BTB (`btb/btb.go`)
- [ ] 集成到 CPU 流水线
- [ ] 测试：分支预测准确性

### 阶段 6：框架集成（第 6-7 天）
- [ ] 实现 `ChampSimIncentive` (`integration/incentive.go`)
- [ ] 实现内存请求/响应桥接 (`integration/bridge.go`)
- [ ] 集成到现有框架的 `IncentiveHook`
- [ ] 端到端测试：CPU + NoC 仿真

### 阶段 7：测试和优化（第 7 天）
- [ ] 完整的集成测试
- [ ] 性能对比（与原版 ChampSim）
- [ ] 代码审查和优化
- [ ] 文档完善

---

## 五、关键技术要点

### 5.1 Trace 读取优化
- 使用缓冲区批量读取（128 条指令）
- 延迟解析：只在需要时才从 `InputInstr` 转换为 `OOOModelInstr`
- 支持 trace 循环（可选）

### 5.2 流水线正确性
- 严格按照 ChampSim 的执行顺序
- 正确处理指令依赖关系（寄存器依赖、内存依赖）
- 准确模拟延迟（dispatch latency, exec latency 等）

### 5.3 内存一致性
- Store-to-Load Forwarding：SQ 中的 store 可以直接转发给 LQ 的 load
- 内存顺序：保证 load/store 的程序顺序
- 与框架的 Transaction 系统正确交互

### 5.4 性能优化
- 使用 slice 替代 deque（Go 没有原生 deque）
- 预分配缓冲区避免频繁分配
- 使用 map 加速查找（如 DIB、BTB）

---

## 六、测试策略

### 单元测试
- `trace_test.go`: 测试 trace 读取和格式解析
- `instruction_test.go`: 测试指令构造和分支类型识别
- `register_test.go`: 测试寄存器分配和释放
- `lsq_test.go`: 测试 LSQ 操作和转发
- `pipeline_test.go`: 测试各流水线阶段

### 集成测试
- 使用简单的手写 trace 文件测试完整流水线
- 对比 ChampSim 的输出（IPC、内存访问序列）
- 测试与框架的集成（创建 Transaction）

### 端到端测试
- 运行真实的 SPEC trace
- 验证内存访问模式的正确性
- 性能基准测试

---

## 七、配置参数（默认值）

参考 ChampSim 的默认配置：
```go
const (
    DefaultIFetchBufferSize   = 64
    DefaultDecodeBufferSize   = 32
    DefaultDispatchBufferSize = 32
    DefaultROBSize            = 352
    DefaultLQSize             = 128
    DefaultSQSize             = 72

    DefaultFetchWidth    = 6
    DefaultDecodeWidth   = 6
    DefaultDispatchWidth = 6
    DefaultExecWidth     = 4
    DefaultRetireWidth   = 5

    DefaultBranchMispredictPenalty = 1  // cycles
    DefaultDecodeLatency           = 1
    DefaultDispatchLatency         = 1
    DefaultSchedulingLatency       = 0
    DefaultExecLatency             = 0
)
```

---

## 八、预期输出

完成后，可以这样使用：

```go
import (
    "github.com/Readm/flow_sim/internal/champsim/integration"
    "github.com/Readm/flow_sim/internal/dataflow/transaction"
)

func main() {
    txnMgr := transaction.NewTxnManager(...)

    // 创建 ChampSim 激励
    incentive := integration.NewChampSimIncentive(
        "traces/600.perlbench_s-210B.champsimtrace.xz",
        0, // CPU ID
        txnMgr,
    )

    // 运行仿真
    for cycle := uint64(0); cycle < 1000000; cycle++ {
        if incentive.ShouldCreateTransaction(0, cycle) {
            txn, err := incentive.CreateTransaction(0, cycle)
            if err == nil && txn != nil {
                // 提交 transaction 到网络
                network.SubmitTransaction(txn)
            }
        }
    }
}
```

---

## 九、风险和缓解

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| Trace 格式理解不准确 | 高 | 参考 ChampSim 源码和测试用例，逐字段验证 |
| 流水线逻辑复杂导致 bug | 中 | 分阶段实现，每阶段充分测试 |
| 性能不如原版 ChampSim | 低 | Go 的性能对于框架已足够，必要时优化热点 |
| 与框架集成困难 | 中 | 先独立开发，最后阶段集成，使用适配器模式 |

---

## 十、后续扩展

- 支持多核 trace（多个 CPU 实例）
- 可插拔的分支预测器（Tournament, Perceptron 等）
- 可配置的预取器
- 支持生成新的 trace（记录内存访问序列）
- 性能分析和可视化

---

## 联系人

如有问题请联系：[项目负责人]

**文档版本：** v1.0
**创建日期：** 2025-12-26
**最后更新：** 2025-12-26
