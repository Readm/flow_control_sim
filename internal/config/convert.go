package config

import (
	"fmt"
	"math"

	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

// ToFlowSimNetwork converts an EntityConfig to a FlowSimNetwork.
// This provides a bridge for legacy code and tests that use EntityConfig.
func (c EntityConfig) ToFlowSimNetwork() protocol.FlowSimNetwork {
	version := "1.0.0"
	cycle := 0

	network := protocol.FlowSimNetwork{
		Version: &version,
		Cycle:   &cycle,
		Nodes:   make([]protocol.Node, 0, len(c.Nodes)),
		Edges:   make([]protocol.Edge, 0, len(c.Edges)),
	}

	// 转换节点
	for i, nodeConfig := range c.Nodes {
		// 生成圆形布局位置
		angle := 2 * math.Pi * float64(i) / float64(len(c.Nodes))
		radius := 200.0
		centerX, centerY := 400.0, 300.0
		x := float32(centerX + radius*math.Cos(angle))
		y := float32(centerY + radius*math.Sin(angle))

		// 默认端口配置：每个节点一个输入端口和一个输出端口
		bufferSize := 64
		bandwidth := 1
		inPorts := []protocol.Port{
			{
				PortId:     0,
				Bandwidth:  bandwidth,
				BufferSize: &bufferSize,
			},
		}
		outPorts := []protocol.Port{
			{
				PortId:    0,
				Bandwidth: bandwidth,
			},
		}

		label := fmt.Sprintf("N%d", nodeConfig.ID)
		node := protocol.Node{
			NodeId:       nodeConfig.ID,
			NodeName:     fmt.Sprintf("Node_%d", nodeConfig.ID),
			NodeFeatures: &[]string{nodeConfig.Type},
			InPorts:      &inPorts,
			OutPorts:     &outPorts,
			Data: protocol.Node_Data{
				Id:    fmt.Sprintf("node-%d", nodeConfig.ID),
				Type:  &nodeConfig.Type,
				Label: &label,
			},
			Position: struct {
				X float32 `json:"x"`
				Y float32 `json:"y"`
			}{
				X: x,
				Y: y,
			},
		}

		network.Nodes = append(network.Nodes, node)
	}

	// 转换边
	latency := int(c.Link.EffectiveDelay().Milliseconds())
	if latency == 0 {
		latency = 1 // 默认最小延迟
	}
	bandwidth := 1

	for i, edgeConfig := range c.Edges {
		lineType := protocol.Solid
		srcPortId := 0
		dstPortId := 0

		edge := protocol.Edge{
			EdgeId:    i + 1,
			SrcNodeId: edgeConfig.Src,
			SrcPortId: &srcPortId,
			DstNodeId: edgeConfig.Dst,
			DstPortId: &dstPortId,
			Latency:   &latency,
			Bandwidth: &bandwidth,
			Data: protocol.Edge_Data{
				Id:       fmt.Sprintf("edge-%d-p0-%d-p0", edgeConfig.Src, edgeConfig.Dst),
				Source:   fmt.Sprintf("node-%d", edgeConfig.Src),
				Target:   fmt.Sprintf("node-%d", edgeConfig.Dst),
				LineType: &lineType,
			},
		}

		network.Edges = append(network.Edges, edge)
	}

	return network
}
