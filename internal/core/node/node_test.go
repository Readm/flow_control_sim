package node

import (
	"context"
	"errors"
	"testing"

	"github.com/Readm/flow_sim/internal/core/capability/cache"
	"github.com/Readm/flow_sim/internal/core/capability/directory"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

func TestNodeCollectsPacketsAndUpdatesBuffer(t *testing.T) {
	t.Parallel()

	iq1 := &mockInputQueue{
		picks: [][]packet.Packet{
			{{Payload: "a"}, {Payload: "b"}},
		},
	}
	iq2 := &mockInputQueue{
		picks: [][]packet.Packet{
			{{Payload: "c"}},
		},
	}
	oq := &mockOutputQueue{}

	n := New(10)
	if err := n.AddInputQueue(iq1); err != nil {
		t.Fatalf("AddInputQueue: %v", err)
	}
	if err := n.AddInputQueue(iq2); err != nil {
		t.Fatalf("AddInputQueue: %v", err)
	}
	if err := n.AddOutputQueue(oq); err != nil {
		t.Fatalf("AddOutputQueue: %v", err)
	}

	if err := n.Tick(context.Background(), 1, 0); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	buf := n.ProcessBuffer()
	if len(buf) != 3 {
		t.Fatalf("expected 3 packets in buffer, got %d", len(buf))
	}
	want := []string{"a", "b", "c"}
	for i, pkt := range buf {
		if string(pkt.Payload) != want[i] {
			t.Fatalf("buffer[%d] = %s, want %s", i, pkt.Payload, want[i])
		}
	}

	if iq1.tickCount != 1 || iq2.tickCount != 1 {
		t.Fatalf("input queues not ticked: %d %d", iq1.tickCount, iq2.tickCount)
	}
	if oq.tickCount != 1 {
		t.Fatalf("output queue not ticked: %d", oq.tickCount)
	}
}

func TestNodeProcessHookCanMutateBuffer(t *testing.T) {
	t.Parallel()

	iq := &mockInputQueue{
		picks: [][]packet.Packet{
			{{Payload: "payload"}, {Payload: "other"}},
		},
	}

	n := New(5)
	if err := n.AddInputQueue(iq); err != nil {
		t.Fatalf("AddInputQueue: %v", err)
	}

	n.SetProcessHook(func(_ context.Context, _ uint64, buf []packet.Packet) ([]packet.Packet, error) {
		return []packet.Packet{{Payload: "hooked"}}, nil
	})

	if err := n.Tick(context.Background(), 2, 0); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	buf := n.ProcessBuffer()
	if len(buf) != 1 || string(buf[0].Payload) != "hooked" {
		t.Fatalf("unexpected buffer after hook: %#v", buf)
	}
}

func TestNodeProcessHookErrorStopsTick(t *testing.T) {
	t.Parallel()

	n := New(3)
	errHook := errors.New("boom")
	n.SetProcessHook(func(_ context.Context, _ uint64, _ []packet.Packet) ([]packet.Packet, error) {
		return nil, errHook
	})

	if err := n.Tick(context.Background(), 0, 0); !errors.Is(err, errHook) {
		t.Fatalf("expected hook error %v, got %v", errHook, err)
	}
}

func TestNodeProcessBufferIsolatedFromCallers(t *testing.T) {
	t.Parallel()

	iq := &mockInputQueue{
		picks: [][]packet.Packet{
			{{Payload: "immutable"}},
		},
	}
	n := New(7)
	_ = n.AddInputQueue(iq)

	if err := n.Tick(context.Background(), 1, 0); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	buf := n.ProcessBuffer()
	buf[0].Payload = "mutated"

	buf2 := n.ProcessBuffer()
	if string(buf2[0].Payload) != "immutable" {
		t.Fatalf("ProcessBuffer should return copy, got %s", buf2[0].Payload)
	}
}

func TestNodeCachesAndDirectories(t *testing.T) {
	t.Parallel()

	n := New(9)
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

	n := New(11)
	_ = n.AddInputQueue(&mockInputQueue{tickErr: iqErr})
	_ = n.AddOutputQueue(&mockOutputQueue{tickErr: oqErr})

	err := n.Tick(context.Background(), 0, 0)
	if !errors.Is(err, iqErr) && !errors.Is(err, oqErr) {
		t.Fatalf("expected queue error, got %v", err)
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

func (m *mockOutputQueue) Length() int   { return 0 }
func (m *mockOutputQueue) Capacity() int { return 0 }
func (m *mockOutputQueue) IsFull() bool  { return false }

type fakeCache struct{ cache.Cache }

type fakeDirectory struct{ directory.Directory }

func TestNodeDataMap(t *testing.T) {
	t.Parallel()

	n := New(1)

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

	// Test GetAllData
	allData := n.GetAllData()
	if len(allData) != 3 {
		t.Errorf("expected 3 keys, got %d", len(allData))
	}
	if allData["key1"].(int) != 123 {
		t.Error("key1 value mismatch")
	}
	if allData["key2"].(bool) != true {
		t.Error("key2 value mismatch")
	}

	// Test that GetAllData returns a copy (isolation)
	allData["key1"] = 999
	if n.GetData("key1").(int) != 123 {
		t.Error("GetAllData should return a copy, not reference")
	}
}

func TestNodeDataMap_DifferentTypes(t *testing.T) {
	t.Parallel()

	n := New(2)

	// Test storing different types
	type CustomStruct struct {
		Field1 string
		Field2 int
	}

	n.SetData("string", "value")
	n.SetData("int", 42)
	n.SetData("bool", false)
	n.SetData("slice", []int{1, 2, 3})
	n.SetData("map", map[string]int{"a": 1})
	n.SetData("struct", CustomStruct{"test", 100})

	// Verify all types can be retrieved
	if n.GetData("string").(string) != "value" {
		t.Error("string type mismatch")
	}
	if n.GetData("int").(int) != 42 {
		t.Error("int type mismatch")
	}
	if n.GetData("bool").(bool) != false {
		t.Error("bool type mismatch")
	}
	if len(n.GetData("slice").([]int)) != 3 {
		t.Error("slice type mismatch")
	}
	if n.GetData("map").(map[string]int)["a"] != 1 {
		t.Error("map type mismatch")
	}
	cs := n.GetData("struct").(CustomStruct)
	if cs.Field1 != "test" || cs.Field2 != 100 {
		t.Error("struct type mismatch")
	}
}

func TestNodeDataMap_DeleteNonexistent(t *testing.T) {
	t.Parallel()

	n := New(3)

	// Deleting nonexistent key should not panic
	n.DeleteData("nonexistent")

	// Multiple deletes should be safe
	n.SetData("key", "value")
	n.DeleteData("key")
	n.DeleteData("key")

	if n.HasData("key") {
		t.Error("key should not exist after delete")
	}
}
