package network

import (
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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
	node1.Node.(*node.WorkerNode).SetProcessHook(forwardAllPacketsHook(outputs1[0]))

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

	mustInject(t, outputs0[0], 0, packet.Packet{Metadata: map[string]interface{}{"payload": "A->B"}})
	mustInject(t, outputs0[1], 0, packet.Packet{Metadata: map[string]interface{}{"payload": "A->C"}})

	if err := net.AdvanceTo(net.CurrentCycle() + 6 - 1); err != nil {
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

	node0.Node.(*node.WorkerNode).SetProcessHook(forwardAllPacketsHook(outputs0[0]))
	node1.Node.(*node.WorkerNode).SetProcessHook(forwardAllPacketsHook(outputs1[0]))
	node2.Node.(*node.WorkerNode).SetProcessHook(forwardAllPacketsHook(outputs2[0]))

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

	mustInject(t, outputs0[0], 0, packet.Packet{Metadata: map[string]interface{}{"payload": "ring"}})

	if err := net.AdvanceTo(net.CurrentCycle() + 9 - 1); err != nil {
		t.Fatalf("Advance ring: %v", err)
	}

	got := recNode0.Payloads()
	if len(got) == 0 || got[0] != "ring" {
		t.Fatalf("node0 did not receive packet back, got %v", got)
	}
}

func TestNetworkAdvanceInterleavesComponentCycles(t *testing.T) {

	net := New()

	node0, _, outputs0 := newTestNodeHandle(t, 0, 1, 1)
	node1, _, outputs1 := newTestNodeHandle(t, 1, 1, 1)
	node2, _, outputs2 := newTestNodeHandle(t, 2, 1, 1)

	for _, handle := range []*NodeHandle{node0, node1, node2} {
		if err := net.AddNode(handle); err != nil {
			t.Fatalf("AddNode %d: %v", handle.Node.ID(), err)
		}
	}

	// Use simple FIFO hooks to keep queues active even without user traffic.
	node0.Node.(*node.WorkerNode).SetProcessHook(forwardAllPacketsHook(outputs0[0]))
	node1.Node.(*node.WorkerNode).SetProcessHook(forwardAllPacketsHook(outputs1[0]))
	node2.Node.(*node.WorkerNode).SetProcessHook(forwardAllPacketsHook(outputs2[0]))

	link01, err := net.Connect(0, 0, 1, 0, 5, 1)
	if err != nil {
		t.Fatalf("connect 0->1: %v", err)
	}
	link12, err := net.Connect(1, 0, 2, 0, 5, 1)
	if err != nil {
		t.Fatalf("connect 1->2: %v", err)
	}
	link20, err := net.Connect(2, 0, 0, 0, 5, 1)
	if err != nil {
		t.Fatalf("connect 2->0: %v", err)
	}

	const (
		advanceCycles        = 12
		payloadAdvanceCycles = 30
		componentsPerCycle   = 6 // 3 nodes + 3 links
	)
	events := make(chan componentCycle, advanceCycles*componentsPerCycle)

	// Test timing: record events to verify interleaving
	node0.Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
		recordEvent(events, componentCycle{Component: "node0", Cycle: int(cycle)})
		return forwardAllPacketsHook(outputs0[0])(cycle, inputs)
	})
	node1.Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
		// Slow node1 down to force interleaving with other components.
		if cycle%2 == 0 {
			time.Sleep(1 * time.Millisecond)
		}
		recordEvent(events, componentCycle{Component: "node1", Cycle: int(cycle)})
		return forwardAllPacketsHook(outputs1[0])(cycle, inputs)
	})
	node2.Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
		recordEvent(events, componentCycle{Component: "node2", Cycle: int(cycle)})
		return forwardAllPacketsHook(outputs2[0])(cycle, inputs)
	})

	link01.SetTickHook(func(cycle int) {
		recordEvent(events, componentCycle{Component: "link0-1", Cycle: cycle})
	})
	link12.SetTickHook(func(cycle int) {
		recordEvent(events, componentCycle{Component: "link1-2", Cycle: cycle})
	})
	link20.SetTickHook(func(cycle int) {
		recordEvent(events, componentCycle{Component: "link2-0", Cycle: cycle})
	})

	if err := net.AdvanceTo(net.CurrentCycle() + advanceCycles - 1); err != nil {
		t.Fatalf("Advance interleave: %v", err)
	}

	// Validate functional correctness: inject a packet and make sure it returns to node0.
	close(events)
	timeline := drainTimeline(events)

	if len(timeline) != advanceCycles*componentsPerCycle {
		t.Fatalf("timeline length = %d, want %d", len(timeline), advanceCycles*componentsPerCycle)
	}
	if isMonotonicByCycle(timeline) {
		t.Fatalf("timeline cycles monotonic, want interleaved: %+v", timeline)
	}
	t.Logf("component timeline:\n%s", formatTimeline(timeline))

	// Disable event hooks
	node0.Node.(*node.WorkerNode).SetProcessHook(nil)
	node1.Node.(*node.WorkerNode).SetProcessHook(nil)
	node2.Node.(*node.WorkerNode).SetProcessHook(nil)
	link01.SetTickHook(nil)
	link12.SetTickHook(nil)
	link20.SetTickHook(nil)

	fnNet := New()
	fnNode0, fnInputs0, fnOutputs0 := newTestNodeHandle(t, 0, 1, 1)
	fnNode1, _, fnOutputs1 := newTestNodeHandle(t, 1, 1, 1)
	fnNode2, _, fnOutputs2 := newTestNodeHandle(t, 2, 1, 1)
	for _, handle := range []*NodeHandle{fnNode0, fnNode1, fnNode2} {
		if err := fnNet.AddNode(handle); err != nil {
			t.Fatalf("AddNode %d (functional) failed: %v", handle.Node.ID(), err)
		}
	}
	fnNode0.Node.(*node.WorkerNode).SetProcessHook(forwardAllPacketsHook(fnOutputs0[0]))
	fnNode1.Node.(*node.WorkerNode).SetProcessHook(forwardAllPacketsHook(fnOutputs1[0]))
	fnNode2.Node.(*node.WorkerNode).SetProcessHook(forwardAllPacketsHook(fnOutputs2[0]))
	if _, err := fnNet.Connect(0, 0, 1, 0, 5, 1); err != nil {
		t.Fatalf("functional connect 0->1: %v", err)
	}
	if _, err := fnNet.Connect(1, 0, 2, 0, 5, 1); err != nil {
		t.Fatalf("functional connect 1->2: %v", err)
	}
	if _, err := fnNet.Connect(2, 0, 0, 0, 5, 1); err != nil {
		t.Fatalf("functional connect 2->0: %v", err)
	}

	recNode0 := newPacketRecorder()
	fnInputs0[0].SetPacketReceivedHook(recNode0.Record)
	mustInject(t, fnOutputs0[0], 0, packet.Packet{Metadata: map[string]interface{}{"payload": "ping"}})
	if err := fnNet.AdvanceTo(fnNet.CurrentCycle() + payloadAdvanceCycles - 1); err != nil {
		t.Fatalf("functional Advance for payload delivery: %v", err)
	}
	if got := recNode0.Payloads(); len(got) == 0 || got[0] != "ping" {
		t.Fatalf("node0 (functional ring) did not receive payload back, got %v", got)
	}
}

func newTestNodeHandle(t *testing.T, id int, inputCount, outputCount int) (*NodeHandle, []*queue.InputQueue, []*queue.OutputQueue) {
	t.Helper()

	n := node.NewWorkerNode(id)
	inputs := make([]*queue.InputQueue, inputCount)
	outputs := make([]*queue.OutputQueue, outputCount)

	for i := 0; i < inputCount; i++ {
		iq := queue.NewInputQueue(8, 8)
		inputs[i] = iq
		if err := n.AddInputQueue(iq); err != nil {
			t.Fatalf("AddInputQueue: %v", err)
		}
	}

	for i := 0; i < outputCount; i++ {
		oq := queue.NewOutputQueue(8, 1)
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

func forwardAllPacketsHook(output *queue.OutputQueue) func(cycle uint64, inputs [][]queue.PacketRef) error {
	return func(cycle uint64, inputs [][]queue.PacketRef) error {
		var flat []packet.Packet
		for _, q := range inputs {
			for _, ref := range q {
				flat = append(flat, ref.Packet)
				ref.Queue.Free(ref.Slot)
			}
		}
		if len(flat) == 0 {
			return nil
		}
		return output.InjectPackets(int(cycle), clonePackets(flat))
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
		result[i] = pkt.Metadata["payload"].(string)
	}
	return result
}

func clonePackets(src []packet.Packet) []packet.Packet {
	cloned := make([]packet.Packet, len(src))
	copy(cloned, src)
	return cloned
}

type componentCycle struct {
	Component string
	Cycle     int
}

func recordEvent(ch chan<- componentCycle, evt componentCycle) {
	select {
	case ch <- evt:
	default:
	}
}

func drainTimeline(events <-chan componentCycle) []componentCycle {
	result := make([]componentCycle, 0, len(events))
	for evt := range events {
		result = append(result, evt)
	}
	return result
}

func isMonotonicByCycle(events []componentCycle) bool {
	if len(events) == 0 {
		return true
	}
	prev := events[0].Cycle
	for i := 1; i < len(events); i++ {
		if events[i].Cycle < prev {
			return false
		}
		prev = events[i].Cycle
	}
	return true
}

func formatTimeline(events []componentCycle) string {
	var b strings.Builder
	for i, evt := range events {
		b.WriteString(evt.Component)
		b.WriteString(" @ ")
		b.WriteString(strconv.Itoa(evt.Cycle))
		if i != len(events)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// TestConnectNodes_PortNaming tests ConnectNodes with named ports
func TestConnectNodes_PortNaming(t *testing.T) {
	t.Parallel()

	net := New()

	// Create source node with named output ports
	source := node.NewWorkerNode(1)
	oq1 := queue.NewOutputQueue(8, 1)
	oq2 := queue.NewOutputQueue(8, 1)
	source.AddOutputQueue(oq1)
	source.AddOutputQueue(oq2)
	source.NameOutputPorts("to_l1", "to_l2")

	// Create target node with named input ports
	target := node.NewWorkerNode(2)
	iq1 := queue.NewInputQueue(8, 1)
	iq2 := queue.NewInputQueue(8, 1)
	target.AddInputQueue(iq1)
	target.AddInputQueue(iq2)
	target.NameInputPorts("from_cpu", "from_mem")

	net.AddNode(newNodeHandle(source, []*queue.InputQueue{}, []*queue.OutputQueue{oq1, oq2}))
	net.AddNode(newNodeHandle(target, []*queue.InputQueue{iq1, iq2}, []*queue.OutputQueue{}))

	// Test 1: Connect using port names (strings)
	link1, err := net.ConnectNodes(source, "to_l1", target, "from_cpu", 1, 1)
	if err != nil {
		t.Fatalf("ConnectNodes with port names failed: %v", err)
	}
	if link1 == nil {
		t.Fatal("Expected non-nil link")
	}

	// Test 2: Connect using port indices (int) - backward compatibility
	link2, err := net.ConnectNodes(source, 1, target, 1, 1, 1)
	if err != nil {
		t.Fatalf("ConnectNodes with port indices failed: %v", err)
	}
	if link2 == nil {
		t.Fatal("Expected non-nil link")
	}

	// Test 3: Mixed - int for source, string for target
	// Create a new target to avoid port conflicts
	target2 := node.NewWorkerNode(3)
	iq3 := queue.NewInputQueue(8, 1)
	iq4 := queue.NewInputQueue(8, 1)
	target2.AddInputQueue(iq3)
	target2.AddInputQueue(iq4)
	target2.NameInputPorts("from_cpu", "from_mem")

	source2 := node.NewWorkerNode(4)
	oq3 := queue.NewOutputQueue(8, 1)
	source2.AddOutputQueue(oq3)

	net.AddNode(newNodeHandle(target2, []*queue.InputQueue{iq3, iq4}, []*queue.OutputQueue{}))
	net.AddNode(newNodeHandle(source2, []*queue.InputQueue{}, []*queue.OutputQueue{oq3}))

	link3, err := net.ConnectNodes(source2, 0, target2, "from_mem", 1, 1)
	if err != nil {
		t.Fatalf("ConnectNodes with mixed port types failed: %v", err)
	}
	if link3 == nil {
		t.Fatal("Expected non-nil link")
	}
}

// TestConnectNodes_PortNamingErrors tests error handling for port naming
func TestConnectNodes_PortNamingErrors(t *testing.T) {
	t.Parallel()

	net := New()

	source := node.NewWorkerNode(1)
	oq := queue.NewOutputQueue(8, 1)
	source.AddOutputQueue(oq)
	source.NameOutputPort(0, "valid_port")

	target := node.NewWorkerNode(2)
	iq := queue.NewInputQueue(8, 1)
	target.AddInputQueue(iq)

	net.AddNode(newNodeHandle(source, []*queue.InputQueue{}, []*queue.OutputQueue{oq}))
	net.AddNode(newNodeHandle(target, []*queue.InputQueue{iq}, []*queue.OutputQueue{}))

	// Test 1: Invalid port type (not int or string)
	_, err := net.ConnectNodes(source, 3.14, target, 0, 1, 1)
	if err == nil {
		t.Fatal("Expected error for invalid port type (float)")
	}
	if !strings.Contains(err.Error(), "invalid port type") {
		t.Errorf("Expected 'invalid port type' error, got: %v", err)
	}

	// Test 2: Port name not found
	_, err = net.ConnectNodes(source, "nonexistent", target, 0, 1, 1)
	if err == nil {
		t.Fatal("Expected error for nonexistent port name")
	}
	if !strings.Contains(err.Error(), "port name") && !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'port name not found' error, got: %v", err)
	}

	// Test 3: Out of range port index
	_, err = net.ConnectNodes(source, 999, target, 0, 1, 1)
	if err == nil {
		t.Fatal("Expected error for out-of-range port index")
	}
}

func newNodeHandle(n node.Node, inputs []*queue.InputQueue, outputs []*queue.OutputQueue) *NodeHandle {
	return &NodeHandle{
		Node:    n,
		Inputs:  inputs,
		Outputs: outputs,
	}
}
