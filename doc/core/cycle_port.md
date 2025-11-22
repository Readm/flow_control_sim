
# CyclePort 同步

Flow 和 Link 之间的交互都使用同一个接口
```go
// CyclePort is a bidirectional interface for cycle-based synchronous communication between Flow and Link components.
// A single CyclePort instance provides both upstream and downstream operations:
// - Upstream component (e.g., Flow0) uses upstream operations to send packets and check downstream readiness.
// - Downstream component (e.g., Flow1) uses downstream operations to receive packets and wait for upstream completion.
// This bidirectional design allows the same port to be used from both perspectives, enabling flexible
// composition of Flow and Link components in a dataflow graph.
type CyclePort interface {
	// ===== Upstream Operations =====
	// These methods are called by the upstream component (the sender).

	// SetDoneUntil is called by upstream to notify downstream that it has completed processing up to cycle N-1.
	// DoneUntil N means:
	//   - Upstream has completed cycle N-1
	//   - All packets for cycle N-1 have been sent
	// This uses atomic store for thread-safe updates.
	// Downstream can use WaitForDoneUntil to block until this value reaches a target cycle.
	SetDoneUntil(cycle int)

	// Chan returns a write-only channel for upstream to push packets to downstream.
	// Upstream sends (Packet, Cycle) pairs through this channel.
	// The same channel is accessible to downstream via ReceiveChan().
	Chan() chan<- PacketWithCycle

	// Ready checks if downstream is ready to process the given cycle.
	// Called by upstream before sending a packet for a specific cycle.
	// Returns true if downstream is ready, false otherwise.
	// This method may block waiting for downstream to become ready.
	// Fast path: if cycle < ReadyUntil, returns true immediately.
	// Otherwise, queries readyMap or blocks until downstream signals readiness.
	Ready(cycle int) bool

	// ReadyNonBlocking checks if downstream is ready to process the given cycle without blocking.
	// Returns (ready, configured):
	//   - ready: true if downstream is ready, false otherwise
	//   - configured: true if the cycle is configured (readyMap contains it or readyUntil covers it),
	//                 false if the cycle is not configured and Ready() would block
	// This method never blocks and is useful for assertions and checking configuration status.
	ReadyNonBlocking(cycle int) (ready bool, configured bool)

	// GetDoneUntil returns the current DoneUntil value set by upstream.
	// Can be called by both upstream and downstream to check progress.
	// This is useful for upstream to verify its own progress, or for downstream
	// to check upstream completion status without blocking.
	GetDoneUntil() int

	// ===== Downstream Operations =====
	// These methods are called by the downstream component (the receiver).

	// ReceiveChan returns a read-only channel for downstream to receive packets from upstream.
	// This is the same underlying channel as Chan(), but from downstream's perspective.
	// Downstream reads (Packet, Cycle) pairs from this channel.
	ReceiveChan() <-chan PacketWithCycle

	// WaitForDoneUntil blocks the calling goroutine until upstream's DoneUntil >= targetCycle.
	// Called by downstream at the start of cycle N to ensure upstream has completed cycle N-1.
	// This uses condition variable to avoid busy waiting - the goroutine will block until
	// upstream calls SetDoneUntil with a value >= targetCycle.
	// Returns immediately if DoneUntil >= targetCycle (no blocking needed).
	WaitForDoneUntil(targetCycle int)
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
        H["获取数据：<br>合并pendingPackets + Chan() -&gt; in_queue"]
        I_Packet["下游反压无关逻辑模拟<br/>有数据包时"]
        I_NoPacket["下游反压无关逻辑模拟<br/>无数据包时也执行"]
        PENDING["保存到pendingPackets<br/>在下一个cycle再次检查"]
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
        B_Packet(["下游.CheckReady(pktCycle)<br/>有数据包时，只检查一次，不循环递增"])
        B_NoPacket(["下游.CheckReady(cycle)<br/>无数据包时也检查"])
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
    H --> CheckData{有数据包?}
    CheckData -->|Yes| I_Packet
    CheckData -->|No| I_NoPacket
    I_Packet == 可能阻塞 ==> B_Packet
    I_NoPacket == 可能阻塞 ==> B_NoPacket
    B_Packet == True ==> C
    B_Packet == False ==> PENDING
    B_NoPacket ==> F
    C ==> E
    F ==> P
    F --> Q
    F -.-> n1
    E ==> F
    PENDING ==> F
    Q -. update .-> RM
    Q -. "if True<br>Ready:=max(ReadyUntil, N+1)" .-> RU
    P ==> A
    A == "<span style=background-color:>wait until M &gt; N</span>" ==> n2
    n2 ==> H
    n2 -. remove &lt; M .-> RM
    s4 --> s3 & M
    n3 -.-> n2

    RU@{ shape: card}
    RM@{ shape: card}
    H@{ shape: subproc}
    I_Packet@{ shape: subproc}
    I_NoPacket@{ shape: subproc}
    PENDING@{ shape: subproc}
    E@{ shape: subproc}
    C@{ shape: subproc}
    n2@{ shape: card}
    n1@{ shape: card}
    style I_Packet stroke-width:2px,stroke-dasharray: 0
    style I_NoPacket stroke-width:2px,stroke-dasharray: 0
    style I_NoPacket fill:#E6F3FF
    style PENDING fill:#FFF4E6
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
    participant L as Link
    participant Flow1

    rect
        note right of Flow0: cycle 0
        Flow0->>L: Set DoneUntil 1
        note right of L: Release: Link Can finish Cycle 0
        L->>Flow1: Set DoneUntil 0(cur) + 0(lat) + 1
        note right of Flow1: Flow Can finish Cycle 0
        note right of L: All Component @ N, wait Src DoneUntil N+1
    end

    rect
        note right of Flow0: cycle 1
        Flow0->>L: Packet @1, DoneUntil 2
        note right of L: Release: Link Can finish Cycle 1
        L->>Flow1: Packet @1, DoneUntil 1(cur) + 0(lat) + 1
        note right of Flow1: Flow Can finish Cycle 1
        note right of L: All Component @ N, wait Src DoneUntil N+1
    end
```
