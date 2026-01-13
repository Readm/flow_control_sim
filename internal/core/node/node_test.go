package node

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/components/directory"
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestNode is a helper for testing BaseNode functionality.
type TestNode struct {
	*BaseNode
	processBuffer []packet.Packet
	bufferMu      sync.Mutex
	hook          func(cycle uint64, inputs [][]queue.PacketRef) error
}

func NewTestNode(id int) *TestNode {
	t := &TestNode{
		processBuffer: make([]packet.Packet, 0),
	}
	t.BaseNode = NewBaseNode(id, t)
	return t
}

func (t *TestNode) SetProcessHook(hook func(cycle uint64, inputs [][]queue.PacketRef) error) {
	t.hook = hook
}

func (t *TestNode) Process(cycle uint64, inputs [][]queue.PacketRef) error {
	t.bufferMu.Lock()
	defer t.bufferMu.Unlock()

	// Collect all inputs into buffer for inspection and consume them
	t.processBuffer = nil
	for _, q := range inputs {
		for _, ref := range q {
			t.processBuffer = append(t.processBuffer, ref.Packet)
			ref.Queue.Free(ref.Slot)
		}
	}

	if t.hook != nil {
		return t.hook(cycle, inputs)
	}
	return nil
}

func (t *TestNode) ProcessBuffer() []packet.Packet {
	t.bufferMu.Lock()
	defer t.bufferMu.Unlock()
	buf := make([]packet.Packet, len(t.processBuffer))
	copy(buf, t.processBuffer)
	return buf
}

func TestNodeCollectsPacketsAndUpdatesBuffer(t *testing.T) {
	t.Parallel()

	iq1 := &mockInputQueue{
		picks: [][]packet.Packet{
			{{Metadata: map[string]interface{}{"payload": "a"}}, {Metadata: map[string]interface{}{"payload": "b"}}},
		},
	}
	iq2 := &mockInputQueue{
		picks: [][]packet.Packet{
			{{Metadata: map[string]interface{}{"payload": "c"}}},
		},
	}
	oq := &mockOutputQueue{}

	n := NewTestNode(10)
	if err := n.AddInputQueue(iq1); err != nil {
		t.Fatalf("AddInputQueue: %v", err)
	}
	if err := n.AddInputQueue(iq2); err != nil {
		t.Fatalf("AddInputQueue: %v", err)
	}
	if err := n.AddOutputQueue(oq); err != nil {
		t.Fatalf("AddOutputQueue: %v", err)
	}

	if err := n.Tick(1, 0); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	buf := n.ProcessBuffer()
	if len(buf) != 3 {
		t.Fatalf("expected 3 packets in buffer, got %d", len(buf))
	}
	// Note: Order depends on tickInputQueues order (inputs slice order)
	// iq1 then iq2
	want := []string{"a", "b", "c"}
	for i, pkt := range buf {
		if pkt.Metadata["payload"].(string) != want[i] {
			t.Fatalf("buffer[%d] = %s, want %s", i, pkt.Metadata["payload"], want[i])
		}
	}

	// mockInputQueue checks
	if iq1.tickCount != 1 || iq2.tickCount != 1 {
		t.Fatalf("input queues not ticked: %d %d", iq1.tickCount, iq2.tickCount)
	}
}

func TestNodeProcessHookExecuted(t *testing.T) {
	t.Parallel()

	n := NewTestNode(5)
	executed := false
	n.SetProcessHook(func(_ uint64, _ [][]queue.PacketRef) error {
		executed = true
		return nil
	})

	if err := n.Tick(2, 0); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if !executed {
		t.Fatalf("ProcessHook should have been executed")
	}
}

func TestNodeProcessHookErrorStopsTick(t *testing.T) {
	t.Parallel()

	n := NewTestNode(3)
	errHook := errors.New("boom")
	n.SetProcessHook(func(_ uint64, _ [][]queue.PacketRef) error {
		return errHook
	})

	if err := n.Tick(0, 0); !errors.Is(err, errHook) {
		t.Fatalf("expected hook error %v, got %v", errHook, err)
	}
}

func TestNodeCachesAndDirectories(t *testing.T) {
	t.Parallel()

	n := NewTestNode(9)
	mockCache := &fakeCache{}
	mockDir := &fakeDirectory{}

	n.AddCache(mockCache)
	n.AddDirectory(mockDir)

	if len(n.Caches()) != 1 {
		t.Fatalf("expected 1 cache")
	}
	if len(n.Directories()) != 1 {
		t.Fatalf("expected 1 directory")
	}
}

func TestNodeTickPropagatesQueueErrors(t *testing.T) {
	t.Parallel()

	iqErr := errors.New("input error")
	oqErr := errors.New("output error")

	n := NewTestNode(11)
	_ = n.AddInputQueue(&mockInputQueue{tickErr: iqErr})

	err := n.Tick(0, 0)
	if !errors.Is(err, iqErr) {
		t.Fatalf("expected input error, got %v", err)
	}

	// Test output error
	n2 := NewTestNode(12)
	_ = n2.AddOutputQueue(&mockOutputQueue{tickErr: oqErr})

	err = n2.Tick(0, 0)
	if !errors.Is(err, oqErr) {
		t.Fatalf("expected output error, got %v", err)
	}
}

type mockInputQueue struct {
	picks     [][]packet.Packet
	tickCount int
	tickErr   error
}

func (m *mockInputQueue) Pick() []packet.Packet {
	if len(m.picks) == 0 {
		return nil
	}
	pkt := m.picks[0]
	m.picks = m.picks[1:]
	return pkt
}

func (m *mockInputQueue) PeekPickTo(out []queue.PacketRef) int {
	if len(m.picks) == 0 {
		return 0
	}
	packets := m.picks[0]
	count := 0
	for i, pkt := range packets {
		if count >= len(out) {
			break
		}
		out[count] = queue.PacketRef{
			Packet: pkt,
			Slot:   i,
			Queue:  m,
		}
		count++
	}
	return count
}

func (m *mockInputQueue) Free(slot int) {
	// No-op for mock, or we could track calls.
	// We rely on 'picks' managing available packets in Pick(),
	// but PeekPickTo doesn't advance.
	// However, TestNode calls Tick once. PickTo returns batch.
	// If we wanted rigorous correctness we'd need to track usage.
	// For this test, it's fine.
}

func (m *mockInputQueue) Tick(int) error {
	m.tickCount++
	return m.tickErr
}

func (m *mockInputQueue) Length() int   { return len(m.picks) }
func (m *mockInputQueue) Capacity() int { return 32 }
func (m *mockInputQueue) IsFull() bool  { return false }

type mockOutputQueue struct {
	tickCount int
	tickErr   error
	injected  []packet.Packet
}

func (m *mockOutputQueue) InjectPackets(_ int, packets []packet.Packet) error {
	m.injected = append(m.injected, packets...)
	return nil
}

func (m *mockOutputQueue) Tick(int) error {
	m.tickCount++
	return m.tickErr
}

func (m *mockOutputQueue) Length() int       { return 0 }
func (m *mockOutputQueue) Capacity() int     { return 0 }
func (m *mockOutputQueue) IsFull() bool      { return false }
func (m *mockOutputQueue) OutBandwidth() int { return 1 }

type fakeCache struct{ cache.Cache }

type fakeDirectory struct{ directory.Directory }

func TestNodeDataMap(t *testing.T) {
	t.Parallel()

	n := NewTestNode(1)

	// Test SetData and GetData
	n.SetData("test_key", "test_value")
	val := n.GetData("test_key")
	if val == nil {
		t.Fatal("expected non-nil value")
	}
	if str, ok := val.(string); !ok || str != "test_value" {
		t.Errorf("expected 'test_value', got %v", val)
	}

	// Test HasData
	if !n.HasData("test_key") {
		t.Error("expected HasData to return true")
	}
	if n.HasData("nonexistent_key") {
		t.Error("expected HasData to return false for nonexistent key")
	}

	// Test GetData for nonexistent key
	val = n.GetData("nonexistent_key")
	if val != nil {
		t.Errorf("expected nil for nonexistent key, got %v", val)
	}

	// Test overwriting existing key
	n.SetData("test_key", "new_value")
	val = n.GetData("test_key")
	if str, ok := val.(string); !ok || str != "new_value" {
		t.Errorf("expected 'new_value', got %v", val)
	}

	// Test DeleteData
	n.DeleteData("test_key")
	if n.HasData("test_key") {
		t.Error("expected HasData to return false after delete")
	}
	val = n.GetData("test_key")
	if val != nil {
		t.Errorf("expected nil after delete, got %v", val)
	}

	// Test multiple keys
	n.SetData("key1", 123)
	n.SetData("key2", true)
	n.SetData("key3", []byte{1, 2, 3})

	if !n.HasData("key1") || !n.HasData("key2") || !n.HasData("key3") {
		t.Error("expected all keys to be present")
	}
}

// Mock port for testing OutQueue behavior
type smartMockPort struct {
	mu      sync.Mutex
	history []struct {
		Cycle int
		Pkt   packet.Packet
	}
	readyMap map[int]bool
}

func newSmartMockPort() *smartMockPort {
	return &smartMockPort{
		readyMap: make(map[int]bool),
		history: make([]struct {
			Cycle int
			Pkt   packet.Packet
		}, 0),
	}
}

func (m *smartMockPort) TrySend(cycle int, pwc ahead_port.PacketWithCycle) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ready, ok := m.readyMap[cycle]; ok && !ready {
		return false
	}

	m.history = append(m.history, struct {
		Cycle int
		Pkt   packet.Packet
	}{cycle, pwc.Packet})
	return true
}

func (m *smartMockPort) MarkDone(cycle int)               {}
func (m *smartMockPort) PeekReady(cycle int) (bool, bool) { return true, true }
func (m *smartMockPort) IsReady(cycle int) bool           { return true }

func TestNode_OutQueueOptimization(t *testing.T) {
	t.Parallel()

	// Scenario 1: Optimization Triggered (Length >= Bandwidth)
	t.Run("OptimizationTriggered", func(t *testing.T) {
		// Node setup
		n := NewTestNode(100)

		// Create REAL OutputQueue properly configured
		// Capacity 20, Bandwidth 2
		oq := queue.NewOutputQueue(20, 2)
		mockPort := newSmartMockPort()
		oq.SetDownstreamPort(mockPort)

		n.AddOutputQueue(oq)

		// Inject 10 packets (Should take 5 ticks: 0, 1, 2, 3, 4)
		pkts := make([]packet.Packet, 10)
		for i := 0; i < 10; i++ {
			pkts[i] = CreatePacket(1, 2, fmt.Sprintf("p%d", i))
		}

		// Manually inject (bypassing Process for simplicity, simulating Process done)
		// Inject into OutQueue at cycle 0
		if err := oq.InjectPackets(0, pkts); err != nil {
			t.Fatalf("InjectPackets failed: %v", err)
		}

		// AdvanceTo target 10.
		// Current cycle is 0.
		// It should process cycle 0 (sends 2), then optimized ahead loop should run for 1, 2, 3, 4.
		// Since 10 packets / 2 BW = 5 cycles.
		// Cycle 0: 2 packets. Rem 8.
		// Cycle 1: 2 packets. Rem 6.
		// ...
		// Cycle 4: 2 packets. Rem 0.
		if err := n.AdvanceTo(10); err != nil {
			t.Fatalf("AdvanceTo failed: %v", err)
		}

		mockPort.mu.Lock()
		defer mockPort.mu.Unlock()

		if len(mockPort.history) != 10 {
			t.Errorf("Expected 10 packets, got %d", len(mockPort.history))
		}

		// Check cycles
		// Cycle 0: p0, p1
		// Cycle 1: p2, p3
		// ...
		cycleCounts := make(map[int]int)
		for _, item := range mockPort.history {
			cycleCounts[item.Cycle]++
		}

		for c := 0; c < 5; c++ {
			if count := cycleCounts[c]; count != 2 {
				t.Errorf("Cycle %d expected 2 packets, got %d", c, count)
			}
		}
	})

	// Scenario 2: Optimization Skipped (Length < Bandwidth)
	t.Run("OptimizationSkipped", func(t *testing.T) {
		n := NewTestNode(200)
		// High bandwidth: 10
		oq := queue.NewOutputQueue(20, 10)
		mockPort := newSmartMockPort()
		oq.SetDownstreamPort(mockPort)
		n.AddOutputQueue(oq)

		// Inject 5 packets (Less than BW)
		pkts := make([]packet.Packet, 5)
		for i := 0; i < 5; i++ {
			pkts[i] = CreatePacket(1, 2, fmt.Sprintf("p%d", i))
		}
		oq.InjectPackets(0, pkts)

		// Cycle 0: Sends 5. Rem 0.
		// AdvanceTo 5.
		// It should send all 5 in Cycle 0.
		// Cycle 1: Length 0 < BW 10. Stop optimization?
		// Actually if Length 0, Tick does nothing anyway.

		if err := n.AdvanceTo(5); err != nil {
			t.Fatalf("AdvanceTo failed: %v", err)
		}

		mockPort.mu.Lock()
		defer mockPort.mu.Unlock()

		if len(mockPort.history) != 5 {
			t.Errorf("Expected 5 packets, got %d", len(mockPort.history))
		}

		// All in cycle 0
		for _, item := range mockPort.history {
			if item.Cycle != 0 {
				t.Errorf("Packet sent at cycle %d, expected 0", item.Cycle)
			}
		}
	})
}

// TestPortNaming_SinglePort tests naming individual ports
func TestPortNaming_SinglePort(t *testing.T) {
	t.Parallel()

	n := NewTestNode(1)
	iq1 := queue.NewInputQueue(8, 1)
	iq2 := queue.NewInputQueue(8, 1)
	oq1 := queue.NewOutputQueue(8, 1)
	oq2 := queue.NewOutputQueue(8, 1)

	n.AddInputQueue(iq1)
	n.AddInputQueue(iq2)
	n.AddOutputQueue(oq1)
	n.AddOutputQueue(oq2)

	// Name input ports
	if err := n.NameInputPort(0, "from_cpu"); err != nil {
		t.Fatalf("NameInputPort(0, 'from_cpu') failed: %v", err)
	}
	if err := n.NameInputPort(1, "from_l2"); err != nil {
		t.Fatalf("NameInputPort(1, 'from_l2') failed: %v", err)
	}

	// Name output ports
	if err := n.NameOutputPort(0, "to_cpu"); err != nil {
		t.Fatalf("NameOutputPort(0, 'to_cpu') failed: %v", err)
	}
	if err := n.NameOutputPort(1, "to_l2"); err != nil {
		t.Fatalf("NameOutputPort(1, 'to_l2') failed: %v", err)
	}

	// Verify lookups
	if idx, ok := n.GetInputPortIndex("from_cpu"); !ok || idx != 0 {
		t.Errorf("GetInputPortIndex('from_cpu') = %d, %v; want 0, true", idx, ok)
	}
	if idx, ok := n.GetInputPortIndex("from_l2"); !ok || idx != 1 {
		t.Errorf("GetInputPortIndex('from_l2') = %d, %v; want 1, true", idx, ok)
	}
	if idx, ok := n.GetOutputPortIndex("to_cpu"); !ok || idx != 0 {
		t.Errorf("GetOutputPortIndex('to_cpu') = %d, %v; want 0, true", idx, ok)
	}
	if idx, ok := n.GetOutputPortIndex("to_l2"); !ok || idx != 1 {
		t.Errorf("GetOutputPortIndex('to_l2') = %d, %v; want 1, true", idx, ok)
	}

	// Lookup nonexistent name
	if idx, ok := n.GetInputPortIndex("nonexistent"); ok {
		t.Errorf("GetInputPortIndex('nonexistent') = %d, %v; want _, false", idx, ok)
	}
}

// TestPortNaming_DuplicateDetection tests that duplicate port names are rejected
func TestPortNaming_DuplicateDetection(t *testing.T) {
	t.Parallel()

	n := NewTestNode(2)
	iq1 := queue.NewInputQueue(8, 1)
	iq2 := queue.NewInputQueue(8, 1)
	n.AddInputQueue(iq1)
	n.AddInputQueue(iq2)

	// Name port 0
	if err := n.NameInputPort(0, "shared_name"); err != nil {
		t.Fatalf("NameInputPort(0, 'shared_name') failed: %v", err)
	}

	// Try to name port 1 with the same name - should fail
	err := n.NameInputPort(1, "shared_name")
	if err == nil {
		t.Fatal("NameInputPort(1, 'shared_name') should have failed with duplicate name error")
	}
	if err.Error() != `input port name "shared_name" already assigned to index 0` {
		t.Errorf("unexpected error: %v", err)
	}

	// Idempotent: renaming same port with same name should succeed
	if err := n.NameInputPort(0, "shared_name"); err != nil {
		t.Errorf("NameInputPort(0, 'shared_name') (idempotent) failed: %v", err)
	}
}

// TestPortNaming_BatchNaming tests NameInputPorts and NameOutputPorts
func TestPortNaming_BatchNaming(t *testing.T) {
	t.Parallel()

	n := NewTestNode(3)
	iq1 := queue.NewInputQueue(8, 1)
	iq2 := queue.NewInputQueue(8, 1)
	iq3 := queue.NewInputQueue(8, 1)
	oq1 := queue.NewOutputQueue(8, 1)
	oq2 := queue.NewOutputQueue(8, 1)

	n.AddInputQueue(iq1)
	n.AddInputQueue(iq2)
	n.AddInputQueue(iq3)
	n.AddOutputQueue(oq1)
	n.AddOutputQueue(oq2)

	// Batch name inputs (skip port 1 with empty string)
	if err := n.NameInputPorts("port0", "", "port2"); err != nil {
		t.Fatalf("NameInputPorts failed: %v", err)
	}

	// Verify
	if idx, ok := n.GetInputPortIndex("port0"); !ok || idx != 0 {
		t.Errorf("GetInputPortIndex('port0') = %d, %v; want 0, true", idx, ok)
	}
	if _, ok := n.GetInputPortIndex(""); ok {
		t.Error("GetInputPortIndex('') should return false")
	}
	if idx, ok := n.GetInputPortIndex("port2"); !ok || idx != 2 {
		t.Errorf("GetInputPortIndex('port2') = %d, %v; want 2, true", idx, ok)
	}

	// Batch name outputs
	if err := n.NameOutputPorts("out0", "out1"); err != nil {
		t.Fatalf("NameOutputPorts failed: %v", err)
	}

	// Verify
	if idx, ok := n.GetOutputPortIndex("out0"); !ok || idx != 0 {
		t.Errorf("GetOutputPortIndex('out0') = %d, %v; want 0, true", idx, ok)
	}
	if idx, ok := n.GetOutputPortIndex("out1"); !ok || idx != 1 {
		t.Errorf("GetOutputPortIndex('out1') = %d, %v; want 1, true", idx, ok)
	}
}

// TestPortNaming_ErrorCases tests various error conditions
func TestPortNaming_ErrorCases(t *testing.T) {
	t.Parallel()

	n := NewTestNode(4)
	iq := queue.NewInputQueue(8, 1)
	n.AddInputQueue(iq)

	// Out of range index
	err := n.NameInputPort(5, "invalid")
	if err == nil {
		t.Fatal("NameInputPort with out-of-range index should fail")
	}

	// Empty name
	err = n.NameInputPort(0, "")
	if err == nil {
		t.Fatal("NameInputPort with empty name should fail")
	}

	// Too many names in batch
	err = n.NameInputPorts("a", "b", "c")
	if err == nil {
		t.Fatal("NameInputPorts with too many names should fail")
	}
}
