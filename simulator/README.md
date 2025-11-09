# Simulator 模块架构

该目录提供运行循环的辅助组件，拆分后的结构如下：

```
Simulator
  ├─ Runner (周期推进)
  ├─ CommandLoop (命令管理)
  └─ VisualBridge (可视化桥接)
```

- **Runner**：封装命令循环与可视化桥接，供主循环统一调度。
- **CommandLoop**：抽象命令来源与处理逻辑，负责串行消费控制指令。
- **VisualBridge**：对接可视化通道，集中管理 headless 状态与帧发布。
