package chi

import (
	"context"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/capability/cache"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/dataflow"
	"github.com/Readm/flow_sim/internal/dataflow/message"
	"github.com/Readm/flow_sim/internal/dataflow/transaction"
)

// setupSnoopNode creates a node with cache for snoop testing
func setupSnoopNode(nodeID int) *node.Node {
	n := node.New(nodeID)
	c := cache.NewFullyAssociativeCache(16)
	n.AddCache(c)

	// Add decoder
	dec := NewTestDecoder()
	dec.AddMapping(0x1000, 0x1FFF, 2) // Address 0x1000 -> HN 2
	n.SetData("CHI_Decoder", dec)

	return n
}

// TestSnpSharedFwdContinuous_DMT tests DMT (Direct Memory Transfer) path
func TestSnpSharedFwdContinuous_DMT(t *testing.T) {
	// Create snooped node and requester node
	snpNode := setupSnoopNode(1)
	reqNode := setupSnoopNode(0) // Requester node for DMT
	snpMgr := transaction.NewTxnManager(snpNode)
	reqMgr := transaction.NewTxnManager(reqNode)

	addr := uint64(0x1000)
	data := []byte{0xAA, 0xBB, 0xCC, 0xDD}

	// Setup: Snooped node has data in Modified state
	snpCache := snpNode.Caches()[0]
	snpCache.SetState(addr, cache.StateModified)
	snpCache.SetData(addr, data)

	// Build SnpSharedFwd message from HN to snooped node
	snpPayload := NewCHIPayload(OpcodeSnpSharedFwd, addr)
	snpPayload.SetReturnInfo(0, 123) // Return to requester RN 0, txn 123

	snpMsg := &message.Message{
		TransactionID: dataflow.TransactionID{NodeID: 2, TxnID: 999},
		Channel:       CHIChannelSNP,
		Type:          OpcodeSnpSharedFwd,
		SourceNodeID:  2, // From HN
		TargetNodeID:  1, // To snooped node
		Payload:       snpPayload,
	}

	// Track handler completion
	done := make(chan error, 1)

	// Launch snoop handler on snooped node with DMT enabled
	snpMgr.Start(context.Background(), func(ctx *transaction.TxnContext) {
		err := SnpSharedFwdContinuous(ctx, snpMsg, true) // useDMT=true
		done <- err
	})

	time.Sleep(1 * time.Millisecond)

	// Tick snooped node to process handler
	var allSentMsgs []*message.Message
	for cycle := uint64(1); cycle <= 30; cycle++ {
		// Tick snooped node
		outSnp, _ := snpMgr.Tick(cycle, nil)
		allSentMsgs = append(allSentMsgs, outSnp...)

		// Route migration messages to requester node in next cycle
		for _, msg := range outSnp {
			if msg.Type == transaction.MsgTypeMigrationRequest && msg.TargetNodeID == 0 {
				// Process migration on requester node
				outReq, _ := reqMgr.Tick(cycle+1, []*message.Message{msg})
				allSentMsgs = append(allSentMsgs, outReq...)
			}
		}

		// Check if handler completed
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Handler error: %v", err)
			}
			goto VERIFY
		default:
			// Continue ticking
		}

		time.Sleep(1 * time.Millisecond)
	}

	// Timeout if handler didn't complete
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Handler error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Handler timed out")
	}

VERIFY:
	// Verify snooped cache downgraded to Shared
	snpState := snpCache.GetState(addr)
	if snpState != cache.StateShared {
		t.Errorf("Expected snooped cache state Shared, got %s", snpState)
	}

	// Verify DMT message sent directly to requester
	foundDMT := false
	for _, msg := range allSentMsgs {
		if msg.Type == OpcodeSnpRespDataFwded && msg.TargetNodeID == 0 {
			foundDMT = true
			payload := msg.Payload.(*CHIPayload)
			if string(payload.Data) != string(data) {
				t.Errorf("Data mismatch: expected %v, got %v", data, payload.Data)
			}
			break
		}
	}

	if !foundDMT {
		t.Error("DMT message (SnpRespDataFwded to RN 0) not found")
	}
}

// TestSnpSharedFwdContinuous_Default tests default path (through HN)
func TestSnpSharedFwdContinuous_Default(t *testing.T) {
	snpNode := setupSnoopNode(1)
	snpMgr := transaction.NewTxnManager(snpNode)

	addr := uint64(0x1000)
	data := []byte{0x11, 0x22, 0x33, 0x44}

	// Setup: Snooped node has data in Exclusive state
	snpCache := snpNode.Caches()[0]
	snpCache.SetState(addr, cache.StateExclusive)
	snpCache.SetData(addr, data)

	snpPayload := NewCHIPayload(OpcodeSnpSharedFwd, addr)
	snpPayload.SetReturnInfo(0, 456)

	snpMsg := &message.Message{
		TransactionID: dataflow.TransactionID{NodeID: 2, TxnID: 888},
		Channel:       CHIChannelSNP,
		Type:          OpcodeSnpSharedFwd,
		SourceNodeID:  2,
		TargetNodeID:  1,
		Payload:       snpPayload,
	}

	done := make(chan error, 1)

	// Launch handler with DMT disabled
	snpMgr.Start(context.Background(), func(ctx *transaction.TxnContext) {
		err := SnpSharedFwdContinuous(ctx, snpMsg, false) // useDMT=false
		done <- err
	})

	// Give goroutine a moment to start (but don't let it finish before ticking)
	time.Sleep(1 * time.Millisecond)

	// Tick simulation
	var allSentMsgs []*message.Message
	for cycle := uint64(1); cycle <= 15; cycle++ {
		outgoing, _ := snpMgr.Tick(cycle, nil)
		allSentMsgs = append(allSentMsgs, outgoing...)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Handler error: %v", err)
			}
			goto VERIFY_DEFAULT
		default:
		}
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Handler error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Handler timed out")
	}

VERIFY_DEFAULT:
	// Verify cache downgraded
	snpState := snpCache.GetState(addr)
	if snpState != cache.StateShared {
		t.Errorf("Expected Shared, got %s", snpState)
	}

	// Verify default message sent to HN (not forwarded)
	foundDefault := false
	for _, msg := range allSentMsgs {
		if msg.Type == OpcodeSnpRespData && msg.TargetNodeID == 2 {
			foundDefault = true
			payload := msg.Payload.(*CHIPayload)
			if string(payload.Data) != string(data) {
				t.Error("Data mismatch")
			}
			break
		}
	}

	if !foundDefault {
		t.Error("Default message (SnpRespData to HN) not found")
	}
}

// TestSnpUniqueFwdContinuous_DMT tests invalidation with DMT
func TestSnpUniqueFwdContinuous_DMT(t *testing.T) {
	snpNode := setupSnoopNode(1)
	reqNode := setupSnoopNode(0) // Requester node for DMT
	snpMgr := transaction.NewTxnManager(snpNode)
	reqMgr := transaction.NewTxnManager(reqNode)

	addr := uint64(0x1000)
	data := []byte{0xFF, 0xEE, 0xDD, 0xCC}

	// Setup: Snooped node has dirty data
	snpCache := snpNode.Caches()[0]
	snpCache.SetState(addr, cache.StateModified)
	snpCache.SetData(addr, data)

	snpPayload := NewCHIPayload(OpcodeSnpUniqueFwd, addr)
	snpPayload.SetReturnInfo(0, 789)

	snpMsg := &message.Message{
		TransactionID: dataflow.TransactionID{NodeID: 2, TxnID: 777},
		Channel:       CHIChannelSNP,
		Type:          OpcodeSnpUniqueFwd,
		SourceNodeID:  2,
		TargetNodeID:  1,
		Payload:       snpPayload,
	}

	done := make(chan error, 1)

	// Launch handler with DMT
	snpMgr.Start(context.Background(), func(ctx *transaction.TxnContext) {
		err := SnpUniqueFwdContinuous(ctx, snpMsg, true)
		done <- err
	})

	time.Sleep(1 * time.Millisecond)

	// Tick
	var allSentMsgs []*message.Message
	for cycle := uint64(1); cycle <= 30; cycle++ {
		// Tick snooped node
		outSnp, _ := snpMgr.Tick(cycle, nil)
		allSentMsgs = append(allSentMsgs, outSnp...)

		// Route migration messages to requester node in next cycle
		for _, msg := range outSnp {
			if msg.Type == transaction.MsgTypeMigrationRequest && msg.TargetNodeID == 0 {
				// Process migration on requester node
				outReq, _ := reqMgr.Tick(cycle+1, []*message.Message{msg})
				allSentMsgs = append(allSentMsgs, outReq...)
			}
		}

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Handler error: %v", err)
			}
			goto VERIFY_UNIQUE
		default:
		}

		time.Sleep(1 * time.Millisecond)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Handler error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Handler timed out")
	}

VERIFY_UNIQUE:
	// Verify cache invalidated
	snpState := snpCache.GetState(addr)
	if snpState != cache.StateInvalid {
		t.Errorf("Expected Invalid, got %s", snpState)
	}

	// Verify DMT message
	foundDMT := false
	for _, msg := range allSentMsgs {
		if msg.Type == OpcodeSnpRespDataFwded && msg.TargetNodeID == 0 {
			foundDMT = true
			break
		}
	}

	if !foundDMT {
		t.Error("DMT invalidation message not found")
	}
}

// TestSnpInvalidateContinuous tests simple invalidation
func TestSnpInvalidateContinuous(t *testing.T) {
	snpNode := setupSnoopNode(1)
	snpMgr := transaction.NewTxnManager(snpNode)

	addr := uint64(0x1000)

	// Setup: Snooped node has clean shared data
	snpCache := snpNode.Caches()[0]
	snpCache.SetState(addr, cache.StateShared)
	snpCache.SetData(addr, []byte{0x00, 0x00})

	snpPayload := NewCHIPayload(OpcodeSnpInvalidate, addr)

	snpMsg := &message.Message{
		TransactionID: dataflow.TransactionID{NodeID: 2, TxnID: 555},
		Channel:       CHIChannelSNP,
		Type:          OpcodeSnpInvalidate,
		SourceNodeID:  2,
		TargetNodeID:  1,
		Payload:       snpPayload,
	}

	done := make(chan error, 1)

	// Launch handler
	snpMgr.Start(context.Background(), func(ctx *transaction.TxnContext) {
		err := SnpInvalidateContinuous(ctx, snpMsg)
		done <- err
	})

	time.Sleep(10 * time.Millisecond)

	// Tick
	var allSentMsgs []*message.Message
	for cycle := uint64(1); cycle <= 10; cycle++ {
		outgoing, _ := snpMgr.Tick(cycle, nil)
		allSentMsgs = append(allSentMsgs, outgoing...)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Handler error: %v", err)
			}
			goto VERIFY_INVALIDATE
		default:
		}
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Handler error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Handler timed out")
	}

VERIFY_INVALIDATE:
	// Verify invalidation
	snpState := snpCache.GetState(addr)
	if snpState != cache.StateInvalid {
		t.Errorf("Expected Invalid, got %s", snpState)
	}

	// Verify SnpResp sent
	foundResp := false
	for _, msg := range allSentMsgs {
		if msg.Type == OpcodeSnpResp && msg.TargetNodeID == 2 {
			foundResp = true
			break
		}
	}

	if !foundResp {
		t.Error("SnpResp message not found")
	}
}

// TestContinuousSnoopDispatcher tests the dispatcher
func TestContinuousSnoopDispatcher(t *testing.T) {
	snpNode := setupSnoopNode(1)
	snpMgr := transaction.NewTxnManager(snpNode)

	dispatcher := NewContinuousSnoopDispatcher(true) // DMT enabled

	addr := uint64(0x1000)
	snpCache := snpNode.Caches()[0]
	snpCache.SetState(addr, cache.StateModified)
	snpCache.SetData(addr, []byte{0xAA})

	snpPayload := NewCHIPayload(OpcodeSnpSharedFwd, addr)
	snpPayload.SetReturnInfo(0, 100)

	snpMsg := &message.Message{
		TransactionID: dataflow.TransactionID{NodeID: 2, TxnID: 1},
		Channel:       CHIChannelSNP,
		Type:          OpcodeSnpSharedFwd,
		SourceNodeID:  2,
		TargetNodeID:  1,
		Payload:       snpPayload,
	}

	// Dispatch snoop
	err := dispatcher.Dispatch(snpMgr, snpMsg)
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Tick to process
	for cycle := uint64(1); cycle <= 15; cycle++ {
		snpMgr.Tick(cycle, nil)
	}

	// Verify cache changed
	snpState := snpCache.GetState(addr)
	if snpState != cache.StateShared {
		t.Errorf("Dispatcher test: expected Shared, got %s", snpState)
	}
}
