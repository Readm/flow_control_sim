# Ring Simulator 快速上手指南

## 🚀 5分钟快速开始

### 1. 启动模拟器
```bash
cd /home/readm/flow_sim
./bin/ring_simulator
```

### 2. 最简单的使用方式：加载测试场景

启动后，直接输入：
```bash
simulator> scenarios
```

你会看到4个预设场景：
- **test1**: 单包传输 (Worker0 → Worker1)
- **test2**: 两跳传输 (Worker0 → Worker2)
- **test3**: 反压和循环
- **test4**: 并发多包

### 3. 加载场景并观察

```bash
simulator> scenario test1
# 场景加载，packet已注入

simulator>
# 按回车执行第1个cycle，变化的地方会高亮（黄色）

simulator>
# 继续按回车观察packet传输

simulator>
simulator>
# 持续按回车，直到packet到达目的地
```

## 🎯 推荐学习路径

### 第1次运行：熟悉基本操作
```bash
simulator> scenario test1     # 加载最简单的场景
simulator>                    # 按5-10次回车，观察完整传输
```

**观察要点**：
- 注意黄色高亮的变化
- 看 `localIn=[1/8]` 如何变化
- 看link上的 `-[1]-` 表示packet在传输
- 看packet如何到达目标Worker

### 第2次运行：理解多跳路由
```bash
simulator> scenario test2     # 两跳传输
simulator>                    # 按15-20次回车
```

**观察要点**：
- packet如何经过多个Router
- 每个Router如何转发packet
- 总延迟 = 链路延迟 × 跳数

### 第3次运行：理解反压机制
```bash
simulator> scenario test3     # 反压场景
simulator>                    # 持续按回车观察循环
```

**观察要点**：
- packet无法ejected时继续在ring上传输
- 观察packet完整循环一圈
- 理解bufferless ring的核心特性

### 第4次运行：观察并发行为
```bash
simulator> scenario test4     # 并发传输
simulator>                    # 观察多个packet同时传输
```

**观察要点**：
- 两个packet在不同链路上同时传输
- Router如何处理concurrent traffic
- 高亮显示多处同时变化

## 💡 实用技巧

### 快速跳过多个cycles
```bash
simulator> run 10    # 自动执行10个cycles
```

### 关闭高亮（如果觉得分散注意力）
```bash
simulator> highlight off
```

### 手动注入packet（高级用法）
```bash
simulator> inject 0 3 my-packet
# 从Worker0发送到Worker3，payload是"my-packet"
```

### 查看帮助
```bash
simulator> help
```

## 📊 理解可视化输出

```
Ring Topology:

    R0[0/4]W0 ---- R1[0/4]W1
      ↓              ↓
    R3[0/4]W3 <--- R2[0/4]W2
```

**符号说明**：
- `R0[2/4]W0` = Router0，buffer占用2/4，连接Worker0
- `----` = 链路空闲
- `-[1]-` = 链路上有1个packet在传输（高亮表示刚变化）
- `[3/8]` = 队列有3个packet，容量8（高亮表示刚变化）
- **黄色高亮** = 这个值刚刚发生变化

## ⚡ 常见问题

**Q: 为什么packet不动了？**
A: 可能在link上传输中（需要5个cycles）。继续按回车等待。

**Q: 如何重置网络状态？**
A: 退出（quit）后重新启动模拟器。

**Q: 高亮太亮眼怎么办？**
A: 输入 `highlight off` 关闭高亮。

**Q: 想看更多cycles怎么办？**
A: 使用 `run 20` 快速执行20个cycles，然后查看最终状态。

**Q: 如何理解"反压"？**
A: 运行 `scenario test3`，观察当目标Worker忙时，packet会在ring上循环而不是被丢弃。

## 🎓 进阶学习

掌握基础后，可以尝试：

1. **自定义场景**：手动注入多个packet到不同目的地
2. **分析延迟**：计算packet从注入到到达的总cycles数
3. **测试饱和**：连续注入多个packet，观察队列填充情况
4. **混合测试**：先加载场景，再手动注入更多packet

---

**最重要的提示**：
- 👉 最开始就用 `scenario test1`，别自己手动注入
- 👉 每次只按一下回车，仔细观察高亮变化
- 👉 遇到不懂的就输入 `help`

祝学习愉快！🎉
