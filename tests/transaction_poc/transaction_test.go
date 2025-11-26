package transaction_poc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/pipeline"
	"github.com/Readm/flow_sim/internal/dataflow"
	"github.com/Readm/flow_sim/internal/dataflow/message"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
	"github.com/Readm/flow_sim/internal/dataflow/transaction"
)

// Message types for Ping/Pong
const (
	MsgPing = 1
	MsgPong = 2
)

// PingNode implements node.Node and starts a Ping transaction.
type PingNode struct {
	id           int
	targetID     int
	flow         pipeline.Pipeline
	txnMgr       *transaction.TxnManager
	pingTxnID    dataflow.TransactionID
	pingStarted  bool
	pingComplete bool
	mu           sync.Mutex
}

// NewPingNode creates a new PingNode.
func NewPingNode(id int, targetID int) *PingNode {
	flow := pipeline.NewFIFO(id, 8)
	nodeCtx := &simpleNodeCtx{}
	txnMgr := transaction.NewTxnManager(id, nodeCtx)

	return &PingNode{
		id:       id,
		targetID: targetID,
		flow:     flow,
		txnMgr:   txnMgr,
	}
}

func (n *PingNode) ID() int {
	return n.id
}

func (n *PingNode) Flows() []pipeline.Pipeline {
	return []pipeline.Pipeline{n.flow}
}

func (n *PingNode) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
	// Start ping transaction on first cycle
	n.mu.Lock()
	if !n.pingStarted && cycle == 0 {
		n.pingStarted = true
		n.pingTxnID = n.txnMgr.Start(ctx, func(txCtx *transaction.TxnContext) {
			pingMsg := &message.Message{
				ID: dataflow.MessageID{
					NodeID:    n.id,
					MessageID: 1,
				},
				TransactionID: txCtx.TxnID(),
				Type:          MsgPing,
				SourceNodeID:  n.id,
				TargetNodeID:  n.targetID,
				Payload:       "ping",
				CreatedCycle:  cycle,
			}

			// Send ping
			if err := txCtx.Send(pingMsg); err != nil {
				return
			}

			// Wait for pong
			reply, err := txCtx.Yield(&transaction.YieldCommand{
				Type: transaction.YieldTypeWaitForMessage,
				WaitFor: &transaction.WaitForMessage{
					Type: MsgPong,
				},
				Timeout: 100 * time.Millisecond,
			})
			if err != nil {
				return
			}

			_ = reply // Process pong reply
			n.mu.Lock()
			n.pingComplete = true
			n.mu.Unlock()

			txCtx.Complete(nil)
		})
	}
	n.mu.Unlock()

	// Step 1: Process pipeline first - this drains in_queue and processes packets
	if err := n.flow.ProcessCycle(int(cycle)); err != nil {
		return err
	}

	// Step 2: Get processed packets from pipeline and convert to messages
	incomingMsgs := n.receiveMessages(cycle)

	// Step 3: Process transactions with incoming messages
	outgoingMsgs, err := n.txnMgr.Tick(cycle, incomingMsgs)
	if err != nil {
		return err
	}

	// Step 4: Send outgoing messages through pipeline
	n.sendMessages(cycle, outgoingMsgs)

	return nil
}

func (n *PingNode) receiveMessages(cycle uint64) []*message.Message {
	var msgs []*message.Message
	// Get processed packets from pipeline (processed in ProcessCycle)
	processedPackets := n.flow.GetProcessedPackets()
	for _, pkt := range processedPackets {
		// Convert packet to message
		msg := &message.Message{
			ID:            pkt.MessageID,
			TransactionID: pkt.TransactionID,
			Type:          n.parseMessageType(pkt.Payload),
			SourceNodeID:  pkt.SourceID,
			TargetNodeID:  pkt.TargetID,
			Payload:       pkt.Payload,
			CreatedCycle:  cycle,
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func (n *PingNode) sendMessages(cycle uint64, msgs []*message.Message) {
	if len(msgs) == 0 {
		return
	}

	// Convert messages to packets
	packets := make([]packet.Packet, 0, len(msgs))
	for _, msg := range msgs {
		payload := ""
		if msg.Payload != nil {
			payload = msg.Payload.(string)
		}
		pkt := packet.Packet{
			SourceID:      msg.SourceNodeID,
			TargetID:      msg.TargetNodeID,
			Payload:       payload,
			TransactionID: msg.TransactionID,
			MessageID:     msg.ID,
		}
		packets = append(packets, pkt)
	}

	// Inject packets into pipeline's out_queue
	// They will be sent through OutPort in the next ProcessCycle call
	n.flow.InjectPackets(int(cycle), packets)
}

func (n *PingNode) parseMessageType(payload string) int {
	if payload == "ping" {
		return MsgPing
	}
	if payload == "pong" {
		return MsgPong
	}
	return 0
}

func (n *PingNode) IsPingComplete() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.pingComplete
}

// PongNode implements node.Node and replies to Ping messages.
type PongNode struct {
	id     int
	flow   pipeline.Pipeline
	txnMgr *transaction.TxnManager
	mu     sync.Mutex
	pings  []*message.Message
}

// NewPongNode creates a new PongNode.
func NewPongNode(id int) *PongNode {
	flow := pipeline.NewFIFO(id, 8)
	nodeCtx := &simpleNodeCtx{}
	txnMgr := transaction.NewTxnManager(id, nodeCtx)

	return &PongNode{
		id:     id,
		flow:   flow,
		txnMgr: txnMgr,
		pings:  make([]*message.Message, 0),
	}
}

func (n *PongNode) ID() int {
	return n.id
}

func (n *PongNode) Flows() []pipeline.Pipeline {
	return []pipeline.Pipeline{n.flow}
}

func (n *PongNode) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
	// Step 1: Process pipeline first - this drains in_queue and processes packets
	if err := n.flow.ProcessCycle(int(cycle)); err != nil {
		return err
	}

	// Step 2: Get processed packets from pipeline and convert to messages
	incomingMsgs := n.receiveMessages(cycle)

	// Step 3: Handle ping messages - create pong replies
	var pongReplies []*message.Message
	for _, msg := range incomingMsgs {
		if msg.Type == MsgPing {
			// Reply with pong
			pongMsg := &message.Message{
				ID: dataflow.MessageID{
					NodeID:    n.id,
					MessageID: len(pongReplies) + 1,
				},
				TransactionID: msg.TransactionID,
				Type:          MsgPong,
				SourceNodeID:  n.id,
				TargetNodeID:  msg.SourceNodeID,
				Payload:       "pong",
				CreatedCycle:  cycle,
			}
			pongReplies = append(pongReplies, pongMsg)
		}
	}

	// Step 4: Process transactions (for pong transaction if needed)
	outgoingMsgs, err := n.txnMgr.Tick(cycle, incomingMsgs)
	if err != nil {
		return err
	}

	// Step 5: Add pong replies to outgoing messages
	outgoingMsgs = append(outgoingMsgs, pongReplies...)

	// Step 6: Send outgoing messages through pipeline
	n.sendMessages(cycle, outgoingMsgs)

	return nil
}

func (n *PongNode) receiveMessages(cycle uint64) []*message.Message {
	var msgs []*message.Message
	// Get processed packets from pipeline (processed in ProcessCycle)
	processedPackets := n.flow.GetProcessedPackets()
	for _, pkt := range processedPackets {
		// Convert packet to message
		msg := &message.Message{
			ID:            pkt.MessageID,
			TransactionID: pkt.TransactionID,
			Type:          n.parseMessageType(pkt.Payload),
			SourceNodeID:  pkt.SourceID,
			TargetNodeID:  pkt.TargetID,
			Payload:       pkt.Payload,
			CreatedCycle:  cycle,
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func (n *PongNode) sendMessages(cycle uint64, msgs []*message.Message) {
	if len(msgs) == 0 {
		return
	}

	// Convert messages to packets
	packets := make([]packet.Packet, 0, len(msgs))
	for _, msg := range msgs {
		payload := ""
		if msg.Payload != nil {
			payload = msg.Payload.(string)
		}
		pkt := packet.Packet{
			SourceID:      msg.SourceNodeID,
			TargetID:      msg.TargetNodeID,
			Payload:       payload,
			TransactionID: msg.TransactionID,
			MessageID:     msg.ID,
		}
		packets = append(packets, pkt)
	}

	// Inject packets into pipeline's out_queue
	// They will be sent through OutPort in the next ProcessCycle call
	n.flow.InjectPackets(int(cycle), packets)
}

func (n *PongNode) parseMessageType(payload string) int {
	if payload == "ping" {
		return MsgPing
	}
	if payload == "pong" {
		return MsgPong
	}
	return 0
}

// simpleNodeCtx is a simple implementation of NodeCtx for testing.
type simpleNodeCtx struct{}

func (c *simpleNodeCtx) GetCacheState(addr transaction.Addr) string {
	return "Invalid"
}

func (c *simpleNodeCtx) ReadCache(addr transaction.Addr) []byte {
	return nil
}

func (c *simpleNodeCtx) UpdateCache(addr transaction.Addr, state string, data []byte) {
	// No-op for testing
}

// TestPingPongTransaction tests the basic ping/pong transaction flow.
func TestPingPongTransaction(t *testing.T) {
	const (
		pingNodeID = 0
		pongNodeID = 1
		cycles     = 20
	)

	// Create nodes
	pingNode := NewPingNode(pingNodeID, pongNodeID)
	pongNode := NewPongNode(pongNodeID)

	// Create output ports
	pingOutPort := ahead_port.NewAheadPort(8)
	pongOutPort := ahead_port.NewAheadPort(8)

	// Connect flows to output ports
	pingNode.Flows()[0].SetOutPort(pingOutPort)
	pongNode.Flows()[0].SetOutPort(pongOutPort)

	// Create links
	linkPingToPong := link.NewLink(pingNodeID, pongNodeID, pingOutPort, pongNode.Flows()[0].InPort(), 1, 1)
	linkPongToPing := link.NewLink(pongNodeID, pingNodeID, pongOutPort, pingNode.Flows()[0].InPort(), 1, 1)

	// Initialize ready states
	if pingInPortImpl, ok := pingNode.Flows()[0].InPort().(*ahead_port.SinglePort); ok {
		pingInPortImpl.SetReadyUntil(cycles + 10)
	}
	if pongInPortImpl, ok := pongNode.Flows()[0].InPort().(*ahead_port.SinglePort); ok {
		pongInPortImpl.SetReadyUntil(cycles + 10)
	}
	pingOutPort.SetReadyUntil(cycles + 10)
	pongOutPort.SetReadyUntil(cycles + 10)

	// Initialize upstream Done
	pingNode.Flows()[0].InPort().SetDone(-1)
	pongNode.Flows()[0].InPort().SetDone(-1)
	pingOutPort.SetDone(-1)
	pongOutPort.SetDone(-1)

	// Create network
	graph := map[int][]*link.Link{
		pingNodeID: {linkPingToPong},
		pongNodeID: {linkPongToPing},
	}
	nodes := []node.Node{pingNode, pongNode}
	manager, err := network.NewManager(nodes, graph)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	// Run network
	ctx := context.Background()
	if err := manager.Run(ctx, cycles); err != nil {
		t.Fatalf("run manager: %v", err)
	}

	// Wait a bit for transaction to complete
	time.Sleep(50 * time.Millisecond)

	// Verify ping transaction
	pingNode.mu.Lock()
	txnID := pingNode.pingTxnID
	pingStarted := pingNode.pingStarted
	pingNode.mu.Unlock()

	if !pingStarted {
		t.Fatal("ping transaction was not started")
	}

	// Check transaction state
	txn := pingNode.txnMgr.GetTransaction(txnID)
	if txn == nil {
		// Transaction may have completed and been cleaned up
		// Check if ping is complete instead
		if pingNode.IsPingComplete() {
			t.Log("ping transaction completed successfully")
			return
		}
		t.Fatal("ping transaction not found and not completed")
	}

	if txn.State != transaction.TransactionStateCompleted {
		t.Logf("ping transaction state: %s (expected Completed)", txn.State)
		// Check if ping is complete as alternative
		if pingNode.IsPingComplete() {
			t.Log("ping transaction completed (via pingComplete flag)")
			return
		}
	}

	// Final check
	if !pingNode.IsPingComplete() && txn.State != transaction.TransactionStateCompleted {
		t.Errorf("ping transaction did not complete: state=%s, pingComplete=%v", txn.State, pingNode.IsPingComplete())
	}
}

