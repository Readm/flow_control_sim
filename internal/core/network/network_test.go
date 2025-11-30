package network

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

func TestNetworkAdvanceMesh(t *testing.T) {
	t.Parallel()

	net := New()

	node0, _, outputs0 := newTestNodeHandle(t, 0, 0, 2)
	node1, inputs1, outputs1 := newTestNodeHandle(t, 1, 1, 1)
	node2, inputs2, _ := newTestNodeHandle(t, 2, 2, 0)

	if err := net.AddNode(node0); err != nil {
		t.Fatalf("AddNode node0: %v", err)
	}
	if err := net.AddNode(node1); err != nil {
		t.Fatalf("AddNode node1: %v", err)
	}
	if err := net.AddNode(node2); err != nil {
		t.Fatalf("AddNode node2: %v", err)
	}

	// Node1 forwards everything from its input to its single output.
	node1.Node.SetProcessHook(forwardAllPacketsHook(outputs1[0]))

	// Record packets received along different paths.
	recNode1 := newPacketRecorder()
	recNode2Direct := newPacketRecorder()
	recNode2ViaNode1 := newPacketRecorder()

	inputs1[0].SetPacketReceivedHook(recNode1.Record)
	inputs2[0].SetPacketReceivedHook(recNode2Direct.Record)
	inputs2[1].SetPacketReceivedHook(recNode2ViaNode1.Record)

	if _, err := net.Connect(0, 0, 1, 0, 1, 1); err != nil {
		t.Fatalf("connect 0->1: %v", err)
	}
	if _, err := net.Connect(0, 1, 2, 0, 1, 1); err != nil {
		t.Fatalf("connect 0->2 direct: %v", err)
	}
	if _, err := net.Connect(1, 0, 2, 1, 1, 1); err != nil {
		t.Fatalf("connect 1->2: %v", err)
	}

	mustInject(t, outputs0[0], 0, packet.Packet{Payload: "A->B"})
	mustInject(t, outputs0[1], 0, packet.Packet{Payload: "A->C"})

	if err := net.Advance(6); err != nil {
		t.Fatalf("Advance mesh: %v", err)
	}

	if got := recNode1.Payloads(); !reflect.DeepEqual(got, []string{"A->B"}) {
		t.Fatalf("node1 input packets = %v, want [A->B]", got)
	}
	if got := recNode2Direct.Payloads(); !reflect.DeepEqual(got, []string{"A->C"}) {
		t.Fatalf("node2 direct input packets = %v, want [A->C]", got)
	}
	if got := recNode2ViaNode1.Payloads(); !reflect.DeepEqual(got, []string{"A->B"}) {
		t.Fatalf("node2 via node1 packets = %v, want [A->B]", got)
	}
}

func TestNetworkAdvanceRing(t *testing.T) {
	t.Parallel()

	net := New()

	node0, inputs0, outputs0 := newTestNodeHandle(t, 0, 1, 1)
	node1, _, outputs1 := newTestNodeHandle(t, 1, 1, 1)
	node2, _, outputs2 := newTestNodeHandle(t, 2, 1, 1)

	if err := net.AddNode(node0); err != nil {
		t.Fatalf("AddNode node0: %v", err)
	}
	if err := net.AddNode(node1); err != nil {
		t.Fatalf("AddNode node1: %v", err)
	}
	if err := net.AddNode(node2); err != nil {
		t.Fatalf("AddNode node2: %v", err)
	}

	node0.Node.SetProcessHook(forwardAllPacketsHook(outputs0[0]))
	node1.Node.SetProcessHook(forwardAllPacketsHook(outputs1[0]))
	node2.Node.SetProcessHook(forwardAllPacketsHook(outputs2[0]))

	recNode0 := newPacketRecorder()
	inputs0[0].SetPacketReceivedHook(recNode0.Record)

	if _, err := net.Connect(0, 0, 1, 0, 1, 1); err != nil {
		t.Fatalf("connect 0->1: %v", err)
	}
	if _, err := net.Connect(1, 0, 2, 0, 1, 1); err != nil {
		t.Fatalf("connect 1->2: %v", err)
	}
	if _, err := net.Connect(2, 0, 0, 0, 1, 1); err != nil {
		t.Fatalf("connect 2->0: %v", err)
	}

	mustInject(t, outputs0[0], 0, packet.Packet{Payload: "ring"})

	if err := net.Advance(9); err != nil {
		t.Fatalf("Advance ring: %v", err)
	}

	got := recNode0.Payloads()
	if len(got) == 0 || got[0] != "ring" {
		t.Fatalf("node0 did not receive packet back, got %v", got)
	}
}

func newTestNodeHandle(t *testing.T, id int, inputCount, outputCount int) (*NodeHandle, []*queue.InputQueue, []*queue.OutputQueue) {
	t.Helper()

	n := node.New(id)
	inputs := make([]*queue.InputQueue, inputCount)
	outputs := make([]*queue.OutputQueue, outputCount)

	for i := 0; i < inputCount; i++ {
		iq := queue.NewInputQueue(8)
		inputs[i] = iq
		if err := n.AddInputQueue(iq); err != nil {
			t.Fatalf("AddInputQueue: %v", err)
		}
	}

	for i := 0; i < outputCount; i++ {
		oq := queue.NewOutputQueue(8)
		outputs[i] = oq
		if err := n.AddOutputQueue(oq); err != nil {
			t.Fatalf("AddOutputQueue: %v", err)
		}
	}

	return &NodeHandle{
		Node:    n,
		Inputs:  inputs,
		Outputs: outputs,
	}, inputs, outputs
}

func forwardAllPacketsHook(output *queue.OutputQueue) node.ProcessHook {
	return func(_ context.Context, cycle uint64, buffer []packet.Packet) ([]packet.Packet, error) {
		if len(buffer) == 0 {
			return buffer, nil
		}
		if err := output.InjectPackets(int(cycle), clonePackets(buffer)); err != nil {
			return nil, err
		}
		return buffer, nil
	}
}

func mustInject(t *testing.T, output *queue.OutputQueue, cycle int, pkt packet.Packet) {
	t.Helper()
	if err := output.InjectPackets(cycle, []packet.Packet{pkt}); err != nil {
		t.Fatalf("InjectPackets: %v", err)
	}
}

type packetRecorder struct {
	mu      sync.Mutex
	packets []packet.Packet
}

func newPacketRecorder() *packetRecorder {
	return &packetRecorder{
		packets: make([]packet.Packet, 0),
	}
}

func (r *packetRecorder) Record(pkt packet.Packet) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.packets = append(r.packets, pkt)
}

func (r *packetRecorder) Payloads() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]string, len(r.packets))
	for i, pkt := range r.packets {
		result[i] = string(pkt.Payload)
	}
	return result
}

func clonePackets(src []packet.Packet) []packet.Packet {
	cloned := make([]packet.Packet, len(src))
	copy(cloned, src)
	return cloned
}
