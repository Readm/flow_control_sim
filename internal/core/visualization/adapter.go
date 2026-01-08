package visualization

import (
	"fmt"
	"math"
	"sync"

	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

// DisplayCache 缓存前端提交的可视化信息
type DisplayCache struct {
	mu    sync.RWMutex
	nodes map[int]NodeDisplayInfo // node_id -> display info
	edges map[int]EdgeDisplayInfo // edge_id -> display info
}

// NodeDisplayInfo 节点显示信息
type NodeDisplayInfo struct {
	Data     map[string]interface{} // CyEditor data 字段
	Position struct {
		X float32 `json:"x"`
		Y float32 `json:"y"`
	} // position 字段
	Style map[string]interface{} // style 字段（可选）
}

// EdgeDisplayInfo 边显示信息
type EdgeDisplayInfo struct {
	Data map[string]interface{} // CyEditor data 字段
}

var globalDisplayCache = &DisplayCache{
	nodes: make(map[int]NodeDisplayInfo),
	edges: make(map[int]EdgeDisplayInfo),
}

// CacheNodeDisplay 缓存节点显示信息
func CacheNodeDisplay(nodeID int, data map[string]interface{}, position struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}, style map[string]interface{}) {
	globalDisplayCache.mu.Lock()
	defer globalDisplayCache.mu.Unlock()
	globalDisplayCache.nodes[nodeID] = NodeDisplayInfo{
		Data:     data,
		Position: position,
		Style:    style,
	}
}

// CacheEdgeDisplay 缓存边显示信息
func CacheEdgeDisplay(edgeID int, data map[string]interface{}) {
	globalDisplayCache.mu.Lock()
	defer globalDisplayCache.mu.Unlock()
	globalDisplayCache.edges[edgeID] = EdgeDisplayInfo{
		Data: data,
	}
}

// GetNodeDisplay 获取缓存的节点显示信息
func GetNodeDisplay(nodeID int) (NodeDisplayInfo, bool) {
	globalDisplayCache.mu.RLock()
	defer globalDisplayCache.mu.RUnlock()
	info, ok := globalDisplayCache.nodes[nodeID]
	return info, ok
}

// GetEdgeDisplay 获取缓存的边显示信息
func GetEdgeDisplay(edgeID int) (EdgeDisplayInfo, bool) {
	globalDisplayCache.mu.RLock()
	defer globalDisplayCache.mu.RUnlock()
	info, ok := globalDisplayCache.edges[edgeID]
	return info, ok
}

// GetEdgeDisplayByPorts 根据节点和端口信息查找缓存的边显示信息
// 遍历所有缓存的边,匹配 source/target 和端口信息
func GetEdgeDisplayByPorts(srcNodeID, srcPortID, dstNodeID, dstPortID int) (EdgeDisplayInfo, bool) {
	globalDisplayCache.mu.RLock()
	defer globalDisplayCache.mu.RUnlock()

	// 遍历所有缓存的边,找到匹配的
	for _, info := range globalDisplayCache.edges {
		// 检查data中的source和target
		source, hasSource := info.Data["source"].(string)
		target, hasTarget := info.Data["target"].(string)

		if !hasSource || !hasTarget {
			continue
		}

		expectedSource := fmt.Sprintf("node-%d", srcNodeID)
		expectedTarget := fmt.Sprintf("node-%d", dstNodeID)

		if source != expectedSource || target != expectedTarget {
			continue
		}

		// 检查端口信息(可能存储在 srcPort/dstPort 字段中)
		if srcPort, ok := info.Data["srcPort"].(int); ok {
			if srcPort != srcPortID {
				continue
			}
		}
		if dstPort, ok := info.Data["dstPort"].(int); ok {
			if dstPort != dstPortID {
				continue
			}
		}

		// 找到匹配的边
		return info, true
	}

	return EdgeDisplayInfo{}, false
}

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
				inPorts[idx] = protocol.Port{
					PortId:       idx,
					Bandwidth:    q.Bandwidth,
					BufferSize:   &q.Capacity,
					BufferLength: &q.Length,
					Capacity:     &q.Capacity,
					Bitmap:       &q.Bitmap,
				}
			}
			node.InPorts = &inPorts
		}

		// 转换输出端口
		if len(nodeState.Outputs) > 0 {
			outPorts := make([]protocol.Port, len(nodeState.Outputs))
			for idx, q := range nodeState.Outputs {
				outPorts[idx] = protocol.Port{
					PortId:       idx,
					Bandwidth:    q.Bandwidth,
					BufferSize:   &q.Capacity,
					BufferLength: &q.Length,
					Capacity:     &q.Capacity,
					Bitmap:       &q.Bitmap,
				}
			}
			node.OutPorts = &outPorts
		}

		// 转换缓存配置和统计
		// 注意: state.CacheState 只包含统计数据,配置信息需要从其他地方获取
		// 这里暂时跳过 cache,因为 state 中没有配置信息
		// TODO: 如果需要完整的 cache 配置,需要在 NodeState 中添加配置字段
		if len(nodeState.Caches) > 0 {
			c := nodeState.Caches[0]
			// 使用默认配置值,只填充统计数据
			hits := int(c.Hits)
			misses := int(c.Misses)
			accesses := int(c.Accesses)
			node.Cache = &protocol.CacheConfig{
				Capacity:          1024,  // 默认值
				NumSets:           1,     // 默认值
				ReplacementPolicy: protocol.LRU, // 默认值
				States:            "MESI", // 默认值
				Hits:              &hits,
				Misses:            &misses,
				Accesses:          &accesses,
			}
		}

		// 转换目录配置（如果有）
		// 注意: state.DirectoryState 只包含运行时条目,配置信息需要从其他地方获取
		// TODO: 如果需要完整的 directory 配置,需要在 NodeState 中添加配置字段
		if len(nodeState.Directories) > 0 {
			// 使用默认配置值
			node.Directory = &protocol.DirectoryConfig{
				Capacity:          256,    // 默认值
				NumSets:           1,      // 默认值
				ReplacementPolicy: "LRU",  // 默认值
				States:            "MESI", // 默认值
			}
		}

		// 一致性域 ID
		if coherenceDomainID, ok := nodeState.CustomData["coherence_domain_id"].(int); ok {
			node.CoherenceDomainId = &coherenceDomainID
		}

		// 恢复或生成 Display 信息
		if displayInfo, cached := GetNodeDisplay(nodeState.ID); cached {
			// 使用缓存的 display 信息
			node.Data = mapToNodeData(displayInfo.Data)
			node.Position = displayInfo.Position
			node.Style = &displayInfo.Style
		} else {
			// 生成默认 display 信息（圆形布局）
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

	for i, linkState := range ns.Links {
		// 跳过无效的链路
		if !nodeIDMap[linkState.SourceID] || !nodeIDMap[linkState.TargetID] {
			continue
		}

		edgeID := i + 1
		edge := protocol.Edge{
			EdgeId:    edgeID,
			SrcNodeId: linkState.SourceID,
			DstNodeId: linkState.TargetID,
		}

		// 端口 ID (现在从 LinkState 中导出)
		edge.SrcPortId = &linkState.SourcePortID
		edge.DstPortId = &linkState.TargetPortID

		// 链路参数
		if linkState.Latency > 0 {
			edge.Latency = &linkState.Latency
		}
		if linkState.Bandwidth > 0 {
			edge.Bandwidth = &linkState.Bandwidth
		}

		// CyEditor data 字段
		// 优先使用基于端口的查找,回退到edgeID查找
		displayInfo, cached := GetEdgeDisplayByPorts(linkState.SourceID, linkState.SourcePortID, linkState.TargetID, linkState.TargetPortID)
		if !cached {
			displayInfo, cached = GetEdgeDisplay(edgeID)
		}

		if cached {
			edge.Data = mapToEdgeData(displayInfo.Data)
			// 从缓存的display数据中恢复端口信息(如果有)
			if srcPort, ok := displayInfo.Data["srcPort"].(int); ok {
				edge.SrcPortId = &srcPort
			}
			if dstPort, ok := displayInfo.Data["dstPort"].(int); ok {
				edge.DstPortId = &dstPort
			}
		} else {
			lineType := protocol.Solid
			// 生成默认 display 信息
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
