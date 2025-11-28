package node

import (
	"context"
	"testing"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/capability/cache"
	"github.com/Readm/flow_sim/internal/core/pipeline"
	"github.com/Readm/flow_sim/internal/dataflow"
	"github.com/Readm/flow_sim/internal/dataflow/message"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
	"github.com/Readm/flow_sim/internal/dataflow/transaction"
)

type mockPipeline struct {
	id       int
	channel  dataflow.Channel
	injected [][]packet.Packet

	processed []packet.Packet
}

func newMockPipeline(id int, ch dataflow.Channel) *mockPipeline {
	return &mockPipeline{
		id:      id,
		channel: ch,
	}
}

func (p *mockPipeline) ID() int                              { return p.id }
func (p *mockPipeline) Channel() dataflow.Channel            { return p.channel }
func (p *mockPipeline) SetChannel(ch dataflow.Channel)       { p.channel = ch }
func (p *mockPipeline) Tick(cycle int) error                 { return nil }
func (p *mockPipeline) InPort() ahead_port.AheadPort         { return nil }
func (p *mockPipeline) OutPort() ahead_port.AheadPort        { return nil }
func (p *mockPipeline) ProcessedCount() int                  { return len(p.processed) }
func (p *mockPipeline) SetOutPort(port ahead_port.AheadPort) {}
func (p *mockPipeline) GetProcessedPackets() []packet.Packet {
	pkts := p.processed
	p.processed = nil
	return pkts
}
func (p *mockPipeline) InjectPackets(cycle int, packets []packet.Packet) error {
	p.injected = append(p.injected, packets)
	return nil
}

type mockTxnProcessor struct {
	incoming [][]*message.Message
	ops      [][]transaction.Operation
	outgoing []*message.Message
}

func (m *mockTxnProcessor) Tick(cycle uint64, incoming []*message.Message) ([]*message.Message, []transaction.Operation, error) {
	m.incoming = append(m.incoming, incoming)
	var operations []transaction.Operation
	if len(m.ops) > 0 {
		operations = m.ops[0]
		m.ops = m.ops[1:]
	}
	return m.outgoing, operations, nil
}

func TestChannelNodeFlushesMessagesPerChannel(t *testing.T) {
	reqPipeline := newMockPipeline(0, dataflow.ChannelREQ)
	rspPipeline := newMockPipeline(1, dataflow.ChannelRSP)

	node := &ChannelNode{
		id:        1,
		pipelines: map[dataflow.Channel]pipeline.Pipeline{},
		outgoing:  make(map[dataflow.Channel][]*message.Message),
		processor: nil,
	}
	node.addPipeline(dataflow.ChannelREQ, reqPipeline)
	node.addPipeline(dataflow.ChannelRSP, rspPipeline)

	// Enqueue outgoing messages for both channels
	node.EnqueueMessage(&message.Message{
		ID:           dataflow.MessageID{NodeID: 1, MessageID: 1},
		Channel:      dataflow.ChannelREQ,
		Type:         1,
		SourceNodeID: 1,
		TargetNodeID: 2,
		Payload:      "req",
	})
	node.EnqueueMessage(&message.Message{
		ID:           dataflow.MessageID{NodeID: 1, MessageID: 2},
		Channel:      dataflow.ChannelRSP,
		Type:         2,
		SourceNodeID: 2,
		TargetNodeID: 1,
		Payload:      "rsp",
	})

	if err := node.flushOutgoing(0); err != nil {
		t.Fatalf("flushOutgoing failed: %v", err)
	}

	if len(reqPipeline.injected) != 1 || len(reqPipeline.injected[0]) != 1 {
		t.Fatalf("expected req pipeline to receive 1 packet, got %+v", reqPipeline.injected)
	}
	if len(rspPipeline.injected) != 1 || len(rspPipeline.injected[0]) != 1 {
		t.Fatalf("expected rsp pipeline to receive 1 packet, got %+v", rspPipeline.injected)
	}

	if reqPipeline.injected[0][0].Channel != dataflow.ChannelREQ {
		t.Fatalf("expected req packet channel REQ, got %s", reqPipeline.injected[0][0].Channel)
	}
	if rspPipeline.injected[0][0].Channel != dataflow.ChannelRSP {
		t.Fatalf("expected rsp packet channel RSP, got %s", rspPipeline.injected[0][0].Channel)
	}
}

func TestChannelNodeCollectsIncomingMessages(t *testing.T) {
	reqPipeline := newMockPipeline(0, dataflow.ChannelREQ)
	reqPipeline.processed = []packet.Packet{{
		SourceID:      1,
		TargetID:      2,
		Payload:       "req",
		Channel:       dataflow.ChannelREQ,
		Type:          1,
		TransactionID: dataflow.TransactionID{NodeID: 1, TxnID: 1},
		MessageID:     dataflow.MessageID{NodeID: 1, MessageID: 10},
	}}

	mockProcessor := &mockTxnProcessor{
		outgoing: []*message.Message{
			{
				ID:           dataflow.MessageID{NodeID: 2, MessageID: 20},
				Channel:      dataflow.ChannelRSP,
				Type:         2,
				SourceNodeID: 2,
				TargetNodeID: 1,
				Payload:      "rsp",
			},
		},
	}
	cacheStore := cache.NewFullyAssociativeCache(4)
	mockProcessor.ops = append(mockProcessor.ops, []transaction.Operation{
		&transaction.CacheOperation{
			Addr:     transaction.Addr(0x2000),
			NewState: cache.StateShared,
			Data:     []byte{0xaa},
		},
	})

	rspPipeline := newMockPipeline(1, dataflow.ChannelRSP)

	node := &ChannelNode{
		id:        2,
		pipelines: map[dataflow.Channel]pipeline.Pipeline{},
		outgoing:  make(map[dataflow.Channel][]*message.Message),
		processor: mockProcessor,
	}
	node.addPipeline(dataflow.ChannelREQ, reqPipeline)
	node.addPipeline(dataflow.ChannelRSP, rspPipeline)
	node.SetCapabilities(cacheStore, nil)

	if err := node.Tick(nil, 0, 0); err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	if len(mockProcessor.incoming) != 1 {
		t.Fatalf("expected txn processor to receive incoming messages once, got %d", len(mockProcessor.incoming))
	}
	if len(mockProcessor.incoming[0]) != 1 {
		t.Fatalf("expected 1 incoming message, got %d", len(mockProcessor.incoming[0]))
	}

	// Outgoing response should have been enqueued on RSP pipeline
	if len(node.outgoing[dataflow.ChannelRSP]) != 0 {
		t.Fatalf("outgoing queue should be flushed after tick")
	}
	if cacheStore.GetState(uint64(transaction.Addr(0x2000))) != cache.StateShared {
		t.Fatalf("expected cache state updated via operations")
	}
}

type mockIncentiveHook struct {
	should bool
	calls  int
}

func (m *mockIncentiveHook) ShouldCreateTransaction(nodeID int, cycle uint64) bool {
	return m.should
}

func (m *mockIncentiveHook) CreateTransaction(ctx context.Context, nodeID int, cycle uint64) error {
	m.calls++
	return nil
}

func TestChannelNodeIncentiveHook(t *testing.T) {
	reqPipeline := newMockPipeline(0, dataflow.ChannelREQ)
	node := &ChannelNode{
		id:        3,
		pipelines: map[dataflow.Channel]pipeline.Pipeline{},
		outgoing:  make(map[dataflow.Channel][]*message.Message),
		processor: &mockTxnProcessor{},
	}
	node.addPipeline(dataflow.ChannelREQ, reqPipeline)

	h := &mockIncentiveHook{should: true}
	node.SetIncentiveHook(h)

	if err := node.Tick(context.Background(), 0, 0); err != nil {
		t.Fatalf("Tick failed: %v", err)
	}
	if h.calls != 1 {
		t.Fatalf("expected incentive hook to be invoked once, got %d", h.calls)
	}
}
