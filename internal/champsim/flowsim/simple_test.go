package flowsim

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// Test_Simple_FlowSim_Ping 测试最简单的两节点通信
func Test_Simple_FlowSim_Ping(t *testing.T) {
	// 创建两个简单的节点
	senderID := 0
	receiverID := 1

	// 创建输出队列（发送方）
	senderOutputQueue := queue.NewOutputQueue(8, 1)

	// 创建输入队列（接收方）
	receiverInputQueue := queue.NewInputQueue(8, 1)

	// 创建发送节点
	senderNode := node.NewWorkerNode(senderID)
	receivedCount := 0

	// 关键：必须调用AddOutputQueue，Node才能在Tick时处理队列！
	if err := senderNode.AddOutputQueue(senderOutputQueue); err != nil {
		t.Fatalf("Failed to add output queue: %v", err)
	}

	// 发送节点的处理逻辑：使用预注入模式（正确的用法）
	// 在AdvanceTo之前注入包，而不是在Process中
	pkt := packet.Packet{
		SourceID: senderID,
		TargetID: receiverID,
		Payload:  "Hello",
	}

	// 预注入包到cycle 0
	t.Log("Pre-injecting packet before AdvanceTo")
	if err := senderOutputQueue.InjectPackets(0, []packet.Packet{pkt}); err != nil {
		t.Fatalf("Failed to inject packet: %v", err)
	}

	// Sender不需要特殊的Process逻辑
	senderNode.SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
		return nil
	})

	// 创建接收节点
	receiverNode := node.NewWorkerNode(receiverID)

	// 关键：必须调用AddInputQueue！
	if err := receiverNode.AddInputQueue(receiverInputQueue); err != nil {
		t.Fatalf("Failed to add input queue: %v", err)
	}

	// 接收节点的处理逻辑：接收并计数
	receiverNode.SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
		if len(inputs) > 0 {
			for _, ref := range inputs[0] {
				t.Logf("Cycle %d: Receiver got packet: %s", cycle, ref.Packet.Payload)
				receivedCount++
				ref.Queue.Free(ref.Slot)
			}
		}
		return nil
	})

	// 创建NodeHandles
	senderHandle := &network.NodeHandle{
		Node:    senderNode,
		Inputs:  []*queue.InputQueue{},
		Outputs: []*queue.OutputQueue{senderOutputQueue},
	}

	receiverHandle := &network.NodeHandle{
		Node:    receiverNode,
		Inputs:  []*queue.InputQueue{receiverInputQueue},
		Outputs: []*queue.OutputQueue{},
	}

	// 创建Network
	net := network.New()
	net.AddNode(senderHandle)
	net.AddNode(receiverHandle)

	// 连接
	_, err := net.Connect(senderID, 0, receiverID, 0, 1, 1)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	t.Log("Starting simple ping test...")

	// 一次性推进10个周期（正确的用法）
	// latency=1意味着包在cycle 1到达接收方
	// 推进到cycle 9确保有足够时间接收
	maxCycles := 10
	t.Logf("Advancing to cycle %d", maxCycles-1)
	if err := net.AdvanceTo(maxCycles - 1); err != nil {
		t.Fatalf("Failed to advance: %v", err)
	}
	t.Logf("Advance completed")

	// 验证
	if receivedCount != 1 {
		t.Errorf("Expected to receive 1 packet, got %d", receivedCount)
	} else {
		t.Log("✅ Simple ping test passed!")
	}
}
