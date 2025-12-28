package node_test

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// MockHandler for testing
type MockHandler struct{}

func (h *MockHandler) Process(cycle uint64, inputs [][]queue.PacketRef) error {
	return nil
}

func TestNode_ExportState(t *testing.T) {
	// 1. Setup Node
	n := node.NewBaseNode(101, &MockHandler{})

	// Add Input Queue
	iq := queue.NewInputQueue(16, 2)
	n.AddInputQueue(iq)

	// Add Output Queue
	oq := queue.NewOutputQueue(16, 2)
	n.AddOutputQueue(oq)

	// Inject some state
	n.SetData("ProtocolState", "Idle")

	// Simulate some activity (fake)
	// We can't easily tick without a full setup, but we can verify initial state export

	// 2. Export State
	cfg := state.ExportConfig{DetailLevel: state.DetailLevelSummary}
	ns := n.ExportState(cfg)

	// 3. Verify
	if ns.ID != 101 {
		t.Errorf("Expected ID 101, got %d", ns.ID)
	}
	// "node_test.MockHandler" because we are in node_test package
	if ns.Type != "*node_test.MockHandler" {
		t.Errorf("Expected Type *node_test.MockHandler, got %s", ns.Type)
	}
	if len(ns.Inputs) != 1 {
		t.Errorf("Expected 1 input, got %d", len(ns.Inputs))
	}
	if len(ns.Outputs) != 1 {
		t.Errorf("Expected 1 output, got %d", len(ns.Outputs))
	}
	if ns.Inputs[0].Type != "Input" {
		t.Errorf("Expected Input queue type, got %s", ns.Inputs[0].Type)
	}
	if ns.Inputs[0].Capacity != 16 {
		t.Errorf("Expected Input capacity 16, got %d", ns.Inputs[0].Capacity)
	}

	// custom data check
	if val, ok := ns.CustomData["ProtocolState"]; !ok || val != "Idle" {
		t.Errorf("Expected CustomData[ProtocolState] = Idle, got %v", val)
	}
}

func TestNode_ExportState_PacketContent(t *testing.T) {
	// Setup Node with specific packet in output queue
	n := node.NewBaseNode(102, &MockHandler{})
	oq := queue.NewOutputQueue(10, 1)
	n.AddOutputQueue(oq)

	// Inject packet
	pkt := packet.Packet{SourceID: 1, TargetID: 2, Payload: "Hello"}
	oq.InjectPackets(10, []packet.Packet{pkt})

	// Export
	cfg := state.ExportConfig{DetailLevel: state.DetailLevelFull}
	ns := n.ExportState(cfg)

	if len(ns.Outputs) < 1 {
		t.Fatal("No output queue exported")
	}
	qs := ns.Outputs[0]
	if len(qs.Packets) != 1 {
		t.Errorf("Expected 1 packet in output queue, got %d", len(qs.Packets))
	} else {
		p := qs.Packets[0]
		if p.Msg != "Hello" {
			t.Errorf("Expected packet msg 'Hello', got '%s'", p.Msg)
		}
		if p.Cycle != 10 {
			t.Errorf("Expected packet cycle 10, got %d", p.Cycle)
		}
	}
}
