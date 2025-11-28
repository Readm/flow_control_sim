package node

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Readm/flow_sim/internal/core/capability/cache"
	"github.com/Readm/flow_sim/internal/core/capability/directory"
	"github.com/Readm/flow_sim/internal/core/pipeline"
	"github.com/Readm/flow_sim/internal/dataflow"
	"github.com/Readm/flow_sim/internal/dataflow/chi"
	"github.com/Readm/flow_sim/internal/dataflow/message"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
	"github.com/Readm/flow_sim/internal/dataflow/transaction"
	"github.com/Readm/flow_sim/pkg/hook"
)

// txnProcessor abstracts transaction processing so ChannelNode can be tested with mocks.
type txnProcessor interface {
	Tick(cycle uint64, incoming []*message.Message) ([]*message.Message, []transaction.Operation, error)
}

// txnManagerAdapter adapts TxnManager to txnProcessor.
type txnManagerAdapter struct {
	mgr *transaction.TxnManager
}

func (a *txnManagerAdapter) Tick(cycle uint64, incoming []*message.Message) ([]*message.Message, []transaction.Operation, error) {
	if a == nil || a.mgr == nil {
		return nil, nil, nil
	}
	return a.mgr.Tick(cycle, incoming)
}

// ChannelNode is a Node implementation that routes messages per-channel through dedicated pipelines.
type ChannelNode struct {
	id        int
	pipelines map[dataflow.Channel]pipeline.Pipeline
	flows     []pipeline.Pipeline

	outgoing map[dataflow.Channel][]*message.Message

	processor txnProcessor

	cacheStore     cache.Cache
	directoryStore directory.Directory
	pendingOps     []transaction.Operation

	incentive hook.IncentiveHook
}

// NewChannelNode creates a new ChannelNode with the provided pipelines (one per channel) and TxnManager.
func NewChannelNode(id int, txnMgr *transaction.TxnManager, pipelines map[dataflow.Channel]pipeline.Pipeline) *ChannelNode {
	node := &ChannelNode{
		id:        id,
		pipelines: make(map[dataflow.Channel]pipeline.Pipeline),
		outgoing:  make(map[dataflow.Channel][]*message.Message),
		processor: &txnManagerAdapter{mgr: txnMgr},
	}

	for ch, p := range pipelines {
		node.addPipeline(ch, p)
	}
	return node
}

// addPipeline registers a pipeline for a specific channel.
func (n *ChannelNode) addPipeline(channel dataflow.Channel, p pipeline.Pipeline) {
	if p == nil {
		return
	}
	p.SetChannel(channel)
	n.pipelines[channel] = p
	n.flows = append(n.flows, p)
	if _, ok := n.outgoing[channel]; !ok {
		n.outgoing[channel] = make([]*message.Message, 0)
	}
}

// ID implements node.Node.
func (n *ChannelNode) ID() int {
	return n.id
}

// Flows implements node.Node.
func (n *ChannelNode) Flows() []pipeline.Pipeline {
	return n.flows
}

// Tick implements node.Node. It processes pipelines, dispatches messages to the TxnManager,
// and flushes outgoing messages to pipelines per channel.
func (n *ChannelNode) Tick(ctx context.Context, cycle uint64, linkDelay time.Duration) error {
	var incoming []*message.Message

	// Step 0: Incentive hook to create new transactions.
	if n.incentive != nil {
		hookCtx := ctx
		if hookCtx == nil {
			hookCtx = context.Background()
		}
		if n.incentive.ShouldCreateTransaction(n.id, cycle) {
			if err := n.incentive.CreateTransaction(hookCtx, n.id, cycle); err != nil {
				return err
			}
		}
	}

	// Step 1: Process pipelines and collect incoming packets/messages.
	for ch, p := range n.pipelines {
		if err := p.Tick(int(cycle)); err != nil {
			return err
		}
		packets := p.GetProcessedPackets()
		for _, pkt := range packets {
			msg := n.packetToMessage(pkt)
			if msg.Channel == "" {
				msg.Channel = ch
			}
			incoming = append(incoming, msg)
		}
	}

	// Step 2: Dispatch to transaction processor.
	var outgoing []*message.Message
	var ops []transaction.Operation
	var err error
	if n.processor != nil {
		outgoing, ops, err = n.processor.Tick(cycle, incoming)
		if err != nil {
			return err
		}
	}

	// Step 3: Enqueue outgoing messages per channel.
	for _, msg := range outgoing {
		n.enqueueOutgoing(msg)
	}

	// Step 4: Flush per-channel queues to pipelines.
	if err := n.flushOutgoing(int(cycle)); err != nil {
		return err
	}

	// Step 5: Apply capability operations.
	n.pendingOps = append(n.pendingOps, ops...)
	return n.applyOperations()
}

// EnqueueMessage allows external components to enqueue messages for sending.
func (n *ChannelNode) EnqueueMessage(msg *message.Message) {
	n.enqueueOutgoing(msg)
}

func (n *ChannelNode) enqueueOutgoing(msg *message.Message) {
	if msg == nil {
		return
	}
	channel := msg.Channel
	if channel == "" {
		channel = dataflow.ChannelREQ
	}
	queue := n.outgoing[channel]
	queue = append(queue, msg)
	n.outgoing[channel] = queue
}

func (n *ChannelNode) flushOutgoing(cycle int) error {
	for ch, queue := range n.outgoing {
		if len(queue) == 0 {
			continue
		}
		p, ok := n.pipelines[ch]
		if !ok {
			return fmt.Errorf("no pipeline registered for channel %s", ch)
		}
		packets := make([]packet.Packet, len(queue))
		for i, msg := range queue {
			packets[i] = n.messageToPacket(msg)
		}
		if err := p.InjectPackets(cycle, packets); err != nil {
			return err
		}
		n.outgoing[ch] = n.outgoing[ch][:0]
	}
	return nil
}

// SetCapabilities assigns the cache and directory instances used by this node.
func (n *ChannelNode) SetCapabilities(cache cache.Cache, directory directory.Directory) {
	n.cacheStore = cache
	n.directoryStore = directory
}

func (n *ChannelNode) applyOperations() error {
	if len(n.pendingOps) == 0 {
		return nil
	}
	exec := capabilityExecutor{
		cache:     n.cacheStore,
		directory: n.directoryStore,
	}
	for _, op := range n.pendingOps {
		if err := op.Apply(exec); err != nil {
			return err
		}
	}
	n.pendingOps = n.pendingOps[:0]
	return nil
}

func (n *ChannelNode) packetToMessage(pkt packet.Packet) *message.Message {
	channel := pkt.Channel
	if channel == "" {
		channel = dataflow.ChannelREQ
	}
	var payload interface{} = pkt.Payload
	if strings.HasPrefix(pkt.Payload, "CHI:") {
		var decoded chi.CHIPayload
		if err := json.Unmarshal([]byte(pkt.Payload[4:]), &decoded); err == nil {
			payload = &decoded
		}
	}
	return &message.Message{
		ID:            pkt.MessageID,
		TransactionID: pkt.TransactionID,
		Channel:       channel,
		Type:          pkt.Type,
		SourceNodeID:  pkt.SourceID,
		TargetNodeID:  pkt.TargetID,
		Payload:       payload,
	}
}

func (n *ChannelNode) messageToPacket(msg *message.Message) packet.Packet {
	channel := msg.Channel
	if channel == "" {
		channel = dataflow.ChannelREQ
	}
	return packet.Packet{
		SourceID:      msg.SourceNodeID,
		TargetID:      msg.TargetNodeID,
		Payload:       encodePayload(msg.Payload),
		Channel:       channel,
		Type:          msg.Type,
		TransactionID: msg.TransactionID,
		MessageID:     msg.ID,
	}
}

func encodePayload(payload interface{}) string {
	switch v := payload.(type) {
	case *chi.CHIPayload:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return "CHI:" + string(data)
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case []byte:
		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

type capabilityExecutor struct {
	cache     cache.Cache
	directory directory.Directory
}

func (c capabilityExecutor) Cache() cache.Cache {
	return c.cache
}

func (c capabilityExecutor) Directory() directory.Directory {
	return c.directory
}

// SetIncentiveHook assigns an IncentiveHook to the node.
func (n *ChannelNode) SetIncentiveHook(h hook.IncentiveHook) {
	n.incentive = h
}
