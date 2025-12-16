# Bufferless Ring Network Interactive Simulator

交互式命令行工具，用于单步执行和可视化 bufferless ring 网络的 packet 传输过程。

## 构建

```bash
cd /home/readm/flow_sim
go build -o bin/ring_simulator ./cmd/ring_simulator/
```

## 运行

```bash
./bin/ring_simulator
```

## 使用方法

### 启动后会显示初始网络状态：

```
╔════════════════════════════════════════════════════════════════╗
║     Bufferless Ring Network Interactive Simulator             ║
╚════════════════════════════════════════════════════════════════╝

✅ Network initialized with 4 routers in a ring topology

╔═══════════════════════════════════════════════════════════════╗
║ Cycle 0  : Bufferless Ring Network                         ║
╚═══════════════════════════════════════════════════════════════╝

Ring Topology:

    R0[0/4]W0 ---- R1[0/4]W1
      ↓              ↓
    R3[0/4]W3 <--- R2[0/4]W2

Router Queues:
  R0: ringIn=[0/8] localIn=[0/8] ringOut=[0/8] localOut=[0/8]
  ...
```

### 可用命令：

| 命令 | 说明 | 示例 |
|------|------|------|
| `[Enter]` | 执行下一个 cycle | 直接按回车 |
| `inject <src> <dst> [payload]` | 注入 packet | `inject 0 1 hello` |
| `scenario <name>` | 加载测试场景 | `scenario test1` |
| `scenarios` | 列出所有可用场景 | `scenarios` |
| `highlight on/off` | 开关变化高亮 | `highlight on` |
| `run <n>` | 自动运行 n 个 cycles | `run 10` |
| `quit` / `q` | 退出模拟器 | `quit` |
| `help` / `h` | 显示帮助信息 | `help` |

### 新功能

#### 🎨 变化高亮
- 默认开启，变化的值会用**黄色**高亮显示
- 帮助你快速识别状态变化
- 可通过 `highlight off` 关闭

#### 📋 测试场景
快速加载预设的测试场景：

| 场景 | 名称 | 说明 |
|------|------|------|
| `test1` | Single Packet (1-hop) | Worker0 → Worker1 单包传输 |
| `test2` | Two Hops | Worker0 → Worker2 跨2跳传输 |
| `test3` | Backpressure & Circulation | 反压场景，packet在ring上循环 |
| `test4` | Concurrent Multi-Packet | 并发多包传输 |

## 使用示例

### 🆕 快速开始：使用测试场景

**最简单的方式**：直接加载测试场景
```bash
simulator> scenarios          # 查看所有场景
simulator> scenario test1     # 加载Test1场景
simulator>                    # 按Enter单步观察
simulator>                    # 继续按Enter
# 变化的部分会用黄色高亮显示！
```

### 示例 1: 使用场景观察单包传输

```bash
simulator> scenario test1
📋 Loading scenario: Single Packet (1-hop)
   Worker0 → Worker1 single packet transmission
✅ Scenario loaded successfully

# 现在单步执行，观察高亮变化
simulator>                    # Cycle 1: localIn=[1/8] 变黄
simulator>                    # Cycle 2: link显示 -[1]- 变黄
simulator>                    # 持续观察...
```

### 示例 2: 观察反压场景

```bash
simulator> scenario test3
# Worker1的队列已被填满
simulator>                    # 单步观察packet无法ejected
simulator>                    # 继续观察packet在ring上循环
```

### 示例 3: 观察并发传输

```bash
simulator> scenario test4
# 两个packet同时注入
simulator>                    # 观察两个packet在不同link上传输
# 高亮会同时显示多处变化
```

### 示例 4: 手动单步执行观察 packet 传输（传统方式）

```bash
simulator> inject 0 1 test        # 从 Worker0 发送到 Worker1
✅ Injected packet: W0 → W1 (test)

simulator>                         # 按回车执行 cycle 1
# 看到 packet 在 localIn 队列

simulator>                         # 按回车执行 cycle 2
# 看到 packet 在 link 上传输 -[1]-

simulator>                         # 继续按回车
# ... 5 个 cycle 后 ...

simulator>                         # cycle 8
# 看到 packet 到达 R1 的 ringIn

simulator>                         # cycle 9
# 看到 packet 到达 Worker1 的 In 队列
```

### 示例 2: 观察多跳传输

```bash
simulator> inject 0 2              # Worker0 → Worker2 (需经过2跳)
simulator> run 20                  # 自动运行20个cycles
✅ Executed 20 cycles
# 查看 packet 如何经过 R0 → R1 → R2
```

### 示例 3: 观察并发传输

```bash
simulator> inject 0 1 pkt1
simulator> inject 2 3 pkt2
simulator>                         # 单步观察两个 packet 同时在 ring 上传输
```

### 示例 4: 观察反压和绕环

```bash
# 先填满 Worker1 的输入队列
simulator> inject 0 1 dummy1
simulator> inject 0 1 dummy2
simulator> inject 0 1 dummy3
simulator> run 5

# 再注入一个到 Worker1 的 packet
simulator> inject 0 1 blocked
# 单步观察这个 packet 因为 Worker1 busy 而在 ring 上循环
```

## 网络拓扑

```
4个 Router 组成环形拓扑:
Router0 → Router1 → Router2 → Router3 → Router0 (loop)

每个 Router 连接一个 Worker:
Router0 ↔ Worker0
Router1 ↔ Worker1
Router2 ↔ Worker2
Router3 ↔ Worker3
```

## 可视化符号说明

- `R0[2/4]W0`: Router0，buffer占用2/4，连接Worker0
- `----`: 链路空闲
- `-[1]-`: 链路上有1个packet在传输
- `[3/8]`: 队列有3个packet，容量8
- `↓`: 数据流向

## 网络参数

- **Routers**: 4个 (ID: 100-103)
- **Workers**: 4个 (ID: 0-3)
- **Ring Latency**: 5 cycles
- **Router Buffer**: 4 packets (仅用于local injection)
- **Queue Size**: 8 packets
- **Bandwidth**: 2 packets/cycle

## 关键观察点

1. **Local Injection**: 观察 packet 从 Worker 进入 Router 的 localIn 队列
2. **Ring Transit**: 观察 packet 在 link 上传输（latency=5 cycles）
3. **Ejection**: 观察 Router 识别目标并将 packet ejected 到 localOut
4. **Circulation**: 如果目标 Worker busy，packet 会在 ring 上循环
5. **Buffer Usage**: 观察 Router 的 injection buffer 何时被使用

## 提示

- 每次按 Enter 只前进1个 cycle，方便精确观察
- 使用 `run <n>` 快速跳过多个 cycles
- 注意 link 的 latency (5 cycles)，packet 传输需要时间
- 观察 buffer 占用变化，理解 bufferless ring 的工作原理
