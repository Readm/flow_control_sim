# Flow Simulation Project Memory

## General Guidelines
- **主要语言**: 中文 (Chinese)。请始终使用中文与用户交流，包括解释、文档和提交信息。


## Testing Configuration
- **Go Test Timeout**: 所有 `go test` 命令使用 `-timeout=3s` 标志
  - 示例: `go test -timeout=3s ./...`
  - 原因: 防止测试无限期挂起

- **测试失败处理原则**:
  - 当测试不通过时，首先分析是否可能是测试本身构建的不合理
  - 检查测试的假设、断言和测试逻辑是否合理
  - 如果怀疑是测试问题而非代码问题，向用户汇报分析结果
  - 不要盲目修改代码以通过可能存在问题的测试

## Decision Making Protocol
- **重要规则**: 当有多个不同的技术方案/选项时，必须停下来征询用户意见
- 不要自行选择方案并实施，应该：
  1. 列出所有可能的选项
  2. 分析每个选项的优缺点
  3. 等待用户明确选择
  4. 然后再实施用户选择的方案
## Design Principles
- **零丢包 (Zero-Drop Packet)**: 仿真系统在任何情况下都不应发生静默丢包 (Silent Drop)。
  - 如果缓冲区溢出，必须通过 `UpdateReady` 协议产生背压，或返回 `error` 中断仿真。
  - 对于 Bufferless 链路，如果下游未就绪，包应缓存在链路内（pending）模拟延迟，而非丢弃。
  - 对于超出同步周期的包，必须抛出 `panic` 以暴露逻辑错误。

## Project Information
- 项目名称: 流仿真
- 语言: Go
- 主要模块:
  - node (节点)
  - link (链路)
  - network (网络)
  - ahead_port (提前端口)
