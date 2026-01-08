package visualization

import (
	"fmt"
	"math"

	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

// StateToFlowSimNetwork 将 NetworkState 转换为 FlowSimNetwork（完整转换，包含状态和 display）
func StateToFlowSimNetwork(ns state.NetworkState) protocol.FlowSimNetwork {
	version := "1.0.0"
	cycle := ns.CurrentCycle

	network := protocol.FlowSimNetwork{
		Version: &version,
		Cycle:   &cycle,
		Nodes:   make([]protocol.Node, 0, len(ns.Nodes)),
		Edges:   make([]protocol.Edge, 0, len(ns.Links)),
	}

	// 恢复网络级别的显示信息
	if zoom, ok := ns.DisplayData["zoom"].(float64); ok {
		zoomFloat32 := float32(zoom)
		network.Zoom = &zoomFloat32
	}
	if pan, ok := ns.DisplayData["pan"].(map[string]interface{}); ok {
		panStruct := struct {
			X *float32 `json:"x,omitempty"`
			Y *float32 `json:"y,omitempty"`
		}{}
		if x, ok := pan["x"].(float64); ok {
			xFloat32 := float32(x)
			panStruct.X = &xFloat32
		}
		if y, ok := pan["y"].(float64); ok {
			yFloat32 := float32(y)
			panStruct.Y = &yFloat32
		}
		network.Pan = &panStruct
	}

	// 转换节点
	for i, nodeState := range ns.Nodes {
		node := protocol.Node{
			NodeId:       nodeState.ID,
			NodeName:     getNodeName(nodeState),
			NodeFeatures: &[]string{nodeState.Type},
		}

		// 转换输入端口（包含运行时状态）
		if len(nodeState.Inputs) > 0 {
			inPorts := make([]protocol.Port, len(nodeState.Inputs))
			for idx, q := range nodeState.Inputs {
				var packetTypes *[]int
				if len(q.PacketTypes) > 0 {
					pts := make([]int, 0, len(q.PacketTypes))
					for _, pt := range q.PacketTypes {
						var ptInt int
						fmt.Sscanf(pt, "%d", &ptInt)
						pts = append(pts, ptInt)
					}
					packetTypes = &pts
				}
				inPorts[idx] = protocol.Port{
					PortId:       idx,
					Bandwidth:    q.Bandwidth,
					BufferSize:   &q.Capacity,
					BufferLength: &q.Length,
					Capacity:     &q.Capacity,
					Bitmap:       &q.Bitmap,
					PacketTypes:  packetTypes,
				}
			}
			node.InPorts = &inPorts
		}

		// 转换输出端口
		if len(nodeState.Outputs) > 0 {
			outPorts := make([]protocol.Port, len(nodeState.Outputs))
			for idx, q := range nodeState.Outputs {
				var packetTypes *[]int
				if len(q.PacketTypes) > 0 {
					pts := make([]int, 0, len(q.PacketTypes))
					for _, pt := range q.PacketTypes {
						var ptInt int
						fmt.Sscanf(pt, "%d", &ptInt)
						pts = append(pts, ptInt)
					}
					packetTypes = &pts
				}
				outPorts[idx] = protocol.Port{
					PortId:       idx,
					Bandwidth:    q.Bandwidth,
					BufferSize:   &q.Capacity,
					BufferLength: &q.Length,
					Capacity:     &q.Capacity,
					Bitmap:       &q.Bitmap,
					PacketTypes:  packetTypes,
				}
			}
			node.OutPorts = &outPorts
		}

		// 从 Features 恢复 Cache 配置，从 Stats 恢复统计数据
		if nodeState.Features != nil {
			if cacheConfig, ok := nodeState.Features["cache"]; ok && cacheConfig != nil {
				capacity, _ := cacheConfig["capacity"].(int)
				numSets, _ := cacheConfig["num_sets"].(int)
				replacementPolicy, _ := cacheConfig["replacement_policy"].(string)
				states, _ := cacheConfig["states"].(string)

			cacheConfigProto := &protocol.CacheConfig{
				Capacity:          capacity,
				NumSets:           numSets,
				ReplacementPolicy: protocol.CacheConfigReplacementPolicy(replacementPolicy),
				States:            states,
			}

			// 从 Stats 恢复统计数据
			if cacheStats, ok := nodeState.Stats["cache"].([]state.CacheState); ok && len(cacheStats) > 0 {
				c := cacheStats[0]
				hits := int(c.Hits)
				misses := int(c.Misses)
				accesses := int(c.Accesses)
				cacheConfigProto.Hits = &hits
				cacheConfigProto.Misses = &misses
				cacheConfigProto.Accesses = &accesses
			}
			node.Cache = cacheConfigProto
			}

			// Directory 配置
			if directoryConfig, ok := nodeState.Features["directory"]; ok && directoryConfig != nil {
				capacity, _ := directoryConfig["capacity"].(int)
				numSets, _ := directoryConfig["num_sets"].(int)
				replacementPolicy, _ := directoryConfig["replacement_policy"].(string)
				states, _ := directoryConfig["states"].(string)

				node.Directory = &protocol.DirectoryConfig{
					Capacity:          capacity,
					NumSets:           numSets,
					ReplacementPolicy: replacementPolicy,
					States:            states,
				}
			}
		}

		// 一致性域 ID
		if nodeState.CoherenceDomainID != nil {
			node.CoherenceDomainId = nodeState.CoherenceDomainID
		}

		// 从 DisplayData 恢复显示信息
		if nodeState.DisplayData != nil {
			// 恢复 position
			if pos, ok := nodeState.DisplayData["position"].(struct {
				X float32 `json:"x"`
				Y float32 `json:"y"`
			}); ok {
				node.Position = pos
			}

			// 恢复 data
			if dataMap, ok := nodeState.DisplayData["data"].(protocol.Node_Data); ok {
				node.Data = dataMap
			} else if dataMap, ok := nodeState.DisplayData["data"].(map[string]interface{}); ok {
				node.Data = mapToNodeData(dataMap)
			}

			// 恢复 style
			if style, ok := nodeState.DisplayData["style"].(map[string]interface{}); ok {
				node.Style = &style
			}
		}

		// 如果没有 DisplayData，生成默认 display 信息（圆形布局）
		if node.Data.Id == "" {
			angle := 2 * math.Pi * float64(i) / float64(len(ns.Nodes))
			radius := 200.0
			centerX, centerY := 400.0, 300.0
			x := float32(centerX + radius*math.Cos(angle))
			y := float32(centerY + radius*math.Sin(angle))

			nodeType := nodeState.Type
			label := fmt.Sprintf("N%d", nodeState.ID)
			node.Data = protocol.Node_Data{
				Id:    fmt.Sprintf("node-%d", nodeState.ID),
				Type:  &nodeType,
				Label: &label,
			}
			node.Position = struct {
				X float32 `json:"x"`
				Y float32 `json:"y"`
			}{
				X: x,
				Y: y,
			}
		}

		network.Nodes = append(network.Nodes, node)
	}

	// 转换边
	nodeIDMap := make(map[int]bool)
	for _, n := range ns.Nodes {
		nodeIDMap[n.ID] = true
	}

	for _, linkState := range ns.Links {
		// 跳过无效的链路
		if !nodeIDMap[linkState.SourceID] || !nodeIDMap[linkState.TargetID] {
			continue
		}

		edge := protocol.Edge{
			EdgeId:    linkState.EdgeID,
			SrcNodeId: linkState.SourceID,
			DstNodeId: linkState.TargetID,
		}

		// 端口 ID - 创建副本避免指针共享
		srcPortID := linkState.SourcePortID
		dstPortID := linkState.TargetPortID
		edge.SrcPortId = &srcPortID
		edge.DstPortId = &dstPortID

		// 链路参数
		if linkState.Latency > 0 {
			edge.Latency = &linkState.Latency
		}
		if linkState.Bandwidth > 0 {
			edge.Bandwidth = &linkState.Bandwidth
		}

		// PacketTypes
		if len(linkState.PacketTypes) > 0 {
			pts := make([]int, 0, len(linkState.PacketTypes))
			for _, pt := range linkState.PacketTypes {
				var ptInt int
				fmt.Sscanf(pt, "%d", &ptInt)
				pts = append(pts, ptInt)
			}
			edge.PacketTypes = &pts
		}

		// 从 DisplayData 恢复显示信息
		if linkState.DisplayData != nil {
			if dataMap, ok := linkState.DisplayData["data"].(protocol.Edge_Data); ok {
				edge.Data = dataMap
			} else if dataMap, ok := linkState.DisplayData["data"].(map[string]interface{}); ok {
				edge.Data = mapToEdgeData(dataMap)
			}
		}

		// 如果没有 DisplayData，生成默认 display 信息
		if edge.Data.Id == "" {
			lineType := protocol.Solid
			edge.Data = protocol.Edge_Data{
				Id:       fmt.Sprintf("edge-%d-p%d-%d-p%d", linkState.SourceID, linkState.SourcePortID, linkState.TargetID, linkState.TargetPortID),
				Source:   fmt.Sprintf("node-%d", linkState.SourceID),
				Target:   fmt.Sprintf("node-%d", linkState.TargetID),
				LineType: &lineType,
			}
		}

		// 链路状态（用于可视化）
		if len(linkState.Occupancy) > 0 {
			linkStatus := []struct {
				Name   string `json:"name"`
				Values []int  `json:"values"`
			}{
				{
					Name:   "occupancy",
					Values: linkState.Occupancy,
				},
			}
			edge.LinkStatus = &linkStatus
		}

		network.Edges = append(network.Edges, edge)
	}

	return network
}

// getNodeName 获取节点名称
func getNodeName(nodeState state.NodeState) string {
	if name, ok := nodeState.CustomData["name"].(string); ok && name != "" {
		return name
	}
	return fmt.Sprintf("Node_%d", nodeState.ID)
}

// mapToNodeData 将 map[string]interface{} 转换为 protocol.Node_Data
func mapToNodeData(data map[string]interface{}) protocol.Node_Data {
	nodeData := protocol.Node_Data{
		AdditionalProperties: make(map[string]interface{}),
	}

	if id, ok := data["id"].(string); ok {
		nodeData.Id = id
	}
	if label, ok := data["label"].(string); ok {
		nodeData.Label = &label
	}
	if nodeType, ok := data["type"].(string); ok {
		nodeData.Type = &nodeType
	}

	// 其他字段放入 AdditionalProperties
	for k, v := range data {
		if k != "id" && k != "label" && k != "type" {
			nodeData.AdditionalProperties[k] = v
		}
	}

	return nodeData
}

// mapToEdgeData 将 map[string]interface{} 转换为 protocol.Edge_Data
func mapToEdgeData(data map[string]interface{}) protocol.Edge_Data {
	edgeData := protocol.Edge_Data{
		AdditionalProperties: make(map[string]interface{}),
	}

	if id, ok := data["id"].(string); ok {
		edgeData.Id = id
	}
	if source, ok := data["source"].(string); ok {
		edgeData.Source = source
	}
	if target, ok := data["target"].(string); ok {
		edgeData.Target = target
	}
	if lineType, ok := data["lineType"].(string); ok {
		lt := protocol.EdgeDataLineType(lineType)
		edgeData.LineType = &lt
	}

	// 其他字段放入 AdditionalProperties
	for k, v := range data {
		if k != "id" && k != "source" && k != "target" && k != "lineType" {
			edgeData.AdditionalProperties[k] = v
		}
	}

	return edgeData
}

// getNodeColor 根据节点类型返回颜色（保留原有逻辑）
func getNodeColor(nodeType string) string {
	switch nodeType {
	case "WorkerNode":
		return "#1890FF" // Blue
	case "HubNode", "CentralSwitch":
		return "#5CDBD3" // Cyan
	case "cpu":
		return "#52C41A" // Green
	case "cache":
		return "#FAAD14" // Orange
	case "memory_controller":
		return "#F5222D" // Red
	default:
		return "#999999" // Grey
	}
}
