package flowsim

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/champsim/cache"
	"github.com/Readm/flow_sim/internal/champsim/cpu"
	"github.com/Readm/flow_sim/internal/champsim/dram"
	"github.com/Readm/flow_sim/internal/champsim/trace"
	compcache "github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
)

// SystemHandlers holds all handlers for cleanup
type SystemHandlers struct {
	TraceReaders []trace.TraceReader
}

func (h *SystemHandlers) Cleanup() {
	for _, reader := range h.TraceReaders {
		reader.Close()
	}
}

// BuildChampSimSystem builds a complete ChampSim system with hierarchical cache
// Topology: 64 CPUs -> 32 L2s -> 8 L3s <-> 8 MemCtrls -> 8 DRAMs
// L3s and MemCtrls are connected via a Bufferless Ring (对称设计)
// Returns network and handlers for cleanup
func BuildChampSimSystem(numCPUs int, traceFile string) (*network.Network, *SystemHandlers, error) {
	const numChannels = 8   // 8个DRAM通道
	const numMemCtrls = 8   // 8个Memory Controllers (与DRAM一对一)
	const numL3s = 8        // 8个L3 (与MemCtrl数量对称)
	const cpusPerL2 = 2     // 每2个CPU共享1个L2
	const l2sPerL3 = 4      // 每4个L2共享1个L3
	const ringLatency = 5   // Ring链路延迟
	const localLatency = 3  // Local链路延迟（测试：1→3 增加流水线深度）
	const routerBuffer = 16 // Ring路由器缓冲区大小

	numL2s := numCPUs / cpusPerL2        // 32个L2
	numRingNodes := numL3s + numMemCtrls // Ring上共16个节点（8个L3 + 8个MemCtrl）

	handlers := &SystemHandlers{}

	// 用于存储 Node 对象的数组 (不再使用 ID 数组)
	cpuNodes := make([]node.Node, numCPUs)
	l2Nodes := make([]node.Node, numL2s)
	l3WorkerNodes := make([]node.Node, numL3s)
	memCtrlWorkerNodes := make([]node.Node, numMemCtrls)
	dramNodes := make([]node.Node, numChannels)
	ringRouterNodes := make([]node.Node, numRingNodes)

	// Create CPU cores (每个CPU连接到对应的L2)
	// Node ID 分配: 0-63 为 CPUs
	var cpuNodeHandles []*network.NodeHandle
	for i := 0; i < numCPUs; i++ {
		cpuNodeID := i

		traceReader, err := trace.NewSharedTraceReader(traceFile, uint8(i), trace.FormatStandard)
		if err != nil {
			return nil, nil, err
		}
		handlers.TraceReaders = append(handlers.TraceReaders, traceReader)

		o3cpu := cpu.NewO3CPU(traceReader, cpu.DefaultO3CPUConfig())
		o3cpu.SetStandaloneMode(false)

		l1dCache, _ := cache.NewSetAssociativeCache(compcache.DefaultL1DConfig())
		memoryAdapter := NewFlowSimMemoryAdapter()
		l1dCache.SetLowerLevel(memoryAdapter)
		o3cpu.SetL1DCache(l1dCache)

		cpuOutputQueue := queue.NewOutputQueue(128, 1)
		cpuInputQueue := queue.NewInputQueue(128, 1)

		// 每2个CPU共享1个L2, L2 ID 从 numCPUs 开始
		myL2NodeID := numCPUs + i/cpusPerL2

		cpuHandler := NewCPUNodeHandler(
			cpuNodeID, myL2NodeID,
			o3cpu, l1dCache, memoryAdapter,
			cpuOutputQueue,
		)

		cpuNode := node.NewWorkerNode(cpuNodeID)
		cpuNode.SetProcessHook(cpuHandler.Process)
		cpuNode.AddInputQueue(cpuInputQueue)
		cpuNode.AddOutputQueue(cpuOutputQueue)

		// 存储 Node 对象供后续连接使用
		cpuNodes[i] = cpuNode

		cpuNodeHandles = append(cpuNodeHandles, &network.NodeHandle{
			Node:    cpuNode,
			Inputs:  []*queue.InputQueue{cpuInputQueue},
			Outputs: []*queue.OutputQueue{cpuOutputQueue},
		})
	}

	// Create L2 Caches (32个，每个服务2个CPU)
	// Node ID 分配: numCPUs + 0 到 numCPUs + numL2s - 1 (64-95)
	var l2NodeHandles []*network.NodeHandle
	for l2Index := 0; l2Index < numL2s; l2Index++ {
		l2NodeID := numCPUs + l2Index

		l2Config := compcache.CacheConfig{
			Name:        fmt.Sprintf("L2_%d", l2Index),
			NumSets:     512,
			NumWays:     16,
			BlockSize:   64,
			MSHRSize:    32,
			HitLatency:  20,
			FillLatency: 10,
		}
		l2Cache, _ := cache.NewSetAssociativeCache(l2Config)

		// 每个L2有2个CPU的输入/输出 + 1个L3的输入/输出
		l2OutputQueues := make([]*queue.OutputQueue, cpusPerL2+1)
		l2InputQueues := make([]*queue.InputQueue, cpusPerL2+1)
		for i := 0; i < cpusPerL2+1; i++ {
			l2OutputQueues[i] = queue.NewOutputQueue(128, 1)
			l2InputQueues[i] = queue.NewInputQueue(128, 1)
		}

		// 这个L2服务的CPU IDs
		startCPU := l2Index * cpusPerL2
		myCPUNodeIDs := make([]int, cpusPerL2)
		for i := 0; i < cpusPerL2; i++ {
			myCPUNodeIDs[i] = startCPU + i
		}

		// 这个L2连接的L3 (ID从 numCPUs+numL2s 开始)
		myL3NodeID := numCPUs + numL2s + l2Index/l2sPerL3

		l2Handler := NewL2CacheNodeHandler(
			l2NodeID, myCPUNodeIDs, myL3NodeID,
			l2Cache, l2OutputQueues,
		)

		l2Node := node.NewWorkerNode(l2NodeID)
		l2Node.SetProcessHook(l2Handler.Process)
		for _, q := range l2InputQueues {
			l2Node.AddInputQueue(q)
		}
		for _, q := range l2OutputQueues {
			l2Node.AddOutputQueue(q)
		}

		// 存储 Node 对象供后续连接使用
		l2Nodes[l2Index] = l2Node

		l2NodeHandles = append(l2NodeHandles, &network.NodeHandle{
			Node:    l2Node,
			Inputs:  l2InputQueues,
			Outputs: l2OutputQueues,
		})
	}

	// Create L3 Worker Nodes (8个，每个服务4个L2，通过Ring与MemCtrl通信)
	// Node ID 分配: numCPUs + numL2s + 0 到 numCPUs + numL2s + numL3s - 1 (96-103)
	var l3WorkerHandles []*network.NodeHandle
	for l3Index := 0; l3Index < numL3s; l3Index++ {
		l3WorkerID := numCPUs + numL2s + l3Index

		l3Config := compcache.CacheConfig{
			Name:        fmt.Sprintf("L3_%d", l3Index),
			NumSets:     2048,
			NumWays:     16,
			BlockSize:   64,
			MSHRSize:    64,
			HitLatency:  40,
			FillLatency: 20,
		}
		l3Cache, _ := cache.NewSetAssociativeCache(l3Config)

		// 每个L3 Worker有4个L2的输入/输出 + 1个Ring的输入/输出
		l3OutputQueues := make([]*queue.OutputQueue, l2sPerL3+1)
		l3InputQueues := make([]*queue.InputQueue, l2sPerL3+1)
		for i := 0; i < l2sPerL3+1; i++ {
			l3OutputQueues[i] = queue.NewOutputQueue(256, 1)
			l3InputQueues[i] = queue.NewInputQueue(256, 1)
		}

		// 这个L3服务的L2 IDs
		startL2 := l3Index * l2sPerL3
		myL2NodeIDs := make([]int, l2sPerL3)
		for i := 0; i < l2sPerL3; i++ {
			myL2NodeIDs[i] = numCPUs + startL2 + i
		}

		// Ring Router ID: 200 + l3Index*2 (偶数位置的 router)
		ringRouterID := 200 + l3Index*2

		l3Handler := NewL2CacheNodeHandler(
			l3WorkerID, myL2NodeIDs, ringRouterID,
			l3Cache, l3OutputQueues,
		)

		l3Worker := node.NewWorkerNode(l3WorkerID)
		l3Worker.SetProcessHook(l3Handler.Process)
		for _, q := range l3InputQueues {
			l3Worker.AddInputQueue(q)
		}
		for _, q := range l3OutputQueues {
			l3Worker.AddOutputQueue(q)
		}

		// 存储 Node 对象供后续连接使用
		l3WorkerNodes[l3Index] = l3Worker

		l3WorkerHandles = append(l3WorkerHandles, &network.NodeHandle{
			Node:    l3Worker,
			Inputs:  l3InputQueues,
			Outputs: l3OutputQueues,
		})
	}

	// Create Memory Controller Worker Nodes (8个，每个对应1个DRAM，通过Ring与L3通信)
	// Node ID 分配: numCPUs + numL2s + numL3s + 0 到 numCPUs + numL2s + numL3s + numMemCtrls - 1 (104-111)
	var memCtrlWorkerHandles []*network.NodeHandle
	for mcIndex := 0; mcIndex < numMemCtrls; mcIndex++ {
		memCtrlWorkerID := numCPUs + numL2s + numL3s + mcIndex

		// 每个MemCtrl Worker有1个Ring的输入/输出 + 1个DRAM的输入/输出
		mcOutputQueues := make([]*queue.OutputQueue, 2)
		mcInputQueues := make([]*queue.InputQueue, 2)
		for i := 0; i < 2; i++ {
			mcOutputQueues[i] = queue.NewOutputQueue(256, 1)
			mcInputQueues[i] = queue.NewInputQueue(256, 1)
		}

		// Ring Router ID: 200 + mcIndex*2 + 1 (奇数位置的 router)
		ringRouterID := 200 + mcIndex*2 + 1

		// DRAM ID
		dramNodeID := numCPUs + numL2s + numL3s + numMemCtrls + mcIndex

		// 每个MemCtrl只连接到1个DRAM
		mcHandler := NewMemoryControllerHandler(
			memCtrlWorkerID,
			[]int{ringRouterID},
			[]int{dramNodeID},
			mcOutputQueues,
			MappingInterleaved,
		)

		mcWorker := node.NewWorkerNode(memCtrlWorkerID)
		mcWorker.SetProcessHook(mcHandler.Process)
		for _, q := range mcInputQueues {
			mcWorker.AddInputQueue(q)
		}
		for _, q := range mcOutputQueues {
			mcWorker.AddOutputQueue(q)
		}

		// 存储 Node 对象供后续连接使用
		memCtrlWorkerNodes[mcIndex] = mcWorker

		memCtrlWorkerHandles = append(memCtrlWorkerHandles, &network.NodeHandle{
			Node:    mcWorker,
			Inputs:  mcInputQueues,
			Outputs: mcOutputQueues,
		})
	}

	// Create DRAM Channels (8个，每个连接到对应的MemCtrl Worker)
	// Node ID 分配: numCPUs + numL2s + numL3s + numMemCtrls + 0 到 ... + numChannels - 1 (112-119)
	var dramNodeHandles []*network.NodeHandle
	for i := 0; i < numChannels; i++ {
		dramNodeID := numCPUs + numL2s + numL3s + numMemCtrls + i
		memCtrlWorkerID := numCPUs + numL2s + numL3s + i

		dramChannel, _ := dram.NewDRAMChannel(dram.DefaultDRAMConfig())
		dramOutputQueue := queue.NewOutputQueue(128, 1)
		dramInputQueue := queue.NewInputQueue(128, 1)

		dramHandler := NewDRAMNodeHandler(
			dramNodeID, memCtrlWorkerID, // 每个DRAM连接到对应的MemCtrl
			dramChannel, dramOutputQueue,
		)

		dramNode := node.NewWorkerNode(dramNodeID)
		dramNode.SetProcessHook(dramHandler.Process)
		dramNode.AddInputQueue(dramInputQueue)
		dramNode.AddOutputQueue(dramOutputQueue)

		// 存储 Node 对象供后续连接使用
		dramNodes[i] = dramNode

		dramNodeHandles = append(dramNodeHandles, &network.NodeHandle{
			Node:    dramNode,
			Inputs:  []*queue.InputQueue{dramInputQueue},
			Outputs: []*queue.OutputQueue{dramOutputQueue},
		})
	}

	// Create Ring Routers (16个，对称交错排列L3和MemCtrl)
	// Ring顺序: L3_0(200), MC_0(201), L3_1(202), MC_1(203), ..., L3_7(214), MC_7(215)
	// Node ID 分配: 200-215
	var ringRouterHandles []*network.NodeHandle
	for i := 0; i < numRingNodes; i++ {
		routerID := 200 + i

		// 确定这个router对应的worker ID（对称交错：偶数=L3，奇数=MC）
		var workerID int
		if i%2 == 0 {
			// 偶数索引：L3 router
			l3Index := i / 2
			workerID = numCPUs + numL2s + l3Index
		} else {
			// 奇数索引：MemCtrl router
			mcIndex := (i - 1) / 2
			workerID = numCPUs + numL2s + numL3s + mcIndex
		}

		router := node.NewBufferlessRingRouter(routerID, workerID, routerBuffer)
		ringInQueue := queue.NewInputQueue(256, 1)
		localInQueue := queue.NewInputQueue(256, 1)
		ringOutQueue := queue.NewOutputQueue(256, 1)
		localOutQueue := queue.NewOutputQueue(256, 1)

		router.AddInputQueue(ringInQueue)
		router.AddInputQueue(localInQueue)
		router.AddOutputQueue(ringOutQueue)
		router.AddOutputQueue(localOutQueue)

		// 存储 Node 对象供后续连接使用
		ringRouterNodes[i] = router

		ringRouterHandles = append(ringRouterHandles, &network.NodeHandle{
			Node:    router,
			Inputs:  []*queue.InputQueue{ringInQueue, localInQueue},
			Outputs: []*queue.OutputQueue{ringOutQueue, localOutQueue},
		})
	}

	// Create Network and add all nodes
	net := network.New()

	// Add all CPU nodes
	for _, handle := range cpuNodeHandles {
		net.AddNode(handle)
	}

	// Add all L2 nodes
	for _, handle := range l2NodeHandles {
		net.AddNode(handle)
	}

	// Add all L3 Worker nodes
	for _, handle := range l3WorkerHandles {
		net.AddNode(handle)
	}

	// Add all MemCtrl Worker nodes
	for _, handle := range memCtrlWorkerHandles {
		net.AddNode(handle)
	}

	// Add all DRAM nodes
	for _, handle := range dramNodeHandles {
		net.AddNode(handle)
	}

	// Add all Ring Router nodes
	for _, handle := range ringRouterHandles {
		net.AddNode(handle)
	}

	// Connect topology: CPUs <-> L2s
	for cpuIdx := 0; cpuIdx < numCPUs; cpuIdx++ {
		l2Idx := cpuIdx / cpusPerL2
		portInL2 := cpuIdx % cpusPerL2
		net.ConnectNodes(cpuNodes[cpuIdx], 0, l2Nodes[l2Idx], portInL2, 10, 1)
		net.ConnectNodes(l2Nodes[l2Idx], portInL2, cpuNodes[cpuIdx], 0, 10, 1)
	}

	// Connect topology: L2s <-> L3 Workers
	for l2Idx := 0; l2Idx < numL2s; l2Idx++ {
		l3Idx := l2Idx / l2sPerL3
		portInL3 := l2Idx % l2sPerL3
		net.ConnectNodes(l2Nodes[l2Idx], cpusPerL2, l3WorkerNodes[l3Idx], portInL3, 20, 1)
		net.ConnectNodes(l3WorkerNodes[l3Idx], portInL3, l2Nodes[l2Idx], cpusPerL2, 20, 1)
	}

	// Connect topology: L3 Workers <-> Ring Routers (local connection，对称设计)
	// L3_0 <-> Router200, L3_1 <-> Router202, ..., L3_7 <-> Router214
	for l3Idx := 0; l3Idx < numL3s; l3Idx++ {
		routerIdx := l3Idx * 2 // 偶数位置的router
		net.ConnectNodes(l3WorkerNodes[l3Idx], l2sPerL3, ringRouterNodes[routerIdx], 1, localLatency, 1,
			network.WithBufferless())
		net.ConnectNodes(ringRouterNodes[routerIdx], 1, l3WorkerNodes[l3Idx], l2sPerL3, localLatency, 1,
			network.WithBufferless())
	}

	// Connect topology: MemCtrl Workers <-> Ring Routers (local connection，对称设计)
	// MC_0 <-> Router201, MC_1 <-> Router203, ..., MC_7 <-> Router215
	for mcIdx := 0; mcIdx < numMemCtrls; mcIdx++ {
		routerIdx := mcIdx*2 + 1 // 奇数位置的router
		net.ConnectNodes(memCtrlWorkerNodes[mcIdx], 0, ringRouterNodes[routerIdx], 1, localLatency, 1,
			network.WithBufferless())
		net.ConnectNodes(ringRouterNodes[routerIdx], 1, memCtrlWorkerNodes[mcIdx], 0, localLatency, 1,
			network.WithBufferless())
	}

	// Connect topology: Ring (使用 BufferlessLinkType)
	// Router[i] -> Router[(i+1) % 16]
	for i := 0; i < numRingNodes; i++ {
		nextRouter := (i + 1) % numRingNodes
		net.ConnectNodes(ringRouterNodes[i], 0, ringRouterNodes[nextRouter], 0, ringLatency, 1,
			network.WithBufferless())
	}

	// Connect topology: MemCtrl Workers <-> DRAMs (一对一)
	for mcIdx := 0; mcIdx < numMemCtrls; mcIdx++ {
		net.ConnectNodes(memCtrlWorkerNodes[mcIdx], 1, dramNodes[mcIdx], 0, 20, 1)
		net.ConnectNodes(dramNodes[mcIdx], 0, memCtrlWorkerNodes[mcIdx], 1, 20, 1)
	}

	return net, handlers, nil
}

// BuildChampSimSingleCoreSystem builds a baseline single-core system
// Topology: 1 CPU -> 1 L2 -> 1 L3 <-> 1 MemCtrl -> 1 DRAM
// No Network/Ring, but uses same latencies where applicable.
func BuildChampSimSingleCoreSystem(traceFile string) (*network.Network, *SystemHandlers, error) {
	// IDs
	const (
		cpuID     = 0
		l2ID      = 1
		l3ID      = 2
		memCtrlID = 3
		dramID    = 4
	)

	handlers := &SystemHandlers{}
	net := network.New()

	// 1. CPU
	traceReader, err := trace.NewSharedTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		return nil, nil, err
	}
	handlers.TraceReaders = append(handlers.TraceReaders, traceReader)

	o3cpu := cpu.NewO3CPU(traceReader, cpu.DefaultO3CPUConfig())
	o3cpu.SetStandaloneMode(false)
	l1dCache, _ := cache.NewSetAssociativeCache(compcache.DefaultL1DConfig())
	memoryAdapter := NewFlowSimMemoryAdapter()
	l1dCache.SetLowerLevel(memoryAdapter)
	o3cpu.SetL1DCache(l1dCache)

	cpuHandler := NewCPUNodeHandler(cpuID, l2ID, o3cpu, l1dCache, memoryAdapter, queue.NewOutputQueue(128, 1))
	cpuNode := node.NewWorkerNode(cpuID)
	cpuNode.SetProcessHook(cpuHandler.Process)
	cpuInputQ := queue.NewInputQueue(128, 1)
	cpuOutputQ := queue.NewOutputQueue(128, 1)
	cpuNode.AddInputQueue(cpuInputQ)
	cpuNode.AddOutputQueue(cpuOutputQ)
	net.AddNode(&network.NodeHandle{Node: cpuNode, Inputs: []*queue.InputQueue{cpuInputQ}, Outputs: []*queue.OutputQueue{cpuOutputQ}})

	// 2. L2 Cache
	l2Config := compcache.CacheConfig{Name: "L2_0", NumSets: 512, NumWays: 16, BlockSize: 64, MSHRSize: 32, HitLatency: 20, FillLatency: 10}
	l2Cache, _ := cache.NewSetAssociativeCache(l2Config)
	// L2 connects to 1 CPU (port 0) and L3 (handled via nextLevelID)
	// But L2 handler expects input queues for CPUs. Here 1 CPU.
	l2Handler := NewL2CacheNodeHandler(l2ID, []int{cpuID}, l3ID, l2Cache, []*queue.OutputQueue{queue.NewOutputQueue(128, 1), queue.NewOutputQueue(128, 1)})
	l2Node := node.NewWorkerNode(l2ID)
	l2Node.SetProcessHook(l2Handler.Process)
	// Ports: 0: CPU-side In/Out, 1: L3-side In/Out (Implicit in Handler implementation? Let's check logic)
	// L2 Handler implementation:
	// Inputs: 0..N-1 from CPUs.
	// Last Input? Usually L2 receives from L3 too.
	// Actually L2CacheNodeHandler uses `queues` array where first N are for CPUs, last one is for L3 (NextLevel).
	// So for 1 CPU: 2 queues. Index 0: CPU, Index 1: L3.
	l2InputQs := []*queue.InputQueue{queue.NewInputQueue(128, 1), queue.NewInputQueue(128, 1)}
	l2OutputQs := []*queue.OutputQueue{queue.NewOutputQueue(128, 1), queue.NewOutputQueue(128, 1)}
	for _, q := range l2InputQs {
		l2Node.AddInputQueue(q)
	}
	for _, q := range l2OutputQs {
		l2Node.AddOutputQueue(q)
	}
	// Re-create handler with correct queues ref?
	// The handler stores `queues`. We passed `queue.NewOutputQueue` above which are NOT the ones attached to the node?
	// Wait, in previous code:
	// l2OutputQueues := make...
	// l2Handler := NewL2CacheNodeHandler(..., l2OutputQueues)
	// l2Node.AddOutputQueue(q) (iterating l2OutputQueues)
	// So I should create queues first.
	// Redoing L2 Setup correctly:
	l2OutQs := []*queue.OutputQueue{queue.NewOutputQueue(128, 1), queue.NewOutputQueue(128, 1)}
	l2Handler = NewL2CacheNodeHandler(l2ID, []int{cpuID}, l3ID, l2Cache, l2OutQs)
	l2Node.SetProcessHook(l2Handler.Process) // Re-set
	for _, q := range l2InputQs {
		l2Node.AddInputQueue(q) // Use previously created inputs
	}
	for _, q := range l2OutQs {
		l2Node.AddOutputQueue(q) // Use newly created outputs
	}
	net.AddNode(&network.NodeHandle{Node: l2Node, Inputs: l2InputQs, Outputs: l2OutQs})

	// 3. L3 Cache
	l3Config := compcache.CacheConfig{Name: "L3_0", NumSets: 2048, NumWays: 16, BlockSize: 64, MSHRSize: 64, HitLatency: 40, FillLatency: 20}
	l3Cache, _ := cache.NewSetAssociativeCache(l3Config)
	// L3 connects to 1 L2 and 1 MemCtrl.
	// Handler: 1 input from L2, 1 input from MemCtrl.
	l3InputQs := []*queue.InputQueue{queue.NewInputQueue(256, 1), queue.NewInputQueue(256, 1)}
	l3OutQs := []*queue.OutputQueue{queue.NewOutputQueue(256, 1), queue.NewOutputQueue(256, 1)}
	l3Handler := NewL2CacheNodeHandler(l3ID, []int{l2ID}, memCtrlID, l3Cache, l3OutQs)
	l3Node := node.NewWorkerNode(l3ID)
	l3Node.SetProcessHook(l3Handler.Process)
	for _, q := range l3InputQs {
		l3Node.AddInputQueue(q)
	}
	for _, q := range l3OutQs {
		l3Node.AddOutputQueue(q)
	}
	net.AddNode(&network.NodeHandle{Node: l3Node, Inputs: l3InputQs, Outputs: l3OutQs})

	// 4. Memory Controller
	// Connects to L3 (simulating 'Ring' side) and DRAM.
	// In System: ringID and dramID.
	// Here, we pretend L3 is the 'ring' source/dest.
	mcInputQs := []*queue.InputQueue{queue.NewInputQueue(256, 1), queue.NewInputQueue(256, 1)}
	mcOutQs := []*queue.OutputQueue{queue.NewOutputQueue(256, 1), queue.NewOutputQueue(256, 1)}
	// Handler expects logic to talk to Ring (index 0) and DRAM (index 1).
	mcHandler := NewMemoryControllerHandler(memCtrlID, []int{l3ID}, []int{dramID}, mcOutQs, MappingInterleaved)
	mcNode := node.NewWorkerNode(memCtrlID)
	mcNode.SetProcessHook(mcHandler.Process)
	for _, q := range mcInputQs {
		mcNode.AddInputQueue(q)
	}
	for _, q := range mcOutQs {
		mcNode.AddOutputQueue(q)
	}
	net.AddNode(&network.NodeHandle{Node: mcNode, Inputs: mcInputQs, Outputs: mcOutQs})

	// 5. DRAM
	dramChannel, _ := dram.NewDRAMChannel(dram.DefaultDRAMConfig())
	dramInputQ := queue.NewInputQueue(128, 1)
	dramOutputQ := queue.NewOutputQueue(128, 1)
	dramHandler := NewDRAMNodeHandler(dramID, memCtrlID, dramChannel, dramOutputQ)
	dramNode := node.NewWorkerNode(dramID)
	dramNode.SetProcessHook(dramHandler.Process)
	dramNode.AddInputQueue(dramInputQ)
	dramNode.AddOutputQueue(dramOutputQ)
	net.AddNode(&network.NodeHandle{Node: dramNode, Inputs: []*queue.InputQueue{dramInputQ}, Outputs: []*queue.OutputQueue{dramOutputQ}})

	// Connections
	// CPU(0) <-> L2(0) [Latency 10]
	net.ConnectNodes(cpuNode, 0, l2Node, 0, 10, 1)
	net.ConnectNodes(l2Node, 0, cpuNode, 0, 10, 1)

	// L2(1) <-> L3(0) [Latency 20]
	net.ConnectNodes(l2Node, 1, l3Node, 0, 20, 1)
	net.ConnectNodes(l3Node, 0, l2Node, 1, 20, 1)

	// L3(1) <-> MemCtrl(0) [Latency 20? Simulating NoC+Local]
	net.ConnectNodes(l3Node, 1, mcNode, 0, 20, 1)
	net.ConnectNodes(mcNode, 0, l3Node, 1, 20, 1)

	// MemCtrl(1) <-> DRAM(0) [Latency 20]
	net.ConnectNodes(mcNode, 1, dramNode, 0, 20, 1)
	net.ConnectNodes(dramNode, 0, mcNode, 1, 20, 1)

	return net, handlers, nil
}
