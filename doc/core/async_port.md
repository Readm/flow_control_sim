
# Flow & Link 同步

Flow 和 Link 之间的交互都使用同一个接口
```go
type ASyncPort interface {
	SetDoneUntil(int)                 // 上游调用，更新 DoneUntil（实现约束，使用Atomic Store）
	Chan() <-chan PacketWithCycle     // 上游调用，获取可 push 的 channel
	Ready(cycle int) bool             // 上游调用，阻塞等待下游计算结果
}
```

Flow -> Link 和 Link -> Flow 都是一样的逻辑。下面，我们按方向称为上游和下游。

``` mermaid
---
config:
  layout: dagre
---
flowchart TB
 subgraph s4["CheckReady(Cycle)"]
        RU["ReadyUntil上游可以提前执行到ReadyUntil"]
        RM["readymap(cycle -> Ready)"]
        TRUE(["True"])
        FALSE(["False"])
        STALL["阻塞"]
        Q["计算N+1是否Ready?"]
        S4IN(["Start"])
  end
 subgraph s3["模拟逻辑"]
        A(["Start Cycle N"])
        H["获取数据：<br>Chan() -&gt; in_queue"]
        I["下游反压无关逻辑模拟"]
        D["下游非Ready时模拟逻辑"]
        E["发送数据 <br>下游.Chan() &lt;- (Packet, Cycle)"]
        C["下游Ready时模拟逻辑"]
        P["N++"]
        F(["SetDoneUntil(N+1)<br>如果可以预测可以Set更远, 例如Fixed Latency Link"])
        n2["上游DoneUntil=M"]
        s4
  end
 subgraph s1["上游同步"]
        K(["上游调用CheckReady(QueryCycle)"])
        M(["Return"])
        n3(["SetDoneUntil(M)"])
  end
 subgraph s2["下游同步"]
        B(["下游.CheckReady(N)"])
        n1["DoneUntil"]
  end
    RU -- Cycle &lt; ReadyUntil --> TRUE
    RU -- "Cycle >= ReadyUntil" --> RM
    RM -- 查询 --> TRUE & FALSE
    RM -- 无数据 wait --> STALL
    STALL -- 重新查询 --> RM
    Q -- Wakeup --> STALL
    S4IN --> RU
    K -- 可能阻塞 --> s4
    H -. ":=N+IdleSlot/Bandwidth" .-> RU
    H == 仿真逻辑 ==> I
    I == 可能阻塞 ==> B
    B == True ==> C
    B == False ==> D
    C ==> E
    F ==> P
    F --> Q
    F -.-> n1
    E ==> F
    Q -. update .-> RM
    Q -. "if True<br>Ready:=max(ReadyUntil, N+1)" .-> RU
    D ==> F
    P ==> A
    A == "<span style=background-color:>wait until M &gt; N</span>" ==> n2
    n2 ==> H
    n2 -. remove &lt; M .-> RM
    s4 --> s3 & M
    n3 -.-> n2

    RU@{ shape: card}
    RM@{ shape: card}
    H@{ shape: subproc}
    I@{ shape: subproc}
    D@{ shape: subproc}
    E@{ shape: subproc}
    C@{ shape: subproc}
    n2@{ shape: card}
    n1@{ shape: card}
    style I stroke-width:2px,stroke-dasharray: 0
    style s4 fill:#BBDEFB
```

在SetDoneUntil前，需要保证所有的包已经通过Chan()中发送完毕。
发包的逻辑是：先CheckReady(cycle) 如果为True，那么发送(packet, cycle);
上游可以配置自身的DoneUntil N，表示自身的N-1的交互已经完成，希望发送的Packet都发送完了。


每个下游开始执行第N Cycle时，需要检查上游的DoneUntil大于等于N。

Link 可以为0 cycle latency。时序图如下，此时
```mermaid
sequenceDiagram
    participant Flow0
    participant Link as "Link Latency 0"
    participant Flow1

    rect
        note right of Flow0: cycle 0
        Flow0->>Link: Set DoneUntil 1
        note right of Link: Release: Link Can finish Cycle 0
        Link->>Flow1: Set DoneUntil 0(cur) + 0(lat) + 1
        note right of Flow1: Flow Can finish Cycle 0
        note right of Link: All Component @ N, wait Src DoneUntil N+1
    end

    rect
        note right of Flow0: cycle 1
        Flow0->>Link: Packet @1, DoneUntil 2
        note right of Link: Release: Link Can finish Cycle 1
        Link->>Flow1: Packet @1, DoneUntil 1(cur) + 0(lat) + 1
        note right of Flow1: Flow Can finish Cycle 1
        note right of Link: All Component @ N, wait Src DoneUntil N+1
    end
```
