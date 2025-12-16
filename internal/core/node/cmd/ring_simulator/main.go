package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// QueueCollection holds all queues for the bufferless ring network.
type QueueCollection struct {
	RingIn    []*queue.InputQueue
	RingOut   []*queue.OutputQueue
	LocalIn   []*queue.InputQueue
	LocalOut  []*queue.OutputQueue
	WorkerIn  []*queue.InputQueue
	WorkerOut []*queue.OutputQueue
}

// NetworkState 网络状态快照（用于高亮变化）
type NetworkState struct {
	routerBuffers  [4]int
	queueLengths   map[string]int
	linkPackets    [4]int
}

// RingSimulator 交互式ring网络模拟器
type RingSimulator struct {
	routers      []*node.BufferlessRingRouterNode
	workers      []*node.Node
	queues       *QueueCollection
	ringLinks    []*link.Link
	cycle        int
	ctx          context.Context
	prevState    *NetworkState
	highlightOn  bool
}

// NewRingSimulator 创建一个新的ring模拟器
func NewRingSimulator() *RingSimulator {
	const (
		numRouters     = 4
		ringLatency    = 5
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
	)

	// Create routers and workers
	routers := make([]*node.BufferlessRingRouterNode, numRouters)
	workers := make([]*node.Node, numRouters)

	for i := 0; i < numRouters; i++ {
		routerID := 100 + i
		workerID := i
		routers[i] = node.NewBufferlessRingRouter(routerID, workerID, routerBuffer)
		workers[i] = node.New(workerID)
	}

	// Create queues
	ringInQueues := make([]*queue.InputQueue, numRouters)
	ringOutQueues := make([]*queue.OutputQueue, numRouters)
	localInQueues := make([]*queue.InputQueue, numRouters)
	localOutQueues := make([]*queue.OutputQueue, numRouters)
	workerInQueues := make([]*queue.InputQueue, numRouters)
	workerOutQueues := make([]*queue.OutputQueue, numRouters)

	for i := 0; i < numRouters; i++ {
		ringInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth, queueBandwidth)
		ringOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		localInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth, queueBandwidth)
		localOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		workerInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth, queueBandwidth)
		workerOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
	}

	// Connect queues to routers
	for i := 0; i < numRouters; i++ {
		routers[i].AddInputQueue(ringInQueues[i])
		routers[i].AddInputQueue(localInQueues[i])
		routers[i].AddOutputQueue(ringOutQueues[i])
		routers[i].AddOutputQueue(localOutQueues[i])
	}

	// Connect queues to workers
	for i := 0; i < numRouters; i++ {
		workers[i].AddInputQueue(workerInQueues[i])
		workers[i].AddOutputQueue(workerOutQueues[i])
	}

	// Create bufferless ring links
	ringLinks := make([]*link.Link, numRouters)
	for i := 0; i < numRouters; i++ {
		nextRouter := (i + 1) % numRouters
		sourceID := 100 + i
		targetID := 100 + nextRouter

		fc := link.NewBufferlessFlowControl()
		ringLink, linkIn, linkOut := link.NewLinkWithFlowControl(sourceID, targetID, ringLatency, 1, fc)
		ringLinks[i] = ringLink

		linkIn.Plug(ringOutQueues[i].QueueOutPort())
		linkOut.Plug(ringInQueues[nextRouter].AsInPort())
	}

	// Create local connections
	for i := 0; i < numRouters; i++ {
		workerOutQueues[i].QueueOutPort().Plug(localInQueues[i].QueueInPort())
		localOutQueues[i].QueueOutPort().Plug(workerInQueues[i].QueueInPort())
	}

	queues := &QueueCollection{
		RingIn:    ringInQueues,
		RingOut:   ringOutQueues,
		LocalIn:   localInQueues,
		LocalOut:  localOutQueues,
		WorkerIn:  workerInQueues,
		WorkerOut: workerOutQueues,
	}

	return &RingSimulator{
		routers:     routers,
		workers:     workers,
		queues:      queues,
		ringLinks:   ringLinks,
		cycle:       0,
		ctx:         context.Background(),
		highlightOn: true, // 默认开启高亮
	}
}

// captureState 捕获当前网络状态
func (sim *RingSimulator) captureState() *NetworkState {
	state := &NetworkState{
		queueLengths: make(map[string]int),
	}

	// 捕获router buffer状态
	for i := 0; i < 4; i++ {
		state.routerBuffers[i] = sim.routers[i].GetInjectionBufferOccupancy()
	}

	// 捕获队列长度
	for i := 0; i < 4; i++ {
		state.queueLengths[fmt.Sprintf("ringIn%d", i)] = sim.queues.RingIn[i].Length()
		state.queueLengths[fmt.Sprintf("ringOut%d", i)] = sim.queues.RingOut[i].Length()
		state.queueLengths[fmt.Sprintf("localIn%d", i)] = sim.queues.LocalIn[i].Length()
		state.queueLengths[fmt.Sprintf("localOut%d", i)] = sim.queues.LocalOut[i].Length()
		state.queueLengths[fmt.Sprintf("workerIn%d", i)] = sim.queues.WorkerIn[i].Length()
		state.queueLengths[fmt.Sprintf("workerOut%d", i)] = sim.queues.WorkerOut[i].Length()
	}

	// 捕获link上的packet数量
	for i := 0; i < 4; i++ {
		if sim.ringLinks[i].Latency() > 0 {
			state.linkPackets[i] = len(sim.ringLinks[i].SnapshotOccupancy())
		}
	}

	return state
}

// Step 执行一个cycle
func (sim *RingSimulator) Step() error {
	// 捕获执行前的状态
	sim.prevState = sim.captureState()

	// Tick all components
	for _, r := range sim.routers {
		if err := r.Tick(sim.ctx, uint64(sim.cycle), 0); err != nil {
			return fmt.Errorf("router tick failed: %w", err)
		}
	}
	for _, w := range sim.workers {
		if err := w.Tick(sim.ctx, uint64(sim.cycle), 0); err != nil {
			return fmt.Errorf("worker tick failed: %w", err)
		}
	}

	// Tick all input queues
	allQueues := [][]*queue.InputQueue{sim.queues.RingIn, sim.queues.LocalIn, sim.queues.WorkerIn}
	for _, queueList := range allQueues {
		for _, q := range queueList {
			if err := q.Tick(sim.cycle); err != nil {
				return fmt.Errorf("input queue tick failed: %w", err)
			}
		}
	}

	// Tick all output queues
	allOutputQueues := [][]*queue.OutputQueue{sim.queues.RingOut, sim.queues.LocalOut, sim.queues.WorkerOut}
	for _, queueList := range allOutputQueues {
		for _, q := range queueList {
			if err := q.Tick(sim.cycle); err != nil {
				return fmt.Errorf("output queue tick failed: %w", err)
			}
		}
	}

	// Tick all links
	for _, l := range sim.ringLinks {
		if err := l.Tick(sim.cycle); err != nil {
			return fmt.Errorf("link tick failed: %w", err)
		}
	}

	sim.cycle++
	return nil
}

// InjectPacket 注入一个packet
func (sim *RingSimulator) InjectPacket(sourceWorker, targetWorker int, payload string) error {
	if sourceWorker < 0 || sourceWorker >= 4 {
		return fmt.Errorf("invalid source worker: %d (must be 0-3)", sourceWorker)
	}
	if targetWorker < 0 || targetWorker >= 4 {
		return fmt.Errorf("invalid target worker: %d (must be 0-3)", targetWorker)
	}

	pkt := packet.Packet{
		SourceID: sourceWorker,
		TargetID: targetWorker,
		Payload:  payload,
	}

	return sim.queues.WorkerOut[sourceWorker].InjectPackets(sim.cycle, []packet.Packet{pkt})
}

// ANSI颜色码
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
)

// highlight 如果高亮开启且值变化，则添加颜色
func (sim *RingSimulator) highlight(text string, changed bool) string {
	if !sim.highlightOn || !changed {
		return text
	}
	return colorYellow + colorBold + text + colorReset
}

// hasChanged 检查某个值是否变化
func (sim *RingSimulator) hasChanged(key string, currentValue int) bool {
	if sim.prevState == nil {
		return false
	}
	if prevValue, ok := sim.prevState.queueLengths[key]; ok {
		return prevValue != currentValue
	}
	return false
}

// Visualize 显示当前网络状态
func (sim *RingSimulator) Visualize() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n╔═══════════════════════════════════════════════════════════════╗\n"))
	sb.WriteString(fmt.Sprintf("║ Cycle %-3d: Bufferless Ring Network                         ║\n", sim.cycle))
	sb.WriteString(fmt.Sprintf("╚═══════════════════════════════════════════════════════════════╝\n\n"))

	// Ring topology
	sb.WriteString("Ring Topology:\n\n")

	// 检查router buffer和link变化
	r0Changed := sim.prevState != nil && sim.prevState.routerBuffers[0] != sim.routers[0].GetInjectionBufferOccupancy()
	r1Changed := sim.prevState != nil && sim.prevState.routerBuffers[1] != sim.routers[1].GetInjectionBufferOccupancy()
	link0Changed := sim.prevState != nil && sim.prevState.linkPackets[0] != len(sim.ringLinks[0].SnapshotOccupancy())

	sb.WriteString(fmt.Sprintf("    %s %s %s\n",
		sim.highlight(sim.routers[0].GetVisualState(), r0Changed),
		sim.highlight(sim.ringLinks[0].GetVisualState(), link0Changed),
		sim.highlight(sim.routers[1].GetVisualState(), r1Changed)))
	sb.WriteString("      ↓              ↓\n")

	r3Changed := sim.prevState != nil && sim.prevState.routerBuffers[3] != sim.routers[3].GetInjectionBufferOccupancy()
	r2Changed := sim.prevState != nil && sim.prevState.routerBuffers[2] != sim.routers[2].GetInjectionBufferOccupancy()

	sb.WriteString(fmt.Sprintf("    %s %s %s\n",
		sim.highlight(sim.routers[3].GetVisualState(), r3Changed),
		"<---",
		sim.highlight(sim.routers[2].GetVisualState(), r2Changed)))
	sb.WriteString("\n")

	// Queue status
	sb.WriteString("Router Queues:\n")
	for i := 0; i < 4; i++ {
		ringInKey := fmt.Sprintf("ringIn%d", i)
		localInKey := fmt.Sprintf("localIn%d", i)
		ringOutKey := fmt.Sprintf("ringOut%d", i)
		localOutKey := fmt.Sprintf("localOut%d", i)

		sb.WriteString(fmt.Sprintf("  R%d: ringIn=%s localIn=%s ringOut=%s localOut=%s\n",
			i,
			sim.highlight(sim.queues.RingIn[i].GetVisualState(), sim.hasChanged(ringInKey, sim.queues.RingIn[i].Length())),
			sim.highlight(sim.queues.LocalIn[i].GetVisualState(), sim.hasChanged(localInKey, sim.queues.LocalIn[i].Length())),
			sim.highlight(sim.queues.RingOut[i].GetVisualState(), sim.hasChanged(ringOutKey, sim.queues.RingOut[i].Length())),
			sim.highlight(sim.queues.LocalOut[i].GetVisualState(), sim.hasChanged(localOutKey, sim.queues.LocalOut[i].Length()))))
	}
	sb.WriteString("\n")

	// Worker queues
	sb.WriteString("Worker Queues:\n")
	for i := 0; i < 4; i++ {
		workerOutKey := fmt.Sprintf("workerOut%d", i)
		workerInKey := fmt.Sprintf("workerIn%d", i)

		sb.WriteString(fmt.Sprintf("  W%d: Out=%s In=%s\n",
			i,
			sim.highlight(sim.queues.WorkerOut[i].GetVisualState(), sim.hasChanged(workerOutKey, sim.queues.WorkerOut[i].Length())),
			sim.highlight(sim.queues.WorkerIn[i].GetVisualState(), sim.hasChanged(workerInKey, sim.queues.WorkerIn[i].Length()))))
	}

	sb.WriteString("\n")
	if sim.highlightOn {
		sb.WriteString(fmt.Sprintf("Legend: R=Router[buf/cap]W=Worker, [len/cap], -[n]-=packets, %sYELLOW=changed%s\n", colorYellow+colorBold, colorReset))
	} else {
		sb.WriteString("Legend: R=Router[buf/cap]W=Worker, [len/cap], -[n]-=packets in flight\n")
	}
	sb.WriteString("═══════════════════════════════════════════════════════════════\n")

	return sb.String()
}

// TestScenario 测试场景定义
type TestScenario struct {
	Name        string
	Description string
	Setup       func(*RingSimulator) error
}

// 预定义测试场景
var testScenarios = map[string]TestScenario{
	"test1": {
		Name:        "Single Packet (1-hop)",
		Description: "Worker0 → Worker1 single packet transmission",
		Setup: func(sim *RingSimulator) error {
			return sim.InjectPacket(0, 1, "Test1-Packet")
		},
	},
	"test2": {
		Name:        "Two Hops",
		Description: "Worker0 → Worker2 crossing 2 hops",
		Setup: func(sim *RingSimulator) error {
			return sim.InjectPacket(0, 2, "Test2-Packet")
		},
	},
	"test3": {
		Name:        "Backpressure & Circulation",
		Description: "Packet circulates when destination busy",
		Setup: func(sim *RingSimulator) error {
			// 策略：向Worker1大量注入packets，利用Worker不消费的特性
			// WorkerIn[1]容量8，持续注入超过容量的packets
			// 早期到达的packets填满WorkerIn[1] → localOut[1]满
			// 后续到达的packets遇到反压 → 在ring上循环

			// 从Worker0注入5个packets (1-hop, ~8 cycles到达)
			// 这些会最先到达，填满WorkerIn[1]的大部分空间
			for i := 0; i < 5; i++ {
				if err := sim.InjectPacket(0, 1, fmt.Sprintf("W0-Pkt%d", i)); err != nil {
					return err
				}
			}

			// 从Worker2注入4个packets (3-hop, ~20 cycles到达)
			// 到达时WorkerIn[1]已满，会在ring上循环
			for i := 0; i < 4; i++ {
				if err := sim.InjectPacket(2, 1, fmt.Sprintf("W2-Pkt%d", i)); err != nil {
					return err
				}
			}

			// 从Worker3注入3个packets (2-hop, ~14 cycles到达)
			// 也会遇到拥塞
			for i := 0; i < 3; i++ {
				if err := sim.InjectPacket(3, 1, fmt.Sprintf("W3-Pkt%d", i)); err != nil {
					return err
				}
			}

			// 总计12个packets，全部发往Worker1
			// WorkerIn[1]容量仅8，必然产生反压和circulation

			// 关键：禁止Worker1 Pick packets，让packets堆积在WorkerIn[1]
			sim.workers[1].SetPickHook(func(ctx context.Context, cycle uint64, defaultPick func() []packet.Packet) ([]packet.Packet, error) {
				// 返回空数组，不Pick任何packet，模拟Worker1繁忙无法处理
				return []packet.Packet{}, nil
			})

			return nil
		},
	},
	"test4": {
		Name:        "Concurrent Multi-Packet",
		Description: "Multiple packets transmitting simultaneously",
		Setup: func(sim *RingSimulator) error {
			if err := sim.InjectPacket(0, 1, "Pkt-0to1"); err != nil {
				return err
			}
			if err := sim.InjectPacket(2, 3, "Pkt-2to3"); err != nil {
				return err
			}
			return nil
		},
	},
}

func main() {
	// 设置可视化模式
	visualization.VisualizationMode = "ascii"

	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║     Bufferless Ring Network Interactive Simulator             ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 创建模拟器
	sim := NewRingSimulator()

	fmt.Println("✅ Network initialized with 4 routers in a ring topology")
	fmt.Println()

	// 显示初始状态
	fmt.Println(sim.Visualize())

	// 交互式循环
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("\nCommands:")
	fmt.Println("  [Enter]              - Execute next cycle")
	fmt.Println("  inject <src> <dst> [payload] - Inject packet")
	fmt.Println("  scenario <name>      - Load test scenario (test1-4)")
	fmt.Println("  scenarios            - List available scenarios")
	fmt.Println("  highlight on/off     - Toggle change highlighting")
	fmt.Println("  run <n>              - Run n cycles automatically")
	fmt.Println("  quit                 - Exit simulator")
	fmt.Println()

	for {
		fmt.Print("simulator> ")
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())

		// 空行 = 执行下一个cycle
		if line == "" {
			if err := sim.Step(); err != nil {
				fmt.Printf("❌ Error: %v\n", err)
				continue
			}
			fmt.Println(sim.Visualize())
			continue
		}

		// 解析命令
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		cmd := parts[0]

		switch cmd {
		case "quit", "q", "exit":
			fmt.Println("👋 Goodbye!")
			return

		case "inject", "i":
			if len(parts) < 3 {
				fmt.Println("❌ Usage: inject <src> <dst> [payload]")
				continue
			}

			src, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Printf("❌ Invalid source: %s\n", parts[1])
				continue
			}

			dst, err := strconv.Atoi(parts[2])
			if err != nil {
				fmt.Printf("❌ Invalid destination: %s\n", parts[2])
				continue
			}

			payload := "packet"
			if len(parts) > 3 {
				payload = strings.Join(parts[3:], " ")
			}

			if err := sim.InjectPacket(src, dst, payload); err != nil {
				fmt.Printf("❌ Error: %v\n", err)
				continue
			}

			fmt.Printf("✅ Injected packet: W%d → W%d (%s)\n", src, dst, payload)
			fmt.Println(sim.Visualize())

		case "run", "r":
			if len(parts) < 2 {
				fmt.Println("❌ Usage: run <n>")
				continue
			}

			n, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Printf("❌ Invalid count: %s\n", parts[1])
				continue
			}

			for i := 0; i < n; i++ {
				if err := sim.Step(); err != nil {
					fmt.Printf("❌ Error at cycle %d: %v\n", sim.cycle, err)
					break
				}
			}
			fmt.Printf("✅ Executed %d cycles\n", n)
			fmt.Println(sim.Visualize())

		case "scenario", "s":
			if len(parts) < 2 {
				fmt.Println("❌ Usage: scenario <name> (e.g., 'scenario test1')")
				fmt.Println("   Type 'scenarios' to list available scenarios")
				continue
			}

			scenarioName := parts[1]
			scenario, ok := testScenarios[scenarioName]
			if !ok {
				fmt.Printf("❌ Unknown scenario: %s\n", scenarioName)
				fmt.Println("   Type 'scenarios' to list available scenarios")
				continue
			}

			fmt.Printf("📋 Loading scenario: %s\n", scenario.Name)
			fmt.Printf("   %s\n", scenario.Description)

			if err := scenario.Setup(sim); err != nil {
				fmt.Printf("❌ Error setting up scenario: %v\n", err)
				continue
			}

			fmt.Println("✅ Scenario loaded successfully")
			fmt.Println(sim.Visualize())

		case "scenarios":
			fmt.Println("\n📚 Available Test Scenarios:")
			fmt.Println()
			scenarios := []string{"test1", "test2", "test3", "test4"}
			for _, name := range scenarios {
				scenario := testScenarios[name]
				fmt.Printf("  %s%-6s%s - %s\n", colorCyan, name, colorReset, scenario.Name)
				fmt.Printf("           %s\n", scenario.Description)
			}
			fmt.Println("\nUsage: scenario <name>")
			fmt.Println()

		case "highlight":
			if len(parts) < 2 {
				fmt.Printf("Current highlight mode: %v\n", sim.highlightOn)
				fmt.Println("Usage: highlight on/off")
				continue
			}

			switch parts[1] {
			case "on":
				sim.highlightOn = true
				fmt.Println("✅ Change highlighting enabled")
			case "off":
				sim.highlightOn = false
				fmt.Println("✅ Change highlighting disabled")
			default:
				fmt.Printf("❌ Invalid option: %s (use 'on' or 'off')\n", parts[1])
			}

		case "help", "h":
			fmt.Println("\nCommands:")
			fmt.Println("  [Enter]              - Execute next cycle")
			fmt.Println("  inject <src> <dst> [payload] - Inject packet")
			fmt.Println("  scenario <name>      - Load test scenario (test1-4)")
			fmt.Println("  scenarios            - List available scenarios")
			fmt.Println("  highlight on/off     - Toggle change highlighting")
			fmt.Println("  run <n>              - Run n cycles automatically")
			fmt.Println("  quit                 - Exit simulator")
			fmt.Println()

		default:
			fmt.Printf("❌ Unknown command: %s (type 'help' for commands)\n", cmd)
		}
	}
}
