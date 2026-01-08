package builder

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

// TestNetworkBuilder 提供简化的测试网络构建 API
type TestNetworkBuilder struct {
	nodes []protocol.Node
	edges []protocol.Edge
}

// NewTestNetworkBuilder 创建测试网络构建器
func NewTestNetworkBuilder() *TestNetworkBuilder {
	return &TestNetworkBuilder{
		nodes: make([]protocol.Node, 0),
		edges: make([]protocol.Edge, 0),
	}
}

// AddNode 添加节点（简化接口）
func (b *TestNetworkBuilder) AddNode(nodeID int, inPorts, outPorts int) *TestNetworkBuilder {
	// 构建输入端口
	var inPortsSlice []protocol.Port
	if inPorts > 0 {
		inPortsSlice = make([]protocol.Port, inPorts)
		for i := 0; i < inPorts; i++ {
			inPortsSlice[i] = protocol.Port{
				PortId:    i,
				Bandwidth: 1,
			}
			bufferSize := 16
			inPortsSlice[i].BufferSize = &bufferSize
		}
	}

	// 构建输出端口
	var outPortsSlice []protocol.Port
	if outPorts > 0 {
		outPortsSlice = make([]protocol.Port, outPorts)
		for i := 0; i < outPorts; i++ {
			outPortsSlice[i] = protocol.Port{
				PortId:    i,
				Bandwidth: 1,
			}
			bufferSize := 16
			outPortsSlice[i].BufferSize = &bufferSize
		}
	}

	node := protocol.Node{
		NodeId:   nodeID,
		NodeName: fmt.Sprintf("Node_%d", nodeID),
		Data: protocol.Node_Data{
			Id: fmt.Sprintf("node-%d", nodeID),
		},
		Position: struct {
			X float32 `json:"x"`
			Y float32 `json:"y"`
		}{
			X: float32(100 + nodeID*100),
			Y: 100,
		},
	}

	if len(inPortsSlice) > 0 {
		node.InPorts = &inPortsSlice
	}
	if len(outPortsSlice) > 0 {
		node.OutPorts = &outPortsSlice
	}

	b.nodes = append(b.nodes, node)
	return b
}

// AddNodeWithPorts 添加节点（自定义端口配置）
func (b *TestNetworkBuilder) AddNodeWithPorts(nodeID int, inPorts, outPorts []protocol.Port) *TestNetworkBuilder {
	node := protocol.Node{
		NodeId:   nodeID,
		NodeName: fmt.Sprintf("Node_%d", nodeID),
		Data: protocol.Node_Data{
			Id: fmt.Sprintf("node-%d", nodeID),
		},
		Position: struct {
			X float32 `json:"x"`
			Y float32 `json:"y"`
		}{
			X: float32(100 + nodeID*100),
			Y: 100,
		},
	}

	if len(inPorts) > 0 {
		node.InPorts = &inPorts
	}
	if len(outPorts) > 0 {
		node.OutPorts = &outPorts
	}

	b.nodes = append(b.nodes, node)
	return b
}

// AddEdge 添加边
func (b *TestNetworkBuilder) AddEdge(edgeID, srcNodeID, srcPortID, dstNodeID, dstPortID int) *TestNetworkBuilder {
	latency := 1
	bandwidth := 1

	edge := protocol.Edge{
		EdgeId:    edgeID,
		SrcNodeId: srcNodeID,
		SrcPortId: &srcPortID,
		DstNodeId: dstNodeID,
		DstPortId: &dstPortID,
		Latency:   &latency,
		Bandwidth: &bandwidth,
		Data: protocol.Edge_Data{
			Id:     fmt.Sprintf("edge-%d", edgeID),
			Source: fmt.Sprintf("node-%d", srcNodeID),
			Target: fmt.Sprintf("node-%d", dstNodeID),
		},
	}

	b.edges = append(b.edges, edge)
	return b
}

// BuildFlowSimNetwork 构建 FlowSimNetwork
func (b *TestNetworkBuilder) BuildFlowSimNetwork() protocol.FlowSimNetwork {
	return protocol.FlowSimNetwork{
		Nodes: b.nodes,
		Edges: b.edges,
	}
}
