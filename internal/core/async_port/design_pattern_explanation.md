# Go 语言中的"基类"模式实现

## 问题

如何在 Go 中实现类似面向对象语言中"基类"的模式，让其他实现可以按照一定规则实现某种函数？

## Go 语言的解决方案

Go 没有传统面向对象语言的"基类"概念，但可以通过以下模式实现类似效果：

### 1. 接口（Interface）+ 组合（Composition）

**核心思想**：定义接口作为"契约"，通过组合提供默认实现。

```go
// 1. 定义接口（契约）
type ProcessorHooks interface {
    Step1()
    Step2()
    Step3()
}

// 2. 提供默认实现（类似基类）
type DefaultHooks struct{}

func (d *DefaultHooks) Step1() { /* 默认实现 */ }
func (d *DefaultHooks) Step2() { /* 默认实现 */ }
func (d *DefaultHooks) Step3() { /* 默认实现 */ }

// 3. 自定义实现（类似派生类）
type CustomHooks struct {
    *DefaultHooks  // 嵌入默认实现
}

// 只覆盖需要的方法
func (c *CustomHooks) Step1() {
    // 自定义实现
    c.DefaultHooks.Step1()  // 可选：调用基类实现
}
```

### 2. 模板方法模式（Template Method Pattern）

**核心思想**：在"基类"中定义算法骨架，具体步骤通过接口/函数参数注入。

```go
// 基类：定义算法流程
type BaseProcessor struct {
    hooks ProcessorHooks
}

func (b *BaseProcessor) Process() {
    b.hooks.Step1()  // 调用钩子
    b.hooks.Step2()  // 调用钩子
    b.hooks.Step3()  // 调用钩子
}

// 使用
hooks := &CustomHooks{&DefaultHooks{}}
processor := &BaseProcessor{hooks: hooks}
processor.Process()  // 执行完整流程
```

### 3. 函数类型（Function Types）

**核心思想**：将步骤定义为函数类型，作为参数传递。

```go
type StepFunc func(int) error

type Processor struct {
    step1 StepFunc
    step2 StepFunc
    step3 StepFunc
}

func (p *Processor) Process(cycle int) error {
    if err := p.step1(cycle); err != nil {
        return err
    }
    if err := p.step2(cycle); err != nil {
        return err
    }
    return p.step3(cycle)
}

// 使用
processor := &Processor{
    step1: func(cycle int) error { /* 实现 */ },
    step2: func(cycle int) error { /* 实现 */ },
    step3: func(cycle int) error { /* 实现 */ },
}
```

## 在我们的场景中的应用

### 方案选择

我们选择了 **接口 + 组合 + 模板方法模式**，因为：

1. **接口**：定义了 `CycleProcessorHooks`，明确需要实现的步骤
2. **组合**：`DefaultHooks` 提供默认实现，可以被嵌入
3. **模板方法**：`CycleProcessor.ProcessCycle()` 定义了固定的流程

### 实现结构

```
CycleProcessor (模板方法)
    ├─ ProcessCycle() - 固定流程
    │   ├─ OnCycleStart() - 钩子
    │   ├─ OnDataReceived() - 钩子
    │   ├─ OnDownstreamBackpressureIndependentLogic() - 钩子
    │   ├─ OnDownstreamReady() / OnDownstreamNotReady() - 钩子
    │   └─ OnCycleEnd() - 钩子
    │
    └─ CycleProcessorHooks (接口)
        ├─ DefaultHooks (默认实现)
        ├─ FIFOFlowHooks (自定义实现)
        └─ PriorityFlowHooks (自定义实现)
```

### 使用示例

```go
// 1. 创建自定义 hooks
hooks := NewFIFOFlowHooks(flowID: 1)

// 2. 创建 processor（使用模板方法）
processor := NewCycleProcessor(upstreamPort, downstreamPort, hooks)

// 3. 执行流程（固定流程，但使用自定义 hooks）
for cycle := 0; cycle < 10; cycle++ {
    processor.ProcessCycle(cycle)
}
```

## 优势

1. **流程固定**：`ProcessCycle()` 确保所有实现都遵循相同的流程
2. **灵活扩展**：通过实现不同的 hooks，可以自定义每个步骤
3. **代码复用**：`DefaultHooks` 提供默认实现，减少重复代码
4. **类型安全**：接口确保所有必需的方法都被实现
5. **易于测试**：可以轻松创建 mock hooks 进行测试

## 与其他语言的对比

| 特性 | C++/Java (基类) | Go (接口+组合) |
|------|----------------|----------------|
| 继承 | 类继承 | 接口实现 + 组合 |
| 默认实现 | 基类方法 | 嵌入结构体 |
| 多态 | 虚函数 | 接口方法 |
| 强制实现 | 抽象方法 | 接口方法 |
| 多重继承 | 支持 | 通过组合实现 |

## 总结

在 Go 中实现"基类"模式的最佳实践：

1. **定义接口**：明确需要实现的方法
2. **提供默认实现**：通过嵌入结构体提供默认行为
3. **使用模板方法**：在"基类"中定义固定流程
4. **组合优于继承**：通过组合实现代码复用

这种模式既保持了流程的一致性，又提供了足够的灵活性让不同实现自定义行为。

