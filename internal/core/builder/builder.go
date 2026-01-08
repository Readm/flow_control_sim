package builder

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/components/directory"
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

	// 1. 创建节点
	for i, nodeProto := range flowNet.Nodes {
		var newNode node.Node

		// 根据 node_features 确定节点类型
		nodeType := "WorkerNode" // 默认
		if nodeProto.NodeFeatures != nil && len(*nodeProto.NodeFeatures) > 0 {
			nodeType = (*nodeProto.NodeFeatures)[0]
		}

		// 创建对应类型的节点
		switch nodeType {
		case "CentralSwitch", "HubNode":
			newNode = node.NewWorkerNode(nodeProto.NodeId)
		default:
			newNode = node.NewWorkerNode(nodeProto.NodeId)
		}

		// Phase 2: 设置 Protocol 配置引用 (直接引用,Export 时会从这里读取)
		// 注意: 必须使用 &flowNet.Nodes[i] 而不是 &nodeProto,因为 nodeProto 是循环变量
		newNode.SetConfigRef(&flowNet.Nodes[i])

		// 添加缓存组件
		if nodeProto.Cache != nil {
			c := cache.NewFullyAssociativeCache(nodeProto.Cache.Capacity)
			newNode.AddCache(c)
		}

		// 添加目录组件
		if nodeProto.Directory != nil {
			d := directory.NewFullyAssociativeDirectory(nodeProto.Directory.Capacity)
			newNode.AddDirectory(d)
		}

		// 保存节点名称到 CustomData
		newNode.SetData("name", nodeProto.NodeName)

		// Phase 2: 不再填充 Features 和 DisplayData
		// Export 时会直接从 configRef 读取

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
				newNode.AddInputQueue(q)
			}
		} else {
			// 默认：8个输入端口
			for i := 0; i < 8; i++ {
				q := queue.NewInputQueue(128, 1)
				inputs = append(inputs, q)
				newNode.AddInputQueue(q)
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
				newNode.AddOutputQueue(q)
			}
		} else {
			// 默认：8个输出端口
			for i := 0; i < 8; i++ {
				q := queue.NewOutputQueue(128, 1)
				outputs = append(outputs, q)
				newNode.AddOutputQueue(q)
			}
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
