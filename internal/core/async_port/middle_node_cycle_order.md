# 中间节点每个 Cycle 的工作顺序

根据 `doc/core/async_port.md` 中的流程图，中间节点（如 Flow1）每个 cycle 应该按照以下顺序执行：

## 文档规定的顺序（流程图 s3 部分）

```
A: Start Cycle N
  ↓
等待上游 DoneUntil >= N  (n2 -> H)
  ↓
H: 获取数据：Chan() -> in_queue
  ↓
I: 下游反压无关逻辑模拟
  ↓
B: 检查下游.CheckReady(N)
  ↓
  ├─ True  -> C: 下游Ready时模拟逻辑 -> E: 发送数据 -> F: SetDoneUntil(N+1)
  └─ False -> D: 下游非Ready时模拟逻辑 -> F: SetDoneUntil(N+1)
  ↓
P: N++
  ↓
回到 A (下一个 cycle)
```

## 当前测试代码中的实现位置

在 `async_port_test.go` 的 `TestChainThreeFlows` 中，Flow1（中间节点）的实现：

```go
// Flow1: receives from Flow0, sends to Flow2
go func() {
    for cycle := 0; cycle < numCycles; cycle++ {
        // ✅ A: Start Cycle N
        
        // ✅ 等待上游 DoneUntil >= N
        for port01.GetDoneUntil() < cycle {
            // wait...
        }
        
        // ⚠️ 这里提前更新了下一个 cycle 的 ready（不在文档流程中）
        if cycle < numCycles-1 {
            port01.UpdateReady(cycle+1, true)
        }
        
        // ✅ H: 获取数据：Chan() -> in_queue
        case pkt := <-port01.ReceiveChan():
        
        // ❌ 缺少 I: 下游反压无关逻辑模拟
        
        // ⚠️ 这里直接设置了 ready，而不是检查
        port01.UpdateReady(cycle, true)
        
        // ⚠️ 这里直接设置了 Flow2 的 ready，而不是检查
        port12.UpdateReady(cycle, true)
        
        // ✅ B: 检查下游.CheckReady(N) (但逻辑不完整)
        if !port12.Ready(cycle) {
            // ❌ 没有实现 D: 下游非Ready时模拟逻辑
            return
        }
        
        // ✅ E: 发送数据
        port12.Chan() <- forwardPkt
        
        // ✅ F: SetDoneUntil(N+1)
        port12.SetDoneUntil(cycle + 1)
        
        // ✅ P: N++ (通过 for 循环实现)
    }
}()
```

## 问题分析

1. **缺少 I: 下游反压无关逻辑模拟** - 测试中没有实现这个步骤
2. **缺少 D: 下游非Ready时模拟逻辑** - 当 Ready(N) == false 时，应该执行特定逻辑，而不是直接返回
3. **Ready 检查的位置不对** - 应该在获取数据后、发送数据前检查
4. **没有实现 cycle 递增逻辑** - 当下游非 Ready 时，应该递增 cycle 后重试

## 应该实现的完整顺序

```go
for cycle := 0; cycle < numCycles; cycle++ {
    // A: Start Cycle N
    
    // 等待上游 DoneUntil >= N
    for upstreamPort.GetDoneUntil() < cycle {
        // wait...
    }
    
    // H: 获取数据：Chan() -> in_queue
    pkt := <-upstreamPort.ReceiveChan()
    
    // I: 下游反压无关逻辑模拟
    // (处理数据，不依赖下游状态)
    processData(pkt)
    
    // B: 检查下游.CheckReady(N)
    if downstreamPort.Ready(cycle) {
        // C: 下游Ready时模拟逻辑
        // E: 发送数据
        sendPacket(downstreamPort, cycle, processedData)
    } else {
        // D: 下游非Ready时模拟逻辑
        // 实现 cycle 递增逻辑
        actualCycle := cycle
        for !downstreamPort.Ready(actualCycle) {
            actualCycle++
        }
        // E: 发送数据（使用递增后的 cycle）
        sendPacket(downstreamPort, actualCycle, processedData)
    }
    
    // F: SetDoneUntil(N+1)
    upstreamPort.SetDoneUntil(cycle + 1)
    downstreamPort.SetDoneUntil(actualCycle + 1) // 或 cycle + 1
    
    // P: N++ (通过 for 循环自动递增)
}
```

## 总结

**当前实现位置**：`async_port_test.go` 的 `TestChainThreeFlows` 函数中，但**不完整**。

**缺失的部分**：
1. 下游反压无关逻辑模拟（步骤 I）
2. 下游非 Ready 时的处理逻辑（步骤 D）
3. 完整的 cycle 递增机制

**建议**：这些逻辑应该在 Flow/Link 的实际实现中完成，而不是在 ASyncPort 接口层。ASyncPort 只提供同步机制，具体的业务逻辑应该在上层实现。

