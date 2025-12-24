# ChampSim O3_CPU 纯 Go 实现进度报告

## 项目目标

**核心目标**：一比一复刻 ChampSim 的 O3_CPU 逻辑到纯 Go 实现，不做任何简化，仅语言不同。

**集成目标**：将 ChampSim CPU 集成到 flow_sim 框架中，作为 IncentiveHook 使用真实的 SPEC CPU trace 进行仿真。

## 当前状态总结

### 整体进度：约 70% 完成

- ✅ **基础架构**：6 段流水线框架、ROB、LSQ、DIB 已实现
- ✅ **Trace 读取**：完全兼容 ChampSim 二进制 trace 格式
- ✅ **框架集成**：CPUIncentiveHook 已实现，支持 CHI/AXI 协议
- ✅ **RegisterAllocator**：已完全重写为 ChampSim 一比一实现
- ⚠️ **依赖管理**：部分实现，缺少关键唤醒机制
- ❌ **Complete 阶段**：尚未实现 `completeInflightInstruction()`
- ❌ **Dispatch 集成**：尚未使用新的 RegisterAllocator Rename 方法

---

## 已完成工作

### 1. 基础组件实现（Stage 1-4）

#### 1.1 Trace Reader
- **文件**：`internal/champsim/trace/`
- **功能**：
  - ✅ 完全兼容 ChampSim 的 `input_instr` 二进制格式（64 字节）
  - ✅ 支持 `.champsimtrace.xz` 压缩格式
  - ✅ 批量读取优化（128 条指令缓冲）
  - ✅ 自动设置分支目标
- **验证**：成功读取真实 SPEC CPU 2006/2017 traces
  - 400.perlbench-41B.champsimtrace (30GB)
  - 429.mcf-22B.champsimtrace (16GB)

#### 1.2 ROB (Reorder Buffer)
- **文件**：`internal/champsim/cpu/rob.go`
- **功能**：
  - ✅ 循环缓冲区实现
  - ✅ In-order retirement
  - ✅ 分支误预测后 Flush
- **测试**：11 个单元测试全部通过

#### 1.3 LSQ (Load-Store Queue)
- **文件**：`internal/champsim/cpu/lsq.go`
- **功能**：
  - ✅ 独立的 Load Queue 和 Store Queue
  - ✅ Store-to-Load Forwarding
  - ✅ 内存顺序检查
  - ✅ HandleLoadResponse/HandleStoreResponse
- **测试**：验证了 Store-to-Load Forwarding 工作正常

#### 1.4 O3_CPU 流水线
- **文件**：`internal/champsim/cpu/o3_cpu.go`
- **功能**：
  - ✅ 6 段流水线：Fetch → Decode → Dispatch → Schedule → Execute → Retire
  - ✅ Dual-mode 支持（standalone / integration）
  - ✅ 集成接口方法（GetReadyLoads/Stores, HandleResponses）
- **问题**：依赖管理和 Complete 阶段缺失

### 2. 框架集成（Stage 6）

#### 2.1 CPUIncentiveHook
- **文件**：`internal/champsim/integration/cpu_hook.go`
- **功能**：
  - ✅ 实现 IncentiveHook 接口
  - ✅ Message-level 集成（CHI/AXI 协议）
  - ✅ 1:1 周期映射
  - ✅ 内存请求/响应处理
- **测试**：9 个集成测试通过（基础测试）

### 3. 真实 Trace 验证

#### 3.1 下载的 Traces
- **来源**：https://dpc3.compas.cs.stonybrook.edu/champsim-traces/speccpu/
- **文件**：
  - `400.perlbench-41B.champsimtrace.xz` (178MB → 30GB)
  - `429.mcf-22B.champsimtrace.xz` (535MB → 16GB)
- **验证**：成功读取前 100 条指令，格式正确

#### 3.2 ChampSim 源码
- **来源**：https://github.com/ChampSim/ChampSim
- **位置**：`ThirdParty/ChampSim/`
- **用途**：对比参考，确保逻辑一致

---

## 发现的关键问题

### 问题 1：内存响应未更新 ROB 中的 Completed 标志 ✅ 已修复

**症状**：
- IPC = 0.004（1000 周期仅退休 4 条指令）

**原因**：
- `HandleLoadResponse/HandleStoreResponse` 只更新了 LSQEntry.Completed
- ROB 中对应指令的 `instr.Completed` 仍然为 false
- Retire 阶段检查 `instr.Completed`，永远无法退休

**修复**：
```go
func (cpu *O3CPU) HandleLoadResponse(instrID uint64, cycle uint64) bool {
    if !cpu.lsq.HandleLoadResponse(instrID, cycle) {
        return false
    }
    // 同时更新 ROB 中的指令状态
    instr := cpu.rob.FindByInstrID(instrID)
    if instr != nil {
        instr.Completed = true
    }
    return true
}
```

**效果**：IPC 从 0.004 提升到 0.03（提升 7.5 倍）

---

### 问题 2：缺少 RegisterAllocator 的 Complete/Retire 机制 ✅ 已修复

**ChampSim 的设计**：
1. **Dispatch 阶段**：`rename_dest_register()` 分配物理寄存器，设置 `valid=false`
2. **Execute 完成后**：`complete_dest_register()` 设置 `valid=true`
3. **Schedule 阶段**：检查源寄存器是否 `valid`，决定是否可调度
4. **Retire 阶段**：`retire_dest_register()` 更新 Backend RAT，释放旧寄存器

**我们的缺失**：
- ❌ 没有 `CompleteDestRegister()` 方法
- ❌ 没有 Frontend/Backend 双 RAT 机制
- ❌ 没有物理寄存器的 `valid` 标志
- ❌ 依赖检查永远失败（NumRegDependent 永不递减）

**修复**：
- ✅ 完全重写 `RegisterAllocator`
- ✅ 添加 `PhysicalRegister` 结构（包含 valid/busy 标志）
- ✅ 实现双 RAT（Frontend/Backend）
- ✅ 实现 `RenameDestRegister/RenameSrcRegister`
- ✅ 实现 `CompleteDestRegister/RetireDestRegister`
- ✅ 实现 `CountRegDependencies()` 检查 valid 标志

**对应的 ChampSim 代码**：
```cpp
// ChampSim: src/register_allocator.cc
void RegisterAllocator::complete_dest_register(PHYSICAL_REGISTER_ID physreg) {
    physical_register_file.at(physreg).valid = true;  // 标记数据有效
}

int RegisterAllocator::count_reg_dependencies(const ooo_model_instr& instr) const {
    return std::count_if(instr.source_registers, [](auto reg) {
        return !isValid(reg);  // 检查源寄存器的 valid 标志
    });
}
```

---

### 问题 3：缺少 completeInflightInstruction() 阶段 ❌ 未修复

**ChampSim 的 operate() 顺序**：
```cpp
long O3_CPU::operate() {
    progress += retire_rob();
    progress += complete_inflight_instruction();  // ← 关键！
    progress += execute_instruction();
    progress += schedule_instruction();
    progress += handle_memory_return();
    progress += operate_lsq();
    progress += dispatch_instruction();
    progress += decode_instruction();
    progress += promote_to_decode();
    progress += fetch_instruction();
    progress += check_dib();
    initialize_instruction();
    return progress;
}
```

**complete_inflight_instruction() 的作用**：
```cpp
long O3_CPU::complete_inflight_instruction() {
    for (auto rob_it = ROB.begin(); rob_it != ROB.end(); ++rob_it) {
        if (rob_it->executed && !rob_it->completed
            && rob_it->ready_time <= current_time
            && rob_it->completed_mem_ops == rob_it->num_mem_ops()) {

            // 调用 complete_dest_register 标记物理寄存器为 valid
            for (auto dreg : rob_it->destination_registers) {
                reg_allocator.complete_dest_register(dreg);
            }

            rob_it->completed = true;  // 标记指令完成
        }
    }
}
```

**我们的缺失**：
- ❌ 没有独立的 complete 阶段
- ❌ Execute 时直接设置 `instr.Completed = true`（非内存指令）
- ❌ 从不调用 `CompleteDestRegister()`
- ❌ 依赖指令的寄存器永远是 invalid，无法 schedule

---

### 问题 4：Dispatch 阶段未使用 Rename 方法 ❌ 未修复

**ChampSim 的 Dispatch 逻辑**：
```cpp
// 为目标寄存器分配物理寄存器
for (auto dreg : instr.destination_registers) {
    auto phys_reg = reg_allocator.rename_dest_register(dreg, instr.instr_id);
    instr.destination_registers[i] = phys_reg;
}

// 为源寄存器查找物理寄存器映射
for (auto sreg : instr.source_registers) {
    auto phys_reg = reg_allocator.rename_src_register(sreg);
    instr.source_registers[i] = phys_reg;
}

// 计算寄存器依赖
instr.num_reg_dependent = reg_allocator.count_reg_dependencies(instr);
```

**我们的问题**：
- ❌ Dispatch 阶段仍在使用旧的 `Allocate()` 方法
- ❌ 没有调用 `RenameDestRegister/RenameSrcRegister`
- ❌ 没有设置 `NumRegDependent`
- ❌ 指令的源/目标寄存器仍是架构寄存器，不是物理寄存器

---

## 下一步计划

### 优先级 P0：完成核心逻辑（必须）

1. **实现 completeInflightInstruction() 阶段**
   - 添加新的流水线阶段函数
   - 检查 executed && !completed 的指令
   - 检查 ready_time 和 completed_mem_ops
   - 调用 `reg_allocator.CompleteDestRegister()`
   - 设置 `instr.Completed = true`

2. **修改 Dispatch 阶段**
   - 为目标寄存器调用 `RenameDestRegister()`
   - 为源寄存器调用 `RenameSrcRegister()`
   - 计算 `NumRegDependent = CountRegDependencies()`
   - 将物理寄存器 ID 写入指令结构

3. **修改 Schedule 阶段**
   - 使用 `CountRegDependencies()` 检查依赖
   - 替换简化的 `checkDependencies()`

4. **修改 Retire 阶段**
   - 为目标寄存器调用 `RetireDestRegister()`
   - 释放物理寄存器资源

5. **调整 Tick() 执行顺序**
   - 按照 ChampSim 的顺序重新排列
   - 添加 `completeInflightInstruction()` 调用

### 优先级 P1：完善细节（重要）

6. **添加 completed_mem_ops 计数器**
   - 在指令结构中添加 `CompletedMemOps` 字段
   - 在 `HandleLoadResponse/HandleStoreResponse` 中递增
   - 在 `complete` 阶段检查 `CompletedMemOps == NumMemOps`

7. **实现 handle_memory_return() 阶段**
   - 对应 ChampSim 的内存响应处理
   - 从缓存返回队列中取出响应
   - 更新 LSQ 和 ROB

8. **添加 promote_to_decode() 和 initialize_instruction()**
   - 完整复刻 ChampSim 的所有阶段

### 优先级 P2：验证和测试（必须）

9. **运行真实 Trace 集成测试**
   - 验证 IPC 恢复到合理范围（0.5-2.0）
   - 对比 ChampSim 的 IPC 数值
   - 检查指令退休数量

10. **性能对比**
    - 与 ChampSim 的相同 trace 对比 IPC
    - 分析差异原因
    - 调优性能

---

## 技术细节参考

### ChampSim 关键文件

1. **src/ooo_cpu.cc**
   - `operate()`: 主循环
   - `complete_inflight_instruction()`: Complete 阶段
   - `dispatch_instruction()`: Dispatch 阶段
   - `retire_rob()`: Retire 阶段

2. **src/register_allocator.cc**
   - `rename_dest_register()`: 目标寄存器重命名
   - `rename_src_register()`: 源寄存器重命名
   - `complete_dest_register()`: 标记寄存器有效
   - `retire_dest_register()`: Retire 时更新 Backend RAT
   - `count_reg_dependencies()`: 计算依赖数量

3. **inc/instruction.h**
   - `ooo_model_instr`: 指令结构定义
   - `num_reg_dependent`: 寄存器依赖计数

### 我们的实现文件

1. **internal/champsim/cpu/o3_cpu.go**
   - `Tick()`: 主循环
   - `dispatch()`, `schedule()`, `execute()`, `retire()`: 各阶段
   - **待添加**: `completeInflightInstruction()`

2. **internal/champsim/cpu/register.go** ✅ 已完成
   - `RegisterAllocator`: 完全重写
   - `PhysicalRegister`: 新增结构
   - Frontend/Backend RAT: 新增
   - 所有 ChampSim 对应方法已实现

3. **internal/champsim/instruction/instruction.go**
   - `OOOModelInstr`: 指令结构
   - **待添加**: `CompletedMemOps` 字段

---

## 测试验证状态

### 单元测试
- ✅ ROB 测试：11/11 通过
- ✅ LSQ 测试：10/10 通过
- ✅ O3CPU 测试：11/11 通过（standalone 模式）
- ⚠️ RegisterAllocator 测试：需要更新以匹配新实现

### 集成测试
- ✅ 基础集成测试：9/9 通过
- ✅ Store 操作测试：通过
- ✅ Store-to-Load Forwarding 测试：通过（ForwardedLoads=1）
- ✅ 分支指令测试：通过
- ❌ 真实 Trace 集成测试：失败（IPC=0.03，低于预期 0.1-4.0）

### Trace 读取测试
- ✅ Perlbench Trace：成功读取 100 条指令
  - 29% Loads, 6% Stores, 21% Branches
  - IP 地址合理：0x47fe85, 0x47feaa...
- ✅ MCF Trace：成功读取 100 条指令
  - 第一条 IP: 0x4012d2

---

## 资源和参考

### ChampSim 资源
- **官方仓库**: https://github.com/ChampSim/ChampSim
- **Trace 仓库**: https://dpc3.compas.cs.stonybrook.edu/champsim-traces/speccpu/
- **文档**: ChampSim README 和源码注释

### SPEC CPU Benchmarks
- **400.perlbench**: Perl 解释器，计算密集型
- **429.mcf**: 单一仓库最短路径问题，内存密集型（大量 cache miss）

### 关键算法
1. **寄存器重命名**: Frontend/Backend 双 RAT 机制
2. **依赖跟踪**: 物理寄存器 valid 标志
3. **Store-to-Load Forwarding**: LSQ 中的地址匹配
4. **分支目标设置**: 反向遍历设置跳转目标

---

## 预计工作量

### 剩余工作量估算
- **P0 任务（核心逻辑）**: ~500-800 行代码，2-3 小时
- **P1 任务（完善细节）**: ~300-500 行代码，1-2 小时
- **P2 任务（测试验证）**: 调试和对比，1-2 小时

**总计**: 约 6-7 小时工作量

---

## 总结

**当前状态**: 已完成基础架构和 RegisterAllocator 的一比一复刻，但缺少关键的 Complete 阶段和 Dispatch/Retire 集成。

**核心问题**: 寄存器依赖机制未完整实现，导致指令无法正常调度和退休。

**下一步**: 实现 completeInflightInstruction() 阶段，并在 Dispatch/Retire 阶段集成新的 RegisterAllocator 方法。

**预期效果**: 完成后 IPC 应恢复到 0.5-2.0 范围，与 ChampSim 的行为一致。

---

*文档更新时间: 2025-12-26*
*项目位置: `/home/readm/flow_sim`*
*ChampSim 源码: `/home/readm/flow_sim/ThirdParty/ChampSim`*
