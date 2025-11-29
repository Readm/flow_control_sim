package node

import (
	"context"
	"testing"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

func TestFIFONodeForwardsOnePacketPerCycle(t *testing.T) {
	input := &fifoMockInput{
		picks: [][]packet.Packet{
			{{Payload: "p1"}, {Payload: "p2"}},
		},
	}
	output := &fifoMockOutput{}

	f := NewFIFONode(1, input, output)

	if err := f.Tick(context.Background(), 0); err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	if output.injectCount != 1 {
		t.Fatalf("expected 1 injection, got %d", output.injectCount)
	}
	if len(output.injected) != 1 || string(output.injected[0].Payload) != "p1" {
		t.Fatalf("output packets = %#v", output.injected)
	}

	buf := f.ProcessBuffer()
	if len(buf) != 1 || string(buf[0].Payload) != "p1" {
		t.Fatalf("ProcessBuffer = %#v", buf)
	}
}

func TestFIFONodeNoPackets(t *testing.T) {
	input := &fifoMockInput{}
	output := &fifoMockOutput{}

	f := NewFIFONode(2, input, output)

	if err := f.Tick(context.Background(), 0); err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	if output.injectCount != 0 {
		t.Fatalf("expected no injections, got %d", output.injectCount)
	}
	if len(f.ProcessBuffer()) != 0 {
		t.Fatalf("expected empty process buffer")
	}
}

type fifoMockInput struct {
	picks     [][]packet.Packet
	tickCount int
	tickErr   error
}

func (m *fifoMockInput) Pick() []packet.Packet {
	if len(m.picks) == 0 {
		return nil
	}
	pkt := m.picks[0]
	m.picks = m.picks[1:]
	return pkt
}

func (m *fifoMockInput) Tick(int) error {
	m.tickCount++
	return m.tickErr
}

func (m *fifoMockInput) Length() int   { return len(m.picks) }
func (m *fifoMockInput) Capacity() int { return 32 }
func (m *fifoMockInput) IsFull() bool  { return false }

type fifoMockOutput struct {
	injected    []packet.Packet
	injectCount int
	tickCount   int
	injectErr   error
	tickErr     error
}

func (m *fifoMockOutput) InjectPackets(_ int, packets []packet.Packet) error {
	m.injectCount++
	m.injected = append(m.injected, packets...)
	return m.injectErr
}

func (m *fifoMockOutput) Tick(int) error {
	m.tickCount++
	return m.tickErr
}

func (m *fifoMockOutput) Length() int   { return 0 }
func (m *fifoMockOutput) Capacity() int { return 0 }
func (m *fifoMockOutput) IsFull() bool  { return false }
