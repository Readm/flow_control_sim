package transaction_poc

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/capability/cache"
	"github.com/Readm/flow_sim/internal/core/capability/directory"
	"github.com/Readm/flow_sim/internal/dataflow"
	"github.com/Readm/flow_sim/internal/dataflow/message"
	"github.com/Readm/flow_sim/internal/dataflow/transaction"
	"github.com/Readm/flow_sim/pkg/hook"
)

func TestCHIReadSharedTransaction(t *testing.T) {
	const (
		requesterID = 0
		homeID      = 1
		subID       = 2
		totalCycles = 20
	)

	addr := transaction.Addr(0x1000)
	expectedData := []byte("ReadySharedData")

	reqEp := newChiEndpoint(requesterID, false)
	homeEp := newChiEndpoint(homeID, true)
	subEp := newChiEndpoint(subID, false)

	var requesterDone, homeDone, subDone atomic.Bool

	// Seed subordinate cache with data and home directory owner info.
	subEp.caps.cache.SetData(uint64(addr), expectedData)
	homeEp.caps.directory.SetOwner(uint64(addr), subID)

	// Configure transaction hooks.
	configureRequesterHook(reqEp, homeID, addr, expectedData, &requesterDone)
	configureHomeResponderHook(homeEp, requesterID, subID, addr, &homeDone)
	configureSubordinateHook(subEp, homeID, addr, &subDone)
	if homeEp.hook == nil {
		t.Fatalf("home hook nil")
	}

	endpoints := []*chiEndpoint{reqEp, homeEp, subEp}
	if err := runChiSimulation(endpoints, totalCycles); err != nil {
		t.Fatalf("simulation failed: %v", err)
	}

	if reqEp.txnMgr.ActiveCount() == 0 && !requesterDone.Load() {
		t.Fatalf("requester transaction never started")
	}
	if homeEp.txnMgr.ActiveCount() == 0 && !homeDone.Load() {
		t.Fatalf("home transaction never started")
	}
	if subEp.txnMgr.ActiveCount() == 0 && !subDone.Load() {
		t.Fatalf("subordinate transaction never started")
	}

	if !requesterDone.Load() || !homeDone.Load() || !subDone.Load() {
		t.Fatalf("transactions incomplete: requester=%v home=%v sub=%v (active=%d,%d,%d)",
			requesterDone.Load(), homeDone.Load(), subDone.Load(),
			reqEp.txnMgr.ActiveCount(), homeEp.txnMgr.ActiveCount(), subEp.txnMgr.ActiveCount())
	}

	if state := reqEp.caps.cache.GetState(uint64(addr)); state != cache.StateShared {
		t.Fatalf("requester cache state = %s, want Shared", state)
	}
	if got := reqEp.caps.cache.GetData(uint64(addr)); string(got) != string(expectedData) {
		t.Fatalf("requester cache data = %q, want %q", got, expectedData)
	}

	if state := homeEp.caps.directory.GetState(uint64(addr)); state != directory.StateShared {
		t.Fatalf("home directory state = %s, want Shared", state)
	}
	sharers := homeEp.caps.directory.GetSharers(uint64(addr))
	if len(sharers) != 1 || sharers[0] != requesterID {
		t.Fatalf("home sharers = %v, want [%d]", sharers, requesterID)
	}
}

// --- Simulation helpers ---

type chiEndpoint struct {
	id       int
	txnMgr   *transaction.TxnManager
	hook     hook.IncentiveHook
	caps     *chiNodeCapabilities
	msgAlloc *messageIDAllocator
	inbox    []*message.Message
}

func newChiEndpoint(id int, withDirectory bool) *chiEndpoint {
	cacheStore := cache.NewFullyAssociativeCache(4)
	var dirStore directory.Directory
	if withDirectory {
		dirStore = directory.NewFullyAssociativeDirectory(4)
	}
	caps := &chiNodeCapabilities{
		cache:     cacheStore,
		directory: dirStore,
	}
	txnMgr := transaction.NewTxnManager(id, caps)
	return &chiEndpoint{
		id:       id,
		txnMgr:   txnMgr,
		caps:     caps,
		msgAlloc: &messageIDAllocator{nodeID: id},
		inbox:    make([]*message.Message, 0),
	}
}

func runChiSimulation(endpoints []*chiEndpoint, cycles int) error {
	ctx := context.Background()
	inboxes := make(map[int][]*message.Message)
	for _, ep := range endpoints {
		inboxes[ep.id] = nil
	}

	for cycle := 0; cycle < cycles; cycle++ {
		next := make(map[int][]*message.Message)
		for _, ep := range endpoints {
			if ep.hook != nil && ep.hook.ShouldCreateTransaction(ep.id, uint64(cycle)) {
				if err := ep.hook.CreateTransaction(ctx, ep.id, uint64(cycle)); err != nil {
					return err
				}
				runtime.Gosched()
				time.Sleep(time.Millisecond)
			}
			incoming := inboxes[ep.id]
			outgoing, _, err := ep.txnMgr.Tick(uint64(cycle), incoming)
			if err != nil {
				return err
			}
			fmt.Printf("cycle %d node %d inbox %d outgoing %d\n", cycle, ep.id, len(incoming), len(outgoing))
			for _, msg := range outgoing {
				fmt.Printf("cycle %d node %d sent type %d to %d\n", cycle, ep.id, msg.Type, msg.TargetNodeID)
				next[msg.TargetNodeID] = append(next[msg.TargetNodeID], msg)
			}
		}
		inboxes = next
	}
	return nil
}

// --- Transaction hooks ---

func configureRequesterHook(ep *chiEndpoint, homeID int, addr transaction.Addr, expectedData []byte, done *atomic.Bool) {
	h := hook.NewMockIncentiveHook(ep.txnMgr)
	h.SetCreateEveryNCycles(1)
	h.SetMaxTransactionsPerNode(1)
	h.SetTransactionFunc(requesterTxnFunc(ep, homeID, addr, expectedData, done))
	ep.hook = h
}

func configureHomeResponderHook(ep *chiEndpoint, requesterID, subordinateID int, addr transaction.Addr, done *atomic.Bool) {
	h := hook.NewMockIncentiveHook(ep.txnMgr)
	h.SetCreateEveryNCycles(1)
	h.SetMaxTransactionsPerNode(1)
	h.SetTransactionFunc(homeResponderTxnFunc(ep, requesterID, subordinateID, addr, done))
	ep.hook = h
}

func configureSubordinateHook(ep *chiEndpoint, homeID int, addr transaction.Addr, done *atomic.Bool) {
	h := hook.NewMockIncentiveHook(ep.txnMgr)
	h.SetCreateEveryNCycles(1)
	h.SetMaxTransactionsPerNode(1)
	h.SetTransactionFunc(subordinateTxnFunc(ep, homeID, addr, done))
	ep.hook = h
}

// --- Transaction logic ---

func requesterTxnFunc(ep *chiEndpoint, homeID int, addr transaction.Addr, expectedData []byte, done *atomic.Bool) func(*transaction.TxnContext) {
	return func(txCtx *transaction.TxnContext) {
		fmt.Println("Requester transaction started")
		payload := messagePayload(addr, chiOpcodeReadShared, txCtx.NodeID(), txCtx.TxnID().TxnID)
		msg := &message.Message{
			ID:            ep.msgAlloc.Next(),
			TransactionID: txCtx.TxnID(),
			Channel:       dataflow.ChannelREQ,
			Type:          chiOpcodeReadShared,
			SourceNodeID:  txCtx.NodeID(),
			TargetNodeID:  homeID,
			Payload:       payload,
		}
		if err := txCtx.Send(msg); err != nil {
			return
		}

		resp, err := txCtx.Yield(&transaction.YieldCommand{
			Type: transaction.YieldTypeWaitForMessage,
			WaitFor: &transaction.WaitForMessage{
				Type: chiOpcodeCompData,
				Addr: addr,
			},
			Timeout: 200 * time.Millisecond,
		})
		if err != nil {
			return
		}
		fmt.Println("Requester received CompData")
		dataMsg, ok := resp.(*message.Message)
		if !ok {
			return
		}
		dataPayload, ok := dataMsg.Payload.(*chiPayload)
		if !ok {
			return
		}

		txCtx.UpdateCache(addr, cache.StateShared, dataPayload.Data)
		txCtx.Complete(nil)
		done.Store(true)
	}
}

func homeResponderTxnFunc(ep *chiEndpoint, requesterID, subordinateID int, addr transaction.Addr, done *atomic.Bool) func(*transaction.TxnContext) {
	return func(txCtx *transaction.TxnContext) {
		fmt.Println("Home transaction started")
		incoming, err := txCtx.Yield(&transaction.YieldCommand{
			Type: transaction.YieldTypeWaitForMessage,
			WaitFor: &transaction.WaitForMessage{
				Type: chiOpcodeReadShared,
				Addr: addr,
			},
			Timeout: 200 * time.Millisecond,
		})
		if err != nil {
			return
		}
		fmt.Println("Subordinate received ReadNoSnp")
		fmt.Println("Home received ReadShared")
		reqMsg, ok := incoming.(*message.Message)
		if !ok {
			return
		}
		reqPayload, ok := reqMsg.Payload.(*chiPayload)
		if !ok {
			return
		}

		forwardPayload := messagePayload(transaction.Addr(reqPayload.Addr), chiOpcodeReadNoSnp, requesterID, reqMsg.TransactionID.TxnID)
		forwardMsg := &message.Message{
			ID:            ep.msgAlloc.Next(),
			TransactionID: txCtx.TxnID(),
			Channel:       dataflow.ChannelREQ,
			Type:          chiOpcodeReadNoSnp,
			SourceNodeID:  txCtx.NodeID(),
			TargetNodeID:  subordinateID,
			Payload:       forwardPayload,
		}
		if err := txCtx.Send(forwardMsg); err != nil {
			return
		}

		if _, err := txCtx.Yield(&transaction.YieldCommand{
			Type: transaction.YieldTypeWaitForMessage,
			WaitFor: &transaction.WaitForMessage{
				Type: chiOpcodeReadReceipt,
				Addr: addr,
			},
			Timeout: 200 * time.Millisecond,
		}); err != nil {
			return
		}

		txCtx.AddDirectorySharer(addr, requesterID)
		txCtx.SetDirectoryState(addr, directory.StateShared)
		txCtx.Complete(nil)
		done.Store(true)
	}
}

func subordinateTxnFunc(ep *chiEndpoint, homeID int, addr transaction.Addr, done *atomic.Bool) func(*transaction.TxnContext) {
	return func(txCtx *transaction.TxnContext) {
		fmt.Println("Subordinate transaction started")
		incoming, err := txCtx.Yield(&transaction.YieldCommand{
			Type: transaction.YieldTypeWaitForMessage,
			WaitFor: &transaction.WaitForMessage{
				Type: chiOpcodeReadNoSnp,
				Addr: addr,
			},
			Timeout: 200 * time.Millisecond,
		})
		if err != nil {
			return
		}
		reqMsg, ok := incoming.(*message.Message)
		if !ok {
			return
		}
		reqPayload, ok := reqMsg.Payload.(*chiPayload)
		if !ok {
			return
		}

		data := txCtx.ReadCache(addr)
		compPayload := &chiPayload{
			Addr:      reqPayload.Addr,
			Data:      append([]byte(nil), data...),
			ReturnNID: reqPayload.ReturnNID,
		}
		compMsg := &message.Message{
			ID:            ep.msgAlloc.Next(),
			TransactionID: txCtx.TxnID(),
			Channel:       dataflow.ChannelDAT,
			Type:          chiOpcodeCompData,
			SourceNodeID:  txCtx.NodeID(),
			TargetNodeID:  reqPayload.ReturnNID,
			Payload:       compPayload,
		}
		if err := txCtx.Send(compMsg); err != nil {
			return
		}

		receiptPayload := &chiPayload{
			Addr: reqPayload.Addr,
		}
		receiptMsg := &message.Message{
			ID:            ep.msgAlloc.Next(),
			TransactionID: txCtx.TxnID(),
			Channel:       dataflow.ChannelRSP,
			Type:          chiOpcodeReadReceipt,
			SourceNodeID:  txCtx.NodeID(),
			TargetNodeID:  homeID,
			Payload:       receiptPayload,
		}
		_ = txCtx.Send(receiptMsg)
		txCtx.Complete(nil)
		done.Store(true)
	}
}

// --- Payload helpers ---

const (
	chiOpcodeReadShared  = 0x00
	chiOpcodeReadNoSnp   = 0x10
	chiOpcodeCompData    = 0x30
	chiOpcodeReadReceipt = 0x22
)

type chiPayload struct {
	Addr      uint64
	ReturnNID int
	ReturnTxn int
	Data      []byte
}

func messagePayload(addr transaction.Addr, opcode int, returnNID int, returnTxn int) *chiPayload {
	return &chiPayload{
		Addr:      uint64(addr),
		ReturnNID: returnNID,
		ReturnTxn: returnTxn,
	}
}

// --- Capability helpers ---

type chiNodeCapabilities struct {
	cache     cache.Cache
	directory directory.Directory
}

func (c *chiNodeCapabilities) Cache() cache.Cache {
	return c.cache
}

func (c *chiNodeCapabilities) Directory() directory.Directory {
	return c.directory
}

type messageIDAllocator struct {
	nodeID int
	next   int
}

func (m *messageIDAllocator) Next() dataflow.MessageID {
	m.next++
	return dataflow.MessageID{
		NodeID:    m.nodeID,
		MessageID: m.next,
	}
}
