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
		Metadata: map[string]interface{}{"payload": "Hello"},
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
	_, err := net.ConnectNodes(senderNode, 0, receiverNode, 0, 1, 1)
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
		t.Log(" Simple ping test passed!")
	}
}

// Test_PortNaming_Example 展示端口命名功能的用法
func Test_PortNaming_Example(t *testing.T) {
	// 创建发送节点（2个输出端口：to_receiver_a, to_receiver_b）
	senderNode := node.NewWorkerNode(0)
	outA := queue.NewOutputQueue(8, 1)
	outB := queue.NewOutputQueue(8, 1)
	senderNode.AddOutputQueue(outA)
	senderNode.AddOutputQueue(outB)
	// 使用命名让代码更易读
	senderNode.NameOutputPorts("to_receiver_a", "to_receiver_b")

	// 创建接收节点 A（1个输入端口：from_sender）
	receiverA := node.NewWorkerNode(1)
	inA := queue.NewInputQueue(8, 1)
	receiverA.AddInputQueue(inA)
	receiverA.NameInputPort(0, "from_sender")

	// 创建接收节点 B（1个输入端口：from_sender）
	receiverB := node.NewWorkerNode(2)
	inB := queue.NewInputQueue(8, 1)
	receiverB.AddInputQueue(inB)
	receiverB.NameInputPort(0, "from_sender")

	// 设置处理逻辑
	senderNode.SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
		return nil
	})

	receivedA := 0
	receiverA.SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
		for _, q := range inputs {
			for _, ref := range q {
				t.Logf("Receiver A got: %v", ref.Packet.Payload)
				receivedA++
				ref.Queue.Free(ref.Slot)
			}
		}
		return nil
	})

	receivedB := 0
	receiverB.SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
		for _, q := range inputs {
			for _, ref := range q {
				t.Logf("Receiver B got: %v", ref.Packet.Payload)
				receivedB++
				ref.Queue.Free(ref.Slot)
			}
		}
		return nil
	})

	// 创建 Network 并添加节点
	net := network.New()
	net.AddNode(&network.NodeHandle{
		Node:    senderNode,
		Inputs:  []*queue.InputQueue{},
		Outputs: []*queue.OutputQueue{outA, outB},
	})
	net.AddNode(&network.NodeHandle{
		Node:    receiverA,
		Inputs:  []*queue.InputQueue{inA},
		Outputs: []*queue.OutputQueue{},
	})
	net.AddNode(&network.NodeHandle{
		Node:    receiverB,
		Inputs:  []*queue.InputQueue{inB},
		Outputs: []*queue.OutputQueue{},
	})

	// 使用端口名称连接节点（更易读！）
	// 对比：使用索引会是 ConnectNodes(senderNode, 0, receiverA, 0, ...)
	_, err := net.ConnectNodes(senderNode, "to_receiver_a", receiverA, "from_sender", 1, 1)
	if err != nil {
		t.Fatalf("Failed to connect sender->A: %v", err)
	}

	// 也可以混合使用端口名称和索引
	_, err = net.ConnectNodes(senderNode, 1, receiverB, "from_sender", 1, 1)
	if err != nil {
		t.Fatalf("Failed to connect sender->B: %v", err)
	}

	// 预注入包到两个输出端口
	pktA := packet.Packet{SourceID: 0, TargetID: 1, Metadata: map[string]interface{}{"payload": "Message to A"}}
	pktB := packet.Packet{SourceID: 0, TargetID: 2, Metadata: map[string]interface{}{"payload": "Message to B"}}
	outA.InjectPackets(0, []packet.Packet{pktA})
	outB.InjectPackets(0, []packet.Packet{pktB})

	// 推进仿真
	t.Log("Starting port naming example test...")
	if err := net.AdvanceTo(10); err != nil {
		t.Fatalf("Failed to advance: %v", err)
	}

	// 验证
	if receivedA != 1 {
		t.Errorf("Expected receiver A to get 1 packet, got %d", receivedA)
	}
	if receivedB != 1 {
		t.Errorf("Expected receiver B to get 1 packet, got %d", receivedB)
	}

	if receivedA == 1 && receivedB == 1 {
		t.Log(" Port naming example test passed!")
		t.Log("   端口命名让连接更清晰：")
		t.Log("   - 使用名称: ConnectNodes(sender, \"to_receiver_a\", ...)")
		t.Log("   - 使用索引: ConnectNodes(sender, 0, ...) // 需要记住 0 是哪个端口")
	}
}
