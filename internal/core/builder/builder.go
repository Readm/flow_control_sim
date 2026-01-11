package builder

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/capabilities/cache"
	compcache "github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/components/directory"
	"github.com/Readm/flow_sim/internal/capabilities/memory/dram"
	cpu "github.com/Readm/flow_sim/internal/nodes/cpu/champsim"
	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/flowsim"
	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/trace"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

// BuildFromFlowSimNetwork 直接从 FlowSimNetwork 构建仿真网络
func BuildFromFlowSimNetwork(flowNet protocol.FlowSimNetwork) (*network.Network, error) {
	net := network.New()

	// 保存原始配置引用 (用于 Display 数据和 Config 访问)
	net.SetSourceConfig(&flowNet)

	// Phase 6: 保存需要 cleanup 的资源（如 TraceReader）
	// TODO: 返回这些资源给调用方进行 cleanup
	// var traceReaders []trace.TraceReader

	// 1. 创建节点
	for i, nodeProto := range flowNet.Nodes {
		var newNode node.Node
		var handler node.NodeHandler
		// var traceReader trace.TraceReader // 暂不处理 cleanup

		// Phase 6: 先创建输入/输出队列（Handler 需要 outputQueue 参数）

		// 创建输入队列
		var inputs []*queue.InputQueue
		if nodeProto.InPorts != nil && len(*nodeProto.InPorts) > 0 {
			for _, port := range *nodeProto.InPorts {
				bufferSize := 128 // 默认
				if port.BufferSize != nil {
					bufferSize = *port.BufferSize
				}
				q := queue.NewInputQueue(bufferSize, port.Bandwidth)
				inputs = append(inputs, q)
			}
		} else {
			// 默认：8个输入端口
			for j := 0; j < 8; j++ {
				q := queue.NewInputQueue(128, 1)
				inputs = append(inputs, q)
			}
		}

		// 创建输出队列
		var outputs []*queue.OutputQueue
		if nodeProto.OutPorts != nil && len(*nodeProto.OutPorts) > 0 {
			for _, port := range *nodeProto.OutPorts {
				bufferSize := 128 // 默认
				if port.BufferSize != nil {
					bufferSize = *port.BufferSize
				}
				q := queue.NewOutputQueue(bufferSize, port.Bandwidth)
				outputs = append(outputs, q)
			}
		} else {
			// 默认：8个输出端口
			for j := 0; j < 8; j++ {
				q := queue.NewOutputQueue(128, 1)
				outputs = append(outputs, q)
			}
		}

		// Phase 6: 根据 node_type 创建对应的 Handler
		if nodeProto.NodeType != nil {
			switch *nodeProto.NodeType {
			case protocol.Cpu:
				// 创建 CPU Handler
				if nodeProto.CpuConfig == nil {
					return nil, fmt.Errorf("node %d has type 'cpu' but missing cpu_config", nodeProto.NodeId)
				}
				// CPU Handler 通常只需要第一个输出队列
				if len(outputs) == 0 {
					return nil, fmt.Errorf("node %d (cpu) requires at least one output queue", nodeProto.NodeId)
				}
				cpuHandler, _, err := createCPUHandler(nodeProto.NodeId, nodeProto.CpuConfig, outputs[0])
				if err != nil {
					return nil, fmt.Errorf("failed to create CPU handler for node %d: %w", nodeProto.NodeId, err)
				}
				handler = cpuHandler
				// traceReader = tr // TODO: 保存供 cleanup

			case protocol.MemoryController:
				// 创建 Memory Controller Handler
				if nodeProto.MemoryConfig == nil {
					return nil, fmt.Errorf("node %d has type 'memory_controller' but missing memory_config", nodeProto.NodeId)
				}
				memHandler, err := createMemoryHandler(nodeProto.NodeId, nodeProto.MemoryConfig, outputs)
				if err != nil {
					return nil, fmt.Errorf("failed to create memory handler for node %d: %w", nodeProto.NodeId, err)
				}
				handler = memHandler

			case protocol.Router, protocol.Generic:
				// 创建通用 Handler（无特殊逻辑，使用 BaseNode 默认行为）
				handler = createGenericHandler(nodeProto.NodeId)

			default:
				return nil, fmt.Errorf("unknown node_type %s for node %d", *nodeProto.NodeType, nodeProto.NodeId)
			}
		} else {
			// 兼容旧逻辑：没有 node_type 字段时，根据 node_features 创建通用节点
			handler = createGenericHandler(nodeProto.NodeId)
		}

		// 创建 BaseNode（WorkerNode）
		workerNode := node.NewWorkerNode(nodeProto.NodeId)
		newNode = workerNode

		// 设置 Handler（如果不为 nil）
		if handler != nil {
			workerNode.SetProcessHook(handler.Process)
			// Phase 6: 同时保存 handler 引用，用于 ExportState
			workerNode.SetHandler(handler)
		}

		// Phase 2: 设置 Protocol 配置引用 (直接引用,Export 时会从这里读取)
		// 注意: 必须使用 &flowNet.Nodes[i] 而不是 &nodeProto,因为 nodeProto 是循环变量
		newNode.SetConfigRef(&flowNet.Nodes[i])

		// 添加缓存组件（兼容旧逻辑）
		if nodeProto.Cache != nil {
			c := compcache.NewFullyAssociativeCache(nodeProto.Cache.Capacity)
			newNode.AddCache(c)
		}

		// 添加目录组件（兼容旧逻辑）
		if nodeProto.Directory != nil {
			d := directory.NewFullyAssociativeDirectory(nodeProto.Directory.Capacity)
			newNode.AddDirectory(d)
		}

		// 保存节点名称到 CustomData
		newNode.SetData("name", nodeProto.NodeName)

		// 添加队列到节点
		for _, q := range inputs {
			newNode.AddInputQueue(q)
		}
		for _, q := range outputs {
			newNode.AddOutputQueue(q)
		}

		// 添加到网络
		handle := &network.NodeHandle{
			Node:    newNode,
			Inputs:  inputs,
			Outputs: outputs,
		}

		if err := net.AddNode(handle); err != nil {
			return nil, fmt.Errorf("failed to add node %d: %w", nodeProto.NodeId, err)
		}
	}

	// 2. 创建链路
	for i, edgeProto := range flowNet.Edges {
		// 默认参数
		srcPort := 0
		dstPort := 0
		latency := 1
		bandwidth := 1

		// 使用配置的参数
		if edgeProto.SrcPortId != nil {
			srcPort = *edgeProto.SrcPortId
		}
		if edgeProto.DstPortId != nil {
			dstPort = *edgeProto.DstPortId
		}
		if edgeProto.Latency != nil {
			latency = *edgeProto.Latency
		}
		if edgeProto.Bandwidth != nil {
			bandwidth = *edgeProto.Bandwidth
		}

		// 连接节点
		linkInstance, err := net.Connect(
			edgeProto.SrcNodeId, srcPort,
			edgeProto.DstNodeId, dstPort,
			latency, bandwidth,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to connect %d:%d->%d:%d: %w",
				edgeProto.SrcNodeId, srcPort,
				edgeProto.DstNodeId, dstPort,
				err)
		}

		// Phase 2: 设置 Protocol 配置引用 (Export 时会从这里读取)
		linkInstance.SetConfigRef(&flowNet.Edges[i])

		// Phase 2: 不再填充业务数据和 DisplayData
		// Export 时会直接从 configRef 读取
	}

	return net, nil
}

// RebuildNetwork 重建现有网络（替代 Network.Reset）
func RebuildNetwork(existingNet *network.Network, flowNet protocol.FlowSimNetwork) error {
	// 使用反射访问私有字段的替代方案：
	// 我们需要添加一个公开方法到 Network
	// 但为了避免循环依赖，让我们在测试中直接使用 BuildFromFlowSimNetwork
	return fmt.Errorf("use BuildFromFlowSimNetwork and replace the network instance instead")
}

// nodeDataToMap 将 protocol.Node_Data 转换为 map[string]interface{}
func nodeDataToMap(data protocol.Node_Data) map[string]interface{} {
	result := make(map[string]interface{})
	result["id"] = data.Id
	if data.Label != nil {
		result["label"] = *data.Label
	}
	if data.Type != nil {
		result["type"] = *data.Type
	}
	// 合并 AdditionalProperties
	for k, v := range data.AdditionalProperties {
		result[k] = v
	}
	return result
}

// edgeDataToMap 将 protocol.Edge_Data 转换为 map[string]interface{}
func edgeDataToMap(data protocol.Edge_Data) map[string]interface{} {
	result := make(map[string]interface{})
	result["id"] = data.Id
	result["source"] = data.Source
	result["target"] = data.Target
	if data.LineType != nil {
		result["lineType"] = string(*data.LineType)
	}
	// 合并 AdditionalProperties
	for k, v := range data.AdditionalProperties {
		result[k] = v
	}
	return result
}

// createCPUHandler 从 Protocol.CPUConfig 创建 CPUNodeHandler
//
// Phase 6: 根据 CPU 配置创建完整的 CPU+L1D Cache Handler
//
// 参数:
//   - nodeID: CPU 节点 ID
//   - cpuConfig: CPU 配置（包含 trace_file, rob_size 等）
//   - outputQueue: 输出队列（发送到下游 Cache/Memory）
//
// 返回:
//   - CPUNodeHandler 实例
//   - Trace Reader（需要调用方负责 cleanup）
//   - error
func createCPUHandler(nodeID int, cpuConfig *protocol.CPUConfig, outputQueue *queue.OutputQueue) (node.NodeHandler, trace.TraceReader, error) {
	// 1. 读取 trace 文件路径
	traceFile := "../../testdata/traces/small.champsimtrace" // 默认值
	if cpuConfig.TraceFile != nil {
		traceFile = *cpuConfig.TraceFile
	}

	// 2. 创建 Trace Reader
	traceReader, err := trace.NewTraceReader(traceFile, 0, trace.FormatStandard)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create trace reader: %w", err)
	}

	// 3. 构建 O3CPUConfig
	o3Config := cpu.DefaultO3CPUConfig()
	if cpuConfig.RobSize != nil {
		o3Config.ROBSize = *cpuConfig.RobSize
	}
	if cpuConfig.LqSize != nil {
		o3Config.LQSize = *cpuConfig.LqSize
	}
	if cpuConfig.SqSize != nil {
		o3Config.SQSize = *cpuConfig.SqSize
	}
	if cpuConfig.FetchWidth != nil {
		o3Config.FetchWidth = *cpuConfig.FetchWidth
	}
	if cpuConfig.DecodeWidth != nil {
		o3Config.DecodeWidth = *cpuConfig.DecodeWidth
	}
	if cpuConfig.DispatchWidth != nil {
		o3Config.DispatchWidth = *cpuConfig.DispatchWidth
	}
	if cpuConfig.ExecuteWidth != nil {
		o3Config.ExecuteWidth = *cpuConfig.ExecuteWidth
	}
	if cpuConfig.RetireWidth != nil {
		o3Config.RetireWidth = *cpuConfig.RetireWidth
	}

	// 4. 创建 O3CPU 实例
	o3cpu := cpu.NewO3CPU(traceReader, o3Config)
	o3cpu.SetStandaloneMode(false)

	// 5. 创建 L1D Cache
	l1dConfig := compcache.DefaultL1DConfig()
	if cpuConfig.L1dCache != nil {
		l1dConfig.NumSets = uint32(cpuConfig.L1dCache.NumSets)
		// TODO: 添加更多 L1D Cache 配置字段
	}
	l1dCache, err := cache.NewSetAssociativeCache(l1dConfig)
	if err != nil {
		traceReader.Close()
		return nil, nil, fmt.Errorf("failed to create L1D cache: %w", err)
	}

	// 6. 创建 Memory Adapter
	memoryAdapter := flowsim.NewFlowSimMemoryAdapter()
	l1dCache.SetLowerLevel(memoryAdapter)
	o3cpu.SetL1DCache(l1dCache)

	// 7. 创建 CPU Handler（下游节点 ID 暂时设为 nodeID+1，后续可以通过配置指定）
	dramID := nodeID + 1
	cpuHandler := flowsim.NewCPUNodeHandler(
		nodeID, dramID,
		o3cpu, l1dCache, memoryAdapter,
		outputQueue,
	)

	return cpuHandler, traceReader, nil
}

// createMemoryHandler 从 Protocol.MemoryConfig 创建 DRAM Handler
//
// Phase 6: 根据内存配置创建 DRAM Channel Handler
//
// 参数:
//   - nodeID: Memory 节点 ID
//   - memConfig: 内存配置（包含 channels, ranks 等）
//   - outputQueues: 输出队列数组（发送到多个 CPU）
//
// 返回:
//   - DRAMNodeHandler 实例
//   - error
func createMemoryHandler(nodeID int, memConfig *protocol.MemoryConfig, outputQueues []*queue.OutputQueue) (node.NodeHandler, error) {
	// 1. 使用默认 DRAM Config
	dramConfig := dram.DefaultDRAMConfig()

	// 2. 从 MemoryConfig 读取配置并覆盖默认值
	if memConfig.TCAS != nil {
		dramConfig.TCAS = uint64(*memConfig.TCAS)
	}
	if memConfig.TRCD != nil {
		dramConfig.TRCD = uint64(*memConfig.TRCD)
	}
	if memConfig.TRP != nil {
		dramConfig.TRP = uint64(*memConfig.TRP)
	}
	if memConfig.TRAS != nil {
		dramConfig.TRAS = uint64(*memConfig.TRAS)
	}
	if memConfig.Channels != nil {
		dramConfig.Channels = uint32(*memConfig.Channels)
	}
	if memConfig.Ranks != nil {
		dramConfig.Ranks = uint32(*memConfig.Ranks)
	}
	if memConfig.Banks != nil {
		dramConfig.Banks = uint32(*memConfig.Banks)
	}
	if memConfig.Rows != nil {
		dramConfig.Rows = uint32(*memConfig.Rows)
	}
	if memConfig.Columns != nil {
		dramConfig.Columns = uint32(*memConfig.Columns)
	}

	// 2. 创建 DRAM Channel 实例
	dramChannel, err := dram.NewDRAMChannel(dramConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create DRAM channel: %w", err)
	}

	// 3. 创建 DRAM Handler（假设只有一个 CPU 连接，CPU ID 为 nodeID-1）
	// TODO: 从配置或拓扑推断 CPU ID
	cpuID := nodeID - 1

	// DRAMNodeHandler 只支持单个 CPU 和单个输出队列
	if len(outputQueues) == 0 {
		return nil, fmt.Errorf("node %d (memory_controller) requires at least one output queue", nodeID)
	}
	outputQueue := outputQueues[0]

	dramHandler := flowsim.NewDRAMNodeHandler(
		nodeID, cpuID,
		dramChannel, outputQueue,
	)

	return dramHandler, nil
}

// createGenericHandler 创建通用 Handler（默认 WorkerNode，无特殊逻辑）
//
// Phase 6: 用于 router 和 generic 类型节点
//
// 参数:
//   - nodeID: 节点 ID
//
// 返回:
//   - nil (使用 BaseNode 的默认 Process 逻辑)
func createGenericHandler(nodeID int) node.NodeHandler {
	// 返回 nil 表示使用 BaseNode 的默认 Process 行为
	// WorkerNode 已经在 BuildFromFlowSimNetwork 中创建
	return nil
}
